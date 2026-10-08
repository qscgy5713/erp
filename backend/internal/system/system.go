// Package system 提供系統管理 API:部門、使用者、角色權限、稽核日誌、單號規則。
package system

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/auth"
	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/response"
	"erp/internal/system/permission"
)

type Module struct {
	store *database.Store
}

func New(store *database.Store) *Module {
	return &Module{store: store}
}

// Register 掛上 /system 路由;r 須已套用 auth.Authenticate。
func (m *Module) Register(r *gin.RouterGroup) {
	g := r.Group("/system")

	g.GET("/permissions", auth.Require(permission.RoleRead, permission.RoleWrite), m.listPermissions)

	g.GET("/departments", auth.Require(permission.DepartmentRead, permission.UserRead, permission.UserWrite), m.listDepartments)
	g.POST("/departments", auth.Require(permission.DepartmentWrite), m.createDepartment)
	g.PUT("/departments/:id", auth.Require(permission.DepartmentWrite), m.updateDepartment)

	// 下拉選單(例如客戶的負責業務)只需登入,不需使用者管理權限
	g.GET("/user-options", m.listUserOptions)
	g.GET("/users", auth.Require(permission.UserRead), m.listUsers)
	g.GET("/users/:id", auth.Require(permission.UserRead), m.getUser)
	g.POST("/users", auth.Require(permission.UserWrite), m.createUser)
	g.PUT("/users/:id", auth.Require(permission.UserWrite), m.updateUser)
	g.POST("/users/:id/reset-password", auth.Require(permission.UserWrite), m.resetPassword)
	g.POST("/users/:id/unlock", auth.Require(permission.UserWrite), m.unlockUser)

	g.GET("/roles", auth.Require(permission.RoleRead, permission.UserRead, permission.UserWrite), m.listRoles)
	g.GET("/roles/:id", auth.Require(permission.RoleRead), m.getRole)
	g.POST("/roles", auth.Require(permission.RoleWrite), m.createRole)
	g.PUT("/roles/:id", auth.Require(permission.RoleWrite), m.updateRole)
	g.DELETE("/roles/:id", auth.Require(permission.RoleWrite), m.deleteRole)

	g.GET("/company", auth.Require(permission.CompanyRead, permission.CompanyWrite), m.getCompany)
	g.PUT("/company", auth.Require(permission.CompanyWrite), m.updateCompany)

	g.GET("/audit-logs", auth.Require(permission.AuditRead), m.listAuditLogs)

	g.GET("/doc-number-rules", auth.Require(permission.DocNumberRead, permission.DocNumberWrite), m.listDocNumberRules)
	g.PUT("/doc-number-rules/:docType", auth.Require(permission.DocNumberWrite), m.updateDocNumberRule)
}

func actor(c *gin.Context) *authctx.Actor { return authctx.ActorFrom(c.Request.Context()) }

// ensureGrantable 非超級管理員只能授予自己擁有的權限,避免藉由指派角色或編輯角色提升權限。
func ensureGrantable(a *authctx.Actor, perms []string) error {
	if a.IsSuperadmin {
		return nil
	}
	for _, p := range perms {
		if !a.Can(p) {
			return apperr.Forbidden("SYS-403", "不可授予自己沒有的權限:"+p)
		}
	}
	return nil
}

// rolesPermissions 取出多個角色的權限聯集。
func rolesPermissions(ctx context.Context, q *db.Queries, roleIDs []int64) ([]string, error) {
	var all []string
	for _, id := range roleIDs {
		perms, err := q.ListRolePermissions(ctx, id)
		if err != nil {
			return nil, err
		}
		all = append(all, perms...)
	}
	return all, nil
}

func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// taipei 台灣無日光節約時間,用固定時區即可,不依賴容器內的 tzdata。
var taipei = time.FixedZone("Asia/Taipei", 8*3600)

func (m *Module) listPermissions(c *gin.Context) {
	response.OK(c, permission.Groups)
}
