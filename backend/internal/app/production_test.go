package app

import (
	"context"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"

	"erp/internal/system/permission"
)

type woDoc struct {
	ID      int64  `json:"id"`
	DocNo   string `json:"doc_no"`
	Status  string `json:"status"`
	Version int32  `json:"version"`
	Lines   []struct {
		ItemID int64  `json:"item_id"`
		Qty    string `json:"qty"`
	} `json:"lines"`
	CompletedQty string `json:"completed_qty"`
	Completions  []struct {
		ID     int64  `json:"id"`
		DocNo  string `json:"doc_no"`
		Qty    string `json:"qty"`
		Status string `json:"status"`
	} `json:"completions"`
}

const prodWO = "/production/work-orders"

func (c *client) woAct(d *woDoc, action string) apiResp {
	c.e.t.Helper()
	res := c.do(http.MethodPost, prodWO+"/"+itoa(d.ID)+"/actions/"+action, map[string]any{"version": d.Version})
	if res.status == http.StatusOK {
		*d = decode[woDoc](c.e.t, res.Data)
	}
	return res
}

func (c *client) woPostAll(d *woDoc) apiResp {
	c.e.t.Helper()
	for _, a := range []string{"submit", "approve"} {
		if res := c.woAct(d, a); res.status != http.StatusOK {
			c.e.t.Fatalf("%s 失敗: %d %+v", a, res.status, res.Error)
		}
	}
	return c.woAct(d, "post")
}

func bomBody(item int64, yield string, lines ...map[string]any) map[string]any {
	return map[string]any{"item_id": item, "yield_qty": yield, "is_active": true, "lines": lines}
}

func bl(item int64, qty string) map[string]any { return map[string]any{"item_id": item, "qty": qty} }

func woBody(item int64, qty string, wh int64, processing string, extra map[string]any) map[string]any {
	b := map[string]any{"doc_date": "2026-09-10", "item_id": item, "plan_qty": qty, "warehouse_id": wh,
		"material_warehouse_id": wh, "processing_cost": processing}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

func TestBomRules(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	fg, raw := e.seedItem("FG1", "goods"), e.seedItem("RAW1", "goods")
	svc := e.seedItem("SVC", "service")

	// 驗證:沒有材料、成品是服務、材料是自己、重複、用量 0、產出量 0
	for name, body := range map[string]map[string]any{
		"沒有材料":  bomBody(fg, "1"),
		"服務成品":  bomBody(svc, "1", bl(raw, "1")),
		"服務材料":  bomBody(fg, "1", bl(svc, "1")),
		"材料是自己": bomBody(fg, "1", bl(fg, "1")),
		"材料重複":  bomBody(fg, "1", bl(raw, "1"), bl(raw, "2")),
		"用量為零":  bomBody(fg, "1", bl(raw, "0")),
		"產出量為零": bomBody(fg, "0", bl(raw, "1")),
	} {
		if res := c.do(http.MethodPost, "/production/boms", body); res.status != http.StatusUnprocessableEntity || res.code() != "SYS-422" {
			t.Errorf("%s → %d %s", name, res.status, res.code())
		}
	}
	res := c.do(http.MethodPost, "/production/boms", bomBody(fg, "10", bl(raw, "25")))
	expect(t, res, http.StatusCreated, "")
	bom := decode[struct {
		ID      int64 `json:"id"`
		Version int32 `json:"version"`
	}](t, res.Data)
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(fg, "1", bl(raw, "1"))), http.StatusConflict, "PRD-001")
	// 循環:RAW1 的 BOM 用到 FG1(FG1 → RAW1 → FG1)
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(raw, "1", bl(fg, "1"))), http.StatusUnprocessableEntity, "PRD-002")
	// 三層循環:A → B、B → C 之後 C → A 不可
	a, b, cc := e.seedItem("A", "goods"), e.seedItem("B", "goods"), e.seedItem("C", "goods")
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(a, "1", bl(b, "1"))), http.StatusCreated, "")
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(b, "1", bl(cc, "1"))), http.StatusCreated, "")
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(cc, "1", bl(a, "1"))), http.StatusUnprocessableEntity, "PRD-002")

	// 依 BOM 展開:25 ÷ 10 × 4 = 10;成品沒有 BOM 時報錯
	lines := decode[[]struct {
		ItemID int64  `json:"item_id"`
		Qty    string `json:"qty"`
	}](t, c.do(http.MethodGet, prodWO+"/explode?item_id="+itoa(fg)+"&qty=4", nil).Data)
	if len(lines) != 1 || lines[0].ItemID != raw || lines[0].Qty != "10" {
		t.Fatalf("explode = %+v", lines)
	}
	expect(t, c.do(http.MethodGet, prodWO+"/explode?item_id="+itoa(raw)+"&qty=4", nil), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, c.do(http.MethodGet, prodWO+"/explode?item_id="+itoa(fg)+"&qty=0", nil), http.StatusUnprocessableEntity, "SYS-422")

	// 損耗率:25 ÷ 10 × 4 × (1 + 10%) = 11;範圍 0 ≤ x < 100、最多 2 位小數
	sc := e.seedItem("SC1", "goods")
	scrapBom := func(pct string) apiResp {
		return c.do(http.MethodPost, "/production/boms", bomBody(sc, "10", map[string]any{"item_id": raw, "qty": "25", "scrap_pct": pct}))
	}
	for _, bad := range []string{"-1", "100", "1.234"} {
		expect(t, scrapBom(bad), http.StatusUnprocessableEntity, "SYS-422")
	}
	expect(t, scrapBom("10"), http.StatusCreated, "")
	sl := decode[[]struct {
		Qty string `json:"qty"`
	}](t, c.do(http.MethodGet, prodWO+"/explode?item_id="+itoa(sc)+"&qty=4", nil).Data)
	if len(sl) != 1 || sl[0].Qty != "11" {
		t.Fatalf("含損耗展開 = %+v", sl)
	}

	// 修改:版本衝突;成品不可更換;停用後不能開工單
	upd := func(version int32, item int64, active bool) apiResp {
		body := bomBody(item, "10", bl(raw, "25"))
		body["is_active"], body["version"] = active, version
		return c.do(http.MethodPut, "/production/boms/"+itoa(bom.ID), body)
	}
	expect(t, upd(bom.Version+5, fg, true), http.StatusConflict, "SYS-409")
	expect(t, upd(bom.Version, a, true), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, upd(bom.Version, fg, false), http.StatusOK, "")
	expect(t, c.do(http.MethodPost, prodWO, woBody(fg, "4", p.wh, "0", nil)), http.StatusUnprocessableEntity, "SYS-422")

	// 權限
	e.seedUser("nobody", pw, false, false)
	nb := e.loggedIn("nobody", pw)
	expect(t, nb.do(http.MethodGet, "/production/boms", nil), http.StatusForbidden, "SYS-403")
	rd := e.seedRole("bomreader", "all", permission.BomRead)
	e.seedUser("reader", pw, false, false, rd.ID)
	r := e.loggedIn("reader", pw)
	expect(t, r.do(http.MethodGet, "/production/boms", nil), http.StatusOK, "")
	expect(t, r.do(http.MethodPost, "/production/boms", bomBody(fg, "1", bl(raw, "1"))), http.StatusForbidden, "SYS-403")
	expect(t, c.do(http.MethodDelete, "/production/boms/"+itoa(bom.ID), nil), http.StatusNoContent, "")
}

