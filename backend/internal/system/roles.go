package system

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/system/permission"
)

var (
	errRoleCodeTaken = apperr.Conflict("ROLE-001", "角色代碼已存在")
	errRoleInUse     = apperr.Conflict("ROLE-002", "此角色仍有使用者,請先移除指派或改為停用")
)

type roleDTO struct {
	ID          int64     `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DataScope   string    `json:"data_scope"`
	IsActive    bool      `json:"is_active"`
	UserCount   int64     `json:"user_count"`
	Permissions []string  `json:"permissions,omitempty"`
	Version     int32     `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toRoleDTO(r db.Role, perms []string) roleDTO {
	return roleDTO{
		ID: r.ID, Code: r.Code, Name: r.Name, Description: r.Description, DataScope: r.DataScope,
		IsActive: r.IsActive, Permissions: perms, Version: r.Version, UpdatedAt: r.UpdatedAt,
	}
}

func (m *Module) listRoles(c *gin.Context) {
	rows, err := m.store.ListRoles(c.Request.Context(), actor(c).CompanyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]roleDTO, len(rows))
	for i, r := range rows {
		out[i] = roleDTO{
			ID: r.ID, Code: r.Code, Name: r.Name, Description: r.Description, DataScope: r.DataScope,
			IsActive: r.IsActive, UserCount: r.UserCount, Version: r.Version, UpdatedAt: r.UpdatedAt,
		}
	}
	response.OK(c, out)
}

func (m *Module) getRole(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	r, err := m.store.GetRole(ctx, db.GetRoleParams{ID: id, CompanyID: actor(c).CompanyID})
	if database.IsNoRows(err) {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	if err != nil {
		response.Error(c, err)
		return
	}
	perms, err := m.store.ListRolePermissions(ctx, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toRoleDTO(r, perms))
}

type roleInput struct {
	Code        string   `json:"code" binding:"required,max=30"`
	Name        string   `json:"name" binding:"required,max=100"`
	Description string   `json:"description" binding:"max=500"`
	DataScope   string   `json:"data_scope" binding:"required,oneof=all department self"`
	Permissions []string `json:"permissions"`
}

// normalize 清理輸入並驗證權限代碼。
func (in *roleInput) normalize() error {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	seen := map[string]bool{}
	perms := make([]string, 0, len(in.Permissions))
	for _, p := range in.Permissions {
		if !permission.Known(p) {
			return apperr.Validation(map[string]string{"permissions": "未知的權限:" + p})
		}
		if !seen[p] {
			seen[p] = true
			perms = append(perms, p)
		}
	}
	sort.Strings(perms)
	in.Permissions = perms
	return nil
}

// checkScopeGrantable 非超級管理員不可設定比自己更大的資料範圍。
func checkScopeGrantable(a *authctx.Actor, scope string) error {
	if authctx.WiderScope(a.Scope(), scope) != a.Scope() {
		return apperr.Forbidden("SYS-403", "不可設定比自己更大的資料範圍")
	}
	return nil
}

func (m *Module) createRole(c *gin.Context) {
	var in roleInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	if err := in.normalize(); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	if err := ensureGrantable(a, in.Permissions); err != nil {
		response.Error(c, err)
		return
	}
	if err := checkScopeGrantable(a, in.DataScope); err != nil {
		response.Error(c, err)
		return
	}

	var role db.Role
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		var err error
		role, err = q.CreateRole(ctx, db.CreateRoleParams{
			CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, Description: in.Description,
			DataScope: in.DataScope, CreatedBy: &a.UserID,
		})
		if database.IsUniqueViolation(err, "roles_company_code_key") {
			return errRoleCodeTaken
		}
		if err != nil {
			return err
		}
		if err := setRolePermissions(ctx, q, role.ID, in.Permissions); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "role", EntityID: &role.ID,
			Summary: "新增角色 " + role.Code + " " + role.Name, After: toRoleDTO(role, in.Permissions),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, toRoleDTO(role, in.Permissions))
}

type updateRoleInput struct {
	roleInput
	IsActive bool  `json:"is_active"`
	Version  int32 `json:"version" binding:"required"`
}

func (m *Module) updateRole(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in updateRoleInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	if err := in.normalize(); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	if err := checkScopeGrantable(a, in.DataScope); err != nil {
		response.Error(c, err)
		return
	}

	var after db.Role
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetRole(ctx, db.GetRoleParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		beforePerms, err := q.ListRolePermissions(ctx, id)
		if err != nil {
			return err
		}
		// 新增與移除的權限都必須是自己擁有的,避免影響超出自己權限的角色
		if err := ensureGrantable(a, slices.Concat(beforePerms, in.Permissions)); err != nil {
			return err
		}
		after, err = q.UpdateRole(ctx, db.UpdateRoleParams{
			ID: id, CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, Description: in.Description,
			DataScope: in.DataScope, IsActive: in.IsActive, Version: in.Version, UpdatedBy: &a.UserID,
		})
		if database.IsNoRows(err) {
			return apperr.ErrVersionConflict
		}
		if database.IsUniqueViolation(err, "roles_company_code_key") {
			return errRoleCodeTaken
		}
		if err != nil {
			return err
		}
		if err := setRolePermissions(ctx, q, id, in.Permissions); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "role", EntityID: &id,
			Summary: "修改角色 " + after.Code + " " + after.Name,
			Before:  toRoleDTO(before, beforePerms), After: toRoleDTO(after, in.Permissions),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toRoleDTO(after, in.Permissions))
}

func (m *Module) deleteRole(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetRole(ctx, db.GetRoleParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		perms, err := q.ListRolePermissions(ctx, id)
		if err != nil {
			return err
		}
		if err := ensureGrantable(a, perms); err != nil {
			return err
		}
		n, err := q.CountRolesUsers(ctx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return errRoleInUse
		}
		if _, err := q.DeleteRole(ctx, db.DeleteRoleParams{ID: id, CompanyID: a.CompanyID}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Delete, EntityType: "role", EntityID: &id,
			Summary: "刪除角色 " + before.Code + " " + before.Name, Before: toRoleDTO(before, perms),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

func setRolePermissions(ctx context.Context, q *db.Queries, roleID int64, perms []string) error {
	if err := q.DeleteRolePermissions(ctx, roleID); err != nil {
		return err
	}
	if len(perms) == 0 {
		return nil
	}
	return q.AddRolePermissions(ctx, db.AddRolePermissionsParams{RoleID: roleID, Permissions: perms})
}
