package app

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"erp/internal/db"
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

func (c *client) yearEnd(year int, action string) apiResp {
	return c.do(http.MethodPost, "/gl/year-end/"+itoa(int64(year))+"/"+action, nil)
}

type stmtLine struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Amount string `json:"amount"`
}

func (c *client) statement(path string) (lines []stmtLine, balanced *bool) {
	c.e.t.Helper()
	res := c.do(http.MethodGet, path, nil)
	expect(c.e.t, res, http.StatusOK, "")
	out := decode[struct {
		Lines    []stmtLine `json:"lines"`
		Balanced *bool      `json:"balanced"`
	}](c.e.t, res.Data)
	return out.Lines, out.Balanced
}

func stmtAmount(t *testing.T, lines []stmtLine, label string) string {
	t.Helper()
	for _, l := range lines {
		if l.Label == label {
			return l.Amount
		}
	}
	t.Fatalf("報表沒有「%s」", label)
	return ""
}

// 損益表、資產負債表與年度結帳:結帳前後損益表不變、資產負債表恆平衡、可撤銷、權限與年度檢查
func TestFinancialStatementsAndYearEnd(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	cash, sales, rent, capital := e.idByCode("accounts", "1101"), e.idByCode("accounts", "4101"), e.idByCode("accounts", "6102"), e.idByCode("accounts", "3101")
	post := func(date string, lines ...map[string]any) {
		t.Helper()
		v, res := c.createVoucherAPI(manualVoucher(date, lines...))
		expect(t, res, http.StatusCreated, "")
		expect(t, c.actVoucher(&v, "post"), http.StatusOK, "")
	}
	post("2025-01-02", vl(cash, "5000", "0"), vl(capital, "0", "5000")) // 股本
	post("2025-06-10", vl(cash, "3000", "0"), vl(sales, "0", "3000"))   // 收入
	post("2025-07-10", vl(rent, "1200", "0"), vl(cash, "0", "1200"))    // 費用
	post("2026-02-10", vl(cash, "500", "0"), vl(sales, "0", "500"))     // 次年收入

	income := func(from, to string) []stmtLine {
		l, _ := c.statement("/gl/reports/income-statement?from=" + from + "&to=" + to)
		return l
	}
	bs := func(asOf string) []stmtLine {
		l, ok := c.statement("/gl/reports/balance-sheet?as_of=" + asOf)
		if ok == nil || !*ok {
			t.Fatalf("資產負債表 %s 不平衡", asOf)
		}
		return l
	}
	if got := stmtAmount(t, income("2025-01-01", "2025-12-31"), "本期淨利(損)"); got != "1800" {
		t.Fatalf("2025 淨利 = %s", got)
	}
	if got := stmtAmount(t, bs("2025-12-31"), "權益總計"); got != "6800" {
		t.Fatalf("結帳前權益 = %s", got)
	}

	// 權限:沒有結帳權限的人不能年結
	e.seedUser("viewer", pw, false, false)
	v := e.loggedIn("viewer", pw)
	expect(t, v.yearEnd(2025, "close"), http.StatusForbidden, "SYS-403")

	// 尚未結束的年度、不存在損益的年度
	expect(t, c.yearEnd(2026, "close"), http.StatusUnprocessableEntity, "GL-030")
	expect(t, c.yearEnd(2024, "close"), http.StatusUnprocessableEntity, "GL-032")
	expect(t, c.yearEnd(2025, "undo"), http.StatusConflict, "GL-033")

	// 年度結帳:產生一張傳票;不可重複
	expect(t, c.yearEnd(2025, "close"), http.StatusOK, "")
	expect(t, c.yearEnd(2025, "close"), http.StatusConflict, "GL-031")
	if got := stmtAmount(t, income("2025-01-01", "2025-12-31"), "本期淨利(損)"); got != "1800" {
		t.Fatalf("年結後 2025 損益表應不變,淨利 = %s", got)
	}
	l := bs("2025-12-31")
	if got := stmtAmount(t, l, "權益總計"); got != "6800" {
		t.Fatalf("年結後權益 = %s", got)
	}
	if got := stmtAmount(t, l, "保留盈餘(累積盈虧)"); got != "1800" {
		t.Fatalf("保留盈餘 = %s", got)
	}
	// 2026 年的收入不受 2025 年結影響
	if got := stmtAmount(t, income("2026-01-01", "2026-12-31"), "本期淨利(損)"); got != "500" {
		t.Fatalf("2026 淨利 = %s", got)
	}
	bs("2026-06-30")

	// 年度清單
	res := c.do(http.MethodGet, "/gl/year-end", nil)
	expect(t, res, http.StatusOK, "")
	years := decode[[]struct {
		Year   int    `json:"year"`
		Status string `json:"status"`
	}](t, res.Data)
	st := map[int]string{}
	for _, y := range years {
		st[y.Year] = y.Status
	}
	if st[2025] != "closed" || st[2026] != "not_ended" {
		t.Fatalf("年度狀態 = %v", st)
	}

	// 撤銷後回到結帳前,可再次年結
	expect(t, c.yearEnd(2025, "undo"), http.StatusOK, "")
	if got := stmtAmount(t, bs("2025-12-31"), "權益總計"); got != "6800" {
		t.Fatalf("撤銷後權益 = %s", got)
	}
	expect(t, c.yearEnd(2025, "close"), http.StatusOK, "")

	// 損益剛好為零的年度(2023)與虧損年度(2024)都可結帳
	post("2023-03-01", vl(cash, "100", "0"), vl(sales, "0", "100"))
	post("2023-04-01", vl(rent, "100", "0"), vl(cash, "0", "100"))
	expect(t, c.yearEnd(2023, "close"), http.StatusOK, "")
	post("2024-03-01", vl(rent, "400", "0"), vl(cash, "0", "400"))
	expect(t, c.yearEnd(2024, "close"), http.StatusOK, "")
	if got := stmtAmount(t, income("2024-01-01", "2024-12-31"), "本期淨利(損)"); got != "-400" {
		t.Fatalf("2024 淨利 = %s", got)
	}
	if re := stmtAmount(t, bs("2024-12-31"), "保留盈餘(累積盈虧)"); re != "-400" { // 截至 2024 底:2023 結零、2024 虧損 400(2025 的年結傳票日期在之後)
		t.Fatalf("2024 底保留盈餘 = %q", re)
	}

	// 年結傳票日期是 12/31:12 月已關帳就不能年結,也不能撤銷
	expect(t, c.do(http.MethodPost, "/gl/periods/2025-12/close", nil), http.StatusOK, "")
	expect(t, c.yearEnd(2025, "undo"), http.StatusConflict, "GL-001")
	expect(t, c.do(http.MethodPost, "/gl/periods/2025-12/reopen", nil), http.StatusOK, "")

	// 匯出 Excel
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gl/reports/balance-sheet?as_of=2025-12-31&format=xlsx", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.Len() < 1000 || !strings.Contains(w.Header().Get("Content-Type"), "spreadsheetml") {
		t.Fatalf("匯出失敗 %d len=%d", w.Code, w.Body.Len())
	}
}

