package app

import (
	"context"
	"net/http"
	"testing"
)

type trialRow struct {
	Code         string `json:"code"`
	Opening      string `json:"opening"`
	PeriodDebit  string `json:"period_debit"`
	PeriodCredit string `json:"period_credit"`
	Closing      string `json:"closing"`
}

// trial 本期試算表,回傳 科目代號 → 列;並檢查借貸平衡。
func (c *client) trial() map[string]trialRow {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/gl/reports/trial-balance?from=2026-10-01&to=2026-10-31", nil)
	expect(c.e.t, res, http.StatusOK, "")
	out := decode[struct {
		Rows     []trialRow `json:"rows"`
		Balanced bool       `json:"balanced"`
	}](c.e.t, res.Data)
	if !out.Balanced {
		c.e.t.Fatalf("試算表不平衡: %+v", out)
	}
	m := map[string]trialRow{}
	for _, r := range out.Rows {
		m[r.Code] = r
	}
	return m
}

// want 檢查科目的本期借貸;沒有異動的科目 want 為 "0"。
func wantTrial(t *testing.T, m map[string]trialRow, code, debit, credit string) {
	t.Helper()
	r, ok := m[code]
	if !ok {
		r = trialRow{PeriodDebit: "0", PeriodCredit: "0"}
	}
	if r.PeriodDebit != debit || r.PeriodCredit != credit {
		t.Fatalf("科目 %s 借 %s 貸 %s,預期 借 %s 貸 %s", code, r.PeriodDebit, r.PeriodCredit, debit, credit)
	}
}

type voucherRow struct {
	ID         int64  `json:"id"`
	DocNo      string `json:"doc_no"`
	Status     string `json:"status"`
	SourceType string `json:"source_type"`
	ReversalOf *int64 `json:"reversal_of"`
	Reversed   bool   `json:"reversed"`
	Version    int32  `json:"version"`
}

func (c *client) vouchers(query string) []voucherRow {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/gl/vouchers?"+query, nil)
	expect(c.e.t, res, http.StatusOK, "")
	return decode[[]voucherRow](c.e.t, res.Data)
}

func manualVoucher(date string, lines ...map[string]any) map[string]any {
	return map[string]any{"voucher_date": date, "description": "測試傳票", "lines": lines}
}

func vl(account int64, debit, credit string) map[string]any {
	return map[string]any{"account_id": account, "debit": debit, "credit": credit}
}

// ---- 測試 ----

