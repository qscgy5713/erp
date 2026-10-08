package app

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// parallel 同時啟動 n 個 goroutine(以柵欄讓它們盡量同時開始)執行 fn,回傳各自的結果。
func parallel(n int, fn func(i int) apiResp) []apiResp {
	out := make([]apiResp, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			out[i] = fn(i)
		})
	}
	close(start)
	wg.Wait()
	return out
}

// tally 統計成功數與各錯誤碼出現次數。
func tally(rs []apiResp) (ok int, codes map[string]int) {
	codes = map[string]int{}
	for _, r := range rs {
		if r.status == http.StatusOK || r.status == http.StatusCreated {
			ok++
		} else {
			codes[r.code()]++
		}
	}
	return ok, codes
}

// 同一批庫存被多張出貨單同時過帳:不可超賣,庫存永遠不為負。
func TestConcurrentDeliveriesNeverOversell(t *testing.T) {
	s := newSalCtx(t) // 庫存 120 個(10 箱)
	c := s.c
	docs := make([]purDoc, 14)
	for i := range docs {
		d, res := c.createPur(deliveries, s.header(map[string]any{
			"doc_type": "delivery", "lines": []map[string]any{line(s.item, s.box, "1", "1000", nil)},
		}))
		expect(t, res, http.StatusCreated, "")
		c.approvePur(deliveries, &d)
		docs[i] = d
	}
	rs := parallel(len(docs), func(i int) apiResp { return c.actPur(deliveries, &docs[i], "post") })
	ok, codes := tally(rs)
	if ok != 10 || codes["INV-001"] != 4 {
		t.Fatalf("成功 %d,錯誤 %v;應為 10 張成功、4 張庫存不足", ok, codes)
	}
	if b := s.e.balance(s.item, s.wh); b != "0" {
		t.Fatalf("結存 %s,應為 0", b)
	}
	// 應收也只有成功的 10 張
	if ar, _ := c.receivables(); len(ar) != 10 {
		t.Fatalf("應收 %d 筆,應為 10", len(ar))
	}
}

// 同一張採購單的多張進貨單同時過帳:累計不可超過訂購量。
func TestConcurrentReceiptsRespectOrderLimit(t *testing.T) {
	p := newPurCtx(t)
	c := p.c
	po := p.newOrder() // 10 箱
	docs := make([]purDoc, 6)
	for i := range docs {
		d, res := p.receive(po, "4") // 每張開單時都合法
		expect(t, res, http.StatusCreated, "")
		c.approvePur(receipts, &d)
		docs[i] = d
	}
	rs := parallel(len(docs), func(i int) apiResp { return c.actPur(receipts, &docs[i], "post") })
	ok, codes := tally(rs)
	if ok != 2 || codes["PUR-004"] != 4 {
		t.Fatalf("成功 %d,錯誤 %v;10 箱訂單每張 4 箱,應只有 2 張成功", ok, codes)
	}
	c.reload(orders, &po)
	if po.Lines[0].RemainingQty != "2" {
		t.Fatalf("未交量 %s,應為 2", po.Lines[0].RemainingQty)
	}
	if b := p.e.balance(p.item, p.wh); b != "96" {
		t.Fatalf("庫存 %s,應為 96(8 箱)", b)
	}
}

// 同一筆應收被多張收款單同時沖帳:累計不可超過餘額。
func TestConcurrentCollectionsNeverOverSettle(t *testing.T) {
	s := newSalCtx(t)
	c := s.c
	so := s.newOrder("3")
	dn, _ := s.deliver(so, "3") // 應收 3150
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")
	ar := c.arIDs()[0]
	docs := make([]settleDoc, 6)
	for i := range docs {
		d, res := c.createSettle("/finance/collections", settleBody(s.cust, sl(ar, "1000")))
		expect(t, res, http.StatusCreated, "")
		c.approveSettle("/finance/collections", &d)
		docs[i] = d
	}
	rs := parallel(len(docs), func(i int) apiResp { return c.actSettle("/finance/collections", &docs[i], "post") })
	ok, codes := tally(rs)
	if ok != 3 || codes["FIN-004"] != 3 {
		t.Fatalf("成功 %d,錯誤 %v;3150 的應收每張沖 1000,應只有 3 張成功", ok, codes)
	}
	if _, sum := c.receivables(); sum != "150" {
		t.Fatalf("未沖餘額 %s,應為 150", sum)
	}
}

