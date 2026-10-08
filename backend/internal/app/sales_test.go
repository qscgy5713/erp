package app

import (
	"context"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"

	"erp/internal/db"
)

// ---- 測試資料 ----

func (e *env) seedCustomer(code string, creditLimit string, salesUserID *int64) int64 {
	e.t.Helper()
	tax, term := e.idByCode("tax_types", "TX5"), e.idByCode("payment_terms", "M30")
	c, err := e.q.CreateCustomer(context.Background(), db.CreateCustomerParams{
		CompanyID: 1, Code: code, Name: code + " 公司", Contacts: []byte("[]"), Addresses: []byte("[]"),
		Currency: "TWD", TaxTypeID: &tax, PaymentTermID: &term, CreditLimit: decimal.RequireFromString(creditLimit),
		SalesUserID: salesUserID,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return c.ID
}

// salCtx 一組銷售測試共用的資料:倉庫 A 有 P1 120 個(10 箱)。
type salCtx struct {
	e                        *env
	c                        *client
	wh, cust, tax, item, box int64
}

func newSalCtx(t *testing.T) *salCtx {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	s := &salCtx{
		e: e, c: e.loggedIn("root", pw), wh: e.seedWarehouse("A", false), cust: e.seedCustomer("C1", "0", nil),
		tax: e.idByCode("tax_types", "TX5"), item: e.seedItem("P1", "goods"), box: e.unitID("BOX"),
	}
	d, res := s.c.createDoc(adjust(s.wh, s.item, s.box, "10"))
	expect(t, res, http.StatusCreated, "")
	expect(t, s.c.postAll(&d), http.StatusOK, "")
	return s
}

func (s *salCtx) header(extra map[string]any) map[string]any {
	h := map[string]any{
		"doc_date": today, "customer_id": s.cust, "warehouse_id": s.wh, "currency": "TWD",
		"tax_type_id": s.tax, "payment_term_id": s.e.idByCode("payment_terms", "M30"),
	}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

const (
	salesOrders = "/sales/orders"
	deliveries  = "/sales/deliveries"
)

// newOrder 建立並核准訂單:P1 qty 箱 @1000。
func (s *salCtx) newOrder(qty string) purDoc {
	s.e.t.Helper()
	so, res := s.c.createPur(salesOrders, s.header(map[string]any{
		"doc_type": "order", "lines": []map[string]any{line(s.item, s.box, qty, "1000", nil)},
	}))
	expect(s.e.t, res, http.StatusCreated, "")
	s.c.approvePur(salesOrders, &so)
	return so
}

func (s *salCtx) deliver(so purDoc, qty string) (purDoc, apiResp) {
	s.e.t.Helper()
	return s.c.createPur(deliveries, s.header(map[string]any{
		"doc_type": "delivery",
		"lines":    []map[string]any{line(s.item, s.box, qty, "1000", map[string]any{"so_line_id": so.Lines[0].ID})},
	}))
}

func (c *client) receivables() ([]payableRow, string) {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/finance/receivables?open_only=true", nil)
	expect(c.e.t, res, http.StatusOK, "")
	meta := decode[struct {
		BaseAmountSum string `json:"base_amount_sum"`
	}](c.e.t, res.Meta)
	return decode[[]payableRow](c.e.t, res.Data), meta.BaseAmountSum
}

type availRow struct {
	OnHand    string `json:"on_hand"`
	Reserved  string `json:"reserved"`
	Available string `json:"available"`
}

func (s *salCtx) availability(exclude int64) availRow {
	s.e.t.Helper()
	res := s.c.do(http.MethodGet, "/sales/availability?warehouse_id="+itoa(s.wh)+"&item_ids="+itoa(s.item)+"&exclude_order_id="+itoa(exclude), nil)
	expect(s.e.t, res, http.StatusOK, "")
	rows := decode[[]availRow](s.e.t, res.Data)
	if len(rows) != 1 {
		s.e.t.Fatalf("availability = %+v", rows)
	}
	return rows[0]
}

// ---- 測試 ----

func TestQuotationOrderDeliveryAndReceivable(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c

	// 報價單 → 核准 → 轉訂單
	qt, res := c.createPur(salesOrders, s.header(map[string]any{
		"doc_type": "quotation", "valid_until": "2026-10-31",
		"lines": []map[string]any{line(s.item, s.box, "6", "1000", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	if qt.DocNo != "QT202610070001" || qt.TotalAmount != "6300" {
		t.Fatalf("quotation = %+v", qt)
	}
	// 未核准的報價單不可轉訂單
	_, res = c.createPur(salesOrders, s.header(map[string]any{
		"doc_type": "order", "quotation_id": qt.ID, "lines": []map[string]any{line(s.item, s.box, "6", "1000", nil)},
	}))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	c.approvePur(salesOrders, &qt)
	so, res := c.createPur(salesOrders, s.header(map[string]any{
		"doc_type": "order", "quotation_id": qt.ID, "lines": []map[string]any{line(s.item, s.box, "6", "1000", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	if so.DocNo != "SO202610070001" {
		t.Fatalf("order = %+v", so)
	}
	// 報價單已轉訂單:不可取消核准
	expect(t, c.actPur(salesOrders, &qt, "unapprove"), http.StatusConflict, "SAL-002")
	// 訂單沒有過帳
	expect(t, c.actPur(salesOrders, &so, "post"), http.StatusNotFound, "SYS-404")

	// 核准後保留 72 個(6 箱);從其他單據看可用量 = 120 − 72
	if a := s.availability(0); a.OnHand != "120" || a.Reserved != "0" {
		t.Fatalf("核准前 availability = %+v", a)
	}
	c.approvePur(salesOrders, &so)
	if a := s.availability(0); a.Reserved != "72" || a.Available != "48" {
		t.Fatalf("核准後 availability = %+v", a)
	}
	if a := s.availability(so.ID); a.Reserved != "0" {
		t.Fatalf("排除本單 availability = %+v", a)
	}

	// 部分出貨 2 箱
	dn, res := s.deliver(so, "2")
	expect(t, res, http.StatusCreated, "")
	if dn.DocNo != "DN202610070001" || dn.TotalAmount != "2100" {
		t.Fatalf("delivery = %+v", dn)
	}
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")
	if b := e.balance(s.item, s.wh); b != "96" {
		t.Fatalf("出貨後現有量 = %s", b)
	}
	if a := s.availability(0); a.OnHand != "96" || a.Reserved != "48" {
		t.Fatalf("出貨後 availability = %+v", a)
	}
	ar, sum := c.receivables()
	if len(ar) != 1 || ar[0].Amount != "2100" || ar[0].DueDate != "2026-11-30" || sum != "2100" {
		t.Fatalf("receivables = %+v sum=%s", ar, sum)
	}
	c.reload(salesOrders, &so)
	if so.Lines[0].RemainingQty != "4" {
		t.Fatalf("remaining = %s", so.Lines[0].RemainingQty)
	}

	// 超過未出貨量
	_, res = s.deliver(so, "5")
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	// 兩張各 4 箱的草稿,第二張過帳時被擋
	d2, _ := s.deliver(so, "4")
	d3, _ := s.deliver(so, "4")
	c.approvePur(deliveries, &d2)
	c.approvePur(deliveries, &d3)
	expect(t, c.actPur(deliveries, &d2, "post"), http.StatusOK, "")
	expect(t, c.actPur(deliveries, &d3, "post"), http.StatusUnprocessableEntity, "SAL-007")
	expect(t, c.actPur(deliveries, &d3, "void"), http.StatusOK, "")

	// 已有出貨單:訂單不可取消核准;出齊後保留量歸零
	expect(t, c.actPur(salesOrders, &so, "unapprove"), http.StatusConflict, "SAL-002")
	if a := s.availability(0); a.Reserved != "0" || a.OnHand != "48" {
		t.Fatalf("出齊後 availability = %+v", a)
	}
	rows := decode[[]struct {
		DocType   string `json:"doc_type"`
		ShipState string `json:"ship_state"`
		Converted bool   `json:"converted"`
	}](t, c.do(http.MethodGet, salesOrders, nil).Data)
	for _, r := range rows {
		if (r.DocType == "order" && r.ShipState != "full") || (r.DocType == "quotation" && !r.Converted) {
			t.Fatalf("orders = %+v", rows)
		}
	}
}

func TestDeliveryStockShortageAndReturn(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	// 訂單可超過現有量(允許缺貨接單),但出貨過帳時庫存不足會被擋
	so := s.newOrder("12")
	dn, _ := s.deliver(so, "12")
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusUnprocessableEntity, "INV-001")
	expect(t, c.actPur(deliveries, &dn, "void"), http.StatusOK, "")

	dn, _ = s.deliver(so, "3")
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")

	ret := func(qty string) (purDoc, apiResp) {
		return c.createPur(deliveries, s.header(map[string]any{
			"doc_type": "return",
			"lines":    []map[string]any{line(s.item, s.box, qty, "1000", map[string]any{"delivery_line_id": dn.Lines[0].ID})},
		}))
	}
	_, res := ret("4") // 超過出貨量
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	sr, res := ret("1")
	expect(t, res, http.StatusCreated, "")
	if sr.DocNo != "SR202610070001" {
		t.Fatalf("return = %+v", sr)
	}
	c.approvePur(deliveries, &sr)
	expect(t, c.actPur(deliveries, &sr, "post"), http.StatusOK, "")
	if b := e.balance(s.item, s.wh); b != "96" { // 120 − 36 + 12
		t.Fatalf("退回後現有量 = %s", b)
	}
	if ar, sum := c.receivables(); len(ar) != 2 || sum != "2100" { // 3150 − 1050
		t.Fatalf("receivables = %+v sum=%s", ar, sum)
	}
	// 已有退回單:出貨單不可反過帳;應收已沖帳則不可反過帳
	expect(t, c.actPur(deliveries, &dn, "unpost"), http.StatusConflict, "SAL-008")
	if _, err := e.pool.Exec(context.Background(), "UPDATE accounts_receivable SET paid_amount = 1 WHERE source_type = 'sales_return'"); err != nil {
		t.Fatal(err)
	}
	expect(t, c.actPur(deliveries, &sr, "unpost"), http.StatusConflict, "FIN-002")
	if _, err := e.pool.Exec(context.Background(), "UPDATE accounts_receivable SET paid_amount = 0"); err != nil {
		t.Fatal(err)
	}
	expect(t, c.actPur(deliveries, &sr, "unpost"), http.StatusOK, "")
	expect(t, c.actPur(deliveries, &sr, "void"), http.StatusOK, "")
	expect(t, c.actPur(deliveries, &dn, "unpost"), http.StatusOK, "")
	if b := e.balance(s.item, s.wh); b != "120" {
		t.Fatalf("全部沖銷後現有量 = %s", b)
	}
}

func TestCreditLimit(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	s.cust = e.seedCustomer("C2", "5000", nil)

	// 3 箱 @1000 含稅 3150;再一張 2 箱 2100 → 3150 + 2100 > 5000
	s.newOrder("3")
	so, res := c.createPur(salesOrders, s.header(map[string]any{
		"doc_type": "order", "lines": []map[string]any{line(s.item, s.box, "2", "1000", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	expect(t, c.actPur(salesOrders, &so, "submit"), http.StatusOK, "")
	expect(t, c.actPur(salesOrders, &so, "approve"), http.StatusUnprocessableEntity, "SAL-003")

	// 1 箱 1050:3150 + 1050 = 4200 ≤ 5000
	ok, _ := c.createPur(salesOrders, s.header(map[string]any{
		"doc_type": "order", "lines": []map[string]any{line(s.item, s.box, "1", "1000", nil)},
	}))
	c.approvePur(salesOrders, &ok)

	// 調高額度後可核准:3150 + 1050 + 2100 = 6300 ≤ 6300
	if _, err := e.pool.Exec(context.Background(), "UPDATE customers SET credit_limit = 6300 WHERE id = $1", s.cust); err != nil {
		t.Fatal(err)
	}
	expect(t, c.actPur(salesOrders, &so, "approve"), http.StatusOK, "")
}

func TestInvoiceRegistration(t *testing.T) {
	s := newSalCtx(t)
	c := s.c
	so := s.newOrder("2")
	dn, _ := s.deliver(so, "1")
	d2, _ := s.deliver(so, "1")
	inv := func(d *purDoc, no, date string) apiResp {
		res := c.do(http.MethodPut, deliveries+"/"+itoa(d.ID)+"/invoice",
			map[string]any{"invoice_no": no, "invoice_date": date, "version": d.Version})
		if res.status == http.StatusOK {
			*d = decode[purDoc](t, res.Data)
		}
		return res
	}
	expect(t, inv(&dn, "AB1234", today), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, inv(&dn, "ab12345678", ""), http.StatusUnprocessableEntity, "SYS-422") // 缺日期
	expect(t, inv(&dn, "ab12345678", today), http.StatusOK, "")                      // 自動轉大寫
	expect(t, inv(&d2, "AB12345678", today), http.StatusConflict, "SAL-006")
	// 過帳後仍可登錄 / 修改
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")
	expect(t, inv(&dn, "AB12345679", today), http.StatusOK, "")
	// 作廢的單據不可登錄;作廢後號碼可再用
	expect(t, c.actPur(deliveries, &d2, "void"), http.StatusOK, "")
	expect(t, inv(&d2, "CD12345678", today), http.StatusConflict, "SAL-005")
	list := c.do(http.MethodGet, deliveries+"?no_invoice=true", nil)
	if n := len(decode[[]any](t, list.Data)); n != 1 { // 只剩作廢的 d2 沒有發票
		t.Fatalf("no_invoice = %d", n)
	}
}

func TestSalesDataScope(t *testing.T) {
	s := newSalCtx(t)
	e, root := s.e, s.c
	role := e.seedRole("SALES", "self", "sales.order.read", "sales.order.write", "sales.delivery.read",
		"sales.delivery.write", "finance.receivable.read")
	amy := e.seedUser("amy", pw, false, false, role.ID)
	ben := e.seedUser("ben", pw, false, false, role.ID)
	cAmy, cBen := e.seedCustomer("C-AMY", "0", &amy.ID), e.seedCustomer("C-BEN", "0", &ben.ID)
	ac := e.loggedIn("amy", pw)

	order := func(cl *client, cust int64) (purDoc, apiResp) {
		return cl.createPur(salesOrders, s.header(map[string]any{
			"doc_type": "order", "customer_id": cust, "lines": []map[string]any{line(s.item, s.box, "1", "100", nil)},
		}))
	}
	mine, res := order(ac, cAmy)
	expect(t, res, http.StatusCreated, "")
	_, res = order(ac, cBen) // 別人的客戶
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	theirs, _ := order(root, cBen)
	noRep, _ := order(root, s.cust) // 沒有負責業務:只有「全部」範圍看得到

	expect(t, ac.do(http.MethodGet, salesOrders+"/"+itoa(mine.ID), nil), http.StatusOK, "")
	expect(t, ac.do(http.MethodGet, salesOrders+"/"+itoa(theirs.ID), nil), http.StatusNotFound, "SYS-404")
	expect(t, ac.do(http.MethodGet, salesOrders+"/"+itoa(noRep.ID), nil), http.StatusNotFound, "SYS-404")
	expect(t, ac.actPur(salesOrders, &theirs, "submit"), http.StatusNotFound, "SYS-404")
	if n := len(decode[[]any](t, ac.do(http.MethodGet, salesOrders, nil).Data)); n != 1 {
		t.Fatalf("amy 看到 %d 張訂單", n)
	}
	opts := decode[[]struct {
		Code string `json:"code"`
	}](t, ac.do(http.MethodGet, "/masterdata/customer-options", nil).Data)
	if len(opts) != 1 || opts[0].Code != "C-AMY" {
		t.Fatalf("customer options = %+v", opts)
	}
	if n := len(decode[[]any](t, root.do(http.MethodGet, salesOrders, nil).Data)); n != 3 {
		t.Fatalf("root 看到 %d 張訂單", n)
	}
}
