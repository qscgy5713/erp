package app

import (
	"context"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"
)

func TestWorkOrderWip(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	raw1, raw2, fg := p.item, e.seedItem("RAW2", "goods"), e.seedItem("FG1", "goods")
	p2 := *p
	p2.item = raw2
	p.receiveOn("2026-08-10", "5", "1200")  // RAW1 60 個 @100
	p2.receiveOn("2026-08-10", "2", "2400") // RAW2 24 個 @200
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(fg, "1", bl(raw1, "2"), bl(raw2, "1"))), http.StatusCreated, "")
	wo := decode[woDoc](t, c.do(http.MethodPost, prodWO, woBody(fg, "10", p.wh, "0", nil)).Data)
	wip := prodWO + "/" + itoa(wo.ID)

	// 還沒核准不能領料
	expect(t, c.do(http.MethodPost, wip+"/wip-issue", map[string]any{"doc_date": "2026-09-11", "lines": scLines(raw1, "20")}), http.StatusConflict, "PRD-006")
	for _, a := range []string{"submit", "approve"} {
		expect(t, c.woAct(&wo, a), http.StatusOK, "")
	}
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
	var wipWH int64
	issue := func(date string, lines []map[string]any) apiResp {
		return c.do(http.MethodPost, wip+"/wip-issue", map[string]any{"doc_date": date, "lines": lines})
	}
	expect(t, issue("2026-09-11", scLines(raw1, "20", raw2, "10")), http.StatusCreated, "")
	if err := e.pool.QueryRow(context.Background(), "SELECT id FROM warehouses WHERE kind = 'wip'").Scan(&wipWH); err != nil {
		t.Fatalf("在製品倉未建立: %v", err)
	}
	wantStock(raw1, p.wh, "40")
	wantStock(raw1, wipWH, "20")
	// 在製品倉不能當工單的倉庫
	expect(t, c.do(http.MethodPost, prodWO, woBody(fg, "1", wipWH, "0", map[string]any{"lines": []map[string]any{bl(raw1, "1")}})), http.StatusUnprocessableEntity, "SYS-422")

	type wipView struct {
		Status string `json:"status"`
		Wip    struct {
			Held []struct {
				ItemID int64  `json:"item_id"`
				Qty    string `json:"qty"`
			} `json:"held"`
			Txns []struct {
				ID   int64  `json:"id"`
				Kind string `json:"kind"`
			} `json:"txns"`
		} `json:"wip"`
		Version int32 `json:"version"`
	}
	get := func() wipView {
		res := c.do(http.MethodGet, wip, nil)
		expect(t, res, http.StatusOK, "")
		return decode[wipView](t, res.Data)
	}
	if v := get(); len(v.Wip.Held) != 2 || v.Wip.Held[0].Qty != "20" {
		t.Fatalf("在製品 = %+v", v.Wip)
	}
	// MRP:領出去的材料不再算工單需求
	res := c.do(http.MethodGet, "/production/mrp?shortage_only=false", nil)
	expect(t, res, http.StatusOK, "")
	for _, r := range decode[[]scMrpRow](t, res.Data) {
		if r.ItemID == raw1 && r.WoDemand != "0" {
			t.Fatalf("領料後 RAW1 工單需求 = %s", r.WoDemand)
		}
	}
	// 有在製品的工單不能一次完工
	expect(t, c.woAct(&wo, "post"), http.StatusConflict, "PRD-011")
	// 一般調撥單不能進在製品倉
	tr, res := c.createDoc(map[string]any{
		"doc_type": "transfer", "doc_date": "2026-09-05", "warehouse_id": p.wh, "to_warehouse_id": wipWH,
		"lines": []map[string]any{{"item_id": raw1, "unit_id": e.unitID("PCS"), "qty": "1"}},
	})
	expect(t, res, http.StatusCreated, "")
	expect(t, c.postAll(&tr), http.StatusUnprocessableEntity, "INV-021")
	// 退料超過在製品的量
	expect(t, c.do(http.MethodPost, wip+"/wip-return", map[string]any{"doc_date": "2026-09-12", "lines": scLines(raw1, "21")}), http.StatusUnprocessableEntity, "SYS-422")

	// 完工 4:預設領料 8 / 4 全從在製品扣
	expect(t, c.woComplete(&wo, compBody("2026-09-12", "4", nil)), http.StatusCreated, "")
	wantStock(raw1, wipWH, "12")
	wantStock(raw1, p.wh, "40")
	// 完工 6:RAW1 用 14(在製品只剩 12,多的 2 從領料倉)、RAW2 用 6;在製品用完 → 自動結案
	expect(t, c.woComplete(&wo, compBody("2026-09-15", "6", map[string]any{"lines": scLines(raw1, "14", raw2, "6")})), http.StatusCreated, "")
	wantStock(raw1, wipWH, "0")
	wantStock(raw1, p.wh, "38")
	if v := get(); v.Status != "posted" || len(v.Wip.Held) != 0 {
		t.Fatalf("完工後 = %+v", v)
	}
	// 作廢第二批 → 重新開啟,在製品回復
	res = c.do(http.MethodGet, wip, nil)
	comps := decode[woDoc](t, res.Data).Completions
	expect(t, c.do(http.MethodPost, wip+"/completions/"+itoa(comps[1].ID)+"/void", nil), http.StatusOK, "")
	wantStock(raw1, wipWH, "12")
	wantStock(raw1, p.wh, "40")
	v := get()
	if v.Status != "approved" || len(v.Wip.Held) != 2 {
		t.Fatalf("作廢後 = %+v", v)
	}
	// 還有材料在在製品倉,不能短交結案;已被耗用的領料不能作廢
	expect(t, c.do(http.MethodPost, wip+"/close", map[string]any{"version": v.Version}), http.StatusConflict, "PRD-009")
	if res := c.do(http.MethodPost, wip+"/wip-txns/"+itoa(v.Wip.Txns[0].ID)+"/void", nil); res.status == http.StatusOK {
		t.Fatal("材料已被耗用,領料不應能作廢")
	}
	// 退料後結案
	expect(t, c.do(http.MethodPost, wip+"/wip-return", map[string]any{"doc_date": "2026-09-20", "lines": scLines(raw1, "12", raw2, "6")}), http.StatusCreated, "")
	wantStock(raw1, wipWH, "0")
	wantStock(raw1, p.wh, "52")
	v = get()
	expect(t, c.do(http.MethodPost, wip+"/close", map[string]any{"version": v.Version}), http.StatusOK, "")

	// 在製品清單:沒有進行中的在製品
	res = c.do(http.MethodGet, "/production/wip", nil)
	expect(t, res, http.StatusOK, "")
	if rows := decode[[]map[string]any](t, res.Data); len(rows) != 0 {
		t.Fatalf("在製品清單 = %v", rows)
	}

	// 月結:領料 / 退料是調撥不影響成本;成品 = (8 × 100 + 4 × 200) ÷ 4 = 400
	expect(t, c.do(http.MethodPost, "/costing/closings/2026-08/run", nil), http.StatusOK, "")
	res = c.do(http.MethodPost, "/costing/closings/2026-09/run", nil)
	expect(t, res, http.StatusOK, "")
	// RAW1 52 × 100 + RAW2 20 × 200 + FG 4 × 400
	if got := decode[costingDTO](t, res.Data); got.InventoryValue != "10800" {
		t.Fatalf("9 月月結 = %+v", got)
	}
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

