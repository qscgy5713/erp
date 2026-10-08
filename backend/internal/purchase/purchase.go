// Package purchase 為採購:採購單、進貨單、進貨退出單。
// 進貨 / 退出過帳時在同一交易內呼叫 inventory.Post 異動庫存,並以 finance.CreatePayable 產生應付。
// 單頭、計價與訂單狀態規則與銷售共用,見 internal/trade。
package purchase

import (
	"context"

	"github.com/gin-gonic/gin"

	"erp/internal/auth"
	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/authctx"
	"erp/internal/system/permission"
	"erp/internal/trade"
)

type Module struct {
	store *database.Store
}

func New(store *database.Store) *Module { return &Module{store: store} }

// Register 掛上 /purchase 路由;r 須已套用 auth.Authenticate。各單據動作的權限在 handler 內依動作判斷。
func (m *Module) Register(r *gin.RouterGroup) {
	g := r.Group("/purchase")
	orderRead := auth.Require(permission.PurchaseOrderRead, permission.PurchaseOrderWrite, permission.PurchaseOrderApprove)
	g.GET("/orders", orderRead, m.listOrders)
	g.GET("/orders/:id", orderRead, m.getOrder)
	g.POST("/orders", auth.Require(permission.PurchaseOrderWrite), m.createOrder)
	g.PUT("/orders/:id", auth.Require(permission.PurchaseOrderWrite), m.updateOrder)
	g.POST("/orders/:id/actions/:action", orderRead, m.orderAction)
	// 未交貨清單:採購人員查詢,也供開進貨單時帶入
	g.GET("/outstanding-lines", auth.Require(permission.PurchaseOrderRead, permission.PurchaseOrderWrite,
		permission.PurchaseOrderApprove, permission.ReceiptWrite), m.outstandingLines)

	receiptRead := auth.Require(permission.ReceiptRead, permission.ReceiptWrite, permission.ReceiptApprove, permission.ReceiptPost)
	g.GET("/receipts", receiptRead, m.listReceipts)
	g.GET("/receipts/:id", receiptRead, m.getReceipt)
	g.POST("/receipts", auth.Require(permission.ReceiptWrite), m.createReceipt)
	g.PUT("/receipts/:id", auth.Require(permission.ReceiptWrite), m.updateReceipt)
	g.POST("/receipts/:id/actions/:action", receiptRead, m.receiptAction)
	g.GET("/returnable-lines", auth.Require(permission.ReceiptWrite), m.returnableLines)
}

func actor(c *gin.Context) *authctx.Actor { return authctx.ActorFrom(c.Request.Context()) }

var (
	fieldErr     = trade.FieldErr
	parseDate    = trade.ParseDate
	optionalDate = trade.OptionalDate
	dateString   = trade.DateString
	actionLabels = trade.ActionLabels
)

// headerInput 採購單與進貨 / 退出單共用的單頭欄位。
type headerInput struct {
	trade.HeaderInput
	SupplierID int64 `json:"supplier_id" binding:"required"`
}

// checkHeader 驗證供應商與共用單頭欄位。
func checkHeader(ctx context.Context, q *db.Queries, companyID int64, in *headerInput) (trade.Header, error) {
	sup, err := q.SupplierForDoc(ctx, db.SupplierForDocParams{ID: in.SupplierID, CompanyID: companyID})
	if database.IsNoRows(err) || (err == nil && !sup.IsActive) {
		return trade.Header{}, fieldErr("supplier_id", "供應商不存在或已停用")
	} else if err != nil {
		return trade.Header{}, err
	}
	return trade.CheckHeader(ctx, q, companyID, &in.HeaderInput)
}

type lineInput struct {
	trade.LineInput
	PoLineID      *int64 `json:"po_line_id"`      // 進貨:來源採購明細
	ReceiptLineID *int64 `json:"receipt_line_id"` // 退出:來源進貨明細
}

// pricedLine 已驗證並計算金額的明細。
type pricedLine struct {
	lineInput
	trade.Priced
}

func priceLines(ctx context.Context, q *db.Queries, companyID int64, h trade.Header, lines []lineInput) ([]pricedLine, trade.Totals, error) {
	in := make([]trade.LineInput, len(lines))
	for i, l := range lines {
		in[i] = l.LineInput
	}
	priced, t, err := trade.PriceLines(ctx, q, companyID, h, in)
	if err != nil {
		return nil, t, err
	}
	out := make([]pricedLine, len(lines))
	for i := range lines {
		out[i] = pricedLine{lineInput: lines[i], Priced: priced[i]}
	}
	return out, t, nil
}
