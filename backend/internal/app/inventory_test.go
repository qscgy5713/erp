package app

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/inventory"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
)

func apperrCode(err error) string {
	if e := apperr.As(err); e != nil {
		return e.Code
	}
	return ""
}

// ---- 測試資料 ----

func (e *env) seedWarehouse(code string, allowNegative bool) int64 {
	e.t.Helper()
	w, err := e.q.CreateWarehouse(context.Background(), db.CreateWarehouseParams{
		CompanyID: 1, Code: code, Name: code + "倉", AllowNegative: allowNegative,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return w.ID
}

// seedItem 建立以「個」為基本單位、1 箱 = 12 個的商品。
func (e *env) seedItem(code, itemType string) int64 {
	e.t.Helper()
	ctx := context.Background()
	it, err := e.q.CreateItem(ctx, db.CreateItemParams{
		CompanyID: 1, Code: code, Name: code, ItemType: itemType, BaseUnitID: e.unitID("PCS"),
		SafetyStock: decimal.Zero, ListPrice: decimal.Zero,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.q.AddItemUnit(ctx, db.AddItemUnitParams{ItemID: it.ID, UnitID: e.unitID("BOX"), Factor: decimal.NewFromInt(12)}); err != nil {
		e.t.Fatal(err)
	}
	return it.ID
}

type stockDoc struct {
	ID      int64  `json:"id"`
	DocNo   string `json:"doc_no"`
	Status  string `json:"status"`
	Version int32  `json:"version"`
	Lines   []struct {
		ItemID    int64   `json:"item_id"`
		UnitID    int64   `json:"unit_id"`
		Qty       *string `json:"qty"`
		BaseQty   *string `json:"base_qty"`
		SystemQty *string `json:"system_qty"`
		DiffQty   *string `json:"diff_qty"`
	} `json:"lines"`
}

func (c *client) createDoc(body map[string]any) (stockDoc, apiResp) {
	c.e.t.Helper()
	res := c.do(http.MethodPost, "/inventory/documents", body)
	if res.status != http.StatusCreated {
		return stockDoc{}, res
	}
	return decode[stockDoc](c.e.t, res.Data), res
}

func (c *client) act(d *stockDoc, action string) apiResp {
	c.e.t.Helper()
	res := c.do(http.MethodPost, "/inventory/documents/"+itoa(d.ID)+"/actions/"+action, map[string]any{"version": d.Version})
	if res.status == http.StatusOK {
		*d = decode[stockDoc](c.e.t, res.Data)
	}
	return res
}

// postAll 送審 → 核准 → 過帳。
func (c *client) postAll(d *stockDoc) apiResp {
	c.e.t.Helper()
	for _, a := range []string{"submit", "approve"} {
		if res := c.act(d, a); res.status != http.StatusOK {
			c.e.t.Fatalf("%s 失敗: %d %+v", a, res.status, res.Error)
		}
	}
	return c.act(d, "post")
}

func (e *env) balance(itemID, whID int64) string {
	e.t.Helper()
	q, err := e.q.GetBalanceQty(context.Background(), db.GetBalanceQtyParams{ItemID: itemID, WarehouseID: whID})
	if err != nil {
		e.t.Fatal(err)
	}
	return q.String()
}

const today = "2026-10-07"

func adjust(whID, itemID, unitID int64, qty string) map[string]any {
	return map[string]any{
		"doc_type": "adjustment", "doc_date": today, "warehouse_id": whID,
		"lines": []map[string]any{{"item_id": itemID, "unit_id": unitID, "qty": qty}},
	}
}

// ---- 測試 ----

func TestAdjustmentPostAndUnpost(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	wh, item := e.seedWarehouse("A", false), e.seedItem("P1", "goods")

	d, res := c.createDoc(adjust(wh, item, e.unitID("BOX"), "10"))
	expect(t, res, http.StatusCreated, "")
	if d.DocNo != "IA202610070001" || d.Status != "draft" || *d.Lines[0].BaseQty != "120" {
		t.Fatalf("doc = %+v", d)
	}
	// 草稿不可直接過帳
	expect(t, c.act(&d, "post"), http.StatusConflict, "DOC-001")
	expect(t, c.postAll(&d), http.StatusOK, "")
	if d.Status != "posted" || e.balance(item, wh) != "120" {
		t.Fatalf("過帳後 status=%s balance=%s", d.Status, e.balance(item, wh))
	}
	// 已過帳不可修改
	expect(t, c.do(http.MethodPut, "/inventory/documents/"+itoa(d.ID), map[string]any{
		"doc_type": "adjustment", "doc_date": today, "warehouse_id": wh, "version": d.Version,
		"lines": []map[string]any{{"item_id": item, "unit_id": e.unitID("PCS"), "qty": "1"}},
	}), http.StatusConflict, "STK-003")

	expect(t, c.act(&d, "unpost"), http.StatusOK, "")
	if d.Status != "approved" || e.balance(item, wh) != "0" {
		t.Fatalf("反過帳後 status=%s balance=%s", d.Status, e.balance(item, wh))
	}
	// 流水帳保留原分錄與反向分錄;收發存在同期間互相抵銷
	sum := decode[[]struct {
		InQty      string `json:"in_qty"`
		OutQty     string `json:"out_qty"`
		ClosingQty string `json:"closing_qty"`
	}](t, c.do(http.MethodGet, "/inventory/movement-summary?from=2026-10-01&to=2026-10-31", nil).Data)
	if len(sum) != 1 || sum[0].InQty != "0" || sum[0].OutQty != "0" || sum[0].ClosingQty != "0" {
		t.Fatalf("summary = %+v", sum)
	}
	if _, err := e.pool.Exec(context.Background(), "DELETE FROM inventory_transactions"); err == nil {
		t.Fatal("流水帳應不可刪除")
	}
	if _, err := e.pool.Exec(context.Background(), "UPDATE inventory_transactions SET qty = 1"); err == nil {
		t.Fatal("流水帳數量應不可修改")
	}
	if _, err := e.pool.Exec(context.Background(), "UPDATE inventory_transactions SET unit_cost = 5"); err != nil {
		t.Fatalf("月結應可回寫單位成本: %v", err)
	}
}

func TestNegativeStockRules(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	strict, loose := e.seedWarehouse("S", false), e.seedWarehouse("L", true)
	item, pcs := e.seedItem("P1", "goods"), e.unitID("PCS")

	d, _ := c.createDoc(adjust(strict, item, pcs, "-5"))
	res := c.postAll(&d)
	expect(t, res, http.StatusUnprocessableEntity, "INV-001")
	if d.Status != "approved" || e.balance(item, strict) != "0" {
		t.Fatalf("庫存不足時不應過帳: status=%s", d.Status)
	}
	d2, _ := c.createDoc(adjust(loose, item, pcs, "-5"))
	expect(t, c.postAll(&d2), http.StatusOK, "")
	if e.balance(item, loose) != "-5" {
		t.Fatalf("允許負庫存的倉庫應可為負: %s", e.balance(item, loose))
	}

	svc := e.seedItem("SVC", "service")
	_, res = c.createDoc(adjust(strict, svc, pcs, "1"))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	_, res = c.createDoc(adjust(strict, item, e.unitID("KG"), "1")) // 料品沒有此單位
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	_, res = c.createDoc(adjust(strict, item, pcs, "0"))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
}

func TestTransferMovesStock(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	a, b := e.seedWarehouse("A", false), e.seedWarehouse("B", false)
	item, pcs := e.seedItem("P1", "goods"), e.unitID("PCS")

	in, _ := c.createDoc(adjust(a, item, pcs, "30"))
	expect(t, c.postAll(&in), http.StatusOK, "")

	tr := map[string]any{
		"doc_type": "transfer", "doc_date": today, "warehouse_id": a, "to_warehouse_id": b,
		"lines": []map[string]any{{"item_id": item, "unit_id": e.unitID("BOX"), "qty": "2"}},
	}
	d, res := c.createDoc(tr)
	expect(t, res, http.StatusCreated, "")
	expect(t, c.postAll(&d), http.StatusOK, "")
	if e.balance(item, a) != "6" || e.balance(item, b) != "24" {
		t.Fatalf("調撥後 A=%s B=%s", e.balance(item, a), e.balance(item, b))
	}
	// 調出不足
	tr["lines"] = []map[string]any{{"item_id": item, "unit_id": pcs, "qty": "7"}}
	d2, _ := c.createDoc(tr)
	expect(t, c.postAll(&d2), http.StatusUnprocessableEntity, "INV-001")
	// 同倉調撥
	tr["to_warehouse_id"] = a
	_, res = c.createDoc(tr)
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
}

func TestStockCountFreezeAndPost(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	wh, pcs := e.seedWarehouse("A", false), e.unitID("PCS")
	p1, p2 := e.seedItem("P1", "goods"), e.seedItem("P2", "goods")
	for _, it := range []int64{p1, p2} {
		d, _ := c.createDoc(adjust(wh, it, pcs, "50"))
		expect(t, c.postAll(&d), http.StatusOK, "")
	}

	count, res := c.createDoc(map[string]any{"doc_type": "count", "doc_date": today, "warehouse_id": wh})
	expect(t, res, http.StatusCreated, "")
	if len(count.Lines) != 2 || *count.Lines[0].SystemQty != "50" || count.Lines[0].Qty != nil {
		t.Fatalf("盤點快照 = %+v", count.Lines)
	}
	// 盤點中:同倉庫的其他異動被凍結;不可再開第二張盤點
	other, _ := c.createDoc(adjust(wh, p1, pcs, "1"))
	expect(t, c.postAll(&other), http.StatusConflict, "INV-005")
	_, res = c.createDoc(map[string]any{"doc_type": "count", "doc_date": today, "warehouse_id": wh})
	expect(t, res, http.StatusConflict, "STK-005")

	// 未盤完不可送審
	expect(t, c.act(&count, "submit"), http.StatusUnprocessableEntity, "STK-002")
	// 不可刪除快照中的料品(否則盤虧可被隱藏)
	expect(t, c.do(http.MethodPut, "/inventory/documents/"+itoa(count.ID), map[string]any{
		"doc_type": "count", "doc_date": today, "warehouse_id": wh, "version": count.Version,
		"lines": []map[string]any{{"item_id": p1, "unit_id": pcs, "qty": "47"}},
	}), http.StatusUnprocessableEntity, "STK-006")
	res = c.do(http.MethodPut, "/inventory/documents/"+itoa(count.ID), map[string]any{
		"doc_type": "count", "doc_date": today, "warehouse_id": wh, "version": count.Version,
		"lines": []map[string]any{
			{"item_id": p1, "unit_id": pcs, "qty": "47"}, // 盤虧 3
			{"item_id": p2, "unit_id": pcs, "qty": "50"}, // 無差異
		},
	})
	expect(t, res, http.StatusOK, "")
	count = decode[stockDoc](t, res.Data)
	if *count.Lines[0].DiffQty != "-3" || *count.Lines[0].SystemQty != "50" {
		t.Fatalf("盤點差異 = %+v", count.Lines[0])
	}
	expect(t, c.postAll(&count), http.StatusOK, "")
	if e.balance(p1, wh) != "47" || e.balance(p2, wh) != "50" {
		t.Fatalf("盤點過帳後 p1=%s p2=%s", e.balance(p1, wh), e.balance(p2, wh))
	}
	// 盤點完成後解除凍結
	expect(t, c.act(&other, "post"), http.StatusOK, "")
	if e.balance(p1, wh) != "48" {
		t.Fatalf("解凍後過帳 p1=%s", e.balance(p1, wh))
	}
}

func TestStockDocumentPermissions(t *testing.T) {
	e := newEnv(t)
	clerk := e.seedRole("CLERK", "all", "inventory.stock.read", "inventory.stock.write")
	boss := e.seedRole("BOSS", "all", "inventory.stock.read", "inventory.stock.approve")
	e.seedUser("clerk", pw, false, false, clerk.ID)
	e.seedUser("boss", pw, false, false, boss.ID)
	wh, item, pcs := e.seedWarehouse("A", true), e.seedItem("P1", "goods"), e.unitID("PCS")
	ck, bs := e.loggedIn("clerk", pw), e.loggedIn("boss", pw)

	d, _ := ck.createDoc(adjust(wh, item, pcs, "5"))
	expect(t, ck.act(&d, "submit"), http.StatusOK, "")
	expect(t, ck.act(&d, "approve"), http.StatusForbidden, "SYS-403") // 開單者不可核准
	expect(t, bs.act(&d, "approve"), http.StatusOK, "")
	expect(t, bs.act(&d, "post"), http.StatusForbidden, "SYS-403") // 核准者不可過帳
	_, res := bs.createDoc(adjust(wh, item, pcs, "1"))
	expect(t, res, http.StatusForbidden, "SYS-403") // 核准者不可開單

	// 草稿可由開單者作廢;樂觀鎖:用舊版本操作失敗
	d2, _ := ck.createDoc(adjust(wh, item, pcs, "1"))
	stale := d2
	expect(t, ck.act(&d2, "void"), http.StatusOK, "")
	expect(t, ck.act(&stale, "submit"), http.StatusConflict, "SYS-409")
	expect(t, ck.act(&d2, "nope"), http.StatusNotFound, "SYS-404")
}

func TestItemBaseUnitLockedAfterMovement(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	wh, item := e.seedWarehouse("A", false), e.seedItem("P1", "goods")
	d, _ := c.createDoc(adjust(wh, item, e.unitID("PCS"), "1"))
	expect(t, c.postAll(&d), http.StatusOK, "")

	cur := decode[idVer](t, c.do(http.MethodGet, "/masterdata/items/"+itoa(item), nil).Data)
	expect(t, c.do(http.MethodPut, "/masterdata/items/"+itoa(item), map[string]any{
		"code": "P1", "name": "P1", "item_type": "goods", "base_unit_id": e.unitID("KG"),
		"safety_stock": "0", "list_price": "0", "is_active": true, "version": cur.Version,
	}), http.StatusConflict, "ITEM-003")
}

func TestLedgerAndBalancesReports(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	wh, item, pcs := e.seedWarehouse("A", false), e.seedItem("P1", "goods"), e.unitID("PCS")
	for _, x := range []struct{ date, qty string }{{"2026-09-20", "10"}, {"2026-10-02", "5"}, {"2026-10-05", "-3"}} {
		body := adjust(wh, item, pcs, x.qty)
		body["doc_date"] = x.date
		d, _ := c.createDoc(body)
		expect(t, c.postAll(&d), http.StatusOK, "")
	}
	led := decode[struct {
		OpeningQty string `json:"opening_qty"`
		Entries    []struct {
			Qty        string `json:"qty"`
			BalanceQty string `json:"balance_qty"`
		} `json:"entries"`
	}](t, c.do(http.MethodGet, "/inventory/items/"+itoa(item)+"/ledger?from=2026-10-01&to=2026-10-31", nil).Data)
	if led.OpeningQty != "10" || len(led.Entries) != 2 || led.Entries[1].BalanceQty != "12" {
		t.Fatalf("ledger = %+v", led)
	}
	sum := decode[[]struct {
		OpeningQty string `json:"opening_qty"`
		InQty      string `json:"in_qty"`
		OutQty     string `json:"out_qty"`
		ClosingQty string `json:"closing_qty"`
	}](t, c.do(http.MethodGet, "/inventory/movement-summary?from=2026-10-01&to=2026-10-31", nil).Data)
	if sum[0].OpeningQty != "10" || sum[0].InQty != "5" || sum[0].OutQty != "3" || sum[0].ClosingQty != "12" {
		t.Fatalf("summary = %+v", sum)
	}
	if _, err := e.pool.Exec(context.Background(), "UPDATE items SET safety_stock = 20 WHERE id = $1", item); err != nil {
		t.Fatal(err)
	}
	bal := decode[[]struct {
		Qty         string `json:"qty"`
		BelowSafety bool   `json:"below_safety"`
	}](t, c.do(http.MethodGet, "/inventory/balances?below_safety=true", nil).Data)
	if len(bal) != 1 || bal[0].Qty != "12" || !bal[0].BelowSafety {
		t.Fatalf("balances = %+v", bal)
	}
}

// 列鎖必要性:交易 1 扣 3 未提交時,交易 2 也扣 3。
// 有 FOR UPDATE 時交易 2 必須等待,交易 1 提交後讀到剩餘 2 而失敗;
// 若沒有列鎖,交易 2 會讀到舊的 5 並成功,造成超賣。
func TestPostingWaitsForRowLock(t *testing.T) {
	e := newEnv(t)
	wh, item := e.seedWarehouse("A", false), e.seedItem("P1", "goods")
	store := database.NewStore(e.pool)
	ctx := context.Background()
	opt := inventory.Options{CompanyID: 1}
	src := func(i int) inventory.Source {
		return inventory.Source{Type: "test", ID: int64(i), No: "T" + itoa(int64(i)), DocDate: time.Now()}
	}
	minus3 := []inventory.Movement{{ItemID: item, WarehouseID: wh, Qty: decimal.NewFromInt(-3)}}
	if err := store.InTx(ctx, func(q *db.Queries) error {
		return inventory.Post(ctx, q, opt, src(0), []inventory.Movement{{ItemID: item, WarehouseID: wh, Qty: decimal.NewFromInt(5)}})
	}); err != nil {
		t.Fatal(err)
	}

	tx1, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := inventory.Post(ctx, db.New(tx1), opt, src(1), minus3); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- store.InTx(ctx, func(q *db.Queries) error { return inventory.Post(ctx, q, opt, src(2), minus3) })
	}()
	select {
	case err := <-done:
		t.Fatalf("交易 2 應等待交易 1 釋放列鎖,卻已結束: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx1.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	err = <-done
	if e := apperrCode(err); e != "INV-001" {
		t.Fatalf("交易 2 應因庫存不足失敗,got %v", err)
	}
	if got := e.balance(item, wh); got != "2" {
		t.Fatalf("結存 = %s,應為 2(未超賣)", got)
	}
}

// 併發扣庫存:現有 5,同時 20 個交易各扣 1,不允許負庫存 → 恰好 5 個成功、結存 0。
func TestConcurrentPostingNeverOversells(t *testing.T) {
	e := newEnv(t)
	wh, item := e.seedWarehouse("A", false), e.seedItem("P1", "goods")
	store := database.NewStore(e.pool)
	ctx := context.Background()
	opt := inventory.Options{CompanyID: 1}
	src := func(i int) inventory.Source {
		return inventory.Source{Type: "test", ID: int64(i), No: "T" + itoa(int64(i)), DocDate: time.Now()}
	}
	if err := store.InTx(ctx, func(q *db.Queries) error {
		return inventory.Post(ctx, q, opt, src(0), []inventory.Movement{{ItemID: item, WarehouseID: wh, Qty: decimal.NewFromInt(5)}})
	}); err != nil {
		t.Fatal(err)
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
		ok int
	)
	for i := 1; i <= 20; i++ {
		wg.Go(func() {
			err := store.InTx(ctx, func(q *db.Queries) error {
				return inventory.Post(ctx, q, opt, src(i), []inventory.Movement{{ItemID: item, WarehouseID: wh, Qty: decimal.NewFromInt(-1)}})
			})
			if err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if ok != 5 || e.balance(item, wh) != "0" {
		t.Fatalf("成功 %d 筆,結存 %s;應為 5 筆、0", ok, e.balance(item, wh))
	}
}
