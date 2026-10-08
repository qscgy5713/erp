package app

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

// 路由權限稽核:列舉所有 API 路由,用「沒有任何權限的登入者」與「未登入」各呼叫一次。
//   - 未登入:除了明確列出的公開路由,一律 401;
//   - 沒有權限:除了「只要登入即可」的白名單,一律 403(或資源不存在的 404)。
//
// 之後新增路由若忘了加權限檢查,這個測試會失敗;確實只要登入即可的才需要加進白名單。
var publicRoutes = map[string]bool{
	"GET /api/v1/health":          true,
	"POST /api/v1/auth/login":     true,
	"POST /api/v1/auth/login/2fa": true,
	"POST /api/v1/auth/refresh":   true,
	"POST /api/v1/auth/logout":    true,
}

// 只要登入即可(下拉選單、自己的帳號安全、儀表板;內容由程式依權限過濾,或本身就是自己的資料)。
var loginOnlyRoutes = map[string]string{
	"GET /api/v1/auth/me":                          "自己的資料",
	"POST /api/v1/auth/change-password":            "自己的密碼",
	"GET /api/v1/auth/2fa":                         "自己的雙因素狀態",
	"POST /api/v1/auth/2fa/setup":                  "自己的雙因素設定",
	"POST /api/v1/auth/2fa/enable":                 "自己的雙因素設定",
	"POST /api/v1/auth/2fa/disable":                "自己的雙因素設定(須密碼與驗證碼)",
	"POST /api/v1/auth/2fa/recovery-codes":         "自己的備援碼(須密碼與驗證碼)",
	"GET /api/v1/dashboard":                        "依權限只回傳有權看的卡片",
	"GET /api/v1/system/user-options":              "下拉選單",
	"GET /api/v1/masterdata/currencies":            "下拉選單",
	"GET /api/v1/masterdata/exchange-rates/lookup": "開單帶匯率",
	"GET /api/v1/masterdata/tax-types":             "下拉選單",
	"GET /api/v1/masterdata/payment-terms":         "下拉選單",
	"GET /api/v1/masterdata/units":                 "下拉選單",
	"GET /api/v1/masterdata/item-categories":       "下拉選單",
	"GET /api/v1/masterdata/warehouses":            "下拉選單",
	"GET /api/v1/masterdata/bins":                  "下拉選單",
	"GET /api/v1/masterdata/item-options":          "開單選料",
}

func TestEveryRouteEnforcesAuthAndPermission(t *testing.T) {
	e := newEnv(t)
	e.seedUser("nobody", pw, false, false)
	nobody := e.loggedIn("nobody", pw)

	fill := func(p string) string {
		parts := strings.Split(p, "/")
		for i, s := range parts {
			if strings.HasPrefix(s, ":") {
				switch s {
				case ":action":
					parts[i] = "post"
				case ":period":
					parts[i] = "2026-01"
				case ":year":
					parts[i] = "2025"
				case ":docType":
					parts[i] = "sales_order"
				case ":code":
					parts[i] = "TWD"
				default:
					parts[i] = "1"
				}
			}
		}
		return strings.Join(parts, "/")
	}
	do := func(method, path, token string) int {
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		e.r.ServeHTTP(w, req)
		return w.Code
	}

	var bad []string
	n := 0
	for _, r := range e.r.Routes() {
		if !strings.HasPrefix(r.Path, "/api/v1/") || r.Method == http.MethodOptions || r.Method == http.MethodHead {
			continue
		}
		key := r.Method + " " + r.Path
		path := fill(r.Path)
		if key == "GET /api/v1/approval/progress" { // 權限在 handler 內依單據類型檢查,須帶有效的參數才走到那裡
			path += "?doc_type=purchase_order&doc_id=1"
		}
		n++
		if publicRoutes[key] {
			continue
		}
		if code := do(r.Method, path, ""); code != http.StatusUnauthorized {
			bad = append(bad, key+" 未登入應回 401,實際 "+itoa(int64(code)))
		}
		if _, ok := loginOnlyRoutes[key]; ok {
			continue
		}
		if code := do(r.Method, path, nobody.token); code != http.StatusForbidden {
			bad = append(bad, key+" 沒有權限應回 403,實際 "+itoa(int64(code)))
		}
	}
	if n < 150 {
		t.Fatalf("只掃到 %d 條路由,稽核可能沒有涵蓋全部", n)
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("%d 條路由沒有正確擋下:\n%s", len(bad), strings.Join(bad, "\n"))
	}
	t.Logf("已稽核 %d 條路由", n)
}
