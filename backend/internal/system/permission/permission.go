// Package permission 定義所有權限點。權限點只存在程式碼中,
// 角色在資料庫只存代碼;新增模組時在此註冊。
// 代碼格式:模組.資源.動作
package permission

const (
	DepartmentRead  = "system.department.read"
	DepartmentWrite = "system.department.write"
	UserRead        = "system.user.read"
	UserWrite       = "system.user.write"
	RoleRead        = "system.role.read"
	RoleWrite       = "system.role.write"
	AuditRead       = "system.audit.read"
	DocNumberRead   = "system.docno.read"
	DocNumberWrite  = "system.docno.write"
)

type Permission struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type Group struct {
	Module      string       `json:"module"`
	Resource    string       `json:"resource"`
	Permissions []Permission `json:"permissions"`
}

// Groups 依模組 → 資源分組,供前端顯示權限勾選樹。
var Groups = []Group{
	{"系統管理", "部門", []Permission{{DepartmentRead, "檢視"}, {DepartmentWrite, "新增/修改"}}},
	{"系統管理", "使用者", []Permission{{UserRead, "檢視"}, {UserWrite, "新增/修改/重設密碼"}}},
	{"系統管理", "角色權限", []Permission{{RoleRead, "檢視"}, {RoleWrite, "新增/修改/刪除"}}},
	{"系統管理", "稽核日誌", []Permission{{AuditRead, "檢視"}}},
	{"系統管理", "單號規則", []Permission{{DocNumberRead, "檢視"}, {DocNumberWrite, "修改"}}},
}

var known = func() map[string]struct{} {
	m := map[string]struct{}{}
	for _, g := range Groups {
		for _, p := range g.Permissions {
			m[p.Code] = struct{}{}
		}
	}
	return m
}()

// Known 是否為已註冊的權限點。
func Known(code string) bool {
	_, ok := known[code]
	return ok
}
