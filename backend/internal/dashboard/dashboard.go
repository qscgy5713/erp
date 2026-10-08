// Package dashboard 為首頁儀表板:依使用者權限只回傳有權看的卡片,銷售與應收類依資料範圍過濾。
package dashboard

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/authctx"
	"erp/internal/shared/response"
	"erp/internal/system/permission"
)

type Module struct {
	store *database.Store
}

func New(store *database.Store) *Module { return &Module{store: store} }

// Register 掛上 /dashboard;r 須已套用 auth.Authenticate。所有登入者都可呼叫,內容依權限決定。
func (m *Module) Register(r *gin.RouterGroup) {
	r.GET("/dashboard", m.get)
}

var tst = time.FixedZone("TST", 8*3600)

type salesCard struct {
	Today      decimal.Decimal `json:"today"`
	TodayCount int64           `json:"today_count"`
	Month      decimal.Decimal `json:"month"`
	MonthCount int64           `json:"month_count"`
	Daily      []dailyDTO      `json:"daily"` // 近 7 天
	ScopeLabel string          `json:"scope_label"`
}

type dailyDTO struct {
	Date   string          `json:"date"`
	Amount decimal.Decimal `json:"amount"`
}

type pendingItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
	Path  string `json:"path"`
}

type lowStockItem struct {
	ID          int64           `json:"id"`
	Code        string          `json:"code"`
	Name        string          `json:"name"`
	UnitName    string          `json:"unit_name"`
	SafetyStock decimal.Decimal `json:"safety_stock"`
	Total       decimal.Decimal `json:"total"`
}

type expiryItem struct {
	LotID      int64           `json:"lot_id"`
	LotNo      string          `json:"lot_no"`
	ExpiryDate string          `json:"expiry_date"`
	ItemCode   string          `json:"item_code"`
	ItemName   string          `json:"item_name"`
	Qty        decimal.Decimal `json:"qty"`
	Expired    bool            `json:"expired"`
}

// expiryCard 效期警示:有庫存的批號中已過期與 30 天內到期的數量,以及最早到期的幾個。
type expiryCard struct {
	Expired  int64        `json:"expired"`
	Expiring int64        `json:"expiring"`
	Days     int          `json:"days"`
	Items    []expiryItem `json:"items"`
}

type lowStockCard struct {
	Count int64          `json:"count"`
	Items []lowStockItem `json:"items"`
}

type receivableCard struct {
	Open         decimal.Decimal `json:"open_amount"`
	Overdue      decimal.Decimal `json:"overdue_amount"`
	OverdueCount int64           `json:"overdue_count"`
}

type payableCard struct {
	Open         decimal.Decimal `json:"open_amount"`
	Overdue      decimal.Decimal `json:"overdue_amount"`
	OverdueCount int64           `json:"overdue_count"`
	DueSoon      decimal.Decimal `json:"due_soon_amount"`
}

