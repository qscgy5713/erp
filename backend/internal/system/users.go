package system

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/auth"
	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
)

var (
	errUsernameTaken    = apperr.Conflict("USER-001", "帳號已存在")
	errBadDepartment    = apperr.Validation(map[string]string{"department_id": "部門不存在"})
	errBadRoles         = apperr.Validation(map[string]string{"role_ids": "包含不存在的角色"})
	errDeactivateSelf   = apperr.Validation(map[string]string{"is_active": "不可停用自己的帳號"})
	errSuperadminTarget = apperr.Forbidden("USER-002", "只有超級管理員可以修改超級管理員帳號")
)

type roleRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type userDTO struct {
	ID                 int64      `json:"id"`
	Username           string     `json:"username"`
	Name               string     `json:"name"`
	Email              *string    `json:"email"`
	DepartmentID       *int64     `json:"department_id"`
	DepartmentName     *string    `json:"department_name,omitempty"`
	IsSuperadmin       bool       `json:"is_superadmin"`
	IsActive           bool       `json:"is_active"`
	MustChangePassword bool       `json:"must_change_password"`
	TwoFactorEnabled   bool       `json:"two_factor_enabled"`
	LockedUntil        *time.Time `json:"locked_until"`
	LastLoginAt        *time.Time `json:"last_login_at"`
	Roles              []roleRef  `json:"roles"`
	RoleIDs            []int64    `json:"role_ids"`
	Version            int32      `json:"version"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// toUserDTO 不含密碼雜湊等機敏欄位,稽核日誌也使用此結構。
func toUserDTO(u db.User, roleIDs []int64) userDTO {
	locked := u.LockedUntil
	if locked != nil && !locked.After(time.Now()) {
		locked = nil
	}
	if roleIDs == nil {
		roleIDs = []int64{}
	}
	return userDTO{
		ID: u.ID, Username: u.Username, Name: u.Name, Email: u.Email, DepartmentID: u.DepartmentID,
		IsSuperadmin: u.IsSuperadmin, IsActive: u.IsActive, MustChangePassword: u.MustChangePassword, TwoFactorEnabled: u.TotpEnabled,
		LockedUntil: locked, LastLoginAt: u.LastLoginAt, Roles: []roleRef{}, RoleIDs: roleIDs,
		Version: u.Version, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

func (m *Module) listUsers(c *gin.Context) {
	ctx := c.Request.Context()
	deptID, err := httpx.QueryInt64(c, "department_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	isActive, err := httpx.QueryBool(c, "is_active")
	if err != nil {
		response.Error(c, err)
		return
	}
	p := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	keyword := httpx.QueryString(c, "keyword")

	rows, err := m.store.ListUsers(ctx, db.ListUsersParams{
		CompanyID: companyID, Keyword: keyword, DepartmentID: deptID, IsActive: isActive,
		Lim: p.Limit(), Off: p.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountUsers(ctx, db.CountUsersParams{
		CompanyID: companyID, Keyword: keyword, DepartmentID: deptID, IsActive: isActive,
	})
	if err != nil {
		response.Error(c, err)
		return
	}

	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	roleRows, err := m.store.ListRolesForUsers(ctx, ids)
	if err != nil {
		response.Error(c, err)
		return
	}
	byUser := map[int64][]roleRef{}
	for _, r := range roleRows {
		byUser[r.UserID] = append(byUser[r.UserID], roleRef{ID: r.ID, Name: r.Name})
	}

	out := make([]userDTO, len(rows))
	for i, r := range rows {
		dto := toUserDTO(db.User{
			ID: r.ID, Username: r.Username, Name: r.Name, Email: r.Email, DepartmentID: r.DepartmentID,
			IsSuperadmin: r.IsSuperadmin, IsActive: r.IsActive, MustChangePassword: r.MustChangePassword,
			LockedUntil: r.LockedUntil, LastLoginAt: r.LastLoginAt, Version: r.Version,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		}, nil)
		dto.DepartmentName = r.DepartmentName
		if roles := byUser[r.ID]; roles != nil {
			dto.Roles = roles
			for _, role := range roles {
				dto.RoleIDs = append(dto.RoleIDs, role.ID)
			}
		}
		out[i] = dto
	}
	response.List(c, out, p.Meta(total))
}

func (m *Module) getUser(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	u, err := m.store.GetUser(ctx, db.GetUserParams{ID: id, CompanyID: actor(c).CompanyID})
	if database.IsNoRows(err) {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	if err != nil {
		response.Error(c, err)
		return
	}
	roleIDs, err := m.store.ListUserRoleIDs(ctx, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toUserDTO(u, roleIDs))
}

type createUserInput struct {
	Username     string  `json:"username" binding:"required,min=3,max=50"`
	Name         string  `json:"name" binding:"required,max=100"`
	Email        *string `json:"email" binding:"omitempty,email,max=255"`
	DepartmentID *int64  `json:"department_id"`
	Password     string  `json:"password" binding:"required,max=200"`
	RoleIDs      []int64 `json:"role_ids"`
}

func (m *Module) createUser(c *gin.Context) {
	var in createUserInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	in.Name = strings.TrimSpace(in.Name)
	in.Email = trimOptional(in.Email)
	if !validUsername(in.Username) {
		response.Error(c, apperr.Validation(map[string]string{"username": "只能使用英數字與 . _ -,長度 3–50"}))
		return
	}
	if err := auth.ValidatePassword("password", in.Password); err != nil {
		response.Error(c, err)
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	roleIDs := uniqueIDs(in.RoleIDs)

	var u db.User
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		if err := m.checkUserRefs(ctx, q, a, in.DepartmentID, roleIDs); err != nil {
			return err
		}
		var err error
		u, err = q.CreateUser(ctx, db.CreateUserParams{
			CompanyID: a.CompanyID, DepartmentID: in.DepartmentID, Username: in.Username, Name: in.Name,
			Email: in.Email, PasswordHash: hash, MustChangePassword: true, CreatedBy: &a.UserID,
		})
		if database.IsUniqueViolation(err, "users_username_key") {
			return errUsernameTaken
		}
		if err != nil {
			return err
		}
		if len(roleIDs) > 0 {
			if err := q.AddUserRoles(ctx, db.AddUserRolesParams{UserID: u.ID, RoleIds: roleIDs}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "user", EntityID: &u.ID,
			Summary: "新增使用者 " + u.Username, After: toUserDTO(u, roleIDs),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, toUserDTO(u, roleIDs))
}

type updateUserInput struct {
	Name         string  `json:"name" binding:"required,max=100"`
	Email        *string `json:"email" binding:"omitempty,email,max=255"`
	DepartmentID *int64  `json:"department_id"`
	IsActive     bool    `json:"is_active"`
	RoleIDs      []int64 `json:"role_ids"`
	Version      int32   `json:"version" binding:"required"`
}

func (m *Module) updateUser(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in updateUserInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Email = trimOptional(in.Email)
	ctx := c.Request.Context()
	a := actor(c)
	if id == a.UserID && !in.IsActive {
		response.Error(c, errDeactivateSelf)
		return
	}
	roleIDs := uniqueIDs(in.RoleIDs)

	var after db.User
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := m.loadTargetUser(ctx, q, a, id)
		if err != nil {
			return err
		}
		beforeRoles, err := q.ListUserRoleIDs(ctx, id)
		if err != nil {
			return err
		}
		if err := m.checkUserRefs(ctx, q, a, in.DepartmentID, roleIDs); err != nil {
			return err
		}
		// 拿掉角色也要檢查:不能移除自己無權授予的角色(否則可藉此影響他人權限)
		if err := m.checkRolesGrantable(ctx, q, a, beforeRoles); err != nil {
			return err
		}
		after, err = q.UpdateUser(ctx, db.UpdateUserParams{
			ID: id, CompanyID: a.CompanyID, DepartmentID: in.DepartmentID, Name: in.Name, Email: in.Email,
			IsActive: in.IsActive, Version: in.Version, UpdatedBy: &a.UserID,
		})
		if database.IsNoRows(err) {
			return apperr.ErrVersionConflict
		}
		if err != nil {
			return err
		}
		if err := q.DeleteUserRoles(ctx, id); err != nil {
			return err
		}
		if len(roleIDs) > 0 {
			if err := q.AddUserRoles(ctx, db.AddUserRolesParams{UserID: id, RoleIds: roleIDs}); err != nil {
				return err
			}
		}
		if before.IsActive && !after.IsActive {
			if err := q.RevokeUserRefreshTokens(ctx, id); err != nil {
				return err
			}
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "user", EntityID: &id,
			Summary: "修改使用者 " + after.Username,
			Before:  toUserDTO(before, beforeRoles), After: toUserDTO(after, roleIDs),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toUserDTO(after, roleIDs))
}

type resetPasswordInput struct {
	Password string `json:"password" binding:"required,max=200"`
}

// resetPassword 管理員重設密碼:使用者下次登入須變更密碼,既有登入全部失效。
func (m *Module) resetPassword(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in resetPasswordInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	if err := auth.ValidatePassword("password", in.Password); err != nil {
		response.Error(c, err)
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		u, err := m.loadTargetUser(ctx, q, a, id)
		if err != nil {
			return err
		}
		if _, err := q.SetPassword(ctx, db.SetPasswordParams{
			ID: id, PasswordHash: hash, MustChangePassword: true, UpdatedBy: &a.UserID,
		}); err != nil {
			return err
		}
		if err := q.RevokeUserRefreshTokens(ctx, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.PasswordReset, EntityType: "user", EntityID: &id, Summary: "重設密碼 " + u.Username,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

func (m *Module) unlockUser(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		u, err := m.loadTargetUser(ctx, q, a, id)
		if err != nil {
			return err
		}
		if err := q.UnlockUser(ctx, db.UnlockUserParams{ID: id, CompanyID: a.CompanyID, UpdatedBy: &a.UserID}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Unlock, EntityType: "user", EntityID: &id, Summary: "解除鎖定 " + u.Username,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

// loadTargetUser 讀取要被修改的使用者;非超級管理員不可修改超級管理員。
func (m *Module) loadTargetUser(ctx context.Context, q *db.Queries, a *authctx.Actor, id int64) (db.User, error) {
	u, err := q.GetUser(ctx, db.GetUserParams{ID: id, CompanyID: a.CompanyID})
	if database.IsNoRows(err) {
		return u, apperr.ErrNotFound
	}
	if err != nil {
		return u, err
	}
	if u.IsSuperadmin && !a.IsSuperadmin {
		return u, errSuperadminTarget
	}
	return u, nil
}

func (m *Module) checkUserRefs(ctx context.Context, q *db.Queries, a *authctx.Actor, deptID *int64, roleIDs []int64) error {
	if deptID != nil {
		if _, err := q.GetDepartment(ctx, db.GetDepartmentParams{ID: *deptID, CompanyID: a.CompanyID}); err != nil {
			if database.IsNoRows(err) {
				return errBadDepartment
			}
			return err
		}
	}
	if len(roleIDs) == 0 {
		return nil
	}
	n, err := q.CountRolesByIDs(ctx, db.CountRolesByIDsParams{CompanyID: a.CompanyID, Ids: roleIDs})
	if err != nil {
		return err
	}
	if n != int64(len(roleIDs)) {
		return errBadRoles
	}
	return m.checkRolesGrantable(ctx, q, a, roleIDs)
}

func (m *Module) checkRolesGrantable(ctx context.Context, q *db.Queries, a *authctx.Actor, roleIDs []int64) error {
	if a.IsSuperadmin || len(roleIDs) == 0 {
		return nil
	}
	perms, err := rolesPermissions(ctx, q, roleIDs)
	if err != nil {
		return err
	}
	return ensureGrantable(a, perms)
}

func validUsername(s string) bool {
	if len(s) < 3 || len(s) > 50 {
		return false
	}
	for _, r := range s {
		alnum := r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if !alnum && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func trimOptional(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

type userOptionDTO struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Name         string `json:"name"`
	DepartmentID *int64 `json:"department_id"`
}

func (m *Module) listUserOptions(c *gin.Context) {
	rows, err := m.store.ListUserOptions(c.Request.Context(), actor(c).CompanyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]userOptionDTO, len(rows))
	for i, r := range rows {
		out[i] = userOptionDTO{ID: r.ID, Username: r.Username, Name: r.Name, DepartmentID: r.DepartmentID}
	}
	response.OK(c, out)
}

// resetTwoFactor 管理員重設某位使用者的雙因素驗證(手機遺失又沒有備援碼時):停用並刪除備援碼,既有登入全部失效。
// 公司要求雙因素驗證時,該使用者下次登入須重新設定。
func (m *Module) resetTwoFactor(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		u, err := m.loadTargetUser(ctx, q, a, id)
		if err != nil {
			return err
		}
		if !u.TotpEnabled {
			return apperr.Conflict("AUTH-011", "這位使用者沒有啟用雙因素驗證")
		}
		if err := q.DisableTOTP(ctx, id); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, id); err != nil {
			return err
		}
		if err := q.BumpTokenVersion(ctx, id); err != nil {
			return err
		}
		if err := q.RevokeUserRefreshTokens(ctx, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: "reset_2fa", EntityType: "user", EntityID: &id, Summary: "重設使用者 " + u.Username + " 的雙因素驗證",
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}
