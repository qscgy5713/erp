package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
)

type repGroup struct {
	Code           string `json:"code"`
	DeliveryAmount string `json:"delivery_amount"`
	ReturnAmount   string `json:"return_amount"`
	NetAmount      string `json:"net_amount"`
	SharePct       string `json:"share_pct"`
	CostedAmount   string `json:"costed_amount"`
	CostAmount     string `json:"cost_amount"`
	Margin         string `json:"margin"`
	MarginPct      string `json:"margin_pct"`
	ReceiptAmount  string `json:"receipt_amount"`
	AvgPrice       string `json:"avg_price"`
}

type repOut struct {
	Rows   []repGroup `json:"rows"`
	Totals struct {
		NetAmount      string `json:"net_amount"`
		CostedAmount   string `json:"costed_amount"`
		UncostedAmount string `json:"uncosted_amount"`
		Margin         string `json:"margin"`
	} `json:"totals"`
}

func decEq(t *testing.T, label, got, want string) {
	t.Helper()
	g, err := decimal.NewFromString(got)
	if err != nil || !g.Round(2).Equal(decimal.RequireFromString(want)) {
		t.Fatalf("%s = %q,預期 %s", label, got, want)
	}
}

func TestSalesReport(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	cust := e.seedCustomer("C1", "0", nil)
	p.receiveOn("2026-08-10", "2", "1200")
	p.receiveOn("2026-09-05", "5", "1200")
	p.receiveOn("2026-09-10", "5", "1440")
	dn, res := c.createPur(deliveries, map[string]any{
		"doc_date": "2026-09-15", "doc_type": "delivery", "customer_id": cust, "warehouse_id": p.wh, "currency": "TWD",
		"tax_type_id": p.tax, "lines": []map[string]any{line(p.item, p.box, "3", "2000", nil)},
	})
	expect(t, res, http.StatusCreated, "")
	// 草稿不計入
	q := "from=2026-09-01&to=2026-09-30"
	out := decode[repOut](t, c.do(http.MethodGet, "/sales/reports?group=customer&"+q, nil).Data)
	if len(out.Rows) != 0 {
		t.Fatalf("草稿不應入報表: %+v", out.Rows)
	}
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")

	// 月結前:有銷售、無毛利
	out = decode[repOut](t, c.do(http.MethodGet, "/sales/reports?group=customer&"+q, nil).Data)
	if len(out.Rows) != 1 || out.Rows[0].Code != "C1" {
		t.Fatalf("rows = %+v", out.Rows)
	}
	decEq(t, "net", out.Totals.NetAmount, "6000")
	decEq(t, "uncosted", out.Totals.UncostedAmount, "6000")
	decEq(t, "costed", out.Totals.CostedAmount, "0")

	// 月結後:成本 3900、毛利 2100
	expect(t, c.do(http.MethodPost, "/costing/closings/2026-08/run", nil), http.StatusOK, "")
	expect(t, c.do(http.MethodPost, "/costing/closings/2026-09/run", nil), http.StatusOK, "")
	for _, g := range []string{"customer", "item"} {
		out = decode[repOut](t, c.do(http.MethodGet, "/sales/reports?group="+g+"&"+q, nil).Data)
		if len(out.Rows) != 1 {
			t.Fatalf("%s rows = %+v", g, out.Rows)
		}
		r := out.Rows[0]
		decEq(t, g+" net", r.NetAmount, "6000")
		decEq(t, g+" share", r.SharePct, "100")
		decEq(t, g+" cost", r.CostAmount, "3900")
		decEq(t, g+" margin", r.Margin, "2100")
		decEq(t, g+" margin_pct", r.MarginPct, "35")
		decEq(t, g+" uncosted", out.Totals.UncostedAmount, "0")
	}
	// 明細 + 篩選
	det := decode[[]struct {
		DocNo  string `json:"doc_no"`
		Amount string `json:"amount"`
		Cost   string `json:"cost"`
		Margin string `json:"margin"`
	}](t, c.do(http.MethodGet, "/sales/reports?group=detail&"+q+"&customer_id="+itoa(cust), nil).Data)
	if len(det) != 1 {
		t.Fatalf("detail = %+v", det)
	}
	decEq(t, "detail margin", det[0].Margin, "2100")
	out = decode[repOut](t, c.do(http.MethodGet, "/sales/reports?group=customer&from=2026-10-01&to=2026-10-31", nil).Data)
	if len(out.Rows) != 0 {
		t.Fatalf("區間外不應有資料: %+v", out.Rows)
	}

	// 驗證
	for _, bad := range []string{
		"group=foo&" + q, "group=customer", "group=customer&from=2026-09-30&to=2026-09-01",
		"group=customer&from=2015-01-01&to=2026-09-30",
	} {
		expect(t, c.do(http.MethodGet, "/sales/reports?"+bad, nil), http.StatusUnprocessableEntity, "SYS-422")
	}

	// Excel
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sales/reports?group=customer&"+q+"&format=xlsx", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("xlsx status = %d", w.Code)
	}
	f, err := excelize.OpenReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := f.GetRows(f.GetSheetName(0), excelize.Options{RawCellValue: true})
	if err != nil || len(rows) < 6 || rows[5][1] != "C1" {
		t.Fatalf("xlsx rows = %v err=%v", rows, err)
	}

	// 無權限
	e.seedUser("nobody", pw, false, false)
	expect(t, e.loggedIn("nobody", pw).do(http.MethodGet, "/sales/reports?group=customer&"+q, nil), http.StatusForbidden, "SYS-403")
}