func TestWorkOrderLifecycleAndCosting(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	raw1, raw2, fg := p.item, e.seedItem("RAW2", "goods"), e.seedItem("FG1", "goods")
	pcs := e.unitID("PCS")
	p2 := *p
	p2.item = raw2
	cust := e.seedCustomer("C1", "0", nil)

	// 8 月進料:RAW1 60 個 @100(5 箱 @1200),RAW2 24 個 @200(2 箱 @2400)
	p.receiveOn("2026-08-10", "5", "1200")
	p2.receiveOn("2026-08-10", "2", "2400")
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(fg, "1", bl(raw1, "2"), bl(raw2, "1"))), http.StatusCreated, "")

	// 開單:沒給明細時依 BOM 展開(10 個成品 → RAW1 20、RAW2 10)
	res := c.do(http.MethodPost, prodWO, woBody(fg, "10", p.wh, "500", nil))
	expect(t, res, http.StatusCreated, "")
	wo := decode[woDoc](t, res.Data)
	if wo.DocNo == "" || wo.Status != "draft" || len(wo.Lines) != 2 || wo.Lines[0].Qty != "20" || wo.Lines[1].Qty != "10" {
		t.Fatalf("工單 = %+v", wo)
	}
	// 驗證:材料是成品自己、重複材料、用量為零、加工費為負、倉庫不存在
	for name, extra := range map[string]map[string]any{
		"材料是成品": {"lines": []map[string]any{{"item_id": fg, "qty": "1"}}},
		"材料重複":  {"lines": []map[string]any{{"item_id": raw1, "qty": "1"}, {"item_id": raw1, "qty": "2"}}},
		"用量為零":  {"lines": []map[string]any{{"item_id": raw1, "qty": "0"}}},
		"加工費為負": {"processing_cost": "-1"},
		"倉庫不存在": {"warehouse_id": 99999},
	} {
		body := woBody(fg, "10", p.wh, "0", extra)
		if res := c.do(http.MethodPost, prodWO, body); res.status != http.StatusUnprocessableEntity || res.code() != "SYS-422" {
			t.Errorf("%s → %d %s", name, res.status, res.code())
		}
	}

	// 材料不足時完工失敗,庫存不變;補足後完工
	short := decode[woDoc](t, c.do(http.MethodPost, prodWO, woBody(fg, "10", p.wh, "0", map[string]any{
		"lines": []map[string]any{{"item_id": raw1, "qty": "999"}}})).Data)
	expect(t, c.woPostAll(&short), http.StatusUnprocessableEntity, "INV-001")
	if e.balance(raw1, p.wh) != "60" || e.balance(fg, p.wh) != "0" {
		t.Fatalf("材料不足後庫存 = %s / %s", e.balance(raw1, p.wh), e.balance(fg, p.wh))
	}
	expect(t, c.woAct(&short, "void"), http.StatusOK, "")

	// 完工:材料扣庫存、成品入庫(同一交易);只有草稿可修改
	expect(t, c.woPostAll(&wo), http.StatusOK, "")
	if e.balance(raw1, p.wh) != "40" || e.balance(raw2, p.wh) != "14" || e.balance(fg, p.wh) != "10" {
		t.Fatalf("完工後庫存 raw1=%s raw2=%s fg=%s", e.balance(raw1, p.wh), e.balance(raw2, p.wh), e.balance(fg, p.wh))
	}
	expect(t, c.do(http.MethodPut, prodWO+"/"+itoa(wo.ID), woBody(fg, "10", p.wh, "0", map[string]any{"version": wo.Version,
		"lines": []map[string]any{{"item_id": raw1, "qty": "1"}}})), http.StatusConflict, "PRD-004")

	// 反完工:兩邊都還原;再完工
	expect(t, c.woAct(&wo, "unpost"), http.StatusOK, "")
	if e.balance(raw1, p.wh) != "60" || e.balance(fg, p.wh) != "0" {
		t.Fatalf("反完工後 raw1=%s fg=%s", e.balance(raw1, p.wh), e.balance(fg, p.wh))
	}
	expect(t, c.woAct(&wo, "post"), http.StatusOK, "")

	// 成品出貨 4 個(單價 800);9 月月結
	dn, res := c.createPur(deliveries, map[string]any{
		"doc_date": "2026-09-20", "doc_type": "delivery", "customer_id": cust, "warehouse_id": p.wh, "currency": "TWD",
		"tax_type_id": p.tax, "lines": []map[string]any{line(fg, pcs, "4", "800", nil)},
	})
	expect(t, res, http.StatusCreated, "")
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")

	expect(t, c.do(http.MethodPost, "/costing/closings/2026-08/run", nil), http.StatusOK, "")
	res = c.do(http.MethodPost, "/costing/closings/2026-09/run", nil)
	expect(t, res, http.StatusOK, "")
	// 完工成本 = 材料(RAW1 20×100 + RAW2 10×200)+ 加工費 500 = 4500 → 單位成本 450;出貨 4 個 COGS 1800
	// 存貨 = RAW1 40×100 + RAW2 14×200 + FG 6×450 = 4000 + 2800 + 2700 = 9500
	if got := decode[costingDTO](t, res.Data); got.CogsAmount != "1800" || got.AdjustAmount != "0" || got.InventoryValue != "9500" {
		t.Fatalf("9 月月結 = %+v", got)
	}
	items := decode[[]struct {
		ItemCode     string `json:"item_code"`
		AvgCost      string `json:"avg_cost"`
		ConsumeQty   string `json:"consume_qty"`
		ConsumeValue string `json:"consume_value"`
		PurchaseQty  string `json:"purchase_qty"`
		ClosingValue string `json:"closing_value"`
	}](t, c.do(http.MethodGet, "/costing/closings/2026-09/items", nil).Data)
	got := map[string][5]string{}
	for _, it := range items {
		got[it.ItemCode] = [5]string{it.AvgCost, it.ConsumeQty, it.ConsumeValue, it.PurchaseQty, it.ClosingValue}
	}
	if got["FG1"] != [5]string{"450", "0", "0", "10", "2700"} || got["P1"][0] != "100" || got["P1"][1] != "-20" || got["P1"][2] != "2000" || got["RAW2"][2] != "2000" {
		t.Fatalf("料品成本 = %v", got)
	}
	// 傳票:銷貨成本 1800;加工費 500 由「加工費轉出」轉入存貨。材料與成品之間的轉換不產生傳票(存貨總額不變)
	m := c.trialRange("2026-09-01", "2026-09-30")
	wantTrial(t, m, "5101", "1800", "0")
	wantTrial(t, m, "5103", "0", "500")
	wantTrial(t, m, "1141", "500", "1800")
	// 完工入庫的單位成本回寫;領料的流水帳帶上材料平均成本
	var recvCost, issueCost decimal.Decimal
	if err := e.pool.QueryRow(context.Background(), "SELECT unit_cost FROM inventory_transactions WHERE source_type = 'work_order_receipt' AND reversal_of IS NULL AND qty > 0 ORDER BY id DESC LIMIT 1").Scan(&recvCost); err != nil || !recvCost.Equal(decimal.NewFromInt(450)) {
		t.Fatalf("完工入庫單位成本 = %s err=%v", recvCost, err)
	}
	if err := e.pool.QueryRow(context.Background(), "SELECT unit_cost FROM inventory_transactions WHERE source_type = 'work_order_issue' AND item_id = $1 AND qty < 0", raw2).Scan(&issueCost); err != nil || !issueCost.Equal(decimal.NewFromInt(200)) {
		t.Fatalf("領料單位成本 = %s err=%v", issueCost, err)
	}
	// 標準成本與差異(D91):實際 450 vs 標準 400 → 差異 +500(+12.5%);未設標準、未月結的月份給對應狀態
	type varRow struct {
		ItemID       int64   `json:"item_id"`
		ProducedQty  string  `json:"produced_qty"`
		ActualUnit   *string `json:"actual_unit"`
		StandardUnit *string `json:"standard_unit"`
		Variance     *string `json:"variance"`
		VariancePct  *string `json:"variance_pct"`
		Status       string  `json:"status"`
	}
	variance := func(period string) []varRow {
		res := c.do(http.MethodGet, "/costing/variance?period="+period, nil)
		expect(t, res, http.StatusOK, "")
		return decode[struct {
			Rows []varRow `json:"rows"`
		}](t, res.Data).Rows
	}
	if v := variance("2026-09"); len(v) != 1 || v[0].ItemID != fg || v[0].ProducedQty != "10" || *v[0].ActualUnit != "450" || v[0].Status != "no_standard" || v[0].Variance != nil {
		t.Fatalf("未設標準成本 = %+v", v)
	}
	stdPath := "/costing/standard-costs/" + itoa(fg)
	expect(t, c.do(http.MethodPut, stdPath, map[string]any{"standard_cost": "-1"}), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, c.do(http.MethodPut, "/costing/standard-costs/99999", map[string]any{"standard_cost": "1"}), http.StatusNotFound, "SYS-404")
	expect(t, c.do(http.MethodPut, stdPath, map[string]any{"standard_cost": "400"}), http.StatusNoContent, "")
	if v := variance("2026-09"); len(v) != 1 || v[0].Status != "ok" || *v[0].StandardUnit != "400" || *v[0].Variance != "500" || *v[0].VariancePct != "12.5" {
		t.Fatalf("差異 = %+v", v)
	}
	if v := variance("2026-10"); len(v) != 0 {
		t.Fatalf("沒有完工的月份應為空,got %+v", v)
	}
	// 差異拆解(D97):材料標準價 RAW1 90、RAW2 210;9/30 起新 BOM 的 RAW1 標準用量 1.5(每 1 成品)
	// 標準材料 = 15×90 + 10×210 = 3450 → 標準加工費 4000 − 3450 = 550
	// 用量差異 = (20−15)×90 = 450;價格差異 = (2000−20×90) + (2000−10×210) = 200 − 100 = 100;加工費差異 = 500 − 550 = −50;合計 500
	expect(t, c.do(http.MethodPut, "/costing/standard-costs/"+itoa(raw1), map[string]any{"standard_cost": "90"}), http.StatusNoContent, "")
	expect(t, c.do(http.MethodPut, "/costing/standard-costs/"+itoa(raw2), map[string]any{"standard_cost": "210"}), http.StatusNoContent, "")
	nb := bomBody(fg, "1", bl(raw1, "1.5"), bl(raw2, "1"))
	nb["effective_from"] = "2026-09-30"
	expect(t, c.do(http.MethodPost, "/production/boms", nb), http.StatusCreated, "")
	type bdRow struct {
		Breakdown *struct {
			QtyVariance        string `json:"qty_variance"`
			PriceVariance      string `json:"price_variance"`
			ProcessingVariance string `json:"processing_variance"`
			StandardProcessing string `json:"standard_processing"`
			ActualProcessing   string `json:"actual_processing"`
			Materials          []struct {
				MaterialID    int64   `json:"material_id"`
				StandardQty   string  `json:"standard_qty"`
				ActualQty     string  `json:"actual_qty"`
				StandardPrice *string `json:"standard_price"`
				ActualPrice   *string `json:"actual_price"`
				QtyVariance   string  `json:"qty_variance"`
				PriceVariance string  `json:"price_variance"`
			} `json:"materials"`
		} `json:"breakdown"`
	}
	bdRes := decode[struct {
		Rows []bdRow `json:"rows"`
	}](t, c.do(http.MethodGet, "/costing/variance?period=2026-09", nil).Data)
	bd := bdRes.Rows[0].Breakdown
	if bd == nil || bd.QtyVariance != "450" || bd.PriceVariance != "100" || bd.ProcessingVariance != "-50" || bd.StandardProcessing != "550" || bd.ActualProcessing != "500" || len(bd.Materials) != 2 {
		t.Fatalf("差異拆解 = %+v", bd)
	}
	for _, m := range bd.Materials {
		switch m.MaterialID {
		case raw1:
			if m.StandardQty != "15" || m.ActualQty != "20" || *m.StandardPrice != "90" || *m.ActualPrice != "100" || m.QtyVariance != "450" || m.PriceVariance != "200" {
				t.Fatalf("RAW1 拆解 = %+v", m)
			}
		case raw2:
			if m.StandardQty != "10" || m.ActualQty != "10" || m.QtyVariance != "0" || m.PriceVariance != "-100" {
				t.Fatalf("RAW2 拆解 = %+v", m)
			}
		}
	}
	expect(t, c.do(http.MethodPut, "/costing/standard-costs/"+itoa(raw1), map[string]any{"standard_cost": nil}), http.StatusNoContent, "")
	expect(t, c.do(http.MethodPut, "/costing/standard-costs/"+itoa(raw2), map[string]any{"standard_cost": nil}), http.StatusNoContent, "")
	expect(t, c.do(http.MethodPut, stdPath, map[string]any{"standard_cost": nil}), http.StatusNoContent, "")
	if v := variance("2026-09"); v[0].Status != "no_standard" {
		t.Fatalf("清除後 = %+v", v)
	}
	expect(t, c.do(http.MethodGet, "/costing/variance?period=bad", nil), http.StatusUnprocessableEntity, "SYS-422")
	// 對帳檢查全數通過(存貨子帳 9500 = 總帳)
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

	// 月結後該月庫存鎖定:不可反完工;成品被出貨後(取消月結前)也不可;BOM 有工單後不可刪除
	expect(t, c.woAct(&wo, "unpost"), http.StatusConflict, "INV-008")
	bomList := decode[[]struct {
		ID int64 `json:"id"`
	}](t, c.do(http.MethodGet, "/production/boms", nil).Data)
	expect(t, c.do(http.MethodDelete, "/production/boms/"+itoa(bomList[0].ID), nil), http.StatusConflict, "PRD-003")
	// 取消月結後回到未計成本,可再反完工(成品已出貨 4 個,只剩 6 個 → 反完工入庫沖銷 10 會庫存不足)
	expect(t, c.do(http.MethodPost, "/costing/closings/2026-09/cancel", nil), http.StatusOK, "")
	expect(t, c.woAct(&wo, "unpost"), http.StatusUnprocessableEntity, "INV-001")
}

