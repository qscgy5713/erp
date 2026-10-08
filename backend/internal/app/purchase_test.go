package app

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"erp/internal/db"
)

// ---- 測試資料 ----

func (e *env) idByCode(table, code string) int64 {
	e.t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(), "SELECT id FROM "+table+" WHERE code = $1", code).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) seedSupplier(code, currency string) int64 {
	e.t.Helper()
	tax, term := e.idByCode("tax_types", "TX5"), e.idByCode("payment_terms", "M30")
	s, err := e.q.CreateSupplier(context.Background(), db.CreateSupplierParams{
		CompanyID: 1, Code: code, Name: code + " 公司", Contacts: []byte("[]"), Addresses: []byte("[]"),
		Currency: currency, TaxTypeID: &tax, PaymentTermID: &term,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return s.ID
}

type purDoc struct {
	ID          int64  `json:"id"`
	DocNo       string `json:"doc_no"`
	Status      string `json:"status"`
	Version     int32  `json:"version"`
	TotalAmount string `json:"total_amount"`
	TaxAmount   string `json:"tax_amount"`
	BaseTotal   string `json:"base_total"`
	BaseTax     string `json:"base_tax"`
	Lines       []struct {
		ID           int64  `json:"id"`
		Qty          string `json:"qty"`
		Amount       string `json:"amount"`
		BaseAmount   string `json:"base_amount"`
		ReceivedQty  string `json:"received_qty"`
		RemainingQty string `json:"remaining_qty"`
	} `json:"lines"`
}

// purCtx 一組採購測試共用的資料。
type purCtx struct {
	e                       *env
	c                       *client
	wh, sup, tax, item, box int64
}

func newPurCtx(t *testing.T) *purCtx {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	return &purCtx{
		e: e, c: e.loggedIn("root", pw), wh: e.seedWarehouse("A", false), sup: e.seedSupplier("S1", "TWD"),
		tax: e.idByCode("tax_types", "TX5"), item: e.seedItem("P1", "goods"), box: e.unitID("BOX"),
	}
}

func (p *purCtx) header(extra map[string]any) map[string]any {
	h := map[string]any{
		"doc_date": today, "supplier_id": p.sup, "warehouse_id": p.wh, "currency": "TWD",
		"tax_type_id": p.tax, "payment_term_id": p.e.idByCode("payment_terms", "M30"),
	}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

func line(itemID, unitID int64, qty, price string, ref map[string]any) map[string]any {
	l := map[string]any{"item_id": itemID, "unit_id": unitID, "qty": qty, "unit_price": price}
	for k, v := range ref {
		l[k] = v
	}
	return l
}

func (c *client) createPur(path string, body map[string]any) (purDoc, apiResp) {
	c.e.t.Helper()
	res := c.do(http.MethodPost, path, body)
	if res.status != http.StatusCreated {
		return purDoc{}, res
	}
	return decode[purDoc](c.e.t, res.Data), res
}

func (c *client) actPur(path string, d *purDoc, action string) apiResp {
	c.e.t.Helper()
	res := c.do(http.MethodPost, path+"/"+itoa(d.ID)+"/actions/"+action, map[string]any{"version": d.Version})
	if res.status == http.StatusOK {
		*d = decode[purDoc](c.e.t, res.Data)
	}
	return res
}

func (c *client) approvePur(path string, d *purDoc) {
	c.e.t.Helper()
	for _, a := range []string{"submit", "approve"} {
		if res := c.actPur(path, d, a); res.status != http.StatusOK {
			c.e.t.Fatalf("%s 失敗: %d %+v", a, res.status, res.Error)
		}
	}
}

func (c *client) reload(path string, d *purDoc) {
	c.e.t.Helper()
	res := c.do(http.MethodGet, path+"/"+itoa(d.ID), nil)
	expect(c.e.t, res, http.StatusOK, "")
	*d = decode[purDoc](c.e.t, res.Data)
}

const (
	orders   = "/purchase/orders"
	receipts = "/purchase/receipts"
)

// newOrder 建立並核准採購單:P1 10 箱 @1200(未稅 12000,稅 600)。
func (p *purCtx) newOrder() purDoc {
	p.e.t.Helper()
	po, res := p.c.createPur(orders, p.header(map[string]any{
		"lines": []map[string]any{line(p.item, p.box, "10", "1200", nil)},
	}))
	expect(p.e.t, res, http.StatusCreated, "")
	p.c.approvePur(orders, &po)
	return po
}

func (p *purCtx) receive(po purDoc, qty string) (purDoc, apiResp) {
	p.e.t.Helper()
	return p.c.createPur(receipts, p.header(map[string]any{
		"doc_type": "receipt", "lines": []map[string]any{line(p.item, p.box, qty, "1200", map[string]any{"po_line_id": po.Lines[0].ID})},
	}))
}

type payableRow struct {
	SourceType string `json:"source_type"`
	SourceNo   string `json:"source_no"`
	DueDate    string `json:"due_date"`
	Amount     string `json:"amount"`
	BaseAmount string `json:"base_amount"`
}

func (c *client) payables() ([]payableRow, string) {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/finance/payables?open_only=true", nil)
	expect(c.e.t, res, http.StatusOK, "")
	meta := decode[struct {
		BaseAmountSum string `json:"base_amount_sum"`
	}](c.e.t, res.Meta)
	return decode[[]payableRow](c.e.t, res.Data), meta.BaseAmountSum
}

// ---- 測試 ----

func TestPurchaseOrderToPartialReceiptsAndPayable(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c

	po, res := c.createPur(orders, p.header(map[string]any{
		"lines": []map[string]any{line(p.item, p.box, "10", "1200", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	if po.DocNo != "PO202610070001" || po.TaxAmount != "600" || po.TotalAmount != "12600" {
		t.Fatalf("po = %+v", po)
	}
	// 草稿採購單不可轉進貨
	_, res = p.receive(po, "1")
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	// 採購單沒有過帳
	expect(t, c.actPur(orders, &po, "post"), http.StatusNotFound, "SYS-404")
	c.approvePur(orders, &po)

	// 部分進貨 4 箱
	gr, res := p.receive(po, "4")
	expect(t, res, http.StatusCreated, "")
	if gr.DocNo != "GR202610070001" || gr.TotalAmount != "5040" {
		t.Fatalf("gr = %+v", gr)
	}
	c.approvePur(receipts, &gr)
	expect(t, c.actPur(receipts, &gr, "post"), http.StatusOK, "")
	if b := e.balance(p.item, p.wh); b != "48" {
		t.Fatalf("進貨後現有量 = %s", b)
	}
	// 入庫成本 = 本位幣金額 / 基本單位數量 = 4800 / 48
	var cost decimal.Decimal
	if err := e.pool.QueryRow(context.Background(),
		"SELECT unit_cost FROM inventory_transactions WHERE source_type = 'goods_receipt' AND source_id = $1", gr.ID).Scan(&cost); err != nil || !cost.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("unit_cost = %s, err = %v", cost, err)
	}
	// 應付:含稅 5040,月結 30 天 → 10/31 + 30 = 11/30
	ap, sum := c.payables()
	if len(ap) != 1 || ap[0].Amount != "5040" || ap[0].DueDate != "2026-11-30" || sum != "5040" {
		t.Fatalf("payables = %+v sum=%s", ap, sum)
	}
	c.reload(orders, &po)
	if po.Lines[0].ReceivedQty != "4" || po.Lines[0].RemainingQty != "6" {
		t.Fatalf("po line = %+v", po.Lines[0])
	}

	// 超過未交量:開單時即擋
	_, res = p.receive(po, "7")
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	if res.Error.Details["lines.0"] == "" {
		t.Fatalf("details = %+v", res.Error.Details)
	}

	// 兩張草稿各收 6 箱(開單時都合法),第二張過帳時應被擋
	g2, _ := p.receive(po, "6")
	g3, _ := p.receive(po, "6")
	c.approvePur(receipts, &g2)
	c.approvePur(receipts, &g3)
	expect(t, c.actPur(receipts, &g2, "post"), http.StatusOK, "")
	expect(t, c.actPur(receipts, &g3, "post"), http.StatusUnprocessableEntity, "PUR-004")
	expect(t, c.actPur(receipts, &g3, "void"), http.StatusOK, "")

	// 已有進貨單:不可取消核准 / 作廢,只能結案
	expect(t, c.actPur(orders, &po, "unapprove"), http.StatusConflict, "PUR-002")
	expect(t, c.actPur(orders, &po, "close"), http.StatusOK, "")
	expect(t, c.actPur(orders, &po, "reopen"), http.StatusOK, "")
	if po.Status != "approved" {
		t.Fatalf("重開後 status = %s", po.Status)
	}
	list := c.do(http.MethodGet, orders, nil)
	rows := decode[[]struct {
		ReceiptState string `json:"receipt_state"`
	}](t, list.Data)
	if len(rows) != 1 || rows[0].ReceiptState != "full" {
		t.Fatalf("orders = %+v", rows)
	}
	// 已交齊:不在未交貨清單
	out := c.do(http.MethodGet, "/purchase/outstanding-lines", nil)
	if n := len(decode[[]any](t, out.Data)); n != 0 {
		t.Fatalf("outstanding = %d", n)
	}
}

func TestReceiptAgainstClosedOrderAndUnpost(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	po := p.newOrder()
	gr, _ := p.receive(po, "3")
	c.approvePur(receipts, &gr)

	// 採購單結案後,尚未過帳的進貨單不可過帳
	expect(t, c.actPur(orders, &po, "close"), http.StatusOK, "")
	expect(t, c.actPur(receipts, &gr, "post"), http.StatusUnprocessableEntity, "PUR-004")
	_, res := p.receive(po, "1")
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	expect(t, c.actPur(orders, &po, "reopen"), http.StatusOK, "")
	expect(t, c.actPur(receipts, &gr, "post"), http.StatusOK, "")

	// 反過帳:沖銷庫存、移除應付,未交量恢復
	expect(t, c.actPur(receipts, &gr, "unpost"), http.StatusOK, "")
	if b := e.balance(p.item, p.wh); b != "0" {
		t.Fatalf("反過帳後現有量 = %s", b)
	}
	if ap, _ := c.payables(); len(ap) != 0 {
		t.Fatalf("反過帳後應付 = %+v", ap)
	}
	c.reload(orders, &po)
	if po.Lines[0].RemainingQty != "10" {
		t.Fatalf("remaining = %s", po.Lines[0].RemainingQty)
	}

	// 應付已沖帳則不可反過帳
	expect(t, c.actPur(receipts, &gr, "post"), http.StatusOK, "")
	if _, err := e.pool.Exec(context.Background(), "UPDATE accounts_payable SET paid_amount = 100"); err != nil {
		t.Fatal(err)
	}
	expect(t, c.actPur(receipts, &gr, "unpost"), http.StatusConflict, "FIN-001")
}

func TestPurchaseReturn(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	po := p.newOrder()
	gr, _ := p.receive(po, "4")
	c.approvePur(receipts, &gr)
	expect(t, c.actPur(receipts, &gr, "post"), http.StatusOK, "")

	ret := func(qty string) (purDoc, apiResp) {
		return c.createPur(receipts, p.header(map[string]any{
			"doc_type": "return",
			"lines":    []map[string]any{line(p.item, p.box, qty, "1200", map[string]any{"receipt_line_id": gr.Lines[0].ID})},
		}))
	}
	_, res := ret("5") // 超過進貨量
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	rt, res := ret("1")
	expect(t, res, http.StatusCreated, "")
	if rt.DocNo != "PT202610070001" {
		t.Fatalf("return = %+v", rt)
	}
	c.approvePur(receipts, &rt)
	expect(t, c.actPur(receipts, &rt, "post"), http.StatusOK, "")
	if b := e.balance(p.item, p.wh); b != "36" {
		t.Fatalf("退出後現有量 = %s", b)
	}
	ap, sum := c.payables()
	if len(ap) != 2 || sum != "3780" { // 5040 − 1260
		t.Fatalf("payables = %+v sum=%s", ap, sum)
	}
	// 可退量剩 3 箱;退出不回補採購單未交量(D35)
	_, res = ret("4")
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	c.reload(orders, &po)
	if po.Lines[0].RemainingQty != "6" {
		t.Fatalf("remaining = %s", po.Lines[0].RemainingQty)
	}

	// 進貨單已有退出單:不可反過帳
	expect(t, c.actPur(receipts, &gr, "unpost"), http.StatusConflict, "PUR-005")
	expect(t, c.actPur(receipts, &rt, "unpost"), http.StatusOK, "")
	expect(t, c.actPur(receipts, &rt, "void"), http.StatusOK, "")
	expect(t, c.actPur(receipts, &gr, "unpost"), http.StatusOK, "")
	if b := e.balance(p.item, p.wh); b != "0" {
		t.Fatalf("全部沖銷後現有量 = %s", b)
	}
}

func TestForeignCurrencyReceiptAndServiceLines(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	usd := e.seedSupplier("S2", "USD")
	if _, err := e.q.CreateExchangeRate(context.Background(), db.CreateExchangeRateParams{
		CompanyID: 1, Currency: "USD", RateDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		Rate: decimal.RequireFromString("30.5"),
	}); err != nil {
		t.Fatal(err)
	}
	fee := e.seedItem("FEE", "service")
	pcs := e.unitID("PCS")

	// 不經採購單直接進貨;未指定匯率時取單據日期(或之前最近)的匯率
	gr, res := c.createPur(receipts, p.header(map[string]any{
		"doc_type": "receipt", "supplier_id": usd, "currency": "USD",
		"lines": []map[string]any{line(p.item, pcs, "3", "1.25", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	// 原幣 3.75,稅 0.1875 → 0.19;本位幣未稅 114.375 → 114,稅 0.19 × 30.5 = 5.795 → 6
	if gr.TotalAmount != "3.94" || gr.Lines[0].BaseAmount != "114" || gr.BaseTax != "6" || gr.BaseTotal != "120" {
		t.Fatalf("gr = %+v", gr)
	}
	// 幣別不符的採購單不可引用
	po := p.newOrder()
	_, res = c.createPur(receipts, p.header(map[string]any{
		"doc_type": "receipt", "supplier_id": usd, "currency": "USD",
		"lines": []map[string]any{line(p.item, p.box, "1", "1", map[string]any{"po_line_id": po.Lines[0].ID})},
	}))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")

	// 只有服務類明細:沒有庫存異動,但仍產生應付;反過帳也不會因為沒有庫存分錄而失敗
	svc, res := c.createPur(receipts, p.header(map[string]any{
		"doc_type": "receipt", "lines": []map[string]any{line(fee, pcs, "1", "500", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(receipts, &svc)
	expect(t, c.actPur(receipts, &svc, "post"), http.StatusOK, "")
	if ap, _ := c.payables(); len(ap) != 1 || ap[0].Amount != "525" {
		t.Fatalf("payables = %+v", ap)
	}
	expect(t, c.actPur(receipts, &svc, "unpost"), http.StatusOK, "")
}

func TestPurchasePermissions(t *testing.T) {
	p := newPurCtx(t)
	e := p.e
	clerk := e.seedRole("BUYER", "all", "purchase.order.write", "purchase.receipt.write")
	boss := e.seedRole("BOSS", "all", "purchase.order.approve", "purchase.receipt.approve")
	e.seedUser("clerk", pw, false, false, clerk.ID)
	e.seedUser("boss", pw, false, false, boss.ID)
	e.seedUser("nobody", pw, false, false)
	ck, bs, nb := e.loggedIn("clerk", pw), e.loggedIn("boss", pw), e.loggedIn("nobody", pw)

	po, res := ck.createPur(orders, p.header(map[string]any{
		"lines": []map[string]any{line(p.item, p.box, "1", "10", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	expect(t, ck.actPur(orders, &po, "submit"), http.StatusOK, "")
	expect(t, ck.actPur(orders, &po, "approve"), http.StatusForbidden, "SYS-403")
	expect(t, bs.actPur(orders, &po, "approve"), http.StatusOK, "")
	expect(t, bs.actPur(orders, &po, "close"), http.StatusOK, "")
	expect(t, ck.actPur(orders, &po, "reopen"), http.StatusForbidden, "SYS-403")
	expect(t, bs.actPur(orders, &po, "reopen"), http.StatusOK, "")

	gr, res := p.receive(po, "1")
	expect(t, res, http.StatusCreated, "")
	p.c.approvePur(receipts, &gr)
	expect(t, bs.actPur(receipts, &gr, "post"), http.StatusForbidden, "SYS-403") // 核准者不可過帳

	expect(t, nb.do(http.MethodGet, orders, nil), http.StatusForbidden, "SYS-403")
	expect(t, nb.do(http.MethodGet, "/finance/payables", nil), http.StatusForbidden, "SYS-403")
	// 下拉選單只需登入
	expect(t, nb.do(http.MethodGet, "/masterdata/supplier-options?keyword=S1", nil), http.StatusOK, "")
}