// 現金流量表(直接法)與部門別損益
func TestCashFlowAndDepartmentPnl(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	id := func(code string) int64 { return e.idByCode("accounts", code) }
	cash, bank, sales, rent, capital, ar := id("1101"), id("1102"), id("4101"), id("6102"), id("3101"), id("1131")
	equip, res := c.do2(http.MethodPost, "/gl/accounts", map[string]any{"code": "1501", "name": "機器設備", "acct_type": "asset", "is_postable": true, "is_active": true})
	expect(t, res, http.StatusCreated, "")
	var deptID int64
	if err := e.pool.QueryRow(context.Background(), "INSERT INTO departments (company_id, code, name) VALUES (1, 'D1', '業務部') RETURNING id").Scan(&deptID); err != nil {
		t.Fatal(err)
	}
	post := func(date string, lines ...map[string]any) {
		t.Helper()
		v, res := c.createVoucherAPI(manualVoucher(date, lines...))
		expect(t, res, http.StatusCreated, "")
		expect(t, c.actVoucher(&v, "post"), http.StatusOK, "")
	}
	post("2026-01-02", vl(cash, "5000", "0"), vl(capital, "0", "5000"))  // 融資 +5000
	post("2026-02-10", vl(cash, "3000", "0"), vl(sales, "0", "3000"))    // 營業 +3000
	post("2026-03-10", vl(rent, "1200", "0"), vl(cash, "0", "1200"))     // 營業 −1200
	post("2026-04-01", vl(equip.ID, "2000", "0"), vl(bank, "0", "2000")) // 投資:銀行不夠?先互轉再買
	post("2026-04-05", vl(ar, "800", "0"), vl(sales, "0", "800"))        // 賒銷:沒有現金,不計
	post("2026-04-06", vl(bank, "100", "0"), vl(cash, "0", "100"))       // 現金互轉:不計
	post("2026-05-01", vl(cash, "400", "0"), vl(ar, "0", "400"))         // 收回帳款 營業 +400

	type cf struct {
		Lines []struct {
			Kind   string `json:"kind"`
			Label  string `json:"label"`
			Amount string `json:"amount"`
		} `json:"lines"`
		Note string `json:"note"`
	}
	get := decode[cf](t, c.do(http.MethodGet, "/gl/reports/cash-flow?from=2026-01-01&to=2026-12-31", nil).Data)
	amt := func(label string) string {
		for _, l := range get.Lines {
			if l.Label == label {
				return l.Amount
			}
		}
		t.Fatalf("現金流量表沒有「%s」", label)
		return ""
	}
	// 銀行 2102... 買設備付的是銀行存款(+0 −2000);現金 = 現金 + 銀行
	decEq(t, "營業", amt("營業活動淨現金流入(出)"), "2200")  // 3000 − 1200 + 400
	decEq(t, "投資", amt("投資活動淨現金流入(出)"), "-2000") // 買設備
	decEq(t, "融資", amt("融資活動淨現金流入(出)"), "5000")
	decEq(t, "淨增減", amt("本期現金淨增減"), "5200")
	decEq(t, "期初", amt("期初現金及銀行存款"), "0")
	decEq(t, "期末", amt("期末現金及銀行存款"), "5200")
	if get.Note != "" {
		t.Fatalf("不應有核對警示: %s", get.Note)
	}
	// 另一個區間:期初為之前的累積
	mid := decode[cf](t, c.do(http.MethodGet, "/gl/reports/cash-flow?from=2026-03-01&to=2026-03-31", nil).Data)
	for _, l := range mid.Lines {
		if l.Label == "期初現金及銀行存款" {
			decEq(t, "3 月期初", l.Amount, "8000")
		}
	}
	// 權限與參數
	e.seedUser("nobody", pw, false, false)
	expect(t, e.loggedIn("nobody", pw).do(http.MethodGet, "/gl/reports/cash-flow?from=2026-01-01&to=2026-12-31", nil), http.StatusForbidden, "SYS-403")

	// 部門別損益:傳票明細指定部門;其餘歸「未指定部門」
	v, res := c.createVoucherAPI(manualVoucher("2026-06-01",
		map[string]any{"account_id": cash, "debit": "700", "credit": "0"},
		map[string]any{"account_id": sales, "debit": "0", "credit": "700", "department_id": deptID}))
	expect(t, res, http.StatusCreated, "")
	expect(t, c.actVoucher(&v, "post"), http.StatusOK, "")
	post("2026-06-02", vl(rent, "100", "0"), vl(cash, "0", "100"))
	pnl := decode[struct {
		Rows []struct {
			Name    string `json:"name"`
			Revenue string `json:"revenue"`
			Expense string `json:"expense"`
			Profit  string `json:"profit"`
		} `json:"rows"`
		Total struct {
			Profit string `json:"profit"`
		} `json:"total"`
	}](t, c.do(http.MethodGet, "/gl/reports/department-pnl?from=2026-01-01&to=2026-12-31", nil).Data)
	if len(pnl.Rows) != 2 || pnl.Rows[0].Name != "業務部" || pnl.Rows[1].Name != "未指定部門" {
		t.Fatalf("部門別損益 = %+v", pnl.Rows)
	}
	decEq(t, "業務部收入", pnl.Rows[0].Revenue, "700")
	decEq(t, "業務部損益", pnl.Rows[0].Profit, "700")
	decEq(t, "未指定收入", pnl.Rows[1].Revenue, "3800") // 3000 + 800
	decEq(t, "未指定費用", pnl.Rows[1].Expense, "1300") // 1200 + 100
	decEq(t, "未指定損益", pnl.Rows[1].Profit, "2500")
	decEq(t, "合計損益", pnl.Total.Profit, "3200")
}