// 同一客戶的多張訂單同時核准:信用額度不可被同時通過而突破。
func TestConcurrentOrderApprovalsRespectCreditLimit(t *testing.T) {
	s := newSalCtx(t)
	c := s.c
	s.cust = s.e.seedCustomer("C2", "5000", nil)
	// 單次核准很快,併發數不夠高時請求會變成依序完成而測不到競爭;用 24 張提高碰撞機會
	docs := make([]purDoc, 24)
	for i := range docs {
		d, res := c.createPur(salesOrders, s.header(map[string]any{
			"doc_type": "order", "lines": []map[string]any{line(s.item, s.box, "2", "1000", nil)}, // 每張含稅 2100
		}))
		expect(t, res, http.StatusCreated, "")
		expect(t, c.actPur(salesOrders, &d, "submit"), http.StatusOK, "")
		docs[i] = d
	}
	rs := parallel(len(docs), func(i int) apiResp { return c.actPur(salesOrders, &docs[i], "approve") })
	ok, codes := tally(rs)
	if ok != 2 || codes["SAL-003"] != len(docs)-2 { // 2100 × 2 = 4200 ≤ 5000,第三張 6300 超過
		t.Fatalf("成功 %d,錯誤 %v;額度 5000 每張 2100,應只有 2 張核准", ok, codes)
	}
}

// 同時建立大量單據:單號不重複、沒有跳號。
func TestConcurrentDocumentNumbersAreUnique(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	wh, item, pcs := e.seedWarehouse("A", true), e.seedItem("P1", "goods"), e.unitID("PCS")
	const n = 30
	nos := make([]string, n)
	rs := parallel(n, func(i int) apiResp {
		res := c.do(http.MethodPost, "/inventory/documents", adjust(wh, item, pcs, "1"))
		if res.status == http.StatusCreated {
			nos[i] = decode[stockDoc](t, res.Data).DocNo
		}
		return res
	})
	if ok, codes := tally(rs); ok != n {
		t.Fatalf("成功 %d,錯誤 %v", ok, codes)
	}
	sort.Strings(nos)
	for i, no := range nos {
		if want := "IA20261007" + pad4(i+1); no != want {
			t.Fatalf("第 %d 個單號 %s,應為 %s(不可重複、不可跳號)", i+1, no, want)
		}
	}
}

