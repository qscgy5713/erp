package app

import (
	"context"
	"net/http"
	"testing"
	"time"

	"erp/internal/auth"
	"erp/internal/db"
	"erp/internal/platform/config"
	"erp/internal/platform/database"
	"erp/internal/platform/tenant"
	"erp/internal/testutil/dbtest"
	"erp/migrations"
)

type tenantEnv struct {
	*env
	mgr   *tenant.Manager
	store *database.Store
}

// newTenantEnv 控制庫 + 兩個租戶資料庫(alpha、beta),每個租戶一個同名的超級管理員 admin、密碼不同。
func newTenantEnv(t *testing.T) *tenantEnv {
	t.Helper()
	ctx := context.Background()
	mgr, err := tenant.NewManager(ctx, dbtest.NewControl(t), 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	if err := mgr.InitControl(ctx); err != nil {
		t.Fatal(err)
	}
	store := database.NewResolvedStore(mgr)
	for _, code := range []string{"alpha", "beta"} {
		dbName, _, err := mgr.Provision(ctx, code, code+" 公司", migrations.FS)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = mgr.DropDatabase(context.Background(), code, dbName) })
		hash, err := auth.HashPassword(code + "-Passw0rd!")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreateUser(tenant.With(ctx, code), db.CreateUserParams{
			CompanyID: 1, Username: "admin", Name: code + " 管理員", PasswordHash: hash, IsSuperadmin: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Config{
		AppEnv: "production", JWTSecret: []byte("test-secret-test-secret-test-secret!!"),
		AccessTokenTTL: 15 * time.Minute, RefreshTokenTTL: time.Hour,
		RateLimitPerMinute: 100000, LoginRateLimitPerMinute: 100000, RefreshRateLimitPerMinute: 100000,
	}
	r, err := NewTenantRouter(cfg, mgr)
	if err != nil {
		t.Fatal(err)
	}
	return &tenantEnv{env: &env{t: t, r: r}, mgr: mgr, store: store}
}

// loginTo 以指定租戶登入,回傳登入後的 client(含 refresh cookie)。
func (e *tenantEnv) loginTo(tenantCode, user, password string) (*client, apiResp) {
	c := &client{e: e.env}
	res := c.do(http.MethodPost, "/auth/login", map[string]string{"tenant": tenantCode, "username": user, "password": password})
	if res.status == http.StatusOK {
		c.token = decode[struct {
			AccessToken string `json:"access_token"`
		}](e.t, res.Data).AccessToken
	}
	return c, res
}

func TestTenantIsolation(t *testing.T) {
	e := newTenantEnv(t)
	ctx := context.Background()

	// 登入頁設定:多租戶
	cfgRes := (&client{e: e.env}).do(http.MethodGet, "/auth/config", nil)
	expect(t, cfgRes, http.StatusOK, "")
	if !decode[struct {
		Multi bool `json:"multi_tenant"`
	}](t, cfgRes.Data).Multi {
		t.Fatal("多租戶部署應回報 multi_tenant = true")
	}

	// ---- 登入:租戶必填;同名帳號各登入各的租戶;密碼錯、租戶錯、不存在的租戶都是同一個錯誤 ----
	a, res := e.loginTo("alpha", "admin", "alpha-Passw0rd!")
	expect(t, res, http.StatusOK, "")
	b, res := e.loginTo("beta", "admin", "beta-Passw0rd!")
	expect(t, res, http.StatusOK, "")
	_, noTenant := e.loginTo("", "admin", "alpha-Passw0rd!")
	_, wrongTenant := e.loginTo("beta", "admin", "alpha-Passw0rd!")
	_, ghost := e.loginTo("ghost", "admin", "alpha-Passw0rd!")
	_, badCode := e.loginTo("../x;drop", "admin", "alpha-Passw0rd!")
	for name, r := range map[string]apiResp{"沒帶租戶": noTenant, "租戶與密碼不符": wrongTenant, "租戶不存在": ghost, "租戶代碼不合法": badCode} {
		if r.status != http.StatusUnauthorized || r.code() != noTenant.code() {
			t.Errorf("%s → %d %s,應與帳密錯誤相同", name, r.status, r.code())
		}
	}
	me := func(c *client) string {
		res := c.do(http.MethodGet, "/auth/me", nil)
		expect(t, res, http.StatusOK, "")
		return decode[struct {
			Name string `json:"name"`
		}](t, res.Data).Name
	}
	if me(a) != "alpha 管理員" || me(b) != "beta 管理員" {
		t.Fatalf("各租戶的 token 應看到各自的帳號:%q / %q", me(a), me(b))
	}

	// ---- 資料隔離:alpha 建的倉庫,beta 看不到也拿不到 ----
	res = a.do(http.MethodPost, "/masterdata/warehouses", map[string]any{"code": "WA", "name": "alpha 倉"})
	expect(t, res, http.StatusCreated, "")
	whID := decode[struct {
		ID int64 `json:"id"`
	}](t, res.Data).ID
	listCodes := func(c *client) []string {
		res := c.do(http.MethodGet, "/masterdata/warehouses", nil)
		expect(t, res, http.StatusOK, "")
		var out []string
		for _, w := range decode[[]struct {
			Code string `json:"code"`
		}](t, res.Data) {
			out = append(out, w.Code)
		}
		return out
	}
	if got := listCodes(a); len(got) != 1 || got[0] != "WA" {
		t.Fatalf("alpha 倉庫 = %v", got)
	}
	if got := listCodes(b); len(got) != 0 {
		t.Fatalf("beta 不應看到 alpha 的倉庫:%v", got)
	}
	// 直接查資料庫確認物理上分開
	for code, want := range map[string]int{"alpha": 1, "beta": 0} {
		var n int
		p, err := e.mgr.Pool(tenant.With(ctx, code))
		if err != nil {
			t.Fatal(err)
		}
		if err := p.QueryRow(ctx, `SELECT count(*) FROM warehouses`).Scan(&n); err != nil || n != want {
			t.Fatalf("%s 資料庫的倉庫數 = %d(%v),應為 %d", code, n, err, want)
		}
	}
	_ = whID

	// ---- 沒有租戶的 context 一律失敗,不會預設到某個租戶 ----
	if _, err := e.store.GetUserByUsername(ctx, "admin"); err == nil {
		t.Fatal("沒有租戶代碼的查詢應失敗")
	}
	if err := e.store.InTx(ctx, func(*db.Queries) error { return nil }); err == nil {
		t.Fatal("沒有租戶代碼的交易應失敗")
	}

	// ---- refresh cookie 帶租戶;換租戶的 cookie 無效 ----
	if a.cookie == nil {
		t.Fatal("登入應發 refresh cookie")
	}
	ra := &client{e: e.env, cookie: a.cookie}
	expect(t, ra.do(http.MethodPost, "/auth/refresh", nil), http.StatusOK, "")
	forged := *b.cookie
	_, raw, _ := cutTilde(forged.Value)
	forged.Value = "alpha~" + raw // 拿 beta 的 token 冒充 alpha
	fc := &client{e: e.env, cookie: &forged}
	if res := fc.do(http.MethodPost, "/auth/refresh", nil); res.status != http.StatusUnauthorized {
		t.Fatalf("跨租戶的 refresh token 應被拒絕,got %d", res.status)
	}
	lo := &client{e: e.env, cookie: b.cookie}
	expect(t, lo.do(http.MethodPost, "/auth/logout", nil), http.StatusNoContent, "")

	// ---- 停用租戶:既有 token 與登入立即失效,其他租戶不受影響;啟用後恢復 ----
	if err := e.mgr.SetActive(ctx, "alpha", false); err != nil {
		t.Fatal(err)
	}
	expect(t, a.do(http.MethodGet, "/auth/me", nil), http.StatusUnauthorized, "SYS-401")
	if _, r := e.loginTo("alpha", "admin", "alpha-Passw0rd!"); r.status != http.StatusUnauthorized {
		t.Fatalf("停用的租戶不可登入,got %d", r.status)
	}
	if me(b) != "beta 管理員" {
		t.Fatal("停用 alpha 不應影響 beta")
	}
	if err := e.mgr.SetActive(ctx, "alpha", true); err != nil {
		t.Fatal(err)
	}
	if me(a) != "alpha 管理員" {
		t.Fatal("啟用後 token 應恢復")
	}

	// ---- 單租戶的 token(沒有租戶宣告)在多租戶部署不被接受 ----
	old, _, _ := auth.NewTokenIssuer([]byte("test-secret-test-secret-test-secret!!"), time.Minute).Issue("", 1, 1, 1)
	expect(t, (&client{e: e.env, token: old}).do(http.MethodGet, "/auth/me", nil), http.StatusUnauthorized, "SYS-401")
}

func cutTilde(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '~' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// TestTenantProvisionRollback 建立租戶失敗(例如代碼重複)不留下半成品。
func TestTenantProvisionRollback(t *testing.T) {
	e := newTenantEnv(t)
	ctx := context.Background()
	if _, _, err := e.mgr.Provision(ctx, "alpha", "重複", migrations.FS); err == nil {
		t.Fatal("代碼重複應失敗")
	}
	for _, bad := range []string{"", "A", "UPPER", "a b", "x;drop database", "-lead", "a"} {
		if _, _, err := e.mgr.Provision(ctx, bad, "x", migrations.FS); err == nil {
			t.Fatalf("代碼 %q 應被拒絕", bad)
		}
	}
	list, err := e.mgr.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("租戶清單 = %v %v", list, err)
	}
}