func TestWorkOrderPermissionsAndLots(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	fg, milk := e.seedItem("FG1", "goods"), e.seedItem("MILK", "goods")
	e.setLotControl(milk, "lot_expiry")
	e.setLotControl(fg, "lot_expiry")
	pcs := e.unitID("PCS")
	expect(t, c.postLot(p.wh, milk, pcs, "30", "M-OLD", "2026-12-01"), http.StatusOK, "")
	expect(t, c.postLot(p.wh, milk, pcs, "30", "M-NEW", "2027-03-01"), http.StatusOK, "")

	// 成品是批號管理:缺批號 / 缺效期被擋
	lines := []map[string]any{{"item_id": milk, "qty": "40"}}
	expect(t, c.do(http.MethodPost, prodWO, woBody(fg, "5", p.wh, "0", map[string]any{"lines": lines})), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, c.do(http.MethodPost, prodWO, woBody(fg, "5", p.wh, "0", map[string]any{"lines": lines, "output_lot_no": "F1"})), http.StatusUnprocessableEntity, "SYS-422")
	res := c.do(http.MethodPost, prodWO, woBody(fg, "5", p.wh, "0", map[string]any{"lines": lines, "output_lot_no": "f1", "output_expiry_date": "2027-06-01"}))
	expect(t, res, http.StatusCreated, "")
	wo := decode[woDoc](t, res.Data)

	// 權限:開單者不能核准、核准者不能完工
	maker := e.seedRole("maker", "all", permission.WorkOrderRead, permission.WorkOrderWrite)
	appr := e.seedRole("appr", "all", permission.WorkOrderRead, permission.WorkOrderApprove)
	poster := e.seedRole("poster", "all", permission.WorkOrderRead, permission.WorkOrderPost)
	e.seedUser("maker", pw, false, false, maker.ID)
	e.seedUser("approver", pw, false, false, appr.ID)
	e.seedUser("poster", pw, false, false, poster.ID)
	mk, ap, po := e.loggedIn("maker", pw), e.loggedIn("approver", pw), e.loggedIn("poster", pw)
	expect(t, mk.woAct(&wo, "submit"), http.StatusOK, "")
	expect(t, mk.woAct(&wo, "approve"), http.StatusForbidden, "SYS-403")
	expect(t, ap.woAct(&wo, "approve"), http.StatusOK, "")
	expect(t, ap.woAct(&wo, "post"), http.StatusForbidden, "SYS-403")
	expect(t, po.woAct(&wo, "post"), http.StatusOK, "")

	// 材料先到期先出:M-OLD 30 + M-NEW 10;成品批號轉大寫並入庫
	if c.lotQty(milk, p.wh, "M-OLD") != "0" || c.lotQty(milk, p.wh, "M-NEW") != "20" || c.lotQty(fg, p.wh, "F1") != "5" {
		t.Fatalf("批號 OLD=%s NEW=%s F1=%s", c.lotQty(milk, p.wh, "M-OLD"), c.lotQty(milk, p.wh, "M-NEW"), c.lotQty(fg, p.wh, "F1"))
	}
	full := decode[struct {
		OutputLots []struct {
			LotNo string `json:"lot_no"`
		} `json:"output_lots"`
		Lines []struct {
			Lots []struct {
				LotNo string `json:"lot_no"`
				Qty   string `json:"qty"`
			} `json:"lots"`
		} `json:"lines"`
	}](t, c.do(http.MethodGet, prodWO+"/"+itoa(wo.ID), nil).Data)
	if len(full.OutputLots) != 1 || full.OutputLots[0].LotNo != "F1" || len(full.Lines[0].Lots) != 2 || full.Lines[0].Lots[0].LotNo != "M-OLD" || full.Lines[0].Lots[0].Qty != "30" {
		t.Fatalf("工單批號 = %+v", full)
	}
	// 反完工還原批號庫存
	expect(t, po.woAct(&wo, "unpost"), http.StatusOK, "")
	if c.lotQty(milk, p.wh, "M-OLD") != "30" || c.lotQty(fg, p.wh, "F1") != "0" {
		t.Fatalf("反完工後 OLD=%s F1=%s", c.lotQty(milk, p.wh, "M-OLD"), c.lotQty(fg, p.wh, "F1"))
	}
	assertReconcileLotsOK(t, c)

	// 分批完工(D94):權限(只有過帳權限能回報 / 作廢 / 結案)、批號管理的成品每批須批號與效期、材料先到期先出
	cb := func(extra map[string]any) map[string]any { return compBody("2026-09-12", "2", extra) }
	expect(t, mk.woComplete(&wo, cb(map[string]any{"output_lot_no": "F2", "output_expiry_date": "2027-06-01"})), http.StatusForbidden, "SYS-403")
	expect(t, ap.woComplete(&wo, cb(nil)), http.StatusForbidden, "SYS-403")
	expect(t, po.woComplete(&wo, cb(nil)), http.StatusUnprocessableEntity, "SYS-422") // 缺成品批號
	expect(t, po.woComplete(&wo, cb(map[string]any{"output_lot_no": "F2", "output_expiry_date": "2027-06-01"})), http.StatusCreated, "")
	// 5 個的工單領 40 個牛奶 → 2 個領 16 個:M-OLD 先出
	if c.lotQty(milk, p.wh, "M-OLD") != "14" || c.lotQty(fg, p.wh, "F2") != "2" {
		t.Fatalf("分批完工後 OLD=%s F2=%s", c.lotQty(milk, p.wh, "M-OLD"), c.lotQty(fg, p.wh, "F2"))
	}
	done := decode[struct {
		Completions []struct {
			OutputLots []struct {
				LotNo string `json:"lot_no"`
			} `json:"output_lots"`
			Lines []struct {
				Lots []struct {
					LotNo string `json:"lot_no"`
					Qty   string `json:"qty"`
				} `json:"lots"`
			} `json:"lines"`
		} `json:"completions"`
	}](t, c.do(http.MethodGet, prodWO+"/"+itoa(wo.ID), nil).Data)
	if len(done.Completions) != 1 || len(done.Completions[0].OutputLots) != 1 || done.Completions[0].OutputLots[0].LotNo != "F2" ||
		len(done.Completions[0].Lines[0].Lots) != 1 || done.Completions[0].Lines[0].Lots[0].LotNo != "M-OLD" || done.Completions[0].Lines[0].Lots[0].Qty != "16" {
		t.Fatalf("回報的批號 = %+v", done)
	}
	assertReconcileLotsOK(t, c)
	expect(t, ap.do(http.MethodPost, prodWO+"/"+itoa(wo.ID)+"/completions/"+itoa(wo.Completions[0].ID)+"/void", nil), http.StatusForbidden, "SYS-403")
	expect(t, po.do(http.MethodPost, prodWO+"/"+itoa(wo.ID)+"/completions/"+itoa(wo.Completions[0].ID)+"/void", nil), http.StatusOK, "")
	if c.lotQty(milk, p.wh, "M-OLD") != "30" || c.lotQty(fg, p.wh, "F2") != "0" {
		t.Fatalf("作廢回報後 OLD=%s F2=%s", c.lotQty(milk, p.wh, "M-OLD"), c.lotQty(fg, p.wh, "F2"))
	}
	assertReconcileLotsOK(t, c)

	// 列表與篩選
	list := decode[[]struct {
		DocNo  string `json:"doc_no"`
		Status string `json:"status"`
	}](t, c.do(http.MethodGet, prodWO+"?status=approved&keyword=FG1", nil).Data)
	if len(list) != 1 || list[0].DocNo != wo.DocNo {
		t.Fatalf("列表 = %+v", list)
	}
	expect(t, c.do(http.MethodGet, prodWO+"?status=bogus", nil), http.StatusUnprocessableEntity, "SYS-422")
}

