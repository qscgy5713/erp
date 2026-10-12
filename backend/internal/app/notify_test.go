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
