package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"erp/internal/auth"
	"erp/internal/db"
	"erp/internal/platform/config"
	"erp/internal/testutil/dbtest"
)

// ---- 測試工具 ----

type env struct {
	t    *testing.T
	pool *pgxpool.Pool
	q    *db.Queries
	r    *gin.Engine
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := dbtest.New(t)
	cfg := config.Config{
		AppEnv:          "production", // 關閉 gin debug 輸出
		JWTSecret:       []byte("test-secret-test-secret-test-secret!!"),
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: time.Hour,
		// 測試會大量登入,放寬限流;限流本身另有測試
		RateLimitPerMinute:        100000,
		LoginRateLimitPerMinute:   100000,
		RefreshRateLimitPerMinute: 100000,
	}
	r, err := NewRouter(cfg, pool)
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, pool: pool, q: db.New(pool), r: r}
}

// seedUser 直接寫入資料庫建立使用者(繞過 API,用於準備資料)。
func (e *env) seedUser(username, password string, superadmin, mustChange bool, roleIDs ...int64) db.User {
	e.t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		e.t.Fatal(err)
	}
	u, err := e.q.CreateUser(context.Background(), db.CreateUserParams{
		CompanyID: 1, Username: username, Name: username, PasswordHash: hash,
		IsSuperadmin: superadmin, MustChangePassword: mustChange,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	if len(roleIDs) > 0 {
		if err := e.q.AddUserRoles(context.Background(), db.AddUserRolesParams{UserID: u.ID, RoleIds: roleIDs}); err != nil {
			e.t.Fatal(err)
		}
	}
	return u
}

func (e *env) seedRole(code, scope string, perms ...string) db.Role {
	e.t.Helper()
	ctx := context.Background()
	r, err := e.q.CreateRole(ctx, db.CreateRoleParams{CompanyID: 1, Code: code, Name: code, DataScope: scope})
	if err != nil {
		e.t.Fatal(err)
	}
	if len(perms) > 0 {
		if err := e.q.AddRolePermissions(ctx, db.AddRolePermissionsParams{RoleID: r.ID, Permissions: perms}); err != nil {
			e.t.Fatal(err)
		}
	}
	return r
}

type client struct {
	e      *env
	token  string
	cookie *http.Cookie
}

type apiResp struct {
	status int
	Data   json.RawMessage `json:"data"`
	Meta   json.RawMessage `json:"meta"`
	Error  *struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Details map[string]string `json:"details"`
	} `json:"error"`
	cookies []*http.Cookie
}

func (r apiResp) code() string {
	if r.Error == nil {
		return ""
	}
	return r.Error.Code
}

func (c *client) do(method, path string, body any) apiResp {
	c.e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "/api/v1"+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.e.r.ServeHTTP(w, req)

	res := apiResp{status: w.Code, cookies: w.Result().Cookies()}
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			c.e.t.Fatalf("%s %s: 回應不是 JSON: %s", method, path, w.Body.String())
		}
	}
	for _, ck := range res.cookies {
		if ck.Name == "erp_refresh" {
			if ck.MaxAge < 0 {
				c.cookie = nil
			} else {
				c.cookie = ck
			}
		}
	}
	return res
}

func (c *client) login(username, password string) apiResp {
	c.e.t.Helper()
	res := c.do(http.MethodPost, "/auth/login", map[string]string{"username": username, "password": password})
	if res.status == http.StatusOK {
		c.token = decode[struct {
			AccessToken string `json:"access_token"`
		}](c.e.t, res.Data).AccessToken
	}
	return res
}

func (e *env) loggedIn(username, password string) *client {
	e.t.Helper()
	c := &client{e: e}
	if res := c.login(username, password); res.status != http.StatusOK {
		e.t.Fatalf("登入 %s 失敗: %d %+v", username, res.status, res.Error)
	}
	return c
}

func decode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	return v
}

func expect(t *testing.T, res apiResp, status int, code string) {
	t.Helper()
	if res.status != status || res.code() != code {
		t.Fatalf("got %d %q %+v, want %d %q", res.status, res.code(), res.Error, status, code)
	}
}

const pw = "Passw0rd!"

// ---- 登入 ----

func TestLoginLockoutAndUnlock(t *testing.T) {
	e := newEnv(t)
	e.seedUser("alice", pw, false, false)
	e.seedUser("root", pw, true, false)
	c := &client{e: e}

	for i := 1; i < auth.MaxLoginAttempts; i++ {
		expect(t, c.login("alice", "wrong-1"), http.StatusUnauthorized, "AUTH-001")
	}
	// 第 5 次失敗觸發鎖定
	expect(t, c.login("alice", "wrong-1"), http.StatusLocked, "AUTH-002")
	// 鎖定期間即使密碼正確也不能登入
	expect(t, c.login("alice", pw), http.StatusLocked, "AUTH-002")

	root := e.loggedIn("root", pw)
	u, _ := e.q.GetUserByUsername(context.Background(), "alice")
	expect(t, root.do(http.MethodPost, "/system/users/"+itoa(u.ID)+"/unlock", nil), http.StatusNoContent, "")
	expect(t, c.login("alice", pw), http.StatusOK, "")
}

