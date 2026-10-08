package app

import (
	"context"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"
)

type costingDTO struct {
	Period         string `json:"period"`
	CogsAmount     string `json:"cogs_amount"`
	AdjustAmount   string `json:"adjust_amount"`
	InventoryValue string `json:"inventory_value"`
}

func (c *client) trialRange(from, to string) map[string]trialRow {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/gl/reports/trial-balance?from="+from+"&to="+to, nil)
	expect(c.e.t, res, http.StatusOK, "")
	out := decode[struct {
		Rows     []trialRow `json:"rows"`
		Balanced bool       `json:"balanced"`
	}](c.e.t, res.Data)
	if !out.Balanced {
		c.e.t.Fatalf("試算表不平衡")
	}
	m := map[string]trialRow{}
	for _, r := range out.Rows {
		m[r.Code] = r
	}
	return m
}

// receiveOn 在指定日期進貨 boxes 箱、每箱 price,並過帳。
func (p *purCtx) receiveOn(date string, boxes, price string) {
	p.e.t.Helper()
	gr, res := p.c.createPur(receipts, p.header(map[string]any{
		"doc_date": date, "doc_type": "receipt",
		"lines": []map[string]any{line(p.item, p.box, boxes, price, nil)},
	}))
	expect(p.e.t, res, http.StatusCreated, "")
	p.c.approvePur(receipts, &gr)
	expect(p.e.t, p.c.actPur(receipts, &gr, "post"), http.StatusOK, "")
}

