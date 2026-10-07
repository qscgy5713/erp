package authctx

import "testing"

func ptr(v int64) *int64 { return &v }

func TestCan(t *testing.T) {
	a := &Actor{Permissions: map[string]struct{}{"system.user.read": {}}}
	if !a.Can("system.user.read") || a.Can("system.user.write") {
		t.Fatal("一般使用者權限判斷錯誤")
	}
	if !(&Actor{IsSuperadmin: true}).Can("anything") {
		t.Fatal("超級管理員應有全部權限")
	}
	var nilActor *Actor
	if nilActor.Can("x") {
		t.Fatal("未登入不應有權限")
	}
}

func TestScopeFilter(t *testing.T) {
	cases := []struct {
		name               string
		actor              Actor
		wantDept, wantUser *int64
	}{
		{"全部", Actor{UserID: 1, DataScope: ScopeAll}, nil, nil},
		{"部門", Actor{UserID: 1, DepartmentID: ptr(7), DataScope: ScopeDepartment}, ptr(7), nil},
		{"部門但沒有部門→本人", Actor{UserID: 1, DataScope: ScopeDepartment}, nil, ptr(1)},
		{"本人", Actor{UserID: 1, DataScope: ScopeSelf}, nil, ptr(1)},
		{"未設定→本人", Actor{UserID: 1}, nil, ptr(1)},
		{"超級管理員→全部", Actor{UserID: 1, IsSuperadmin: true, DataScope: ScopeSelf}, nil, nil},
	}
	eq := func(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
	for _, tc := range cases {
		d, u := tc.actor.ScopeFilter()
		if !eq(d, tc.wantDept) || !eq(u, tc.wantUser) {
			t.Errorf("%s: got dept=%v user=%v", tc.name, d, u)
		}
	}
}

func TestWiderScope(t *testing.T) {
	if WiderScope(ScopeSelf, ScopeAll) != ScopeAll || WiderScope(ScopeDepartment, ScopeSelf) != ScopeDepartment {
		t.Fatal("WiderScope 錯誤")
	}
}