// get GET /dashboard:沒有權限的卡片為 null,前端不顯示。
func (m *Module) get(c *gin.Context) {
	ctx := c.Request.Context()
	a := authctx.ActorFrom(ctx)
	q := m.store.Queries
	now := time.Now().In(tst)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	out := gin.H{"date": today.Format(time.DateOnly)}
	deptID, userID := a.ScopeFilter()

	if a.Can(permission.DeliveryRead) || a.Can(permission.DeliveryWrite) || a.Can(permission.DeliveryApprove) || a.Can(permission.DeliveryPost) {
		ms := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
		td, err := q.DashboardSales(ctx, db.DashboardSalesParams{CompanyID: a.CompanyID, FromDate: today, ToDate: today, ScopeUserID: userID, ScopeDeptID: deptID})
		if err != nil {
			response.Error(c, err)
			return
		}
		mo, err := q.DashboardSales(ctx, db.DashboardSalesParams{CompanyID: a.CompanyID, FromDate: ms, ToDate: today, ScopeUserID: userID, ScopeDeptID: deptID})
		if err != nil {
			response.Error(c, err)
			return
		}
		from := today.AddDate(0, 0, -6)
		days, err := q.DashboardSalesDaily(ctx, db.DashboardSalesDailyParams{CompanyID: a.CompanyID, FromDate: from, ToDate: today, ScopeUserID: userID, ScopeDeptID: deptID})
		if err != nil {
			response.Error(c, err)
			return
		}
		byDay := map[string]decimal.Decimal{}
		for _, d := range days {
			byDay[d.Day.Format(time.DateOnly)] = d.Amount
		}
		daily := make([]dailyDTO, 7)
		for i := range daily {
			d := from.AddDate(0, 0, i).Format(time.DateOnly)
			daily[i] = dailyDTO{Date: d, Amount: byDay[d]}
		}
		scope := "全公司"
		if userID != nil {
			scope = "僅本人負責的客戶"
		} else if deptID != nil {
			scope = "本部門負責的客戶"
		}
		out["sales"] = salesCard{Today: td.Amount, TodayCount: td.DeliveryCount, Month: mo.Amount, MonthCount: mo.DeliveryCount, Daily: daily, ScopeLabel: scope}
	}

	// 待審:只列使用者有核准權限、且數量大於 0 的項目
	var pending []pendingItem
	add := func(perm, key, label, path string, n int64) {
		if n > 0 && a.Can(perm) {
			pending = append(pending, pendingItem{Key: key, Label: label, Count: n, Path: path})
		}
	}
	ps, err := q.DashboardPendingSales(ctx, db.DashboardPendingSalesParams{CompanyID: a.CompanyID, ScopeUserID: userID, ScopeDeptID: deptID})
	if err != nil {
		response.Error(c, err)
		return
	}
	po, err := q.DashboardPendingOthers(ctx, a.CompanyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	add(permission.SalesOrderApprove, "sales_orders", "報價 / 訂單", "/sales/orders?status=pending", ps.SalesOrders)
	add(permission.DeliveryApprove, "deliveries", "出貨 / 銷貨退回", "/sales/deliveries?status=pending", ps.Deliveries)
	add(permission.CollectionApprove, "collections", "收款單", "/finance/collections?status=pending", ps.Collections)
	add(permission.PurchaseOrderApprove, "purchase_orders", "採購單", "/purchase/orders?status=pending", po.PurchaseOrders)
	add(permission.ReceiptApprove, "receipts", "進貨 / 進貨退出", "/purchase/receipts?status=pending", po.Receipts)
	add(permission.PaymentApprove, "payments", "付款單", "/finance/payments?status=pending", po.Payments)
	add(permission.InventoryApprove, "stock_documents", "庫存單據", "/inventory/documents?status=pending", po.StockDocuments)
	add(permission.VoucherPost, "draft_vouchers", "草稿傳票(待過帳)", "/gl/vouchers?status=draft", po.DraftVouchers)
	if pending == nil {
		pending = []pendingItem{}
	}
	out["pending"] = pending

	if a.Can(permission.InventoryRead) || a.Can(permission.InventoryWrite) || a.Can(permission.InventoryApprove) || a.Can(permission.InventoryPost) {
		n, err := q.DashboardLowStockCount(ctx, a.CompanyID)
		if err != nil {
			response.Error(c, err)
			return
		}
		rows, err := q.DashboardLowStock(ctx, a.CompanyID)
		if err != nil {
			response.Error(c, err)
			return
		}
		card := lowStockCard{Count: n, Items: make([]lowStockItem, len(rows))}
		for i, r := range rows {
			card.Items[i] = lowStockItem{ID: r.ID, Code: r.Code, Name: r.Name, UnitName: r.UnitName, SafetyStock: r.SafetyStock, Total: r.Total}
		}
		out["low_stock"] = card

		// 效期警示:公司有效期管理的料品或已有即將到期 / 過期的批號時才顯示
		const expiryDays = 30
		until := today.AddDate(0, 0, expiryDays)
		ec, err := q.DashboardExpiryCounts(ctx, db.DashboardExpiryCountsParams{CompanyID: a.CompanyID, Today: today, Until: until})
		if err != nil {
			response.Error(c, err)
			return
		}
		if ec.Controlled || ec.Expired+ec.Expiring > 0 {
			top, err := q.DashboardExpiryTop(ctx, db.DashboardExpiryTopParams{CompanyID: a.CompanyID, Until: until})
			if err != nil {
				response.Error(c, err)
				return
			}
			card := expiryCard{Expired: ec.Expired, Expiring: ec.Expiring, Days: expiryDays, Items: make([]expiryItem, len(top))}
			for i, r := range top {
				card.Items[i] = expiryItem{LotID: r.LotID, LotNo: r.LotNo, ExpiryDate: r.ExpiryDate.Format(time.DateOnly),
					ItemCode: r.Code, ItemName: r.Name, Qty: r.Qty, Expired: r.ExpiryDate.Before(today)}
			}
			out["expiry"] = card
		}
	}

	if a.Can(permission.ReceivableRead) {
		r, err := q.DashboardReceivables(ctx, db.DashboardReceivablesParams{CompanyID: a.CompanyID, Today: today, ScopeUserID: userID, ScopeDeptID: deptID})
		if err != nil {
			response.Error(c, err)
			return
		}
		out["receivable"] = receivableCard{Open: r.OpenAmount, Overdue: r.OverdueAmount, OverdueCount: r.OverdueCount}
	}
	if a.Can(permission.PayableRead) {
		p, err := q.DashboardPayables(ctx, db.DashboardPayablesParams{CompanyID: a.CompanyID, Today: today, Soon: today.AddDate(0, 0, 7)})
		if err != nil {
			response.Error(c, err)
			return
		}
		out["payable"] = payableCard{Open: p.OpenAmount, Overdue: p.OverdueAmount, OverdueCount: p.OverdueCount, DueSoon: p.DueSoonAmount}
	}

	// 提醒:有月結權限的人看到「上次月結」,提醒該結帳了
	if a.Can(permission.CostRead) || a.Can(permission.CostClose) {
		last := ""
		if l, err := q.DashboardLatestClosing(ctx, a.CompanyID); err == nil {
			last = l
		} else if !database.IsNoRows(err) {
			response.Error(c, err)
			return
		}
		out["costing"] = gin.H{"last_closing": last, "current_month": now.Format("2006-01")}
	}
	response.OK(c, out)
}
