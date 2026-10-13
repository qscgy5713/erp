package app

import (
	"context"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"
)

type scDoc struct {
	ID          int64  `json:"id"`
	Status      string `json:"status"`
	Version     int32  `json:"version"`
	ReceivedQty string `json:"received_qty"`
	ScWarehouse int64  `json:"sc_warehouse_id"`
	CanClose    bool   `json:"can_close"`
	Lines       []struct {
		ItemID      int64  `json:"item_id"`
		Qty         string `json:"qty"`
		IssuedQty   string `json:"issued_qty"`
		ConsumedQty string `json:"consumed_qty"`
		HeldQty     string `json:"held_qty"`
	} `json:"lines"`
	Txns []struct {
		ID     int64  `json:"id"`
		Kind   string `json:"kind"`
		Status string `json:"status"`
	} `json:"txns"`
}

const prodSC = "/production/subcontracts"

type scMrpRow struct {
	ItemID     int64  `json:"item_id"`
	IncomingWO string `json:"incoming_wo"`
	WoDemand   string `json:"wo_demand"`
}

func (c *client) scGet(id int64) scDoc {
	c.e.t.Helper()
	res := c.do(http.MethodGet, prodSC+"/"+itoa(id), nil)
	expect(c.e.t, res, http.StatusOK, "")
	return decode[scDoc](c.e.t, res.Data)
}

func scLines(pairs ...any) []map[string]any {
	out := []map[string]any{}
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, map[string]any{"item_id": pairs[i], "qty": pairs[i+1]})
	}
	return out
}