func TestLoginUnknownUserAndDisabled(t *testing.T) {
	e := newEnv(t)
	c := &client{e: e}
	expect(t, c.login("nobody", pw), http.StatusUnauthorized, "AUTH-001")

	u := e.seedUser("bob", pw, false, false)
	_, err := e.pool.Exec(context.Background(), "UPDATE users SET is_active = false WHERE id = $1", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 密碼錯誤時不透露帳號已停用
	expect(t, c.login("bob", "wrong-1"), http.StatusUnauthorized, "AUTH-001")
	expect(t, c.login("bob", pw), http.StatusUnauthorized, "AUTH-003")
}

func TestMustChangePasswordFlow(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, true)
	c := e.loggedIn("root", pw)

	expect(t, c.do(http.MethodGet, "/system/users", nil), http.StatusForbidden, "AUTH-005")
	expect(t, c.do(http.MethodGet, "/auth/me", nil), http.StatusOK, "")

	oldToken := c.token
	res := c.do(http.MethodPost, "/auth/change-password", map[string]string{"old_password": "bad", "new_password": "NewPassw0rd"})
	expect(t, res, http.StatusUnprocessableEntity, "AUTH-006")
	res = c.do(http.MethodPost, "/auth/change-password", map[string]string{"old_password": pw, "new_password": "short"})
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	res = c.do(http.MethodPost, "/auth/change-password", map[string]string{"old_password": pw, "new_password": "NewPassw0rd"})
	expect(t, res, http.StatusOK, "")
	c.token = decode[struct {
		AccessToken string `json:"access_token"`
	}](t, res.Data).AccessToken

	expect(t, c.do(http.MethodGet, "/system/users", nil), http.StatusOK, "")
	// 改密碼後舊 token 立即失效
	old := &client{e: e, token: oldToken}
	expect(t, old.do(http.MethodGet, "/auth/me", nil), http.StatusUnauthorized, "SYS-401")
}

func TestRefreshRotationAndReuseDetection(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	first := c.cookie

	expect(t, c.do(http.MethodPost, "/auth/refresh", nil), http.StatusOK, "")
	second := c.cookie
	if second == nil || second.Value == first.Value {
		t.Fatal("refresh 應輪替出新的 token")
	}

	// 寬限期內重用舊 token:拒絕,但不影響新 token(兩個分頁同時刷新的情境)
	stale := &client{e: e, cookie: first}
	expect(t, stale.do(http.MethodPost, "/auth/refresh", nil), http.StatusUnauthorized, "AUTH-004")
	expect(t, c.do(http.MethodPost, "/auth/refresh", nil), http.StatusOK, "")

	// 超過寬限期重用:視為被竊用,整串撤銷
	if _, err := e.pool.Exec(context.Background(),
		"UPDATE refresh_tokens SET revoked_at = now() - interval '1 hour' WHERE revoked_at IS NOT NULL"); err != nil {
		t.Fatal(err)
	}
	stale = &client{e: e, cookie: first}
	expect(t, stale.do(http.MethodPost, "/auth/refresh", nil), http.StatusUnauthorized, "AUTH-004")
	expect(t, c.do(http.MethodPost, "/auth/refresh", nil), http.StatusUnauthorized, "AUTH-004")

	var n int
	if err := e.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM audit_logs WHERE action = 'token_reuse'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("應記錄一筆 token_reuse 稽核,got %d err=%v", n, err)
	}
}

func TestDeactivatedUserLosesAccessImmediately(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	bob := e.seedUser("bob", pw, false, false)
	root := e.loggedIn("root", pw)
	b := e.loggedIn("bob", pw)
	expect(t, b.do(http.MethodGet, "/auth/me", nil), http.StatusOK, "")

	res := root.do(http.MethodPut, "/system/users/"+itoa(bob.ID), map[string]any{
		"name": "bob", "is_active": false, "role_ids": []int64{}, "version": bob.Version,
	})
	expect(t, res, http.StatusOK, "")
	expect(t, b.do(http.MethodGet, "/auth/me", nil), http.StatusUnauthorized, "SYS-401")
	expect(t, b.do(http.MethodPost, "/auth/refresh", nil), http.StatusUnauthorized, "AUTH-004")
}