func TestMonthlyCostClosing(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	cust := e.seedCustomer("C1", "0", nil)

	// 8 月:進貨 2 箱 @1200(24 個,2400)
	p.receiveOn("2026-08-10", "2", "1200")
	// 9 月:進貨 5 箱 @1200 與 5 箱 @1440(共 120 個、13200)、出貨 3 箱(36 個)、盤虧 6 個
	p.receiveOn("2026-09-05", "5", "1200")
	p.receiveOn("2026-09-10", "5", "1440")
	so, res := c.createPur(salesOrders, map[string]any{
		"doc_date": "2026-09-12", "doc_type": "order", "customer_id": cust, "warehouse_id": p.wh, "currency": "TWD",
		"tax_type_id": p.tax, "lines": []map[string]any{line(p.item, p.box, "3", "2000", nil)},
	})
	expect(t, res, http.StatusCreated, "")
	c.approvePur(salesOrders, &so)
	dn, res := c.createPur(deliveries, map[string]any{
		"doc_date": "2026-09-15", "doc_type": "delivery", "customer_id": cust, "warehouse_id": p.wh, "currency": "TWD",
		"tax_type_id": p.tax, "lines": []map[string]any{line(p.item, p.box, "3", "2000", map[string]any{"so_line_id": so.Lines[0].ID})},
	})
	expect(t, res, http.StatusCreated, "")
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")
	adj, res := c.createDoc(map[string]any{
		"doc_type": "adjustment", "doc_date": "2026-09-20", "warehouse_id": p.wh,
		"lines": []map[string]any{{"item_id": p.item, "unit_id": e.unitID("PCS"), "qty": "-6"}},
	})
	expect(t, res, http.StatusCreated, "")
	expect(t, c.postAll(&adj), http.StatusOK, "")

	// 月結規則:只能結已結束的月份、須依序、不可重複
	expect(t, c.do(http.MethodPost, "/costing/closings/2099-01/run", nil), http.StatusUnprocessableEntity, "CST-002")
	expect(t, c.do(http.MethodPost, "/costing/closings/2026-09/run", nil), http.StatusConflict, "CST-003")
	res = c.do(http.MethodPost, "/costing/closings/2026-08/run", nil)
	expect(t, res, http.StatusOK, "")
	if a := decode[costingDTO](t, res.Data); a.CogsAmount != "0" || a.InventoryValue != "2400" {
		t.Fatalf("8 月月結 = %+v", a)
	}
	expect(t, c.do(http.MethodPost, "/costing/closings/2026-08/run", nil), http.StatusConflict, "CST-001")

	// 9 月:期初 24 個 2400 + 進貨 120 個 13200 → 平均 108.333333
	res = c.do(http.MethodPost, "/costing/closings/2026-09/run", nil)
	expect(t, res, http.StatusOK, "")
	sep := decode[costingDTO](t, res.Data)
	if sep.CogsAmount != "3900" || sep.AdjustAmount != "650" || sep.InventoryValue != "11050" {
		t.Fatalf("9 月月結 = %+v", sep)
	}
	items := decode[[]struct {
		ItemCode   string `json:"item_code"`
		AvgCost    string `json:"avg_cost"`
		OpeningQty string `json:"opening_qty"`
		ClosingQty string `json:"closing_qty"`
	}](t, c.do(http.MethodGet, "/costing/closings/2026-09/items", nil).Data)
	if len(items) != 1 || items[0].AvgCost != "108.333333" || items[0].OpeningQty != "24" || items[0].ClosingQty != "102" {
		t.Fatalf("items = %+v", items)
	}

	// 傳票:銷貨成本與盤損;存貨科目 9 月淨變動 = 進貨 13200 − 3900 − 650
	m := c.trialRange("2026-09-01", "2026-09-30")
	wantTrial(t, m, "5101", "3900", "0")
	wantTrial(t, m, "5102", "650", "0")
	wantTrial(t, m, "1141", "13200", "4550")

	// 成本回寫:出貨與盤點的流水帳帶上平均成本;進貨保留原單位成本
	var delCost, rcvCost decimal.Decimal
	if err := e.pool.QueryRow(context.Background(), "SELECT unit_cost FROM inventory_transactions WHERE source_type = 'delivery'").Scan(&delCost); err != nil || !delCost.Equal(decimal.RequireFromString("108.333333")) {
		t.Fatalf("delivery unit_cost = %s err=%v", delCost, err)
	}
	if err := e.pool.QueryRow(context.Background(), "SELECT unit_cost FROM inventory_transactions WHERE source_type = 'goods_receipt' AND doc_date = '2026-09-10'").Scan(&rcvCost); err != nil || !rcvCost.Equal(decimal.NewFromInt(120)) {
		t.Fatalf("receipt unit_cost = %s err=%v", rcvCost, err)
	}

	// 月結後該月庫存異動鎖定(連同進貨過帳、庫存單據);其他月份不受影響
	late, res := c.createDoc(map[string]any{
		"doc_type": "adjustment", "doc_date": "2026-09-25", "warehouse_id": p.wh,
		"lines": []map[string]any{{"item_id": p.item, "unit_id": e.unitID("PCS"), "qty": "1"}},
	})
	expect(t, res, http.StatusCreated, "")
	for _, a := range []string{"submit", "approve"} {
		expect(t, c.act(&late, a), http.StatusOK, "")
	}
	expect(t, c.act(&late, "post"), http.StatusConflict, "INV-008")
	expect(t, c.actPur(deliveries, &dn, "unpost"), http.StatusConflict, "INV-008")

	// 對帳檢查:存貨(容許 1 元四捨五入)、應收、應付、現有量、傳票皆通過
	rec := decode[struct {
		OK     bool `json:"ok"`
		Checks []struct {
			Key    string `json:"key"`
			Status string `json:"status"`
			Diff   string `json:"diff"`
		} `json:"checks"`
	}](t, c.do(http.MethodGet, "/costing/reconcile", nil).Data)
	if !rec.OK || len(rec.Checks) != 7 {
		t.Fatalf("reconcile = %+v", rec)
	}
	for _, ch := range rec.Checks {
		if ch.Status != "ok" {
			t.Fatalf("check %s = %s", ch.Key, ch.Status)
		}
	}

	// 取消月結:只能取消最新的;取消後沖銷傳票、解除鎖定、可重算
	expect(t, c.do(http.MethodPost, "/costing/closings/2026-08/cancel", nil), http.StatusConflict, "CST-005")
	expect(t, c.do(http.MethodPost, "/costing/closings/2026-09/cancel", nil), http.StatusOK, "")
	expect(t, c.do(http.MethodPost, "/costing/closings/2026-09/cancel", nil), http.StatusConflict, "CST-004")
	m = c.trialRange("2026-09-01", "2026-09-30")
	wantTrial(t, m, "5101", "3900", "3900") // 沖銷後淨額為 0
	expect(t, c.act(&late, "post"), http.StatusOK, "")
	res = c.do(http.MethodPost, "/costing/closings/2026-09/run", nil)
	expect(t, res, http.StatusOK, "")
	// 重算:多了盤盈 1 個(+108.333333),盤損淨額 650 − 108 = 542
	if again := decode[costingDTO](t, res.Data); again.AdjustAmount != "542" {
		t.Fatalf("重算 = %+v", again)
	}
}

func TestCostingPermissionsAndEmpty(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	viewer := e.seedRole("COSTVIEW", "all", "costing.read")
	e.seedUser("viewer", pw, false, false, viewer.ID)
	e.seedUser("nobody", pw, false, false)
	root, vw, nb := e.loggedIn("root", pw), e.loggedIn("viewer", pw), e.loggedIn("nobody", pw)

	expect(t, vw.do(http.MethodGet, "/costing/closings", nil), http.StatusOK, "")
	expect(t, vw.do(http.MethodPost, "/costing/closings/2026-09/run", nil), http.StatusForbidden, "SYS-403")
	expect(t, nb.do(http.MethodGet, "/costing/reconcile", nil), http.StatusForbidden, "SYS-403")
	// 沒有任何異動的月份可月結(結果全為 0),也可取消
	expect(t, root.do(http.MethodPost, "/costing/closings/2026-09/run", nil), http.StatusOK, "")
	expect(t, root.do(http.MethodGet, "/costing/closings/2026-09/items", nil), http.StatusOK, "")
	expect(t, root.do(http.MethodGet, "/costing/closings/2026-10/items", nil), http.StatusNotFound, "SYS-404")
	expect(t, root.do(http.MethodPost, "/costing/closings/2026-09/cancel", nil), http.StatusOK, "")
}
