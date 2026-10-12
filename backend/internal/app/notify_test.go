package app

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"erp/internal/notify"
	"erp/internal/system/permission"
)

type fakeSender struct {
	sent []string // 收件人
	fail bool
}

func (f *fakeSender) Send(to, subject, body string) error {
	if f.fail {
		return errors.New("smtp down")
	}
	f.sent = append(f.sent, to)
	return nil
}

func (e *env) countNotif(where string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM notifications WHERE `+where, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func enableNotify(t *testing.T) {
	notify.Configure(true, "http://erp.test")
	t.Cleanup(func() { notify.Configure(false, "") })
}

// TestApprovalNotifications 送審與進到下一層時通知輪到的人(含代理人),寄信失敗會重試,測試信只寄給自己。
func TestApprovalNotifications(t *testing.T) {
	e := newEnv(t)
	enableNotify(t)
	wh, item := e.seedWarehouse("A", false), e.seedItem("P1", "goods")
	e.seedUser("root", pw, true, false)
	root := e.loggedIn("root", pw)

	maker := e.seedRole("maker", "all", permission.InventoryRead, permission.InventoryWrite)
	l1 := e.seedRole("l1", "all", permission.InventoryRead, permission.InventoryApprove)
	cfo := e.seedRole("cfo", "all", permission.InventoryRead, permission.InventoryApprove)
	plain := e.seedRole("plain", "all", permission.InventoryRead)
	mkU := e.seedUser("maker", pw, false, false, maker.ID)
	apU := e.seedUser("appr", pw, false, false, l1.ID)
	bossU := e.seedUser("boss", pw, false, false, cfo.ID)
	depU := e.seedUser("deputy", pw, false, false, plain.ID)
	e.seedUser("noemail", pw, false, false, l1.ID)
	for _, u := range []struct {
		id    int64
		email string
	}{{mkU.ID, "maker@x.test"}, {apU.ID, "appr@x.test"}, {bossU.ID, "boss@x.test"}, {depU.ID, "deputy@x.test"}} {
		if _, err := e.pool.Exec(context.Background(), `UPDATE users SET email = $2 WHERE id = $1`, u.id, u.email); err != nil {
			t.Fatal(err)
		}
	}
	mk, ap := e.loggedIn("maker", pw), e.loggedIn("appr", pw)
	expect(t, root.do(http.MethodPost, "/approval/rules", map[string]any{
		"doc_type": "stock_document", "min_amount": "0", "role_ids": []int64{cfo.ID},
	}), http.StatusCreated, "")
	day := func(off int) string { return time.Now().AddDate(0, 0, off).Format(time.DateOnly) }
	expect(t, ap.do(http.MethodPost, "/approval/delegations", map[string]any{"delegate_id": depU.ID, "valid_from": day(-1), "valid_to": day(2)}), http.StatusCreated, "")

	d, res := mk.createDoc(adjust(wh, item, e.unitID("PCS"), "5"))
	expect(t, res, http.StatusCreated, "")
	expect(t, mk.act(&d, "submit"), http.StatusOK, "")
	// 第 1 層:所有有核准權限的人(appr、boss)與 appr 的代理人 deputy;noemail 沒有 email、maker 是送審者,不通知
	if n := e.countNotif(`event = 'approval_pending'`); n != 3 {
		t.Fatalf("送審後應通知 appr、boss 與代理人,實際 %d 封", n)
	}
	if e.countNotif(`to_email = 'maker@x.test'`) != 0 {
		t.Fatal("不應通知送審者")
	}
	expect(t, ap.act(&d, "approve"), http.StatusOK, "")
	// 進到第 2 層(cfo):boss 收到
	if e.countNotif(`to_email = 'boss@x.test'`) != 2 || e.countNotif(`event = 'approval_pending'`) != 4 {
		t.Fatal("第 1 層核准後應通知第 2 層的 boss")
	}

	// 寄信:失敗重試,成功標記
	w := &notify.Worker{Q: e.q, Sender: &fakeSender{fail: true}}
	if n, err := w.Flush(context.Background()); err != nil || n != 0 {
		t.Fatalf("失敗時應寄出 0 封,got %d %v", n, err)
	}
	if e.countNotif(`status = 'pending' AND attempts = 1 AND last_error <> ''`) != 4 {
		t.Fatal("失敗的信應保留待重試")
	}
	if n, _ := w.Flush(context.Background()); n != 0 {
		t.Fatal("預約時間未到不應立刻重寄")
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE notifications SET next_attempt_at = now()`); err != nil {
		t.Fatal(err)
	}
	ok := &fakeSender{}
	w.Sender = ok
	if n, err := w.Flush(context.Background()); err != nil || n != 4 || len(ok.sent) != 4 {
		t.Fatalf("重試應寄出 4 封,got %d %v", n, err)
	}
	if e.countNotif(`status = 'sent'`) != 4 {
		t.Fatal("寄出後應標記 sent")
	}

	// 測試信:只寄給自己;沒有 email 422;未啟用 409
	bossC := e.loggedIn("boss", pw)
	expect(t, bossC.do(http.MethodPost, "/notifications/test", nil), http.StatusOK, "")
	if e.countNotif(`event = 'test' AND to_email = 'boss@x.test'`) != 1 {
		t.Fatal("測試信應排入佇列")
	}
	expect(t, e.loggedIn("noemail", pw).do(http.MethodPost, "/notifications/test", nil), http.StatusUnprocessableEntity, "SYS-422")
	notify.Configure(false, "")
	expect(t, bossC.do(http.MethodPost, "/notifications/test", nil), http.StatusConflict, "NTF-001")
}