// 工單納入多層簽核:金額以加工費計;低於門檻維持單層;超過門檻須第二層角色核准後才能完工
func TestWorkOrderMultiLevelApproval(t *testing.T) {
	p := newPurCtx(t)
	e, root := p.e, p.c
	fg, mat := e.seedItem("FG1", "goods"), e.seedItem("MAT", "goods")
	expect(t, root.postLot(p.wh, mat, e.unitID("PCS"), "100", "", ""), http.StatusOK, "")

	maker := e.seedRole("maker", "all", permission.WorkOrderRead, permission.WorkOrderWrite)
	l1 := e.seedRole("l1", "all", permission.WorkOrderRead, permission.WorkOrderApprove)
	cfo := e.seedRole("cfo", "all", permission.WorkOrderRead, permission.WorkOrderApprove)
	poster := e.seedRole("poster", "all", permission.WorkOrderRead, permission.WorkOrderPost)
	e.seedUser("maker", pw, false, false, maker.ID)
	e.seedUser("appr1", pw, false, false, l1.ID)
	e.seedUser("boss", pw, false, false, cfo.ID)
	e.seedUser("poster", pw, false, false, poster.ID)
	mk, a1, boss, po := e.loggedIn("maker", pw), e.loggedIn("appr1", pw), e.loggedIn("boss", pw), e.loggedIn("poster", pw)

	expect(t, root.do(http.MethodPost, "/approval/rules", map[string]any{"doc_type": "work_order", "min_amount": "5000", "role_ids": []int64{cfo.ID}}), http.StatusCreated, "")

	newWO := func(fee string) woDoc {
		res := mk.do(http.MethodPost, prodWO, woBody(fg, "5", p.wh, fee, map[string]any{"lines": []map[string]any{{"item_id": mat, "qty": "10"}}}))
		expect(t, res, http.StatusCreated, "")
		return decode[woDoc](t, res.Data)
	}

	// 加工費低於門檻:單層
	small := newWO("100")
	expect(t, mk.woAct(&small, "submit"), http.StatusOK, "")
	if pr := mk.progress("work_order", small.ID); pr.Required != 0 {
		t.Fatalf("低於門檻應為單層: %+v", pr)
	}
	expect(t, a1.woAct(&small, "approve"), http.StatusOK, "")
	if small.Status != "approved" {
		t.Fatalf("單層核准後 status=%s", small.Status)
	}

	// 加工費超過門檻:兩層,第 1 層核准後仍待審,第 2 層須 CFO 角色
	big := newWO("6000")
	expect(t, mk.woAct(&big, "submit"), http.StatusOK, "")
	if pr := mk.progress("work_order", big.ID); pr.Required != 2 {
		t.Fatalf("送審後流程: %+v", pr)
	}
	expect(t, a1.woAct(&big, "approve"), http.StatusOK, "")
	if big.Status != "pending" {
		t.Fatalf("第 1 層核准後應為待審,status=%s", big.Status)
	}
	expect(t, a1.woAct(&big, "approve"), http.StatusForbidden, "APR-002")
	expect(t, po.woAct(&big, "post"), http.StatusConflict, "DOC-001") // 尚未核准不能完工
	expect(t, boss.woAct(&big, "approve"), http.StatusOK, "")
	if big.Status != "approved" {
		t.Fatalf("全部核准後 status=%s", big.Status)
	}
	expect(t, po.woAct(&big, "post"), http.StatusOK, "")

	// 沒有工單檢視權限者看不到進度;不存在的工單 404
	nobody := e.seedRole("nobody", "all", permission.CustomerRead)
	e.seedUser("nobody", pw, false, false, nobody.ID)
	expect(t, e.loggedIn("nobody", pw).do(http.MethodGet, "/approval/progress?doc_type=work_order&doc_id="+itoa(big.ID), nil), http.StatusForbidden, "SYS-403")
	expect(t, mk.do(http.MethodGet, "/approval/progress?doc_type=work_order&doc_id=999999", nil), http.StatusNotFound, "SYS-404")
}

