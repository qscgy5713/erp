package app

import (
	"net/http"
	"testing"
	"time"
)

type dashboardDTO struct {
	Sales *struct {
		Today      string `json:"today"`
		TodayCount int    `json:"today_count"`
		Month      string `json:"month"`
		ScopeLabel string `json:"scope_label"`
		Daily      []struct {
			Date   string `json:"date"`
			Amount string `json:"amount"`
		} `json:"daily"`
	} `json:"sales"`
	Pending []struct {
		Key   string `json:"key"`
		Count int    `json:"count"`
	} `json:"pending"`
	LowStock *struct {
		Count int `json:"count"`
		Items []struct {
			Code string `json:"code"`
		} `json:"items"`
	} `json:"low_stock"`
	Receivable *struct {
		Open         string `json:"open_amount"`
		Overdue      string `json:"overdue_amount"`
		OverdueCount int    `json:"overdue_count"`
	} `json:"receivable"`
	Payable *struct {
		Open    string `json:"open_amount"`
		Overdue string `json:"overdue_amount"`
		DueSoon string `json:"due_soon_amount"`
	} `json:"payable"`
	Costing *struct {
		LastClosing  string `json:"last_closing"`
		CurrentMonth string `json:"current_month"`
	} `json:"costing"`
}

func (c *client) dashboard() dashboardDTO {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/dashboard", nil)
	expect(c.e.t, res, http.StatusOK, "")
	return decode[dashboardDTO](c.e.t, res.Data)
}

func (d dashboardDTO) pending(key string) int {
	for _, p := range d.Pending {
		if p.Key == key {
			return p.Count
		}
	}
	return 0
}

func TestDashboardCardsAndScope(t *testing.T) {
	s := newSalCtx(t)
	e, root := s.e, s.c
	todayStr := time.Now().In(time.FixedZone("TST", 8*3600)).Format(time.DateOnly)

	// 今天出貨 2 箱 @1000(未稅 2000),今天退回 1 箱(未稅 1000),另外兩個業務各有客戶
	mk := func(cust int64, qty string) purDoc {
		so, res := root.createPur(salesOrders, s.header(map[string]any{
			"doc_date": todayStr, "doc_type": "order", "customer_id": cust, "lines": []map[string]any{line(s.item, s.box, qty, "1000", nil)},
		}))
		expect(t, res, http.StatusCreated, "")
		root.approvePur(salesOrders, &so)
		dn, res := root.createPur(deliveries, s.header(map[string]any{
			"doc_date": todayStr, "doc_type": "delivery", "customer_id": cust,
			"lines": []map[string]any{line(s.item, s.box, qty, "1000", map[string]any{"so_line_id": so.Lines[0].ID})},
		}))
		expect(t, res, http.StatusCreated, "")
		root.approvePur(deliveries, &dn)
		expect(t, root.actPur(deliveries, &dn, "post"), http.StatusOK, "")
		return dn
	}
	role := e.seedRole("SALES", "self", "sales.order.read", "sales.delivery.read", "finance.receivable.read")
	amy := e.seedUser("amy", pw, false, false, role.ID)
	cAmy := e.seedCustomer("C-AMY", "0", &amy.ID)
	dn := mk(s.cust, "2") // 無負責業務的客戶:只有「全部」範圍看得到
	mk(cAmy, "1")
	sr, res := root.createPur(deliveries, s.header(map[string]any{
		"doc_date": todayStr, "doc_type": "return",
		"lines": []map[string]any{line(s.item, s.box, "1", "1000", map[string]any{"delivery_line_id": dn.Lines[0].ID})},
	}))
	expect(t, res, http.StatusCreated, "")
	root.approvePur(deliveries, &sr)
	expect(t, root.actPur(deliveries, &sr, "post"), http.StatusOK, "")

	// 全公司:出貨 2000 + 1000 − 退回 1000 = 2000,2 張出貨
	d := root.dashboard()
	if d.Sales == nil || d.Sales.Today != "2000" || d.Sales.TodayCount != 2 || d.Sales.Month != "2000" || d.Sales.ScopeLabel != "全公司" {
		t.Fatalf("root sales = %+v", d.Sales)
	}
	if len(d.Sales.Daily) != 7 || d.Sales.Daily[6].Date != todayStr || d.Sales.Daily[6].Amount != "2000" || d.Sales.Daily[0].Amount != "0" {
		t.Fatalf("daily = %+v", d.Sales.Daily)
	}
	// 本人範圍:只看自己客戶的 1000;沒有庫存 / 應付 / 月結權限的卡片不出現
	ad := e.loggedIn("amy", pw).dashboard()
	if ad.Sales == nil || ad.Sales.Today != "1000" || ad.Sales.TodayCount != 1 || ad.Sales.ScopeLabel != "僅本人負責的客戶" {
		t.Fatalf("amy sales = %+v", ad.Sales)
	}
	if ad.LowStock != nil || ad.Payable != nil || ad.Costing != nil || ad.Receivable == nil {
		t.Fatalf("amy 卡片 = %+v", ad)
	}
	// 沒有任何權限的人:只有日期與空的待審清單
	e.seedUser("nobody", pw, false, false)
	nd := e.loggedIn("nobody", pw).dashboard()
	if nd.Sales != nil || nd.Receivable != nil || nd.Payable != nil || nd.LowStock != nil || len(nd.Pending) != 0 {
		t.Fatalf("nobody = %+v", nd)
	}

	// 應收:出貨 2100 + 1050 − 退回 1050(負數帳款一併計入,與應收帳款頁一致)= 2100 未沖;
	// 到期日是 M30(下月底),尚未逾期;把一筆改成已逾期
	if d.Receivable == nil || d.Receivable.Open != "2100" || d.Receivable.Overdue != "0" {
		t.Fatalf("receivable = %+v", d.Receivable)
	}
	if _, err := e.pool.Exec(t.Context(), "UPDATE accounts_receivable SET due_date = CURRENT_DATE - 5 WHERE source_type = 'delivery' AND amount = 2100"); err != nil {
		t.Fatal(err)
	}
	if d = root.dashboard(); d.Receivable.Overdue != "2100" || d.Receivable.OverdueCount != 1 {
		t.Fatalf("逾期應收 = %+v", d.Receivable)
	}
	// 本人範圍的應收只含自己客戶(1050)
	if r := e.loggedIn("amy", pw).dashboard().Receivable; r.Open != "1050" {
		t.Fatalf("amy receivable = %+v", r)
	}

	// 庫存警示:P1 現有 120 − 出貨,把安全庫存設高;月結提醒
	if _, err := e.pool.Exec(t.Context(), "UPDATE items SET safety_stock = 1000 WHERE id = $1", s.item); err != nil {
		t.Fatal(err)
	}
	if d = root.dashboard(); d.LowStock == nil || d.LowStock.Count != 1 || d.LowStock.Items[0].Code != "P1" {
		t.Fatalf("low stock = %+v", d.LowStock)
	}
	if d.Costing == nil || d.Costing.LastClosing != "" || d.Costing.CurrentMonth != todayStr[:7] {
		t.Fatalf("costing = %+v", d.Costing)
	}
}