func TestRateLimitPerUserAndPerIP(t *testing.T) {
	pool := dbtest.New(t)
	cfg := config.Config{
		AppEnv:                    "production",
		JWTSecret:                 []byte("test-secret-test-secret-test-secret!!"),
		AccessTokenTTL:            15 * time.Minute,
		RefreshTokenTTL:           time.Hour,
		RateLimitPerMinute:        2, // burst 1
		LoginRateLimitPerMinute:   4, // burst 2
		RefreshRateLimitPerMinute: 100000,
	}
	r, err := NewRouter(cfg, pool)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, pool: pool, q: db.New(pool), r: r}
	e.seedUser("alice", pw, true, false)
	e.seedUser("bob", pw, true, false)

	// 登入以 IP 計算:同一 IP(httptest 預設 192.0.2.1)第 3 次被擋
	a := e.loggedIn("alice", pw)
	b := e.loggedIn("bob", pw)
	res := (&client{e: e}).login("alice", pw)
	expect(t, res, http.StatusTooManyRequests, "SYS-429")
	// 刷新有獨立額度,不受登入額度用完影響
	expect(t, a.do(http.MethodPost, "/auth/refresh", nil), http.StatusOK, "")

	// 已登入 API 以使用者計算:alice 用完額度不影響同 IP 的 bob
	expect(t, a.do(http.MethodGet, "/auth/me", nil), http.StatusOK, "")
	expect(t, a.do(http.MethodGet, "/auth/me", nil), http.StatusTooManyRequests, "SYS-429")
	expect(t, b.do(http.MethodGet, "/auth/me", nil), http.StatusOK, "")

	// 健康檢查不限流
	expect(t, a.do(http.MethodGet, "/health", nil), http.StatusOK, "")
}

// ---- 權限 ----

func TestPermissionsAndPrivilegeEscalation(t *testing.T) {
	e := newEnv(t)
	root := e.seedUser("root", pw, true, false)
	viewer := e.seedRole("VIEWER", "self", "system.user.read")
	manager := e.seedRole("MANAGER", "department", "system.user.read", "system.user.write", "system.role.read")
	powerful := e.seedRole("POWER", "all", "system.role.write")
	e.seedUser("viewer", pw, false, false, viewer.ID)
	mgr := e.seedUser("mgr", pw, false, false, manager.ID)

	v := e.loggedIn("viewer", pw)
	expect(t, v.do(http.MethodGet, "/system/users", nil), http.StatusOK, "")
	expect(t, v.do(http.MethodPost, "/system/users", map[string]any{
		"username": "x1", "name": "x", "password": "Passw0rd1",
	}), http.StatusForbidden, "SYS-403")
	expect(t, v.do(http.MethodGet, "/system/audit-logs", nil), http.StatusForbidden, "SYS-403")

	m := e.loggedIn("mgr", pw)
	// 可指派自己權限範圍內的角色
	expect(t, m.do(http.MethodPost, "/system/users", map[string]any{
		"username": "newbie", "name": "新人", "password": "Passw0rd1", "role_ids": []int64{viewer.ID},
	}), http.StatusCreated, "")
	// 不可指派含自己沒有的權限的角色
	expect(t, m.do(http.MethodPost, "/system/users", map[string]any{
		"username": "evil", "name": "x", "password": "Passw0rd1", "role_ids": []int64{powerful.ID},
	}), http.StatusForbidden, "SYS-403")
	// 不可替自己加上更大的角色
	expect(t, m.do(http.MethodPut, "/system/users/"+itoa(mgr.ID), map[string]any{
		"name": "mgr", "is_active": true, "role_ids": []int64{manager.ID, powerful.ID}, "version": mgr.Version,
	}), http.StatusForbidden, "SYS-403")
	// 不可修改超級管理員
	expect(t, m.do(http.MethodPost, "/system/users/"+itoa(root.ID)+"/reset-password", map[string]any{
		"password": "Passw0rd1",
	}), http.StatusForbidden, "USER-002")

	me := decode[struct {
		DataScope   string   `json:"data_scope"`
		Permissions []string `json:"permissions"`
	}](t, m.do(http.MethodGet, "/auth/me", nil).Data)
	if me.DataScope != "department" || len(me.Permissions) != 3 {
		t.Fatalf("me = %+v", me)
	}
}

