package app

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type settleDoc struct {
	ID      int64  `json:"id"`
	DocNo   string `json:"doc_no"`
	Status  string `json:"status"`
	Version int32  `json:"version"`
	Amount  string `json:"amount"`
	Lines   []struct {
		TargetID int64  `json:"target_id"`
		Balance  string `json:"balance"`
		Amount   string `json:"amount"`
	} `json:"lines"`
}

func (c *client) createSettle(path string, body map[string]any) (settleDoc, apiResp) {
	c.e.t.Helper()
	res := c.do(http.MethodPost, path, body)
	if res.status != http.StatusCreated {
		return settleDoc{}, res
	}
	return decode[settleDoc](c.e.t, res.Data), res
}

func (c *client) actSettle(path string, d *settleDoc, action string) apiResp {
	c.e.t.Helper()
	res := c.do(http.MethodPost, path+"/"+itoa(d.ID)+"/actions/"+action, map[string]any{"version": d.Version})
	if res.status == http.StatusOK {
		*d = decode[settleDoc](c.e.t, res.Data)
	}
	return res
}

func (c *client) approveSettle(path string, d *settleDoc) {
	c.e.t.Helper()
	for _, a := range []string{"submit", "approve"} {
		if res := c.actSettle(path, d, a); res.status != http.StatusOK {
			c.e.t.Fatalf("%s 失敗: %d %+v", a, res.status, res.Error)
		}
	}
}

func settleBody(partner int64, lines ...map[string]any) map[string]any {
	return map[string]any{
		"doc_date": today, "partner_id": partner, "currency": "TWD", "method": "transfer", "lines": lines,
	}
}

func sl(target int64, amount string) map[string]any {
	return map[string]any{"target_id": target, "amount": amount}
}

// arIDs 目前所有應收(依到期日)的 id。
func (c *client) arIDs() []int64 {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/finance/receivables?open_only=true", nil)
	expect(c.e.t, res, http.StatusOK, "")
	rows := decode[[]struct {
		ID int64 `json:"id"`
	}](c.e.t, res.Data)
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

// twoDeliveries 開兩張出貨單並過帳:DN1 含稅 3150(3 箱)、DN2 含稅 1050(1 箱);再開一張退回 1 箱 −1050 掛在 DN1。
func (s *salCtx) twoDeliveries() {
	s.e.t.Helper()
	c := s.c
	so := s.newOrder("6")
	for _, qty := range []string{"3", "1"} {
		d, res := s.deliver(so, qty)
		expect(s.e.t, res, http.StatusCreated, "")
		c.approvePur(deliveries, &d)
		expect(s.e.t, c.actPur(deliveries, &d, "post"), http.StatusOK, "")
	}
}

func TestCollectionSettlesReceivables(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	s.twoDeliveries()
	ids := c.arIDs()
	if len(ids) != 2 {
		t.Fatalf("應收 %v", ids)
	}
	const path = "/finance/collections"

	// 驗證:超過餘額、金額 0、幣別 / 對象不符、重複
	_, res := c.createSettle(path, settleBody(s.cust, sl(ids[0], "3151")))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	_, res = c.createSettle(path, settleBody(s.cust, sl(ids[0], "0")))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	_, res = c.createSettle(path, settleBody(s.cust, sl(ids[0], "100"), sl(ids[0], "100")))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	other := e.seedCustomer("C9", "0", nil)
	_, res = c.createSettle(path, settleBody(other, sl(ids[0], "100")))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	_, res = c.createSettle(path, settleBody(s.cust))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")

	// 一筆收款沖多張、其中一張只沖部分:3150 全沖 + 1050 沖 500
	rc, res := c.createSettle(path, settleBody(s.cust, sl(ids[0], "3150"), sl(ids[1], "500")))
	expect(t, res, http.StatusCreated, "")
	if rc.DocNo != "RC202610070001" || rc.Amount != "3650" {
		t.Fatalf("receipt = %+v", rc)
	}
	c.approveSettle(path, &rc)
	expect(t, c.actSettle(path, &rc, "post"), http.StatusOK, "")
	ar, sum := c.receivables()
	if len(ar) != 1 || sum != "550" { // 只剩第二張的 550
		t.Fatalf("沖帳後 open receivables = %+v sum=%s", ar, sum)
	}
	// 另一張草稿收款單同時要沖 600(超過剩餘 550):開單當下已擋;先開 550 的兩張,第二張過帳時被擋
	d1, res := c.createSettle(path, settleBody(s.cust, sl(ids[1], "550")))
	expect(t, res, http.StatusCreated, "")
	d2, res := c.createSettle(path, settleBody(s.cust, sl(ids[1], "550")))
	expect(t, res, http.StatusCreated, "")
	c.approveSettle(path, &d1)
	c.approveSettle(path, &d2)
	expect(t, c.actSettle(path, &d1, "post"), http.StatusOK, "")
	expect(t, c.actSettle(path, &d2, "post"), http.StatusUnprocessableEntity, "FIN-004")
	expect(t, c.actSettle(path, &d2, "void"), http.StatusOK, "")
	if ar, _ := c.receivables(); len(ar) != 0 {
		t.Fatalf("全沖清後 open = %+v", ar)
	}

	// 已被收款單沖過的應收,來源出貨單不可反過帳
	dns := decode[[]struct {
		ID     int64  `json:"id"`
		DocNo  string `json:"doc_no"`
		Status string `json:"status"`
	}](t, c.do(http.MethodGet, deliveries, nil).Data)
	dn := purDoc{ID: dns[0].ID}
	c.reload(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "unpost"), http.StatusConflict, "FIN-002")

	// 反過帳收款單:餘額恢復
	expect(t, c.actSettle(path, &d1, "unpost"), http.StatusOK, "")
	if _, sum := c.receivables(); sum != "550" {
		t.Fatalf("反過帳後 sum = %s", sum)
	}
	expect(t, c.actSettle(path, &rc, "unpost"), http.StatusOK, "")
	if _, sum := c.receivables(); sum != "4200" {
		t.Fatalf("全部反過帳後 sum = %s", sum)
	}
	// 草稿 / 核准中的收款單引用應收時,來源出貨單也不可反過帳
	pending, _ := c.createSettle(path, settleBody(s.cust, sl(ids[0], "100")))
	expect(t, c.actPur(deliveries, &dn, "unpost"), http.StatusConflict, "FIN-002")
	expect(t, c.actSettle(path, &pending, "void"), http.StatusOK, "")
	_ = e
}