// 傳票附件:類型 / 大小 / 數量限制、下載、草稿才能刪除、權限
func TestVoucherAttachments(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	cash, sales := e.idByCode("accounts", "1101"), e.idByCode("accounts", "4101")
	v, res := c.createVoucherAPI(manualVoucher("2026-02-10", vl(cash, "100", "0"), vl(sales, "0", "100")))
	expect(t, res, http.StatusCreated, "")
	base := "/gl/vouchers/" + itoa(v.ID) + "/attachments"

	up := func(cl *client, path, name string, data []byte) apiResp {
		t.Helper()
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		part, _ := w.CreateFormFile("file", name)
		_, _ = part.Write(data)
		_ = w.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/v1"+path, &body)
		req.Header.Set("Content-Type", w.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+cl.token)
		rec := httptest.NewRecorder()
		e.r.ServeHTTP(rec, req)
		var r apiResp
		_ = json.Unmarshal(rec.Body.Bytes(), &r)
		r.status = rec.Code
		return r
	}
	pdf := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n")
	res = up(c, base, "invoice.pdf", pdf)
	expect(t, res, http.StatusCreated, "")
	att := decode[struct {
		ID int64 `json:"id"`
	}](t, res.Data)

	// 類型不符:副檔名是 pdf 但內容不是、不支援的副檔名、HTML 偽裝成圖片
	expect(t, up(c, base, "fake.pdf", []byte("<html><script>alert(1)</script>")), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, up(c, base, "x.exe", []byte("MZ....")), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, up(c, base, "pic.png", []byte("<html></html>")), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, up(c, base, "big.pdf", append([]byte("%PDF-"), make([]byte, 5<<20)...)), http.StatusUnprocessableEntity, "SYS-422")

	list := decode[[]struct {
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
		SizeBytes   int    `json:"size_bytes"`
	}](t, c.do(http.MethodGet, base, nil).Data)
	if len(list) != 1 || list[0].Filename != "invoice.pdf" || list[0].ContentType != "application/pdf" || list[0].SizeBytes != len(pdf) {
		t.Fatalf("list = %+v", list)
	}

	// 下載:原內容、附件方式、nosniff
	req := httptest.NewRequest(http.MethodGet, "/api/v1"+base+"/"+itoa(att.ID), nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	rec := httptest.NewRecorder()
	e.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pdf) || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	expect(t, c.do(http.MethodGet, base+"/99999", nil), http.StatusNotFound, "SYS-404")

	// 數量上限:再傳到 10 個之後被擋
	for i := 1; i < 10; i++ {
		expect(t, up(c, base, "a"+itoa(int64(i))+".pdf", pdf), http.StatusCreated, "")
	}
	expect(t, up(c, base, "extra.pdf", pdf), http.StatusConflict, "GL-051")

	// 權限:只有檢視權限的人可看可下載,不能上傳 / 刪除
	role := e.seedRole("VIEW", "all", "gl.voucher.read")
	e.seedUser("viewer", pw, false, false, role.ID)
	vw := e.loggedIn("viewer", pw)
	expect(t, vw.do(http.MethodGet, base, nil), http.StatusOK, "")
	expect(t, up(vw, base, "z.pdf", pdf), http.StatusForbidden, "SYS-403")
	expect(t, vw.do(http.MethodDelete, base+"/"+itoa(att.ID), nil), http.StatusForbidden, "SYS-403")

	// 草稿可刪;過帳後附件留存不能刪,但還能補傳(受數量限制)
	expect(t, c.do(http.MethodDelete, base+"/"+itoa(att.ID), nil), http.StatusOK, "")
	expect(t, c.actVoucher(&v, "post"), http.StatusOK, "")
	expect(t, up(c, base, "after-post.pdf", pdf), http.StatusCreated, "")
	list2 := decode[[]struct {
		ID int64 `json:"id"`
	}](t, c.do(http.MethodGet, base, nil).Data)
	expect(t, c.do(http.MethodDelete, base+"/"+itoa(list2[0].ID), nil), http.StatusConflict, "GL-052")
}