func TestSubcontract(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	raw1, raw2, fg := p.item, e.seedItem("RAW2", "goods"), e.seedItem("FG1", "goods")
	recvWH := e.seedWarehouse("B", false)
	p2 := *p
	p2.item = raw2
	p.receiveOn("2026-08-10", "5", "1200")  // RAW1 60 個 @100
	p2.receiveOn("2026-08-10", "2", "2400") // RAW2 24 個 @200
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(fg, "1", bl(raw1, "2"), bl(raw2, "1"))), http.StatusCreated, "")

	body := map[string]any{"doc_date": "2026-09-10", "supplier_id": p.sup, "item_id": fg, "plan_qty": "10", "unit_fee": "50",
		"issue_warehouse_id": p.wh, "receive_warehouse_id": recvWH}
	res := c.do(http.MethodPost, prodSC, body)
	expect(t, res, http.StatusCreated, "")
	sc := decode[scDoc](t, res.Data)
	if len(sc.Lines) != 2 || sc.Lines[0].Qty != "20" || sc.Lines[1].Qty != "10" {
		t.Fatalf("依 BOM 展開的預計用量 = %+v", sc.Lines)
	}
	var kind string
	if err := e.pool.QueryRow(context.Background(), "SELECT kind FROM warehouses WHERE id = $1", sc.ScWarehouse).Scan(&kind); err != nil || kind != "subcontract" {
		t.Fatalf("委外倉 kind = %q err=%v", kind, err)
	}
	// 委外倉不能當發料倉
	bad := map[string]any{}
	for k, v := range body {
		bad[k] = v
	}
	bad["issue_warehouse_id"] = sc.ScWarehouse
	expect(t, c.do(http.MethodPost, prodSC, bad), http.StatusUnprocessableEntity, "SYS-422")

	// 一般調撥單不能把貨轉進委外倉(只有委外發料 / 收回 / 退料可以)
	tr, res := c.createDoc(map[string]any{
		"doc_type": "transfer", "doc_date": "2026-09-05", "warehouse_id": p.wh, "to_warehouse_id": sc.ScWarehouse,
		"lines": []map[string]any{{"item_id": raw1, "unit_id": e.unitID("PCS"), "qty": "1"}},
	})
	expect(t, res, http.StatusCreated, "")
	expect(t, c.postAll(&tr), http.StatusUnprocessableEntity, "INV-021")

	stock := func(item, wh int64) decimal.Decimal {
		var q decimal.Decimal
		if err := e.pool.QueryRow(context.Background(), "SELECT COALESCE(sum(qty), 0) FROM inventory_balances WHERE item_id = $1 AND warehouse_id = $2", item, wh).Scan(&q); err != nil {
			t.Fatal(err)
		}
		return q
	}
	wantStock := func(item, wh int64, want string) {
		t.Helper()
		if got := stock(item, wh); !got.Equal(decimal.RequireFromString(want)) {
			t.Fatalf("料品 %d 倉 %d 庫存 = %s,預期 %s", item, wh, got, want)
		}
	}

	// MRP:委外單未收回的成品算在途,還沒發給加工廠的材料算需求
	mrp := func() map[int64]scMrpRow {
		res := c.do(http.MethodGet, "/production/mrp?shortage_only=false", nil)
		expect(t, res, http.StatusOK, "")
		out := map[int64]scMrpRow{}
		for _, r := range decode[[]scMrpRow](t, res.Data) {
			out[r.ItemID] = r
		}
		return out
	}
	if r := mrp(); r[fg].IncomingWO != "10" || r[raw1].WoDemand != "20" || r[raw2].WoDemand != "10" {
		t.Fatalf("發料前 MRP = %+v", r)
	}

	// 發料:材料轉入委外倉;日期早於委外單不行
	issue := map[string]any{"doc_date": "2026-09-11", "lines": scLines(raw1, "20", raw2, "10")}
	expect(t, c.do(http.MethodPost, prodSC+"/"+itoa(sc.ID)+"/issue", map[string]any{"doc_date": "2026-09-01", "lines": scLines(raw1, "1")}), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, c.do(http.MethodPost, prodSC+"/"+itoa(sc.ID)+"/issue", issue), http.StatusCreated, "")
	wantStock(raw1, p.wh, "40")
	wantStock(raw1, sc.ScWarehouse, "20")
	wantStock(raw2, sc.ScWarehouse, "10")
	if r := mrp(); r[fg].IncomingWO != "10" || r[raw1].WoDemand != "0" {
		t.Fatalf("發料後 MRP = %+v", r)
	}

	// 已發料的材料不能從預計用量拿掉;改倉庫也不行
	sc = c.scGet(sc.ID)
	upd := map[string]any{}
	for k, v := range body {
		upd[k] = v
	}
	upd["version"], upd["lines"] = sc.Version, scLines(raw1, "20")
	expect(t, c.do(http.MethodPut, prodSC+"/"+itoa(sc.ID), upd), http.StatusUnprocessableEntity, "SYS-422")
	upd["lines"], upd["plan_qty"] = scLines(raw1, "20", raw2, "10"), "12"
	expect(t, c.do(http.MethodPut, prodSC+"/"+itoa(sc.ID), upd), http.StatusOK, "")
	upd["plan_qty"], upd["version"] = "10", sc.Version+1
	expect(t, c.do(http.MethodPut, prodSC+"/"+itoa(sc.ID), upd), http.StatusOK, "")

	// 收回:超量、耗料超過還在加工廠的都擋下
	recv := func(date, qty string, extra map[string]any) apiResp {
		b := map[string]any{"doc_date": date, "qty": qty}
		for k, v := range extra {
			b[k] = v
		}
		return c.do(http.MethodPost, prodSC+"/"+itoa(sc.ID)+"/receive", b)
	}
	expect(t, recv("2026-09-15", "11", nil), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, recv("2026-09-15", "4", map[string]any{"lines": scLines(raw1, "21", raw2, "4")}), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, recv("2026-09-15", "4", nil), http.StatusCreated, "") // 預設耗料 8 / 4,加工費 4 × 50 = 200
	wantStock(fg, recvWH, "4")
	wantStock(raw1, sc.ScWarehouse, "12")
	var fee decimal.Decimal
	if err := e.pool.QueryRow(context.Background(), "SELECT amount FROM accounts_payable WHERE source_type = 'subcontract_receive'").Scan(&fee); err != nil || !fee.Equal(decimal.NewFromInt(200)) {
		t.Fatalf("應付加工費 = %s err=%v", fee, err)
	}
	sc = c.scGet(sc.ID)
	if sc.ReceivedQty != "4" || sc.Lines[0].HeldQty != "12" || sc.Lines[0].ConsumedQty != "8" || sc.Status != "open" {
		t.Fatalf("收回後 = %+v", sc)
	}

	// 已被耗用的發料不能作廢;退料超過還在加工廠的不行
	var issueTxn int64
	for _, tx := range sc.Txns {
		if tx.Kind == "issue" {
			issueTxn = tx.ID
		}
	}
	if res := c.do(http.MethodPost, prodSC+"/"+itoa(sc.ID)+"/txns/"+itoa(issueTxn)+"/void", nil); res.status == http.StatusOK {
		t.Fatal("材料已被耗用,發料不應能作廢")
	}
	expect(t, c.do(http.MethodPost, prodSC+"/"+itoa(sc.ID)+"/return", map[string]any{"doc_date": "2026-09-16", "lines": scLines(raw1, "13")}), http.StatusUnprocessableEntity, "SYS-422")
	// 還有材料在加工廠,不能結案;有收回紀錄所以也不能整張作廢
	expect(t, c.do(http.MethodPost, prodSC+"/"+itoa(sc.ID)+"/close", map[string]any{"version": sc.Version}), http.StatusConflict, "SUB-005")
	expect(t, c.do(http.MethodPost, prodSC+"/"+itoa(sc.ID)+"/void", map[string]any{"version": sc.Version}), http.StatusConflict, "SUB-003")

	// 收回第二批(剩 6):耗用 12 / 6,但材料還在加工廠 12 / 6 → 收完且材料用盡,自動結案
	expect(t, recv("2026-09-18", "6", map[string]any{"processing_fee": "330"}), http.StatusCreated, "")
	sc = c.scGet(sc.ID)
	if sc.Status != "closed" || sc.ReceivedQty != "10" {
		t.Fatalf("收完應自動結案: %+v", sc)
	}
	// 作廢第二批 → 重新開啟,應付移除
	var second int64
	for _, tx := range sc.Txns {
		if tx.Kind == "receive" {
			second = tx.ID
		}
	}
	expect(t, c.do(http.MethodPost, prodSC+"/"+itoa(sc.ID)+"/txns/"+itoa(second)+"/void", nil), http.StatusOK, "")
	sc = c.scGet(sc.ID)
	if sc.Status != "open" || sc.ReceivedQty != "4" {
		t.Fatalf("作廢後 = %+v", sc)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(), "SELECT count(*) FROM accounts_payable WHERE source_type = 'subcontract_receive'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("應付筆數 = %d err=%v", n, err)
	}
	wantStock(fg, recvWH, "4")
	wantStock(raw1, sc.ScWarehouse, "12")

	// 短交:退回剩餘材料後結案
	expect(t, c.do(http.MethodPost, prodSC+"/"+itoa(sc.ID)+"/return", map[string]any{"doc_date": "2026-09-20", "lines": scLines(raw1, "12", raw2, "6")}), http.StatusCreated, "")
	wantStock(raw1, sc.ScWarehouse, "0")
	wantStock(raw1, p.wh, "52")
	sc = c.scGet(sc.ID)
	if !sc.CanClose || sc.Status != "open" {
		t.Fatalf("退料後應可結案: %+v", sc)
	}
	expect(t, c.do(http.MethodPost, prodSC+"/"+itoa(sc.ID)+"/close", map[string]any{"version": sc.Version}), http.StatusOK, "")
	expect(t, recv("2026-09-21", "1", nil), http.StatusConflict, "SUB-001")

	// 月結:發料 / 退料是調撥不影響成本;成品 = (8 × 100 + 4 × 200 + 加工費 200) ÷ 4 = 450
	expect(t, c.do(http.MethodPost, "/costing/closings/2026-08/run", nil), http.StatusOK, "")
	res = c.do(http.MethodPost, "/costing/closings/2026-09/run", nil)
	expect(t, res, http.StatusOK, "")
	// RAW1 52 × 100 + RAW2 20 × 200 + FG 4 × 450 = 5200 + 4000 + 1800
	if got := decode[costingDTO](t, res.Data); got.CogsAmount != "0" || got.InventoryValue != "11000" {
		t.Fatalf("9 月月結 = %+v", got)
	}
	var unit decimal.Decimal
	if err := e.pool.QueryRow(context.Background(), "SELECT unit_cost FROM inventory_transactions WHERE source_type = 'sc_receive_receipt' AND qty > 0 ORDER BY id LIMIT 1").Scan(&unit); err != nil || !unit.Equal(decimal.NewFromInt(450)) {
		t.Fatalf("收回成品單位成本 = %s err=%v", unit, err)
	}
	m := c.trialRange("2026-09-01", "2026-09-30")
	wantTrial(t, m, "5103", "0", "200")
	rec := decode[struct {
		Checks []struct {
			Key    string `json:"key"`
			Status string `json:"status"`
		} `json:"checks"`
	}](t, c.do(http.MethodGet, "/costing/reconcile", nil).Data)
	for _, ch := range rec.Checks {
		if ch.Status != "ok" {
			t.Fatalf("對帳檢查 %s = %s", ch.Key, ch.Status)
		}
	}
}

func TestSubcontractPermissions(t *testing.T) {
	p := newPurCtx(t)
	e := p.e
	e.seedUser("viewer", pw, false, false)
	v := e.loggedIn("viewer", pw)
	expect(t, v.do(http.MethodGet, prodSC, nil), http.StatusForbidden, "SYS-403")
	expect(t, v.do(http.MethodPost, prodSC, map[string]any{}), http.StatusForbidden, "SYS-403")
}