// TestCheckDueNotifications 未兌現票據在 3 天內到期:通知具過帳權限的人,每張票據每人只通知一次。
func TestCheckDueNotifications(t *testing.T) {
	s := newSalCtx(t)
	enableNotify(t)
	s.twoDeliveries()
	ids := s.c.arIDs()
	const path = "/finance/collections"
	body := settleBody(s.cust, sl(ids[0], "1000"))
	body["method"], body["reference"], body["check_due_date"] = "check", "AB1", time.Now().AddDate(0, 0, 2).Format(time.DateOnly)
	d, res := s.c.createSettle(path, body)
	expect(t, res, http.StatusCreated, "")
	s.c.approveSettle(path, &d)
	expect(t, s.c.actSettle(path, &d, "post"), http.StatusOK, "")
	if _, err := s.e.pool.Exec(context.Background(), `UPDATE users SET email = username || '@x.test'`); err != nil {
		t.Fatal(err)
	}
	w := &notify.Worker{Q: s.e.q, Sender: &fakeSender{}}
	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := s.e.countNotif(`event = 'check_due'`)
	if n == 0 {
		t.Fatal("到期票據應產生通知")
	}
	if err := w.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.e.countNotif(`event = 'check_due'`) != n {
		t.Fatal("重複掃描不應重複通知")
	}
}