// 專案別損益:傳票明細帶專案,報表依專案分組,未填的歸「未指定」(D80)。
func TestProjectPnl(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	res := c.do(http.MethodPost, "/gl/projects", map[string]any{"code": "p1", "name": "專案一"})
	expect(t, res, http.StatusCreated, "")
	pid := decode[struct {
		ID int64 `json:"id"`
	}](t, res.Data).ID
	expect(t, c.do(http.MethodPost, "/gl/projects", map[string]any{"code": "P1", "name": "重複"}), http.StatusConflict, "GL-060")
	// 欄位名稱須為小寫 snake_case(曾因漏掉 json tag 而在畫面顯示空白)
	if raw := string(c.do(http.MethodGet, "/gl/projects", nil).Data); !strings.Contains(raw, `"code":"P1"`) || !strings.Contains(raw, `"is_active":true`) {
		t.Fatalf("專案列表欄位 %s", raw)
	}

	cash, sales := e.idByCode("accounts", "1101"), e.idByCode("accounts", "4101")
	mk := func(project *int64, amt string) {
		v, r := c.createVoucherAPI(manualVoucher("2026-10-05",
			map[string]any{"account_id": cash, "debit": amt, "credit": "0"},
			map[string]any{"account_id": sales, "debit": "0", "credit": amt, "project_id": project}))
		expect(t, r, http.StatusCreated, "")
		expect(t, c.actVoucher(&v, "post"), http.StatusOK, "")
	}
	mk(&pid, "300")
	mk(nil, "50")
	res = c.do(http.MethodGet, "/gl/reports/project-pnl?from=2026-10-01&to=2026-10-31", nil)
	expect(t, res, http.StatusOK, "")
	out := decode[struct {
		Rows []struct {
			Code    string `json:"code"`
			Revenue string `json:"revenue"`
			Profit  string `json:"profit"`
		} `json:"rows"`
	}](t, res.Data)
	if len(out.Rows) != 2 || out.Rows[0].Code != "P1" || out.Rows[0].Profit != "300" || out.Rows[1].Revenue != "50" {
		t.Fatalf("專案損益 %+v", out.Rows)
	}
	bad := int64(99999)
	expect(t, c.do(http.MethodPost, "/gl/vouchers", manualVoucher("2026-10-06",
		map[string]any{"account_id": cash, "debit": "10", "credit": "0"},
		map[string]any{"account_id": sales, "debit": "0", "credit": "10", "project_id": bad})), http.StatusUnprocessableEntity, "SYS-422")
}