func TestWipReport(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	raw1, fg := p.item, e.seedItem("FG1", "goods")
	p.receiveOn("2026-09-01", "5", "1200")
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(fg, "1", bl(raw1, "2"))), http.StatusCreated, "")
	wo := decode[woDoc](t, c.do(http.MethodPost, prodWO, woBody(fg, "10", p.wh, "0", nil)).Data)
	for _, a := range []string{"submit", "approve"} {
		expect(t, c.woAct(&wo, a), http.StatusOK, "")
	}
	expect(t, c.do(http.MethodPost, prodWO+"/"+itoa(wo.ID)+"/wip-issue", map[string]any{"doc_date": "2026-09-11", "lines": scLines(raw1, "20")}), http.StatusCreated, "")
	res := c.do(http.MethodGet, "/production/wip", nil)
	expect(t, res, http.StatusOK, "")
	rows := decode[[]struct {
		DocNo string `json:"doc_no"`
		Qty   string `json:"qty"`
	}](t, res.Data)
	if len(rows) != 1 || rows[0].DocNo != wo.DocNo || rows[0].Qty != "20" {
		t.Fatalf("在製品清單 = %+v", rows)
	}
	// 沒有權限的人不能領料
	e.seedUser("viewer", pw, false, false)
	v := e.loggedIn("viewer", pw)
	expect(t, v.do(http.MethodGet, "/production/wip", nil), http.StatusForbidden, "SYS-403")
	expect(t, v.do(http.MethodPost, prodWO+"/"+itoa(wo.ID)+"/wip-issue", map[string]any{}), http.StatusForbidden, "SYS-403")
}

// 月結把在製品倉的材料價值從存貨轉到在製品科目 1142;退回後下個月轉回
func TestWipAccount(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	raw1, fg := p.item, e.seedItem("FG1", "goods")
	p.receiveOn("2026-07-10", "5", "1200") // RAW1 60 個 @100
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(fg, "1", bl(raw1, "2"))), http.StatusCreated, "")
	wo := decode[woDoc](t, c.do(http.MethodPost, prodWO, woBody(fg, "10", p.wh, "0", map[string]any{"doc_date": "2026-07-05"})).Data)
	for _, a := range []string{"submit", "approve"} {
		expect(t, c.woAct(&wo, a), http.StatusOK, "")
	}
	wip := prodWO + "/" + itoa(wo.ID)
	expect(t, c.do(http.MethodPost, wip+"/wip-issue", map[string]any{"doc_date": "2026-07-11", "lines": scLines(raw1, "20")}), http.StatusCreated, "")

	expect(t, c.do(http.MethodPost, "/costing/closings/2026-07/run", nil), http.StatusOK, "")
	// 7 月底:存貨總值 6000,其中 20 個 × 100 = 2000 在在製品科目
	tb := c.trialRange("2026-07-01", "2026-07-31")
	if r := tb["1142"]; r.Closing != "2000" {
		t.Fatalf("1142 在製品 = %+v", r)
	}
	if r := tb["1141"]; r.Closing != "4000" {
		t.Fatalf("1141 存貨 = %+v", r)
	}

	// 8 月退料 → 月結後在製品科目歸零
	expect(t, c.do(http.MethodPost, wip+"/wip-return", map[string]any{"doc_date": "2026-08-05", "lines": scLines(raw1, "20")}), http.StatusCreated, "")
	expect(t, c.do(http.MethodPost, "/costing/closings/2026-08/run", nil), http.StatusOK, "")
	tb = c.trialRange("2026-08-01", "2026-08-31")
	if r := tb["1142"]; r.Closing != "0" && r.Closing != "0.00" {
		t.Fatalf("8 月 1142 在製品 = %+v", r)
	}
	if r := tb["1141"]; r.Closing != "6000" {
		t.Fatalf("8 月 1141 存貨 = %+v", r)
	}
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