// TestNotificationDigestsAndPrefs 低庫存 / 批號效期 / 逾期應收週報:只寄給有權限的人、每週一封、
// 可由個人關閉;逾期應收不寄給資料範圍受限者;訂閱設定 API 只改自己的。
func TestNotificationDigestsAndPrefs(t *testing.T) {
	e := newEnv(t)
	enableNotify(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := e.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	wh, item := e.seedWarehouse("A", false), e.seedItem("P1", "goods")
	cust := e.seedCustomer("C1", "0", nil)
	exec(`UPDATE items SET safety_stock = 10 WHERE id = $1`, item) // 沒有庫存 → 低於安全庫存
	var lot int64
	if err := e.pool.QueryRow(ctx, `INSERT INTO item_lots (company_id, item_id, lot_no, expiry_date) VALUES (1, $1, 'L1', CURRENT_DATE + 5) RETURNING id`, item).Scan(&lot); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO inventory_lot_balances (company_id, item_id, warehouse_id, lot_id, qty) VALUES (1, $1, $2, $3, 3)`, item, wh, lot)
	exec(`INSERT INTO accounts_receivable (company_id, customer_id, source_type, source_id, source_no, doc_date, due_date, currency, exchange_rate, amount, base_amount)
		VALUES (1, $1, 'delivery', 1, 'D1', CURRENT_DATE - 40, CURRENT_DATE - 10, 'TWD', 1, 500, 500)`, cust)

	stockRole := e.seedRole("stock", "all", permission.InventoryRead)
	arAll := e.seedRole("arall", "all", permission.CollectionPost)
	arSelf := e.seedRole("arself", "self", permission.CollectionPost)
	e.seedUser("root", pw, true, false)
	e.seedUser("stocker", pw, false, false, stockRole.ID)
	e.seedUser("quiet", pw, false, false, stockRole.ID)
	e.seedUser("arall", pw, false, false, arAll.ID)
	e.seedUser("arself", pw, false, false, arSelf.ID)
	e.seedUser("nobody", pw, false, false)
	exec(`UPDATE users SET email = username || '@x.test'`)

	quiet := e.loggedIn("quiet", pw)
	expect(t, quiet.do(http.MethodPut, "/notifications/prefs", map[string]any{"prefs": map[string]bool{"low_stock": false}}), http.StatusOK, "")
	expect(t, quiet.do(http.MethodPut, "/notifications/prefs", map[string]any{"prefs": map[string]bool{"nope": false}}), http.StatusUnprocessableEntity, "SYS-422")
	prefs := decode[[]struct {
		Event   string `json:"event"`
		Enabled bool   `json:"enabled"`
	}](t, quiet.do(http.MethodGet, "/notifications/prefs", nil).Data)
	off := map[string]bool{}
	for _, p := range prefs {
		off[p.Event] = !p.Enabled
	}
	if len(prefs) != len(notify.Events) || !off["low_stock"] || off["lot_expiry"] {
		t.Fatalf("prefs = %+v", prefs)
	}

	w := &notify.Worker{Q: e.q, Sender: &fakeSender{}}
	for i := 0; i < 2; i++ { // 重複掃描不重複寄
		if err := w.Scan(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got := func(event string) string {
		rows, err := e.pool.Query(ctx, `SELECT string_agg(u.username, ',' ORDER BY u.username) FROM notifications n JOIN users u ON u.id = n.user_id WHERE n.event = $1`, event)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var s *string
		for rows.Next() {
			_ = rows.Scan(&s)
		}
		if s == nil {
			return ""
		}
		return *s
	}
	if g := got("low_stock"); g != "root,stocker" { // quiet 已關閉、其餘沒庫存權限
		t.Fatalf("low_stock 收件人 = %q", g)
	}
	if g := got("lot_expiry"); g != "quiet,root,stocker" {
		t.Fatalf("lot_expiry 收件人 = %q", g)
	}
	if g := got("ar_overdue"); g != "arall,root" { // arself 資料範圍受限不寄
		t.Fatalf("ar_overdue 收件人 = %q", g)
	}
	// 已過期天數內沒有逾期時不寄:沖平帳款後新的一週不會有逾期可報
	exec(`UPDATE accounts_receivable SET paid_amount = amount`)
	exec(`DELETE FROM notifications WHERE event = 'ar_overdue'`)
	if err := w.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if got("ar_overdue") != "" {
		t.Fatal("沒有逾期應收不該寄")
	}
}

// TestEnqueueRespectsPrefs 關閉某事件的人不會被排入該事件的信,其他事件與其他人不受影響。
func TestEnqueueRespectsPrefs(t *testing.T) {
	e := newEnv(t)
	enableNotify(t)
	ctx := context.Background()
	a := e.seedUser("user_a", pw, false, false)
	b := e.seedUser("user_b", pw, false, false)
	if _, err := e.pool.Exec(ctx, `UPDATE users SET email = username || '@x.test'`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO notification_prefs (user_id, event, enabled) VALUES ($1, 'approval_pending', false)`, a.ID); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []string{"approval_pending", "check_due"} {
		if err := notify.Enqueue(ctx, e.q, 1, []int64{a.ID, b.ID}, ev, "s", "b", ""); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.countNotif(`user_id = $1 AND event = 'approval_pending'`, a.ID); n != 0 {
		t.Fatalf("已關閉的人仍收到 %d 封", n)
	}
	if e.countNotif(`user_id = $1 AND event = 'approval_pending'`, b.ID) != 1 || e.countNotif(`user_id = $1 AND event = 'check_due'`, a.ID) != 1 {
		t.Fatal("其他人 / 其他事件不該受影響")
	}
}
