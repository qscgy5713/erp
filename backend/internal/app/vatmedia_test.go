package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mediaPreview struct {
	FileName string `json:"file_name"`
	Count    int    `json:"count"`
	Totals   struct {
		SalesCount    int    `json:"sales_count"`
		SalesAmount   string `json:"sales_amount"`
		SalesTax      string `json:"sales_tax"`
		PurchaseCount int    `json:"purchase_count"`
		PurchaseTax   string `json:"purchase_tax"`
	} `json:"totals"`
	Excluded []struct {
		Side   string `json:"side"`
		DocNo  string `json:"doc_no"`
		Reason string `json:"reason"`
	} `json:"excluded"`
}

func TestVatMediaFile(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	post := func(path string, d *purDoc) {
		t.Helper()
		c.approvePur(path, d)
		expect(t, c.actPur(path, d, "post"), http.StatusOK, "")
	}
	media := func(q string) apiResp {
		return c.do(http.MethodGet, "/gl/reports/vat401/media?year=2026&period=5"+q, nil)
	}

	// ---- 公司資料:沒設定前不能產生;驗證統編與稅籍編號 ----
	expect(t, media(""), http.StatusUnprocessableEntity, "GL-040")
	co := c.do(http.MethodGet, "/system/company", nil)
	expect(t, co, http.StatusOK, "")
	ver := decode[struct {
		Version int32 `json:"version"`
	}](t, co.Data).Version
	put := func(taxID, reg string, version int32) apiResp {
		return c.do(http.MethodPut, "/system/company", map[string]any{"name": "測試公司", "tax_id": taxID, "tax_reg_no": reg, "version": version})
	}
	expect(t, put("12345678", "A12345678", ver), http.StatusUnprocessableEntity, "SYS-422") // 統編檢查碼錯
	expect(t, put("04595257", "abc", ver), http.StatusUnprocessableEntity, "SYS-422")       // 稅籍編號長度
	expect(t, put("04595257", "a12345678", ver+9), http.StatusConflict, "SYS-409")          // 版本衝突
	expect(t, put("04595257", "a12345678", ver), http.StatusOK, "")                         // 小寫自動轉大寫

	// ---- 進貨單發票欄位驗證:選了憑證種類就要有「字軌+8 碼」與發票日期 ----
	p := &purCtx{e: e, c: c, wh: s.wh, sup: e.seedSupplier("S1", "TWD"), tax: s.tax, item: s.item, box: s.box}
	if _, err := e.pool.Exec(t.Context(), "UPDATE suppliers SET tax_id = '22099131' WHERE id = $1", p.sup); err != nil {
		t.Fatal(err)
	}
	recv := func(extra map[string]any) (purDoc, apiResp) {
		h := map[string]any{"doc_type": "receipt", "lines": []map[string]any{line(p.item, p.box, "2", "1200", nil)}}
		for k, v := range extra {
			h[k] = v
		}
		return c.createPur(receipts, p.header(h))
	}
	_, res := recv(map[string]any{"invoice_kind": "triplicate", "invoice_no": "12345", "invoice_date": today})
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	_, res = recv(map[string]any{"invoice_kind": "triplicate", "invoice_no": "XY12345678"}) // 缺發票日期
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	_, res = recv(map[string]any{"invoice_kind": "bogus", "invoice_no": "XY12345678", "invoice_date": today})
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")

	// ---- 資料:銷項 1 筆有發票 + 1 筆沒發票;進項 1 筆完整 + 1 筆沒憑證種類 ----
	so := s.newOrder("5")
	d1, _ := s.deliver(so, "3")
	post(deliveries, &d1)
	expect(t, c.do(http.MethodPut, deliveries+"/"+itoa(d1.ID)+"/invoice",
		map[string]any{"invoice_no": "AB00000001", "invoice_date": today, "version": d1.Version}), http.StatusOK, "")
	d2, _ := s.deliver(so, "1")
	post(deliveries, &d2)
	r1, res := recv(map[string]any{"invoice_kind": "register3", "invoice_no": "xy12345678", "invoice_date": today}) // 小寫自動轉大寫
	expect(t, res, http.StatusCreated, "")
	post(receipts, &r1)
	r2, res := recv(map[string]any{"invoice_no": "手寫備註"}) // 沒選憑證種類:不驗格式,但不列入申報檔
	expect(t, res, http.StatusCreated, "")
	post(receipts, &r2)

	res = media("")
	expect(t, res, http.StatusOK, "")
	pv := decode[mediaPreview](t, res.Data)
	if pv.FileName != "04595257.TXT" || pv.Count != 2 || pv.Totals.SalesCount != 1 || pv.Totals.SalesAmount != "3000" ||
		pv.Totals.SalesTax != "150" || pv.Totals.PurchaseCount != 1 || pv.Totals.PurchaseTax != "120" {
		t.Fatalf("預覽 = %+v", pv)
	}
	if len(pv.Excluded) != 2 {
		t.Fatalf("未納入 = %+v", pv.Excluded)
	}
	reasons := map[string]string{}
	for _, x := range pv.Excluded {
		reasons[x.DocNo] = x.Reason
	}
	if !strings.Contains(reasons[d2.DocNo], "尚未登錄發票") || !strings.Contains(reasons[r2.DocNo], "沒有供應商發票") {
		t.Fatalf("未納入原因 = %+v", reasons)
	}

	// ---- 下載:2 筆、每筆 81 字元、CRLF;第一筆為銷項 32(客戶沒有統編)、第二筆為進項 25 ----
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gl/reports/vat401/media?year=2026&period=5&format=txt", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "04595257.TXT") {
		t.Fatalf("下載失敗 %d %s", w.Code, w.Header().Get("Content-Disposition"))
	}
	rows := strings.Split(strings.TrimSuffix(w.Body.String(), "\r\n"), "\r\n")
	if len(rows) != 2 {
		t.Fatalf("筆數 = %d: %q", len(rows), w.Body.String())
	}
	for i, r := range rows {
		if len(r) != 81 || r[2:11] != "A12345678" {
			t.Errorf("第 %d 筆 = %q", i+1, r)
		}
	}
	if rows[0][:2] != "32" || rows[0][39:49] != "AB00000001" || rows[0][49:61] != "000000003000" || rows[0][62:72] != "0000000150" {
		t.Errorf("銷項 = %q", rows[0])
	}
	if rows[1][:2] != "25" || rows[1][31:39] != "22099131" || rows[1][39:49] != "XY12345678" || rows[1][72:73] != "1" {
		t.Errorf("進項 = %q", rows[1])
	}

	// 沒有資料的期別不能下載;無權限者不能看
	if res := c.do(http.MethodGet, "/gl/reports/vat401/media?year=2026&period=1&format=txt", nil); res.status != http.StatusUnprocessableEntity || res.code() != "GL-041" {
		t.Errorf("空期別 = %d %s", res.status, res.code())
	}
	e.seedUser("nobody", pw, false, false)
	expect(t, e.loggedIn("nobody", pw).do(http.MethodGet, "/gl/reports/vat401/media?year=2026&period=5", nil), http.StatusForbidden, "SYS-403")
}