// BOM 版本:同一成品可有多份不同生效日的 BOM,展開時依日期選用
func TestBomVersions(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	fg, m1, m2, m3 := e.seedItem("FG1", "goods"), e.seedItem("M1", "goods"), e.seedItem("M2", "goods"), e.seedItem("M3", "goods")
	mk := func(item int64, eff string, lines ...map[string]any) apiResp {
		body := bomBody(item, "1", lines...)
		body["effective_from"] = eff
		return c.do(http.MethodPost, "/production/boms", body)
	}
	explodeQty := func(date string) (string, apiResp) {
		res := c.do(http.MethodGet, prodWO+"/explode?item_id="+itoa(fg)+"&qty=1&date="+date, nil)
		if res.status != http.StatusOK {
			return "", res
		}
		l := decode[[]struct {
			ItemID int64  `json:"item_id"`
			Qty    string `json:"qty"`
		}](t, res.Data)
		return itoa(l[0].ItemID) + ":" + l[0].Qty, res
	}

	v1 := mk(fg, "2026-09-01", bl(m1, "5"))
	expect(t, v1, http.StatusCreated, "")
	v2 := mk(fg, "2026-11-01", bl(m2, "7"))
	expect(t, v2, http.StatusCreated, "")
	expect(t, mk(fg, "2026-11-01", bl(m3, "1")), http.StatusConflict, "PRD-001") // 同生效日重複
	expect(t, mk(fg, "11/01", bl(m3, "1")), http.StatusUnprocessableEntity, "SYS-422")

	// 依日期選用:生效日前用 v1、之後用 v2、最早生效日之前沒有適用的 BOM
	if got, _ := explodeQty("2026-10-15"); got != itoa(m1)+":5" {
		t.Fatalf("10/15 應用 v1,得 %s", got)
	}
	if got, _ := explodeQty("2026-11-01"); got != itoa(m2)+":7" {
		t.Fatalf("11/01 應用 v2,得 %s", got)
	}
	_, res := explodeQty("2026-08-01")
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")

	// 停用 v2 後回頭用 v1
	v2b := decode[struct {
		ID      int64 `json:"id"`
		Version int32 `json:"version"`
	}](t, v2.Data)
	body := bomBody(fg, "1", bl(m2, "7"))
	body["effective_from"], body["is_active"], body["version"] = "2026-11-01", false, v2b.Version
	expect(t, c.do(http.MethodPut, "/production/boms/"+itoa(v2b.ID), body), http.StatusOK, "")
	if got, _ := explodeQty("2026-12-01"); got != itoa(m1)+":5" {
		t.Fatalf("v2 停用後應回到 v1,得 %s", got)
	}

	// 循環檢查涵蓋所有版本:M2 的 BOM 用到 FG1 → FG1(v2) → M2 → FG1
	expect(t, mk(m2, "2026-01-01", bl(fg, "1")), http.StatusUnprocessableEntity, "PRD-002")
	// 修改 v1 時不會丟掉 v2 的關係(仍會被檢查到)
	v1d := decode[struct {
		ID      int64 `json:"id"`
		Version int32 `json:"version"`
	}](t, v1.Data)
	body = bomBody(fg, "1", bl(m1, "6"))
	body["effective_from"], body["version"] = "2026-09-01", v1d.Version
	expect(t, c.do(http.MethodPut, "/production/boms/"+itoa(v1d.ID), body), http.StatusOK, "")
	expect(t, mk(m2, "2026-02-01", bl(fg, "1")), http.StatusUnprocessableEntity, "PRD-002")

	// 工單開單依工單日期自動展開(2026-09-10 → v1,已改為用量 6)
	res = c.do(http.MethodPost, prodWO, woBody(fg, "2", p.wh, "0", nil))
	expect(t, res, http.StatusCreated, "")
	wo := decode[struct {
		Lines []struct {
			ItemID int64  `json:"item_id"`
			Qty    string `json:"qty"`
		} `json:"lines"`
	}](t, res.Data)
	if len(wo.Lines) != 1 || wo.Lines[0].ItemID != m1 || wo.Lines[0].Qty != "12" {
		t.Fatalf("工單領料 = %+v", wo.Lines)
	}
}