func TestDashboardPendingOnlyForApprovers(t *testing.T) {
	p := newPurCtx(t)
	e, c := p.e, p.c
	boss := e.seedRole("BOSS", "all", "purchase.order.approve", "inventory.stock.approve")
	clerk := e.seedRole("CLERK", "all", "purchase.order.write")
	e.seedUser("boss", pw, false, false, boss.ID)
	e.seedUser("clerk", pw, false, false, clerk.ID)

	po, res := c.createPur(orders, p.header(map[string]any{"lines": []map[string]any{line(p.item, p.box, "1", "10", nil)}}))
	expect(t, res, http.StatusCreated, "")
	expect(t, c.actPur(orders, &po, "submit"), http.StatusOK, "") // 待審
	adj, _ := c.createDoc(adjust(p.wh, p.item, p.e.unitID("PCS"), "5"))
	expect(t, c.act(&adj, "submit"), http.StatusOK, "")

	if d := c.dashboard(); d.pending("purchase_orders") != 1 || d.pending("stock_documents") != 1 {
		t.Fatalf("root pending = %+v", d.Pending)
	}
	// 核准者看得到待審;只有開單權限的人看不到(他沒有核准權限)
	if d := e.loggedIn("boss", pw).dashboard(); d.pending("purchase_orders") != 1 || d.pending("stock_documents") != 1 {
		t.Fatalf("boss pending = %+v", d.Pending)
	}
	if d := e.loggedIn("clerk", pw).dashboard(); len(d.Pending) != 0 {
		t.Fatalf("clerk pending = %+v", d.Pending)
	}
	// 處理完就消失
	expect(t, c.actPur(orders, &po, "approve"), http.StatusOK, "")
	if d := c.dashboard(); d.pending("purchase_orders") != 0 {
		t.Fatalf("approved 後 pending = %+v", d.Pending)
	}
}