func pad4(n int) string {
	s := itoa(int64(n))
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

// 月結成本與過帳同時進行:不論誰先,結果都要一致(月結的期末量 = 現有量,且月結後不再有異動漏進去)。
func TestConcurrentCostClosingAndPosting(t *testing.T) {
	p := newPurCtx(t)
	c := p.c
	// 8 月已有 2 箱進貨;9 月 6 張進貨單(各 1 箱)待過帳,同時對 9 月月結
	p.receiveOn("2026-08-10", "2", "1200")
	expect(t, c.do(http.MethodPost, "/costing/closings/2026-08/run", nil), http.StatusOK, "")
	docs := make([]purDoc, 6)
	for i := range docs {
		d, res := c.createPur(receipts, p.header(map[string]any{
			"doc_date": "2026-09-10", "doc_type": "receipt",
			"lines": []map[string]any{line(p.item, p.box, "1", "1200", nil)},
		}))
		expect(t, res, http.StatusCreated, "")
		c.approvePur(receipts, &d)
		docs[i] = d
	}
	rs := parallel(len(docs)+1, func(i int) apiResp {
		if i == len(docs) {
			return c.do(http.MethodPost, "/costing/closings/2026-09/run", nil)
		}
		return c.actPur(receipts, &docs[i], "post")
	})
	closed := rs[len(docs)].status == http.StatusOK
	posted := 0
	for _, r := range rs[:len(docs)] {
		switch {
		case r.status == http.StatusOK:
			posted++
		case r.code() != "INV-008":
			t.Fatalf("過帳失敗只應是已月結鎖定(INV-008),得到 %d %s", r.status, r.code())
		}
	}
	if !closed {
		t.Fatalf("月結失敗: %+v", rs[len(docs)].Error)
	}
	// 月結之前完成的過帳都被算進去:月結的期末量 = 24 + 12×(已過帳張數);之後的過帳被擋
	items := decode[[]struct {
		ClosingQty string `json:"closing_qty"`
	}](t, c.do(http.MethodGet, "/costing/closings/2026-09/items", nil).Data)
	want := decimalStr(24 + 12*posted)
	if len(items) != 1 || items[0].ClosingQty != want {
		t.Fatalf("月結期末量 %+v,應為 %s(已過帳 %d 張)", items, want, posted)
	}
	if b := p.e.balance(p.item, p.wh); b != want {
		t.Fatalf("現有量 %s 與月結期末量 %s 不一致", b, want)
	}
}

func decimalStr(n int) string { return itoa(int64(n)) }

// ---- 確定性的鎖測試:先手動持有鎖,驗證請求必須等到鎖釋放才能完成 ----
// 併發測試靠時間碰撞,單次操作很快時窗口太窄,拿掉鎖也可能偶然通過;這類測試不靠運氣。

// blockedUntilCommit 在 tx 持有鎖期間執行 fn(背景),確認它不會完成;提交後確認它完成並回傳結果。
func blockedUntilCommit(t *testing.T, tx pgx.Tx, fn func() apiResp) apiResp {
	t.Helper()
	// 測試失敗(Fatal)時也要放掉鎖,否則清理暫存資料庫會一直等待
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	done := make(chan apiResp, 1)
	go func() { done <- fn() }()
	select {
	case r := <-done:
		t.Fatalf("請求應等待鎖釋放,卻已結束: %d %s", r.status, r.code())
	case <-time.After(400 * time.Millisecond):
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("鎖釋放後請求仍未完成")
		return apiResp{}
	}
}

// 核准訂單必須先鎖客戶列,同一客戶的核准才會依序檢查信用額度。
func TestOrderApprovalWaitsForCustomerLock(t *testing.T) {
	s := newSalCtx(t)
	c := s.c
	s.cust = s.e.seedCustomer("C2", "100000", nil)
	so, res := c.createPur(salesOrders, s.header(map[string]any{
		"doc_type": "order", "lines": []map[string]any{line(s.item, s.box, "1", "1000", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	expect(t, c.actPur(salesOrders, &so, "submit"), http.StatusOK, "")

	tx, err := s.e.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), "SELECT id FROM customers WHERE id = $1 FOR UPDATE", s.cust); err != nil {
		t.Fatal(err)
	}
	r := blockedUntilCommit(t, tx, func() apiResp { return c.actPur(salesOrders, &so, "approve") })
	expect(t, r, http.StatusOK, "")
}

// 過帳必須在「檢查期間是否已關帳」之前先取共享鎖,與關帳(排他鎖)序列化。
// 只靠外鍵鎖不夠:檢查若發生在等鎖之前,會讀到尚未提交的「開放」,等關帳提交後仍照樣寫進已關帳的月份。
// 用庫存調整單測試:它不產生傳票,所以沒有第二次期間檢查可以補救(出貨 / 進貨單的傳票拋轉會再檢查一次)。
func TestStockPostingSeesPeriodClosedByConcurrentClose(t *testing.T) {
	s := newSalCtx(t) // 庫存 120 個
	c := s.c
	adj, res := c.createDoc(adjust(s.wh, s.item, s.e.unitID("PCS"), "5"))
	expect(t, res, http.StatusCreated, "")
	for _, a := range []string{"submit", "approve"} {
		expect(t, c.act(&adj, a), http.StatusOK, "")
	}

	tx, err := s.e.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 模擬關帳:持有公司列的排他鎖並寫入關帳紀錄(與 gl.changePeriod 相同),尚未提交
	for _, sql := range []string{
		"SELECT id FROM companies WHERE id = 1 FOR UPDATE",
		"INSERT INTO accounting_periods (company_id, period, status) VALUES (1, '2026-10', 'closed')",
	} {
		if _, err := tx.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
	r := blockedUntilCommit(t, tx, func() apiResp { return c.act(&adj, "post") })
	expect(t, r, http.StatusConflict, "GL-001") // 關帳提交後,過帳要看到「已關帳」而被擋
	if b := s.e.balance(s.item, s.wh); b != "120" {
		t.Fatalf("過帳被擋後庫存不應改變,現有量 %s", b)
	}
}