// TestMrp 待審工單的領料算進需求、成品算進在途,材料不夠時建議採購。
func TestMrp(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	fg := e.seedItem("FG1", "goods")
	p.receiveOn("2026-08-10", "1", "1200") // RAW1 入庫 1 箱 = 12 個
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(fg, "1", bl(p.item, "2"))), http.StatusCreated, "")
	wo := decode[woDoc](t, c.do(http.MethodPost, prodWO, woBody(fg, "10", p.wh, "0", nil)).Data) // 領 20 個
	type row struct {
		ItemID       int64  `json:"item_id"`
		Action       string `json:"action"`
		SuggestedQty string `json:"suggested_qty"`
		WoDemand     string `json:"wo_demand"`
		IncomingWO   string `json:"incoming_wo"`
		OnHand       string `json:"on_hand"`
	}
	list := func(q string) map[int64]row {
		res := c.do(http.MethodGet, "/production/mrp"+q, nil)
		expect(t, res, http.StatusOK, "")
		m := map[int64]row{}
		for _, r := range decode[[]row](t, res.Data) {
			m[r.ItemID] = r
		}
		return m
	}
	if len(list("")) != 0 {
		t.Fatal("草稿工單不算需求")
	}
	expect(t, c.woAct(&wo, "submit"), http.StatusOK, "")
	got := list("")
	if r, ok := got[p.item]; !ok || r.Action != "buy" || r.SuggestedQty != "8" || r.WoDemand != "20" || r.OnHand != "12" {
		t.Fatalf("材料應建議採購 8,got %+v", got[p.item])
	}
	if _, ok := got[fg]; ok {
		t.Fatal("成品已有在途工單,不應再建議")
	}
	if r := list("?shortage_only=false")[fg]; r.IncomingWO != "10" {
		t.Fatalf("成品在途 = %+v", r)
	}
}