func TestPurchaseVouchersAndPayment(t *testing.T) {
	p := newPurCtx(t)
	c := p.c
	po := p.newOrder()
	gr, _ := p.receive(po, "4") // 未稅 4800、稅 240、含稅 5040
	c.approvePur(receipts, &gr)
	expect(t, c.actPur(receipts, &gr, "post"), http.StatusOK, "")
	m := c.trial()
	wantTrial(t, m, "1141", "4800", "0") // 存貨
	wantTrial(t, m, "1181", "240", "0")  // 進項稅額
	wantTrial(t, m, "2101", "0", "5040") // 應付帳款
	if vs := c.vouchers("source=auto"); len(vs) != 1 || vs[0].SourceType != "goods_receipt" || vs[0].Status != "posted" {
		t.Fatalf("vouchers = %+v", vs)
	}

	// 現金付款 2000:借 應付 貸 現金;匯款 → 銀行存款
	apList := decode[[]struct {
		ID int64 `json:"id"`
	}](t, c.do(http.MethodGet, "/finance/payables?open_only=true", nil).Data)
	pm, res := c.createSettle("/finance/payments", map[string]any{
		"doc_date": today, "partner_id": p.sup, "currency": "TWD", "method": "cash", "lines": []map[string]any{sl(apList[0].ID, "2000")},
	})
	expect(t, res, http.StatusCreated, "")
	c.approveSettle("/finance/payments", &pm)
	expect(t, c.actSettle("/finance/payments", &pm, "post"), http.StatusOK, "")
	m = c.trial()
	wantTrial(t, m, "2101", "2000", "5040")
	wantTrial(t, m, "1101", "0", "2000")

	// 付款單反過帳:沖銷傳票使借貸各增 2000(沖銷後淨額回到未付)
	expect(t, c.actSettle("/finance/payments", &pm, "unpost"), http.StatusOK, "")
	m = c.trial()
	wantTrial(t, m, "2101", "2000", "7040")
	wantTrial(t, m, "1101", "2000", "2000")
	rev := c.vouchers("source=auto")
	reversed := 0
	for _, v := range rev {
		if v.ReversalOf != nil {
			reversed++
		}
	}
	if reversed != 1 {
		t.Fatalf("沖銷傳票數 = %d, vouchers = %+v", reversed, rev)
	}

	// 退出 1 箱(1200 + 稅 60 = 1260):借 應付,貸 存貨 / 進項稅額
	rt, res := c.createPur(receipts, p.header(map[string]any{
		"doc_type": "return",
		"lines":    []map[string]any{line(p.item, p.box, "1", "1200", map[string]any{"receipt_line_id": gr.Lines[0].ID})},
	}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(receipts, &rt)
	expect(t, c.actPur(receipts, &rt, "post"), http.StatusOK, "")
	m = c.trial()
	wantTrial(t, m, "1141", "4800", "1200")
	wantTrial(t, m, "1181", "240", "60")
	wantTrial(t, m, "2101", "3260", "7040")

	// 應付子帳與總帳核對:應付帳款科目餘額(貸方)= 未沖應付合計
	ledger := c.do(http.MethodGet, "/gl/reports/ledger?account_id="+itoa(p.e.idByCode("accounts", "2101"))+"&from=2026-10-01&to=2026-10-31", nil)
	expect(t, ledger, http.StatusOK, "")
	lg := decode[struct {
		Closing string `json:"closing"`
	}](t, ledger.Data)
	if _, sum := c.payables(); lg.Closing != "-"+sum {
		t.Fatalf("總帳應付餘額 %s 與子帳 %s 不符", lg.Closing, sum)
	}
}

func TestSalesVouchersAndCollection(t *testing.T) {
	s := newSalCtx(t)
	c := s.c
	so := s.newOrder("4")
	dn, _ := s.deliver(so, "3") // 未稅 3000、稅 150、含稅 3150
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")
	m := c.trial()
	wantTrial(t, m, "1131", "3150", "0")
	wantTrial(t, m, "4101", "0", "3000")
	wantTrial(t, m, "2111", "0", "150")

	// 銷貨退回 1 箱:借 銷貨退回及折讓 / 銷項稅額,貸 應收
	sr, res := c.createPur(deliveries, s.header(map[string]any{
		"doc_type": "return",
		"lines":    []map[string]any{line(s.item, s.box, "1", "1000", map[string]any{"delivery_line_id": dn.Lines[0].ID})},
	}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(deliveries, &sr)
	expect(t, c.actPur(deliveries, &sr, "post"), http.StatusOK, "")
	m = c.trial()
	wantTrial(t, m, "4102", "1000", "0")
	wantTrial(t, m, "2111", "50", "150")
	wantTrial(t, m, "1131", "3150", "1050")

	// 匯款收款 1000:借 銀行存款,貸 應收
	ids := c.arIDs()
	rc, res := c.createSettle("/finance/collections", settleBody(s.cust, sl(ids[0], "1000")))
	expect(t, res, http.StatusCreated, "")
	c.approveSettle("/finance/collections", &rc)
	expect(t, c.actSettle("/finance/collections", &rc, "post"), http.StatusOK, "")
	m = c.trial()
	wantTrial(t, m, "1102", "1000", "0")
	wantTrial(t, m, "1131", "3150", "2050")

	// 應收子帳與總帳核對
	lg := decode[struct {
		Closing string `json:"closing"`
	}](t, c.do(http.MethodGet, "/gl/reports/ledger?account_id="+itoa(s.e.idByCode("accounts", "1131"))+"&from=2026-10-01&to=2026-10-31", nil).Data)
	if _, sum := c.receivables(); lg.Closing != sum || sum != "1100" {
		t.Fatalf("總帳應收餘額 %s 與子帳 %s 不符", lg.Closing, sum)
	}

	// 反過帳出貨單前須先處理退回與收款;退回反過帳 → 沖銷傳票,試算表仍平衡
	expect(t, c.actSettle("/finance/collections", &rc, "unpost"), http.StatusOK, "")
	expect(t, c.actPur(deliveries, &sr, "unpost"), http.StatusOK, "")
	c.trial()
	// 日記帳:只列已過帳傳票的分錄,依日期與傳票號排序
	j := c.do(http.MethodGet, "/gl/reports/journal?from=2026-10-01&to=2026-10-31", nil)
	expect(t, j, http.StatusOK, "")
	if n := len(decode[[]struct {
		DocNo string `json:"doc_no"`
	}](t, j.Data)); n < 10 {
		t.Fatalf("journal lines = %d", n)
	}
}

func TestManualVoucherLifecycle(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	cash, rent, ar := e.idByCode("accounts", "1101"), e.idByCode("accounts", "6102"), e.idByCode("accounts", "1131")

	// 驗證:少於兩行、兩方都填、金額小數、彙總科目、不存在的科目
	bad := func(body map[string]any) {
		t.Helper()
		_, res := c.createVoucherAPI(body)
		expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	}
	bad(manualVoucher(today, vl(cash, "100", "0")))
	bad(manualVoucher(today, vl(cash, "100", "100"), vl(rent, "0", "100")))
	bad(manualVoucher(today, vl(cash, "100.5", "0"), vl(rent, "0", "100.5")))
	bad(manualVoucher(today, vl(cash, "100", "0"), vl(999999, "0", "100")))
	// 輔助核算:客戶、部門需存在
	badAux := manualVoucher(today, vl(ar, "100", "0"), vl(cash, "0", "100"))
	badAux["lines"].([]map[string]any)[0]["customer_id"] = 999999
	bad(badAux)

	// 草稿允許不平衡;過帳要平衡
	v, res := c.createVoucherAPI(manualVoucher(today, vl(rent, "1000", "0"), vl(cash, "0", "900")))
	expect(t, res, http.StatusCreated, "")
	if v.DocNo != "JV202610070001" || v.Status != "draft" {
		t.Fatalf("voucher = %+v", v)
	}
	expect(t, c.actVoucher(&v, "post"), http.StatusUnprocessableEntity, "GL-004")
	res = c.do(http.MethodPut, "/gl/vouchers/"+itoa(v.ID), map[string]any{
		"voucher_date": today, "description": "房租", "version": v.Version,
		"lines": []map[string]any{vl(rent, "1000", "0"), vl(cash, "0", "1000")},
	})
	expect(t, res, http.StatusOK, "")
	v = decode[voucherRow](t, res.Data)
	expect(t, c.actVoucher(&v, "post"), http.StatusOK, "")
	wantTrial(t, c.trial(), "6102", "1000", "0")

	// 已過帳不可修改;資料庫也擋(trigger)
	expect(t, c.do(http.MethodPut, "/gl/vouchers/"+itoa(v.ID), map[string]any{
		"voucher_date": today, "version": v.Version, "lines": []map[string]any{vl(rent, "1", "0"), vl(cash, "0", "1")},
	}), http.StatusConflict, "GL-006")
	if _, err := e.pool.Exec(context.Background(), "UPDATE voucher_lines SET debit = 5 WHERE voucher_id = $1 AND debit > 0", v.ID); err == nil {
		t.Fatal("資料庫應拒絕修改已過帳傳票的分錄")
	}
	if _, err := e.pool.Exec(context.Background(), "DELETE FROM vouchers WHERE id = $1", v.ID); err == nil {
		t.Fatal("資料庫應拒絕刪除傳票")
	}

	// 沖銷:產生借貸相反的沖銷傳票;不可重複沖銷;沖銷傳票不可再沖銷
	expect(t, c.actVoucher(&v, "reverse"), http.StatusOK, "")
	wantTrial(t, c.trial(), "6102", "1000", "1000")
	expect(t, c.actVoucher(&v, "reverse"), http.StatusConflict, "GL-007")
	vs := c.vouchers("")
	for _, x := range vs {
		if x.ReversalOf != nil {
			expect(t, c.actVoucher(&x, "reverse"), http.StatusConflict, "GL-008")
		}
	}

	// 草稿可作廢,作廢後不影響報表
	d, _ := c.createVoucherAPI(manualVoucher(today, vl(rent, "50", "0"), vl(cash, "0", "50")))
	expect(t, c.actVoucher(&d, "void"), http.StatusOK, "")
	wantTrial(t, c.trial(), "6102", "1000", "1000")
}

func TestPeriodClosing(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	cash, rent := e.idByCode("accounts", "1101"), e.idByCode("accounts", "6102")

	// 有草稿傳票不可關帳
	draft, res := c.createVoucherAPI(manualVoucher(today, vl(rent, "10", "0"), vl(cash, "0", "10")))
	expect(t, res, http.StatusCreated, "")
	expect(t, c.do(http.MethodPost, "/gl/periods/2026-10/close", nil), http.StatusConflict, "GL-023")
	expect(t, c.actVoucher(&draft, "void"), http.StatusOK, "")
	expect(t, c.do(http.MethodPost, "/gl/periods/2027-12/close", nil), http.StatusUnprocessableEntity, "GL-022") // 未來
	expect(t, c.do(http.MethodPost, "/gl/periods/2026-13/close", nil), http.StatusNotFound, "SYS-404")

	// 先備好一張已核准的出貨單與庫存調整草稿,再關帳
	so := s.newOrder("2")
	dn, _ := s.deliver(so, "1")
	c.approvePur(deliveries, &dn)
	adj, _ := c.createDoc(adjust(s.wh, s.item, s.e.unitID("PCS"), "5"))
	for _, a := range []string{"submit", "approve"} {
		expect(t, c.act(&adj, a), http.StatusOK, "")
	}
	expect(t, c.do(http.MethodPost, "/gl/periods/2026-10/close", nil), http.StatusOK, "")
	expect(t, c.do(http.MethodPost, "/gl/periods/2026-10/close", nil), http.StatusConflict, "GL-020")

	// 關帳後:過帳、反過帳、新增傳票都被擋(含不產生傳票的庫存調整單)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusConflict, "GL-001")
	expect(t, c.act(&adj, "post"), http.StatusConflict, "GL-001")
	_, res = c.createVoucherAPI(manualVoucher(today, vl(rent, "10", "0"), vl(cash, "0", "10")))
	expect(t, res, http.StatusConflict, "GL-001")
	// 其他期間不受影響
	other, res := c.createVoucherAPI(manualVoucher("2026-09-15", vl(rent, "10", "0"), vl(cash, "0", "10")))
	expect(t, res, http.StatusCreated, "")
	expect(t, c.actVoucher(&other, "post"), http.StatusOK, "")

	// 重開後恢復
	expect(t, c.do(http.MethodPost, "/gl/periods/2026-10/reopen", nil), http.StatusOK, "")
	expect(t, c.do(http.MethodPost, "/gl/periods/2026-10/reopen", nil), http.StatusConflict, "GL-021")
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")
	expect(t, c.act(&adj, "post"), http.StatusOK, "")
}

func TestAccountsMappingsAndPermissions(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	clerkRole := e.seedRole("ACCT", "all", "gl.voucher.read", "gl.voucher.write")
	bossRole := e.seedRole("ACCTBOSS", "all", "gl.voucher.read", "gl.voucher.post", "gl.account.read", "gl.account.write", "gl.report.read")
	e.seedUser("clerk", pw, false, false, clerkRole.ID)
	e.seedUser("boss", pw, false, false, bossRole.ID)
	e.seedUser("nobody", pw, false, false)
	root, ck, bs, nb := e.loggedIn("root", pw), e.loggedIn("clerk", pw), e.loggedIn("boss", pw), e.loggedIn("nobody", pw)
	cash, rent := e.idByCode("accounts", "1101"), e.idByCode("accounts", "6102")

	// 開單者可建立、不可過帳;核准者可過帳、不可開單
	v, res := ck.createVoucherAPI(manualVoucher(today, vl(rent, "100", "0"), vl(cash, "0", "100")))
	expect(t, res, http.StatusCreated, "")
	expect(t, ck.actVoucher(&v, "post"), http.StatusForbidden, "SYS-403")
	_, res = bs.createVoucherAPI(manualVoucher(today, vl(rent, "1", "0"), vl(cash, "0", "1")))
	expect(t, res, http.StatusForbidden, "SYS-403")
	expect(t, bs.actVoucher(&v, "post"), http.StatusOK, "")
	expect(t, nb.do(http.MethodGet, "/gl/vouchers", nil), http.StatusForbidden, "SYS-403")
	expect(t, nb.do(http.MethodGet, "/gl/reports/trial-balance?from=2026-10-01&to=2026-10-31", nil), http.StatusForbidden, "SYS-403")
	expect(t, ck.do(http.MethodPost, "/gl/periods/2026-10/close", nil), http.StatusForbidden, "SYS-403")
	// 科目下拉:開傳票的人可用
	expect(t, ck.do(http.MethodGet, "/gl/account-options", nil), http.StatusOK, "")

	// 科目:彙總科目不可記帳;新增、代號格式與重複、下層類別一致
	grp, res := bs.do2(http.MethodPost, "/gl/accounts", map[string]any{"code": "1100", "name": "流動資產", "acct_type": "asset", "is_postable": false, "is_active": true})
	expect(t, res, http.StatusCreated, "")
	_, res = bs.do2(http.MethodPost, "/gl/accounts", map[string]any{"code": "1100", "name": "重複", "acct_type": "asset", "is_postable": true, "is_active": true})
	expect(t, res, http.StatusConflict, "GL-010")
	_, res = bs.do2(http.MethodPost, "/gl/accounts", map[string]any{"code": "ABC", "name": "壞代號", "acct_type": "asset", "is_postable": true, "is_active": true})
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	_, res = bs.do2(http.MethodPost, "/gl/accounts", map[string]any{"code": "1103", "name": "類別不符", "acct_type": "liability", "parent_id": grp.ID, "is_postable": true, "is_active": true})
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	child, res := bs.do2(http.MethodPost, "/gl/accounts", map[string]any{"code": "1103", "name": "零用金", "acct_type": "asset", "parent_id": grp.ID, "is_postable": true, "is_active": true})
	expect(t, res, http.StatusCreated, "")
	_, res = ck.createVoucherAPI(manualVoucher(today, vl(grp.ID, "5", "0"), vl(cash, "0", "5")))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422") // 彙總科目不可記帳
	_ = child

	// 已有分錄的科目不可改類別;被拋轉規則使用的科目不可停用
	acct := bs.getAccount(cash)
	acct["acct_type"] = "liability"
	expect(t, bs.putAccount(cash, acct), http.StatusConflict, "GL-011")
	acct = bs.getAccount(e.idByCode("accounts", "1141"))
	acct["is_active"] = false
	expect(t, bs.putAccount(e.idByCode("accounts", "1141"), acct), http.StatusConflict, "GL-011")

	// 拋轉規則:改指向另一個明細科目後,新的進貨傳票用新科目;彙總科目不可設定
	expect(t, root.do(http.MethodPut, "/gl/mappings/purchase.inventory", map[string]any{"account_id": grp.ID}), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, root.do(http.MethodPut, "/gl/mappings/nope", map[string]any{"account_id": cash}), http.StatusNotFound, "SYS-404")
	expect(t, root.do(http.MethodPut, "/gl/mappings/purchase.inventory", map[string]any{"account_id": child.ID}), http.StatusOK, "")
	pc := &purCtx{e: e, c: root, wh: e.seedWarehouse("A", false), sup: e.seedSupplier("S1", "TWD"), tax: e.idByCode("tax_types", "TX5"),
		item: e.seedItem("P1", "goods"), box: e.unitID("BOX")}
	po := pc.newOrder()
	gr, _ := pc.receive(po, "1")
	root.approvePur(receipts, &gr)
	expect(t, root.actPur(receipts, &gr, "post"), http.StatusOK, "")
	wantTrial(t, root.trial(), "1103", "1200", "0")
}

// ---- 輔助 ----

func (c *client) createVoucherAPI(body map[string]any) (voucherRow, apiResp) {
	c.e.t.Helper()
	res := c.do(http.MethodPost, "/gl/vouchers", body)
	if res.status != http.StatusCreated {
		return voucherRow{}, res
	}
	return decode[voucherRow](c.e.t, res.Data), res
}

func (c *client) actVoucher(v *voucherRow, action string) apiResp {
	c.e.t.Helper()
	res := c.do(http.MethodPost, "/gl/vouchers/"+itoa(v.ID)+"/actions/"+action, map[string]any{"version": v.Version})
	if res.status == http.StatusOK {
		*v = decode[voucherRow](c.e.t, res.Data)
	}
	return res
}

type acctRow struct {
	ID int64 `json:"id"`
}

func (c *client) do2(method, path string, body any) (acctRow, apiResp) {
	c.e.t.Helper()
	res := c.do(method, path, body)
	if res.status >= 300 {
		return acctRow{}, res
	}
	return decode[acctRow](c.e.t, res.Data), res
}

// getAccount 取得科目並轉成可直接 PUT 的欄位。
func (c *client) getAccount(id int64) map[string]any {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/gl/accounts", nil)
	expect(c.e.t, res, http.StatusOK, "")
	for _, a := range decode[[]map[string]any](c.e.t, res.Data) {
		if int64(a["id"].(float64)) == id {
			return map[string]any{
				"code": a["code"], "name": a["name"], "acct_type": a["acct_type"], "parent_id": a["parent_id"],
				"is_postable": a["is_postable"], "is_active": a["is_active"], "note": a["note"], "version": a["version"],
			}
		}
	}
	c.e.t.Fatalf("找不到科目 %d", id)
	return nil
}

func (c *client) putAccount(id int64, body map[string]any) apiResp {
	return c.do(http.MethodPut, "/gl/accounts/"+itoa(id), body)
}
