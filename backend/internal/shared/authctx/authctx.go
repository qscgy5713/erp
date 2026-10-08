// Package authctx 在 context 中傳遞目前登入者與請求資訊,
// 供 service 層(權限判斷、資料範圍、稽核)使用,而不必依賴 gin。
package authctx

import "context"

// 資料範圍:角色可見的資料範圍,多角色取最大者。
const (
	ScopeSelf       = "self"
	ScopeDepartment = "department"
	ScopeAll        = "all"
)

var scopeRank = map[string]int{ScopeSelf: 1, ScopeDepartment: 2, ScopeAll: 3}

// WiderScope 回傳兩者中範圍較大的。
func WiderScope(a, b string) string {
	if scopeRank[b] > scopeRank[a] {
		return b
	}
	return a
}

type Actor struct {
	UserID       int64
	CompanyID    int64
	DepartmentID *int64
	Username     string
	Name         string
	IsSuperadmin bool
	DataScope    string
	Permissions  map[string]struct{}
	// MustChangePassword 為 true 時只能呼叫改密碼等少數 API
	MustChangePassword bool
	// MustSetup2FA 公司要求雙因素驗證、但此使用者尚未啟用:只能存取設定雙因素驗證所需的少數 API
	MustSetup2FA bool
}

// Can 判斷是否具備權限;超級管理員擁有全部權限。
func (a *Actor) Can(perm string) bool {
	if a == nil {
		return false
	}
	if a.IsSuperadmin {
		return true
	}
	_, ok := a.Permissions[perm]
	return ok
}

// Scope 回傳實際資料範圍;超級管理員一律為 all。
func (a *Actor) Scope() string {
	if a.IsSuperadmin {
		return ScopeAll
	}
	if a.DataScope == "" {
		return ScopeSelf
	}
	return a.DataScope
}

// ScopeFilter 回傳列表查詢要套用的條件:nil 表示不限制。
// 例:部門範圍 → deptID 有值;本人範圍 → userID 有值。
func (a *Actor) ScopeFilter() (deptID, userID *int64) {
	switch a.Scope() {
	case ScopeAll:
		return nil, nil
	case ScopeDepartment:
		if a.DepartmentID != nil {
			return a.DepartmentID, nil
		}
		// 沒有部門的人退回本人範圍,避免看到全部資料
		return nil, &a.UserID
	default:
		return nil, &a.UserID
	}
}

type RequestMeta struct {
	RequestID string
	IP        string
	UserAgent string
}

type actorKey struct{}
type metaKey struct{}

func WithActor(ctx context.Context, a *Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom 取出登入者;未登入時為 nil。
func ActorFrom(ctx context.Context) *Actor {
	a, _ := ctx.Value(actorKey{}).(*Actor)
	return a
}

func WithMeta(ctx context.Context, m RequestMeta) context.Context {
	return context.WithValue(ctx, metaKey{}, m)
}

func MetaFrom(ctx context.Context) RequestMeta {
	m, _ := ctx.Value(metaKey{}).(RequestMeta)
	return m
}