func TestRoleScopeAndPermissionGrantRules(t *testing.T) {
	e := newEnv(t)
	editor := e.seedRole("EDITOR", "department", "system.role.read", "system.role.write")
	e.seedUser("editor", pw, false, false, editor.ID)
	c := e.loggedIn("editor", pw)

	expect(t, c.do(http.MethodPost, "/system/roles", map[string]any{
		"code": "r1", "name": "r1", "data_scope": "self", "permissions": []string{"system.role.read"},
	}), http.StatusCreated, "")
	expect(t, c.do(http.MethodPost, "/system/roles", map[string]any{
		"code": "r2", "name": "r2", "data_scope": "all", "permissions": []string{},
	}), http.StatusForbidden, "SYS-403")
	expect(t, c.do(http.MethodPost, "/system/roles", map[string]any{
		"code": "r3", "name": "r3", "data_scope": "self", "permissions": []string{"system.audit.read"},
	}), http.StatusForbidden, "SYS-403")
	expect(t, c.do(http.MethodPost, "/system/roles", map[string]any{
		"code": "r4", "name": "r4", "data_scope": "self", "permissions": []string{"no.such.perm"},
	}), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, c.do(http.MethodPost, "/system/roles", map[string]any{
		"code": "R1", "name": "dup", "data_scope": "self",
	}), http.StatusConflict, "ROLE-001")
	// 角色仍有使用者時不可刪除
	expect(t, c.do(http.MethodDelete, "/system/roles/"+itoa(editor.ID), nil), http.StatusConflict, "ROLE-002")
}

// ---- 部門、樂觀鎖、稽核 ----

func TestDepartmentsTreeRulesAndVersionConflict(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)

	type dept struct {
		ID      int64 `json:"id"`
		Version int32 `json:"version"`
	}
	parent := decode[dept](t, c.do(http.MethodPost, "/system/departments", map[string]any{"code": "hq", "name": "總公司"}).Data)
	child := decode[dept](t, c.do(http.MethodPost, "/system/departments", map[string]any{
		"code": "sales", "name": "業務部", "parent_id": parent.ID,
	}).Data)

	expect(t, c.do(http.MethodPost, "/system/departments", map[string]any{"code": "HQ", "name": "重複"}),
		http.StatusConflict, "DEPT-001")
	// 不可把上層設成自己的下層(形成循環)
	expect(t, c.do(http.MethodPut, "/system/departments/"+itoa(parent.ID), map[string]any{
		"code": "HQ", "name": "總公司", "parent_id": child.ID, "is_active": true, "version": parent.Version,
	}), http.StatusUnprocessableEntity, "SYS-422")

	upd := map[string]any{"code": "HQ", "name": "總公司2", "is_active": true, "version": parent.Version}
	expect(t, c.do(http.MethodPut, "/system/departments/"+itoa(parent.ID), upd), http.StatusOK, "")
	// 用舊版本再改一次:樂觀鎖衝突
	expect(t, c.do(http.MethodPut, "/system/departments/"+itoa(parent.ID), upd), http.StatusConflict, "SYS-409")
	expect(t, c.do(http.MethodPut, "/system/departments/99999", upd), http.StatusNotFound, "SYS-404")
}

func TestAuditLogOmitsSecrets(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	expect(t, c.do(http.MethodPost, "/system/users", map[string]any{
		"username": "carol", "name": "Carol", "password": "Passw0rd1", "email": "carol@example.com",
	}), http.StatusCreated, "")

	res := c.do(http.MethodGet, "/system/audit-logs?entity_type=user&action=create", nil)
	expect(t, res, http.StatusOK, "")
	logs := decode[[]struct {
		Summary  string          `json:"summary"`
		After    json.RawMessage `json:"after"`
		Username string          `json:"username"`
	}](t, res.Data)
	if len(logs) != 1 || logs[0].Username != "root" || !strings.Contains(logs[0].Summary, "carol") {
		t.Fatalf("logs = %+v", logs)
	}
	if strings.Contains(string(logs[0].After), "password_hash") || strings.Contains(string(logs[0].After), "$2a$") {
		t.Fatalf("稽核不應包含密碼資訊: %s", logs[0].After)
	}
	if _, err := e.pool.Exec(context.Background(), "DELETE FROM audit_logs"); err == nil {
		t.Fatal("稽核日誌應不可刪除")
	}
}

func TestListUsersPaginationAndFilters(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	for _, n := range []string{"amy", "andy", "ben"} {
		e.seedUser(n, pw, false, false)
	}
	c := e.loggedIn("root", pw)
	res := c.do(http.MethodGet, "/system/users?keyword=an&size=1", nil)
	expect(t, res, http.StatusOK, "")
	meta := decode[struct {
		Total int64 `json:"total"`
		Size  int   `json:"size"`
	}](t, res.Meta)
	users := decode[[]struct {
		Username string `json:"username"`
	}](t, res.Data)
	if meta.Total != 1 || meta.Size != 1 || len(users) != 1 || users[0].Username != "andy" {
		t.Fatalf("meta=%+v users=%+v", meta, users)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