// TestBomSubstitutes 替代料:驗證、展開時依比例換算、修改時整批取代、納入循環檢查。
func TestBomSubstitutes(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	raw1, raw2, raw3, fg := p.item, e.seedItem("RAW2", "goods"), e.seedItem("RAW3", "goods"), e.seedItem("FG1", "goods")
	sub := func(item int64, ratio string) map[string]any { return map[string]any{"item_id": item, "ratio": ratio} }
	line := func(item int64, qty string, subs ...map[string]any) map[string]any {
		return map[string]any{"item_id": item, "qty": qty, "substitutes": subs}
	}
	for name, subs := range map[string][]map[string]any{
		"替代料是成品自己":   {sub(fg, "1")},
		"替代料是別的材料":   {sub(raw3, "1")},
		"替代料是自己這個材料": {sub(raw1, "1")},
		"替代料重複":      {sub(raw2, "1"), sub(raw2, "2")},
		"比例為零":       {sub(raw2, "0")},
		"超過五個":       {sub(raw2, "1"), sub(e.seedItem("S2", "goods"), "1"), sub(e.seedItem("S3", "goods"), "1"), sub(e.seedItem("S4", "goods"), "1"), sub(e.seedItem("S5", "goods"), "1"), sub(e.seedItem("S6", "goods"), "1")},
	} {
		body := bomBody(fg, "1", line(raw1, "2", subs...), bl(raw3, "1"))
		if res := c.do(http.MethodPost, "/production/boms", body); res.status != http.StatusUnprocessableEntity {
			t.Errorf("%s → %d %s", name, res.status, res.code())
		}
	}
	res := c.do(http.MethodPost, "/production/boms", bomBody(fg, "1", line(raw1, "2", sub(raw2, "1.5")), bl(raw3, "1")))
	expect(t, res, http.StatusCreated, "")
	bom := decode[struct {
		ID      int64 `json:"id"`
		Version int32 `json:"version"`
		Lines   []struct {
			Substitutes []struct {
				ItemID int64  `json:"item_id"`
				Ratio  string `json:"ratio"`
			} `json:"substitutes"`
		} `json:"lines"`
	}](t, res.Data)
	if len(bom.Lines[0].Substitutes) != 1 || bom.Lines[0].Substitutes[0].ItemID != raw2 || bom.Lines[0].Substitutes[0].Ratio != "1.5" || len(bom.Lines[1].Substitutes) != 0 {
		t.Fatalf("替代料 = %+v", bom.Lines)
	}
	// 展開:主料 10 個成品 × 2 = 20,替代料 20 × 1.5 = 30
	res = c.do(http.MethodGet, "/production/work-orders/explode?item_id="+itoa(fg)+"&qty=10", nil)
	expect(t, res, http.StatusOK, "")
	ex := decode[[]struct {
		ItemID      int64  `json:"item_id"`
		Qty         string `json:"qty"`
		Substitutes []struct {
			ItemID int64  `json:"item_id"`
			Qty    string `json:"qty"`
		} `json:"substitutes"`
	}](t, res.Data)
	if ex[0].Qty != "20" || len(ex[0].Substitutes) != 1 || ex[0].Substitutes[0].ItemID != raw2 || ex[0].Substitutes[0].Qty != "30" || len(ex[1].Substitutes) != 0 {
		t.Fatalf("展開 = %+v", ex)
	}
	// 修改:整批取代(raw3 改當替代料、raw2 改當主料)
	body := bomBody(fg, "1", line(raw1, "2", sub(raw3, "1")), bl(raw2, "1"))
	body["version"] = bom.Version
	res = c.do(http.MethodPut, "/production/boms/"+itoa(bom.ID), body)
	expect(t, res, http.StatusOK, "")
	upd := decode[struct {
		Lines []struct {
			Substitutes []struct {
				ItemID int64 `json:"item_id"`
			} `json:"substitutes"`
		} `json:"lines"`
	}](t, res.Data)
	if len(upd.Lines[0].Substitutes) != 1 || upd.Lines[0].Substitutes[0].ItemID != raw3 {
		t.Fatalf("修改後替代料 = %+v", upd.Lines)
	}
	// 循環:raw3 的 BOM 用到成品,而成品的 BOM 以 raw3 為替代料
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(raw3, "1", bl(fg, "1"))), http.StatusUnprocessableEntity, "PRD-002")
}

// compBody 完工回報的請求本文。
func compBody(date, qty string, extra map[string]any) map[string]any {
	b := map[string]any{"doc_date": date, "qty": qty}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

func (c *client) woComplete(d *woDoc, body map[string]any) apiResp {
	c.e.t.Helper()
	res := c.do(http.MethodPost, prodWO+"/"+itoa(d.ID)+"/completions", body)
	if res.status == http.StatusCreated {
		*d = decode[woDoc](c.e.t, res.Data)
	}
	return res
}

// TestWorkOrderPartialCompletion 分批完工(D94):預設領料依比例、累計達計畫自動結案、作廢回報退回已核准、
// 短交結案、有回報後不能用一次完工、MRP 只算尚未完工的部分、權限。
func TestWorkOrderPartialCompletion(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	raw1, raw2, fg := p.item, e.seedItem("RAW2", "goods"), e.seedItem("FG1", "goods")
	p2 := *p
	p2.item = raw2
	p.receiveOn("2026-08-10", "5", "1200")  // RAW1 60 個
	p2.receiveOn("2026-08-10", "2", "2400") // RAW2 24 個
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(fg, "1", bl(raw1, "2"), bl(raw2, "1"))), http.StatusCreated, "")
	wo := decode[woDoc](t, c.do(http.MethodPost, prodWO, woBody(fg, "10", p.wh, "500", nil)).Data)

	// 草稿 / 待審不能回報
	expect(t, c.woComplete(&wo, compBody("2026-09-12", "4", nil)), http.StatusConflict, "PRD-006")
	for _, a := range []string{"submit", "approve"} {
		expect(t, c.woAct(&wo, a), http.StatusOK, "")
	}

	// 預覽:4 個 → RAW1 8、RAW2 4、加工費 200
	prev := decode[struct {
		Qty            string `json:"qty"`
		Remaining      string `json:"remaining"`
		ProcessingCost string `json:"processing_cost"`
		Lines          []struct {
			ItemID int64  `json:"item_id"`
			Qty    string `json:"qty"`
		} `json:"lines"`
	}](t, c.do(http.MethodGet, prodWO+"/"+itoa(wo.ID)+"/completion-preview?qty=4", nil).Data)
	if prev.Qty != "4" || prev.Remaining != "10" || prev.ProcessingCost != "200" || len(prev.Lines) != 2 || prev.Lines[0].Qty != "8" || prev.Lines[1].Qty != "4" {
		t.Fatalf("預覽 = %+v", prev)
	}

	// 驗證:數量 0 / 超過剩餘 / 日期早於工單 / 加工費為負
	for name, body := range map[string]map[string]any{
		"數量為零":  compBody("2026-09-12", "0", nil),
		"超過剩餘":  compBody("2026-09-12", "11", nil),
		"早於工單":  compBody("2026-09-09", "4", nil),
		"加工費為負": compBody("2026-09-12", "4", map[string]any{"processing_cost": "-1"}),
	} {
		if res := c.woComplete(&wo, body); res.status != http.StatusUnprocessableEntity || res.code() != "SYS-422" {
			t.Errorf("%s → %d %s", name, res.status, res.code())
		}
	}

	// 第 1 批:4 個,領料依比例
	expect(t, c.woComplete(&wo, compBody("2026-09-12", "4", nil)), http.StatusCreated, "")
	if wo.Status != "approved" || wo.CompletedQty != "4" || len(wo.Completions) != 1 || wo.Completions[0].Status != "posted" {
		t.Fatalf("第 1 批後 = %+v", wo)
	}
	if e.balance(raw1, p.wh) != "52" || e.balance(raw2, p.wh) != "20" || e.balance(fg, p.wh) != "4" {
		t.Fatalf("第 1 批後庫存 raw1=%s raw2=%s fg=%s", e.balance(raw1, p.wh), e.balance(raw2, p.wh), e.balance(fg, p.wh))
	}
	// 有回報後:一次完工 / 反過帳 / 取消核准 / 作廢 都不行
	for _, a := range []string{"post", "unapprove", "void"} {
		expect(t, c.woAct(&wo, a), http.StatusConflict, "PRD-005")
	}
	// MRP:工單在途只剩 6、領料需求只剩 60%(RAW1 12、RAW2 6)
	mrp := decode[[]struct {
		ItemID     int64  `json:"item_id"`
		WoDemand   string `json:"wo_demand"`
		IncomingWO string `json:"incoming_wo"`
	}](t, c.do(http.MethodGet, "/production/mrp?shortage_only=false", nil).Data)
	for _, r := range mrp {
		switch r.ItemID {
		case fg:
			if r.IncomingWO != "6" {
				t.Errorf("成品工單在途 = %s,want 6", r.IncomingWO)
			}
		case raw1:
			if r.WoDemand != "12" {
				t.Errorf("RAW1 工單領料 = %s,want 12", r.WoDemand)
			}
		}
	}

	// 第 2 批:想做 6 個但自訂領料(RAW1 13、RAW2 6)與加工費 320;累計達 10 → 工單自動結案(已過帳)
	expect(t, c.woComplete(&wo, compBody("2026-09-15", "6", map[string]any{"processing_cost": "320",
		"lines": []map[string]any{{"item_id": raw1, "qty": "13"}, {"item_id": raw2, "qty": "6"}}})), http.StatusCreated, "")
	if wo.Status != "posted" || wo.CompletedQty != "10" || len(wo.Completions) != 2 {
		t.Fatalf("第 2 批後 = %+v", wo)
	}
	if e.balance(raw1, p.wh) != "39" || e.balance(raw2, p.wh) != "14" || e.balance(fg, p.wh) != "10" {
		t.Fatalf("第 2 批後庫存 raw1=%s raw2=%s fg=%s", e.balance(raw1, p.wh), e.balance(raw2, p.wh), e.balance(fg, p.wh))
	}
	expect(t, c.woComplete(&wo, compBody("2026-09-16", "1", nil)), http.StatusConflict, "PRD-006") // 已結案不能再回報

	// 作廢第 2 批 → 庫存退回、工單回到已核准、可重新回報
	second := wo.Completions[1].ID
	void := func(id int64) apiResp {
		res := c.do(http.MethodPost, prodWO+"/"+itoa(wo.ID)+"/completions/"+itoa(id)+"/void", nil)
		if res.status == http.StatusOK {
			wo = decode[woDoc](t, res.Data)
		}
		return res
	}
	expect(t, void(second), http.StatusOK, "")
	if wo.Status != "approved" || wo.CompletedQty != "4" || e.balance(raw1, p.wh) != "52" || e.balance(fg, p.wh) != "4" {
		t.Fatalf("作廢第 2 批後 = %+v raw1=%s fg=%s", wo, e.balance(raw1, p.wh), e.balance(fg, p.wh))
	}
	expect(t, void(second), http.StatusConflict, "PRD-007") // 重複作廢

	// 短交結案:只做了 4 個就結案;結案後不能再回報;作廢第 1 批會退回已核准
	expect(t, c.do(http.MethodPost, prodWO+"/"+itoa(wo.ID)+"/close", map[string]any{"version": wo.Version + 99}), http.StatusConflict, "SYS-409")
	res := c.do(http.MethodPost, prodWO+"/"+itoa(wo.ID)+"/close", map[string]any{"version": wo.Version})
	expect(t, res, http.StatusOK, "")
	wo = decode[woDoc](t, res.Data)
	if wo.Status != "posted" || wo.CompletedQty != "4" {
		t.Fatalf("短交結案 = %+v", wo)
	}
	expect(t, void(wo.Completions[0].ID), http.StatusOK, "")
	if wo.Status != "approved" || e.balance(fg, p.wh) != "0" || e.balance(raw1, p.wh) != "60" {
		t.Fatalf("全部作廢後 = %+v fg=%s raw1=%s", wo, e.balance(fg, p.wh), e.balance(raw1, p.wh))
	}
	// 沒有有效完工回報就不能結案
	expect(t, c.do(http.MethodPost, prodWO+"/"+itoa(wo.ID)+"/close", map[string]any{"version": wo.Version}), http.StatusConflict, "PRD-008")
}