func TestCreditNoteOffsetsInvoice(t *testing.T) {
	s := newSalCtx(t)
	c := s.c
	so := s.newOrder("4")
	dn, _ := s.deliver(so, "4") // 應收 4200
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")
	sr, res := c.createPur(deliveries, s.header(map[string]any{
		"doc_type": "return",
		"lines":    []map[string]any{line(s.item, s.box, "1", "1000", map[string]any{"delivery_line_id": dn.Lines[0].ID})},
	}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(deliveries, &sr)
	expect(t, c.actPur(deliveries, &sr, "post"), http.StatusOK, "") // 應收 −1050
	ids := c.arIDs()                                                // 依到期日 / id:先出貨後退回
	const path = "/finance/collections"

	// 正負必須同號:對負數的應收開正數會被擋
	_, res = c.createSettle(path, settleBody(s.cust, sl(ids[1], "1050")))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	// 收款 3150:沖出貨 4200 全額 + 退回 −1050 = 3150
	rc, res := c.createSettle(path, settleBody(s.cust, sl(ids[0], "4200"), sl(ids[1], "-1050")))
	expect(t, res, http.StatusCreated, "")
	if rc.Amount != "3150" {
		t.Fatalf("amount = %s", rc.Amount)
	}
	c.approveSettle(path, &rc)
	expect(t, c.actSettle(path, &rc, "post"), http.StatusOK, "")
	if ar, _ := c.receivables(); len(ar) != 0 {
		t.Fatalf("互抵後 open = %+v", ar)
	}
	// 合計為負數不允許
	_, res = c.createSettle(path, settleBody(s.cust, sl(ids[1], "-1050")))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
}

func TestPaymentSettlesPayables(t *testing.T) {
	p := newPurCtx(t)
	c := p.c
	po := p.newOrder()
	gr, _ := p.receive(po, "4") // 5040
	c.approvePur(receipts, &gr)
	expect(t, c.actPur(receipts, &gr, "post"), http.StatusOK, "")
	const path = "/finance/payments"
	res := c.do(http.MethodGet, "/finance/payables", nil)
	ap := decode[[]struct {
		ID int64 `json:"id"`
	}](t, res.Data)

	pm, r := c.createSettle(path, settleBody(p.sup, sl(ap[0].ID, "2000")))
	expect(t, r, http.StatusCreated, "")
	if pm.DocNo != "PM202610070001" {
		t.Fatalf("payment = %+v", pm)
	}
	c.approveSettle(path, &pm)
	expect(t, c.actSettle(path, &pm, "post"), http.StatusOK, "")
	rows, sum := c.payables()
	if len(rows) != 1 || sum != "3040" { // 5040 − 已付 2000
		t.Fatalf("payables = %+v sum=%s", rows, sum)
	}
	// 進貨單已被付款單沖過,不可反過帳
	expect(t, c.actPur(receipts, &gr, "unpost"), http.StatusConflict, "FIN-001")
	// 收款單端點不可存取付款單(邊別隔離)
	expect(t, c.do(http.MethodGet, "/finance/collections/"+itoa(pm.ID), nil), http.StatusNotFound, "SYS-404")
	expect(t, c.actSettle(path, &pm, "unpost"), http.StatusOK, "")
	// 付款單只是反過帳(仍是核准狀態、引用著這筆應付):進貨單仍不可反過帳,作廢付款單後才可
	expect(t, c.actPur(receipts, &gr, "unpost"), http.StatusConflict, "FIN-001")
	expect(t, c.actSettle(path, &pm, "void"), http.StatusOK, "")
	expect(t, c.actPur(receipts, &gr, "unpost"), http.StatusOK, "")
}

func TestStatementAndAging(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	s.twoDeliveries() // 3150 + 1050,到期日 2026-11-30
	ids := c.arIDs()
	rc, _ := c.createSettle("/finance/collections", settleBody(s.cust, sl(ids[0], "1000")))
	c.approveSettle("/finance/collections", &rc)
	expect(t, c.actSettle("/finance/collections", &rc, "post"), http.StatusOK, "")

	stmt := c.do(http.MethodGet, "/finance/statements?side=receivable&partner_id="+itoa(s.cust)+"&currency=TWD&from=2026-10-01&to=2026-10-31", nil)
	expect(t, stmt, http.StatusOK, "")
	st := decode[struct {
		Opening string `json:"opening"`
		Closing string `json:"closing"`
		Rows    []struct {
			Kind    string `json:"kind"`
			Delta   string `json:"delta"`
			Balance string `json:"balance"`
		} `json:"rows"`
	}](t, stmt.Data)
	if st.Opening != "0" || st.Closing != "3200" || len(st.Rows) != 3 || st.Rows[2].Kind != "receipt" || st.Rows[2].Delta != "-1000" {
		t.Fatalf("statement = %+v", st)
	}
	// 期初:查詢區間在單據日期之後 → 期初 3200、無明細
	late := c.do(http.MethodGet, "/finance/statements?side=receivable&partner_id="+itoa(s.cust)+"&currency=TWD&from=2026-11-01&to=2026-11-30", nil)
	lt := decode[struct {
		Opening string `json:"opening"`
		Closing string `json:"closing"`
		Rows    []any  `json:"rows"`
	}](t, late.Data)
	if lt.Opening != "3200" || lt.Closing != "3200" || len(lt.Rows) != 0 {
		t.Fatalf("late statement = %+v", lt)
	}

	// 帳齡:12/05 距到期日 11/30 逾期 5 天;11/20 尚未到期
	ag := func(asOf string) (notDue, d130, total string) {
		res := c.do(http.MethodGet, "/finance/aging?side=receivable&as_of="+asOf, nil)
		expect(t, res, http.StatusOK, "")
		out := decode[struct {
			Rows []struct {
				NotDue string `json:"not_due"`
				D130   string `json:"d1_30"`
				Total  string `json:"total"`
			} `json:"rows"`
		}](t, res.Data)
		if len(out.Rows) != 1 {
			t.Fatalf("aging rows = %+v", out.Rows)
		}
		return out.Rows[0].NotDue, out.Rows[0].D130, out.Rows[0].Total
	}
	if nd, d, tot := ag("2026-11-20"); nd != "3200" || d != "0" || tot != "3200" {
		t.Fatalf("11/20: %s %s %s", nd, d, tot)
	}
	if nd, d, tot := ag("2026-12-05"); nd != "0" || d != "3200" || tot != "3200" {
		t.Fatalf("12/05: %s %s %s", nd, d, tot)
	}
	expect(t, c.do(http.MethodGet, "/finance/aging?side=bogus", nil), http.StatusUnprocessableEntity, "SYS-422")
	_ = e
}

func TestSettlementPermissionsAndScope(t *testing.T) {
	s := newSalCtx(t)
	e, root := s.e, s.c
	role := e.seedRole("FIN", "self", "finance.collection.read", "finance.collection.write", "finance.receivable.read")
	boss := e.seedRole("FINBOSS", "all", "finance.collection.approve", "finance.collection.read")
	amy := e.seedUser("amy", pw, false, false, role.ID)
	e.seedUser("boss", pw, false, false, boss.ID)
	cAmy := e.seedCustomer("C-AMY", "0", &amy.ID)
	ac, bc := e.loggedIn("amy", pw), e.loggedIn("boss", pw)

	// 為 amy 的客戶製造一筆應收
	so, _ := root.createPur(salesOrders, s.header(map[string]any{
		"doc_type": "order", "customer_id": cAmy, "lines": []map[string]any{line(s.item, s.box, "1", "1000", nil)},
	}))
	root.approvePur(salesOrders, &so)
	dn, _ := root.createPur(deliveries, s.header(map[string]any{
		"doc_type": "delivery", "customer_id": cAmy,
		"lines": []map[string]any{line(s.item, s.box, "1", "1000", map[string]any{"so_line_id": so.Lines[0].ID})},
	}))
	root.approvePur(deliveries, &dn)
	expect(t, root.actPur(deliveries, &dn, "post"), http.StatusOK, "")
	ids := root.arIDs()

	const path = "/finance/collections"
	_, res := ac.createSettle(path, settleBody(s.cust, sl(ids[0], "1")))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422") // 非自己範圍的客戶
	rc, res := ac.createSettle(path, settleBody(cAmy, sl(ids[0], "1050")))
	expect(t, res, http.StatusCreated, "")
	expect(t, ac.actSettle(path, &rc, "submit"), http.StatusOK, "")
	expect(t, ac.actSettle(path, &rc, "approve"), http.StatusForbidden, "SYS-403") // 開單者不可核准
	expect(t, bc.actSettle(path, &rc, "approve"), http.StatusOK, "")
	expect(t, bc.actSettle(path, &rc, "post"), http.StatusForbidden, "SYS-403") // 核准者不可過帳
	// 付款單權限獨立
	_, res = ac.createSettle("/finance/payments", settleBody(s.cust, sl(1, "1")))
	expect(t, res, http.StatusForbidden, "SYS-403")
	// 對帳單 / 帳齡依資料範圍:amy 看不到其他客戶
	expect(t, ac.do(http.MethodGet, "/finance/statements?side=receivable&partner_id="+itoa(s.cust)+"&currency=TWD&from=2026-10-01&to=2026-10-31", nil), http.StatusNotFound, "SYS-404")
	expect(t, ac.do(http.MethodGet, "/finance/statements?side=payable&partner_id=1&currency=TWD&from=2026-10-01&to=2026-10-31", nil), http.StatusForbidden, "SYS-403")
	out := decode[struct {
		Rows []struct {
			PartnerCode string `json:"partner_code"`
		} `json:"rows"`
	}](t, ac.do(http.MethodGet, "/finance/aging?side=receivable", nil).Data)
	if len(out.Rows) != 1 || !strings.HasPrefix(out.Rows[0].PartnerCode, "C-AMY") {
		t.Fatalf("amy aging = %+v", out.Rows)
	}
	// 另一個業務的收款單看不到
	_ = context.Background()
}