func TestSalesReportDataScope(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	p.receiveOn("2026-09-05", "5", "1200")
	role := e.seedRole("SALES", "self", "sales.delivery.read")
	amy := e.seedUser("amy", pw, false, false, role.ID)
	ben := e.seedUser("ben", pw, false, false, role.ID)
	cAmy, cBen := e.seedCustomer("C-AMY", "0", &amy.ID), e.seedCustomer("C-BEN", "0", &ben.ID)
	for _, cu := range []int64{cAmy, cBen} {
		dn, res := c.createPur(deliveries, map[string]any{
			"doc_date": "2026-09-15", "doc_type": "delivery", "customer_id": cu, "warehouse_id": p.wh, "currency": "TWD",
			"tax_type_id": p.tax, "lines": []map[string]any{line(p.item, p.box, "1", "2000", nil)},
		})
		expect(t, res, http.StatusCreated, "")
		c.approvePur(deliveries, &dn)
		expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")
	}
	q := "/sales/reports?group=customer&from=2026-09-01&to=2026-09-30"
	if out := decode[repOut](t, c.do(http.MethodGet, q, nil).Data); len(out.Rows) != 2 {
		t.Fatalf("root rows = %+v", out.Rows)
	}
	out := decode[repOut](t, e.loggedIn("amy", pw).do(http.MethodGet, q, nil).Data)
	if len(out.Rows) != 1 || out.Rows[0].Code != "C-AMY" {
		t.Fatalf("amy rows = %+v", out.Rows)
	}
}

func TestPurchaseReport(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	p.receiveOn("2026-09-05", "5", "1200")
	p.receiveOn("2026-09-10", "5", "1440")
	q := "from=2026-09-01&to=2026-09-30"
	for _, g := range []string{"supplier", "item"} {
		out := decode[repOut](t, c.do(http.MethodGet, "/purchase/reports?group="+g+"&"+q, nil).Data)
		if len(out.Rows) != 1 {
			t.Fatalf("%s rows = %+v", g, out.Rows)
		}
		decEq(t, g+" net", out.Rows[0].NetAmount, "13200")
		decEq(t, g+" total", out.Totals.NetAmount, "13200")
		decEq(t, g+" share", out.Rows[0].SharePct, "100")
		if g == "item" { // 120 個 → 單價 110
			decEq(t, "avg", out.Rows[0].AvgPrice, "110")
		}
	}
	det := decode[[]struct {
		Amount string `json:"amount"`
	}](t, c.do(http.MethodGet, "/purchase/reports?group=detail&"+q, nil).Data)
	if len(det) != 2 {
		t.Fatalf("detail = %+v", det)
	}
	expect(t, c.do(http.MethodGet, "/purchase/reports?group=x&"+q, nil), http.StatusUnprocessableEntity, "SYS-422")
	e.seedUser("nobody", pw, false, false)
	expect(t, e.loggedIn("nobody", pw).do(http.MethodGet, "/purchase/reports?group=supplier&"+q, nil), http.StatusForbidden, "SYS-403")
}

func TestLastPurchasePrices(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	type lp struct {
		ItemID    int64  `json:"item_id"`
		UnitID    int64  `json:"unit_id"`
		UnitPrice string `json:"unit_price"`
		DocDate   string `json:"doc_date"`
	}
	get := func(cl *client, q string) apiResp {
		return cl.do(http.MethodGet, "/purchase/last-prices?"+q, nil)
	}
	base := "supplier_id=" + itoa(p.sup) + "&currency=TWD&item_ids=" + itoa(p.item)
	if got := decode[[]lp](t, get(c, base).Data); len(got) != 0 {
		t.Fatalf("尚無進貨應為空: %+v", got)
	}
	p.receiveOn("2026-09-05", "5", "1200")
	p.receiveOn("2026-09-10", "5", "1440")
	// 草稿不算
	_, res := c.createPur(receipts, p.header(map[string]any{
		"doc_date": "2026-09-20", "doc_type": "receipt",
		"lines": []map[string]any{line(p.item, p.box, "1", "9999", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	got := decode[[]lp](t, get(c, base).Data)
	if len(got) != 1 || got[0].UnitID != p.box || got[0].DocDate != "2026-09-10" {
		t.Fatalf("last price = %+v", got)
	}
	decEq(t, "price", got[0].UnitPrice, "1440")
	if got := decode[[]lp](t, get(c, "supplier_id="+itoa(p.sup)+"&currency=USD&item_ids="+itoa(p.item)).Data); len(got) != 0 {
		t.Fatalf("不同幣別不應帶出: %+v", got)
	}
	expect(t, get(c, "currency=TWD"), http.StatusUnprocessableEntity, "SYS-422")
	e.seedUser("nobody", pw, false, false)
	expect(t, get(e.loggedIn("nobody", pw), base), http.StatusForbidden, "SYS-403")
}

// 傳票編輯者要能列出部門(輔助核算選單),但沒有任何相關權限的人不行
func TestDepartmentsReadableByVoucherWriter(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	role := e.seedRole("ACC", "all", "gl.voucher.write")
	e.seedUser("acc", pw, false, false, role.ID)
	e.seedUser("nobody", pw, false, false)
	expect(t, e.loggedIn("acc", pw).do(http.MethodGet, "/system/departments", nil), http.StatusOK, "")
	expect(t, e.loggedIn("nobody", pw).do(http.MethodGet, "/system/departments", nil), http.StatusForbidden, "SYS-403")
}
