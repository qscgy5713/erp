package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type vatResp struct {
	Summary map[string]string `json:"summary"`
	Issues  []struct {
		Kind  string `json:"kind"`
		DocNo string `json:"doc_no"`
	} `json:"issues"`
	Sales     []map[string]string `json:"sales"`
	Purchases []map[string]string `json:"purchases"`
}

func (c *client) vat(query string) (vatResp, apiResp) {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/gl/reports/vat401?"+query, nil)
	if res.status != http.StatusOK {
		return vatResp{}, res
	}
	return decode[vatResp](c.e.t, res.Data), res
}

func TestVat401(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	post := func(path string, d *purDoc) {
		t.Helper()
		c.approvePur(path, d)
		expect(t, c.actPur(path, d, "post"), http.StatusOK, "")
	}
	invoice := func(d *purDoc, no string) {
		t.Helper()
		expect(t, c.do(http.MethodPut, deliveries+"/"+itoa(d.ID)+"/invoice",
			map[string]any{"invoice_no": no, "invoice_date": today, "version": d.Version}), http.StatusOK, "")
	}
	if _, err := e.pool.Exec(t.Context(), "UPDATE customers SET tax_id = '12345678' WHERE id = $1", s.cust); err != nil {
		t.Fatal(err)
	}

	// 銷項:C1(有統編)出貨 3 箱並登錄發票,退回 1 箱並登錄發票;另一次出貨 1 箱沒有發票 → 待處理
	so := s.newOrder("5")
	d1, _ := s.deliver(so, "3")
	post(deliveries, &d1)
	invoice(&d1, "AB00000001")
	ret, res := c.createPur(deliveries, s.header(map[string]any{
		"doc_type": "return", "lines": []map[string]any{line(s.item, s.box, "1", "1000", map[string]any{"delivery_line_id": d1.Lines[0].ID})},
	}))
	expect(t, res, http.StatusCreated, "")
	post(deliveries, &ret)
	invoice(&ret, "AB00000002")
	d2, _ := s.deliver(so, "1")
	post(deliveries, &d2)
	// C2(無統編)直接出貨 1 箱並登錄發票 → 二聯式
	c2 := e.seedCustomer("C2", "0", nil)
	d3, res := c.createPur(deliveries, s.header(map[string]any{
		"customer_id": c2, "doc_type": "delivery", "lines": []map[string]any{line(s.item, s.box, "1", "1000", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	post(deliveries, &d3)
	invoice(&d3, "AB00000003")

	// 進項:有發票的進貨(商品 5 箱 @1200 + 服務 1 @500)與沒有發票的進貨(→ 待處理)
	p := &purCtx{e: e, c: c, wh: s.wh, sup: e.seedSupplier("S1", "TWD"), tax: s.tax, item: s.item, box: s.box}
	svc := e.seedItem("SVC", "service")
	r1, res := c.createPur(receipts, p.header(map[string]any{
		"doc_type": "receipt", "invoice_no": "XY12345678",
		"lines": []map[string]any{line(p.item, p.box, "5", "1200", nil), line(svc, p.box, "1", "500", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	post(receipts, &r1)
	r3, res := c.createPur(receipts, p.header(map[string]any{
		"doc_type": "receipt", "lines": []map[string]any{line(p.item, p.box, "1", "1200", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	post(receipts, &r3)

	v, res := c.vat("year=2026&period=5")
	expect(t, res, http.StatusOK, "")
	want := map[string]string{
		"taxable_triplicate": "2000", "taxable_duplicate": "1000", "taxable_sales": "3000", "total_sales": "3000",
		"output_tax": "150", "deductible_goods": "6000", "deductible_expense": "500", "input_tax": "325",
		"net_tax": "-175", "zero_sales": "0", "exempt_sales": "0", "non_tax_purchase": "0",
	}
	for k, w := range want {
		if v.Summary[k] != w {
			t.Errorf("%s = %s, want %s", k, v.Summary[k], w)
		}
	}
	kinds := map[string]string{}
	for _, i := range v.Issues {
		kinds[i.Kind] = i.DocNo
	}
	if len(v.Issues) != 2 || kinds["sales_no_invoice"] != d2.DocNo || kinds["purchase_no_invoice"] != r3.DocNo {
		t.Errorf("待處理 = %+v", v.Issues)
	}

	// 其他期別沒有資料;參數檢查;權限;Excel 匯出
	if v, _ := c.vat("year=2026&period=4"); v.Summary["total_sales"] != "0" || len(v.Issues) != 0 {
		t.Errorf("7–8 月應無資料: %+v", v)
	}
	for _, q := range []string{"year=2026&period=7", "year=2026&period=0", "year=abc&period=1", "period=1"} {
		if _, res := c.vat(q); res.status != http.StatusUnprocessableEntity {
			t.Errorf("%s → %d", q, res.status)
		}
	}
	e.seedUser("nobody", pw, false, false)
	expect(t, e.loggedIn("nobody", pw).do(http.MethodGet, "/gl/reports/vat401?year=2026&period=5", nil), http.StatusForbidden, "SYS-403")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gl/reports/vat401?year=2026&period=5&format=xlsx", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.Len() < 1000 || !strings.Contains(w.Header().Get("Content-Disposition"), "401") {
		t.Fatalf("匯出失敗 %d len=%d", w.Code, w.Body.Len())
	}
}
