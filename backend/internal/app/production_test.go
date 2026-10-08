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
