package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"erp/internal/auth"
)

// 資安 review 發現的問題的回歸測試。

func TestResponsesAreNotCacheableAndNoSniff(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	for _, path := range []string{"/api/v1/health", "/api/v1/auth/me", "/api/v1/masterdata/units"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+c.token)
		w := httptest.NewRecorder()
		e.r.ServeHTTP(w, req)
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s 缺少安全標頭: %v", path, w.Header())
		}
	}
}

func TestOversizedRequestBodyIsRejected(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	big := `{"code":"` + strings.Repeat("A", 7<<20) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/masterdata/units", bytes.NewBufferString(big))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超大請求應被拒絕,實際 %d", w.Code)
	}
}

// 登入後輸錯目前密碼也要計入失敗次數:拿到 access token 的人不能無限次試密碼。
func TestChangePasswordWrongOldPasswordLocksAccount(t *testing.T) {
	e := newEnv(t)
	e.seedUser("alice", pw, false, false)
	c := e.loggedIn("alice", pw)
	for i := 1; i < auth.MaxLoginAttempts; i++ {
		expect(t, c.do(http.MethodPost, "/auth/change-password", map[string]string{"old_password": "wrong-pass-1", "new_password": "New-Pass-12345"}), http.StatusUnprocessableEntity, "AUTH-006")
	}
	expect(t, c.do(http.MethodPost, "/auth/change-password", map[string]string{"old_password": "wrong-pass-1", "new_password": "New-Pass-12345"}), http.StatusLocked, "AUTH-002")
	// 鎖定中連登入都不行(含正確密碼)
	expect(t, (&client{e: e}).login("alice", pw), http.StatusLocked, "AUTH-002")
}

// 停用 / 重新產生備援碼時輸錯密碼或驗證碼,同樣計入鎖定。
func TestTwoFactorSensitiveEndpointsCountFailures(t *testing.T) {
	e := newEnv(t)
	e.seedUser("alice", pw, false, false)
	c := e.loggedIn("alice", pw)
	en := c.enroll()
	_ = en
	for i := 1; i < auth.MaxLoginAttempts; i++ {
		expect(t, c.do(http.MethodPost, "/auth/2fa/disable", map[string]string{"password": pw, "code": "000000"}), http.StatusUnauthorized, "AUTH-008")
	}
	expect(t, c.do(http.MethodPost, "/auth/2fa/disable", map[string]string{"password": pw, "code": "000000"}), http.StatusLocked, "AUTH-002")
}

// 客戶 / 供應商下拉清單:沒有任何相關權限的登入者不能列舉(含全部資料範圍的人)。
func TestPartnerOptionsRequireRelatedPermission(t *testing.T) {
	s := newSalCtx(t)
	e := s.e
	e.seedSupplier("S1", "TWD")
	norole := e.seedRole("OTHER", "all", "inventory.stock.read") // 資料範圍全部,但和客戶 / 供應商無關
	e.seedUser("zed", pw, false, false, norole.ID)
	sales := e.seedRole("SAL", "all", "sales.order.write")
	pur := e.seedRole("PUR", "all", "purchase.order.read")
	e.seedUser("sam", pw, false, false, sales.ID)
	e.seedUser("pat", pw, false, false, pur.ID)
	zed, sam, pat := e.loggedIn("zed", pw), e.loggedIn("sam", pw), e.loggedIn("pat", pw)
	expect(t, zed.do(http.MethodGet, "/masterdata/customer-options", nil), http.StatusForbidden, "SYS-403")
	expect(t, zed.do(http.MethodGet, "/masterdata/supplier-options", nil), http.StatusForbidden, "SYS-403")
	expect(t, sam.do(http.MethodGet, "/masterdata/customer-options", nil), http.StatusOK, "")
	expect(t, sam.do(http.MethodGet, "/masterdata/supplier-options", nil), http.StatusForbidden, "SYS-403")
	expect(t, pat.do(http.MethodGet, "/masterdata/supplier-options", nil), http.StatusOK, "")
	expect(t, pat.do(http.MethodGet, "/masterdata/customer-options", nil), http.StatusForbidden, "SYS-403")
}

// 簽核進度:單據須存在,且業務類單據受資料範圍限制(範圍外視為不存在)。
func TestApprovalProgressRespectsDataScope(t *testing.T) {
	s := newSalCtx(t)
	e, root := s.e, s.c
	role := e.seedRole("SALES", "self", "sales.order.read", "sales.order.write")
	amy := e.seedUser("amy", pw, false, false, role.ID)
	ben := e.seedUser("ben", pw, false, false, role.ID)
	cAmy, cBen := e.seedCustomer("C-AMY", "0", &amy.ID), e.seedCustomer("C-BEN", "0", &ben.ID)
	mk := func(cust int64) purDoc {
		d, res := root.createPur(salesOrders, s.header(map[string]any{
			"doc_type": "order", "customer_id": cust, "lines": []map[string]any{line(s.item, s.box, "1", "100", nil)},
		}))
		expect(t, res, http.StatusCreated, "")
		return d
	}
	mine, theirs := mk(cAmy), mk(cBen)
	ac := e.loggedIn("amy", pw)
	expect(t, ac.do(http.MethodGet, "/approval/progress?doc_type=sales_order&doc_id="+itoa(mine.ID), nil), http.StatusOK, "")
	expect(t, ac.do(http.MethodGet, "/approval/progress?doc_type=sales_order&doc_id="+itoa(theirs.ID), nil), http.StatusNotFound, "SYS-404")
	expect(t, ac.do(http.MethodGet, "/approval/progress?doc_type=sales_order&doc_id=999999", nil), http.StatusNotFound, "SYS-404")
	// 全部範圍的人看得到
	expect(t, root.do(http.MethodGet, "/approval/progress?doc_type=sales_order&doc_id="+itoa(theirs.ID), nil), http.StatusOK, "")
}