// 外幣期末重估:月底匯率重估外幣應付,下一期沖回上期後重新估算;只能依序、可撤銷最近一次(D86)。
func TestFxRevaluation(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	usd := e.seedSupplier("S2", "USD")
	for date, rate := range map[string]string{"2026-08-01": "30", "2026-08-31": "31", "2026-09-30": "32"} {
		d, _ := time.Parse(time.DateOnly, date)
		if _, err := e.q.CreateExchangeRate(context.Background(), db.CreateExchangeRateParams{
			CompanyID: 1, Currency: "USD", RateDate: d, Rate: decimal.RequireFromString(rate),
		}); err != nil {
			t.Fatal(err)
		}
	}
	gr, res := c.createPur(receipts, p.header(map[string]any{
		"doc_type": "receipt", "supplier_id": usd, "currency": "USD", "doc_date": "2026-08-15",
		"lines": []map[string]any{line(p.item, e.unitID("PCS"), "10", "10", nil)}, // 100 USD + 稅 5 = 105 USD
	}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(receipts, &gr)
	expect(t, c.actPur(receipts, &gr, "post"), http.StatusOK, "")

	bal := func(code string) string {
		var v string
		if err := e.pool.QueryRow(context.Background(),
			`SELECT COALESCE(SUM(l.debit - l.credit), 0)::numeric(18,0)::text FROM voucher_lines l JOIN accounts a ON a.id = l.account_id WHERE a.code = $1`, code).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	type preview struct {
		ApDiff  string `json:"ap_diff"`
		NetGain string `json:"net_gain"`
		Rows    []struct {
			Currency   string `json:"currency"`
			ApCarrying string `json:"ap_carrying"`
		} `json:"rows"`
	}
	res = c.do(http.MethodGet, "/gl/revaluation?period=2026-08", nil)
	expect(t, res, http.StatusOK, "")
	pv := decode[preview](t, res.Data)
	if len(pv.Rows) != 1 || pv.Rows[0].Currency != "USD" || pv.Rows[0].ApCarrying != "3150" || pv.ApDiff != "105" || pv.NetGain != "-105" {
		t.Fatalf("8 月試算 %+v", pv)
	}
	expect(t, c.do(http.MethodPost, "/gl/revaluation/2026-13", nil), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, c.do(http.MethodPost, "/gl/revaluation/2099-01", nil), http.StatusConflict, "GL-071")

	// 8 月重估:借 未實現兌換損失 105、貸 應付 105
	expect(t, c.do(http.MethodPost, "/gl/revaluation/2026-08", nil), http.StatusCreated, "")
	if bal("7103") != "105" || bal("2101") != "-3255" {
		t.Fatalf("8 月重估後 損失 %s 應付 %s", bal("7103"), bal("2101"))
	}
	expect(t, c.do(http.MethodPost, "/gl/revaluation/2026-08", nil), http.StatusConflict, "GL-070")

	// 9 月重估:沖回 8 月的 105,再依 32 重估(差額 210)→ 損失淨 210、應付 3360 = 105 × 32
	expect(t, c.do(http.MethodPost, "/gl/revaluation/2026-09", nil), http.StatusCreated, "")
	if bal("7103") != "210" || bal("2101") != "-3360" {
		t.Fatalf("9 月重估後 損失 %s 應付 %s", bal("7103"), bal("2101"))
	}
	// 只能撤銷最近一次
	expect(t, c.do(http.MethodPost, "/gl/revaluation/2026-08/undo", nil), http.StatusConflict, "GL-072")
	expect(t, c.do(http.MethodPost, "/gl/revaluation/2026-09/undo", nil), http.StatusNoContent, "")
	if bal("7103") != "105" || bal("2101") != "-3255" {
		t.Fatalf("撤銷 9 月後 損失 %s 應付 %s", bal("7103"), bal("2101"))
	}
	// 撤銷後可重做 9 月
	expect(t, c.do(http.MethodPost, "/gl/revaluation/2026-09", nil), http.StatusCreated, "")
	if bal("7103") != "210" {
		t.Fatalf("重做後 損失 %s", bal("7103"))
	}
	// 權限
	e.seedUser("nobody", pw, false, false)
	expect(t, e.loggedIn("nobody", pw).do(http.MethodPost, "/gl/revaluation/2026-09", nil), http.StatusForbidden, "SYS-403")
}
