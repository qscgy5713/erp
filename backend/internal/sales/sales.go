// Package sales 為銷售:報價單、訂單、出貨單、銷貨退回單。
// 出貨 / 退回過帳時在同一交易內呼叫 inventory.Post 異動庫存,並以 finance.CreateReceivable 產生應收。
// 單據依負責業務套用資料範圍(D38),範圍外的單據視為不存在。
package sales

import (
	"context"

	"github.com/gin-gonic/gin"

	"erp/internal/auth"
	"erp/internal/db"
	"erp/internal/masterdata"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/system/permission"
	"erp/internal/trade"
)

type Module struct {
	store *database.Store
}

func New(store *database.Store) *Module { return &Module{store: store} }

// Register 掛上 /sales 路由;r 須已套用 auth.Authenticate。各單據動作的權限在 handler 內依動作判斷。
func (m *Module) Register(r *gin.RouterGroup) {
	g := r.Group("/sales")
	orderRead := auth.Require(permission.SalesOrderRead, permission.SalesOrderWrite, permission.SalesOrderApprove)
	g.GET("/orders", orderRead, m.listOrders)
	g.GET("/orders/:id", orderRead, m.getOrder)
	g.POST("/orders", auth.Require(permission.SalesOrderWrite), m.createOrder)
	g.PUT("/orders/:id", auth.Require(permission.SalesOrderWrite), m.updateOrder)
	g.POST("/orders/:id/actions/:action", orderRead, m.orderAction)
	// 可用量:開訂單 / 出貨單時參考
	g.GET("/availability", auth.Require(permission.SalesOrderRead, permission.SalesOrderWrite,
		permission.SalesOrderApprove, permission.DeliveryWrite), m.availability)
	// 未出貨清單:業務查詢,也供開出貨單時帶入
	g.GET("/unshipped-lines", auth.Require(permission.SalesOrderRead, permission.SalesOrderWrite,
		permission.SalesOrderApprove, permission.DeliveryWrite), m.unshippedLines)

	deliveryRead := auth.Require(permission.DeliveryRead, permission.DeliveryWrite, permission.DeliveryApprove, permission.DeliveryPost)
	g.GET("/deliveries", deliveryRead, m.listDeliveries)
	g.GET("/deliveries/:id", deliveryRead, m.getDelivery)
	g.POST("/deliveries", auth.Require(permission.DeliveryWrite), m.createDelivery)
	g.PUT("/deliveries/:id", auth.Require(permission.DeliveryWrite), m.updateDelivery)
	g.PUT("/deliveries/:id/invoice", auth.Require(permission.DeliveryWrite), m.setInvoice)
	g.POST("/deliveries/:id/actions/:action", deliveryRead, m.deliveryAction)
	g.GET("/returnable-lines", auth.Require(permission.DeliveryWrite), m.returnableLines)
}

func actor(c *gin.Context) *authctx.Actor { return authctx.ActorFrom(c.Request.Context()) }

var (
	fieldErr     = trade.FieldErr
	optionalDate = trade.OptionalDate
	dateString   = trade.DateString
	actionLabels = trade.ActionLabels
)

type actionInput struct {
	Version int32 `json:"version" binding:"required"`
}

// headerInput 銷售單據共用的單頭欄位。
type headerInput struct {
	trade.HeaderInput
	CustomerID int64 `json:"customer_id" binding:"required"`
}

// checkHeader 驗證客戶(啟用且在資料範圍內)與共用單頭欄位;回傳客戶以取得負責業務與信用額度。
func checkHeader(ctx context.Context, q *db.Queries, a *authctx.Actor, in *headerInput) (trade.Header, db.CustomerForDocRow, error) {
	cust, err := q.CustomerForDoc(ctx, db.CustomerForDocParams{ID: in.CustomerID, CompanyID: a.CompanyID})
	if database.IsNoRows(err) || (err == nil && (!cust.IsActive || !masterdata.CustomerVisible(a, cust.SalesUserID, cust.SalesDepartmentID))) {
		return trade.Header{}, cust, fieldErr("customer_id", "客戶不存在、已停用或不在你的資料範圍內")
	} else if err != nil {
		return trade.Header{}, cust, err
	}
	h, err := trade.CheckHeader(ctx, q, a.CompanyID, &in.HeaderInput)
	return h, cust, err
}

// visible 單據是否在資料範圍內;範圍外視為不存在(不透露資料存在與否)。
func visible(a *authctx.Actor, salesUserID, salesDeptID *int64) error {
	if !masterdata.CustomerVisible(a, salesUserID, salesDeptID) {
		return apperr.ErrNotFound
	}
	return nil
}

type lineInput struct {
	trade.LineInput
	SoLineID       *int64 `json:"so_line_id"`       // 出貨:來源訂單明細
	DeliveryLineID *int64 `json:"delivery_line_id"` // 退回:來源出貨明細
}

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