// TestWorkOrderPartialCompletionCosting 分批完工的月結:完工量、領料量取自庫存流水帳,加工費取各回報的本次加工費。
func TestWorkOrderPartialCompletionCosting(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	raw1, raw2, fg := p.item, e.seedItem("RAW2", "goods"), e.seedItem("FG1", "goods")
	pcs := e.unitID("PCS")
	p2 := *p
	p2.item = raw2
	cust := e.seedCustomer("C1", "0", nil)
	p.receiveOn("2026-08-10", "5", "1200")
	p2.receiveOn("2026-08-10", "2", "2400")
	expect(t, c.do(http.MethodPost, "/production/boms", bomBody(fg, "1", bl(raw1, "2"), bl(raw2, "1"))), http.StatusCreated, "")
	wo := decode[woDoc](t, c.do(http.MethodPost, prodWO, woBody(fg, "10", p.wh, "500", nil)).Data)
	for _, a := range []string{"submit", "approve"} {
		expect(t, c.woAct(&wo, a), http.StatusOK, "")
	}
	expect(t, c.woComplete(&wo, compBody("2026-09-12", "4", nil)), http.StatusCreated, "")
	expect(t, c.woComplete(&wo, compBody("2026-09-15", "6", map[string]any{"processing_cost": "320",
		"lines": []map[string]any{{"item_id": raw1, "qty": "13"}, {"item_id": raw2, "qty": "6"}}})), http.StatusCreated, "")
	dn, res := c.createPur(deliveries, map[string]any{
		"doc_date": "2026-09-20", "doc_type": "delivery", "customer_id": cust, "warehouse_id": p.wh, "currency": "TWD",
		"tax_type_id": p.tax, "lines": []map[string]any{line(fg, pcs, "4", "800", nil)},
	})
	expect(t, res, http.StatusCreated, "")
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")

	expect(t, c.do(http.MethodPost, "/costing/closings/2026-08/run", nil), http.StatusOK, "")
	res = c.do(http.MethodPost, "/costing/closings/2026-09/run", nil)
	expect(t, res, http.StatusOK, "")
	// 材料 RAW1 21×100 + RAW2 10×200 = 4100,加工費 200 + 320 = 520 → 4620 ÷ 10 = 462;出貨 4 個 COGS 1848
	// 存貨 = RAW1 39×100 + RAW2 14×200 + FG 6×462 = 3900 + 2800 + 2772 = 9472
	if got := decode[costingDTO](t, res.Data); got.CogsAmount != "1848" || got.InventoryValue != "9472" {
		t.Fatalf("9 月月結 = %+v", got)
	}
	m := c.trialRange("2026-09-01", "2026-09-30")
	wantTrial(t, m, "5103", "0", "520")
	var recv decimal.Decimal
	if err := e.pool.QueryRow(context.Background(), "SELECT unit_cost FROM inventory_transactions WHERE source_type = 'wo_completion_receipt' AND qty > 0 ORDER BY id LIMIT 1").Scan(&recv); err != nil || !recv.Equal(decimal.NewFromInt(462)) {
		t.Fatalf("完工入庫單位成本 = %s err=%v", recv, err)
	}
	// 月結後該月鎖定:不能作廢回報
	if res := c.do(http.MethodPost, prodWO+"/"+itoa(wo.ID)+"/completions/"+itoa(wo.Completions[0].ID)+"/void", nil); res.status != http.StatusConflict || res.code() != "INV-008" {
		t.Fatalf("月結後作廢 = %d %s", res.status, res.code())
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
