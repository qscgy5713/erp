// Package finance 為應收應付。目前提供應收 / 應付帳款的產生、移除與查詢,收付款沖帳於 M5 加入。
package finance

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/auth"
	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/system/permission"
)

// 應收應付來源類型
const (
	SourceGoodsReceipt   = "goods_receipt"
	SourcePurchaseReturn = "purchase_return"
	SourceDelivery       = "delivery"
	SourceSalesReturn    = "sales_return"
)

var errPayableSettled = apperr.New(http.StatusConflict, "FIN-001", "應付帳款已有付款單沖帳或引用,請先作廢 / 反過帳付款單")

// CreatePayable 由進貨 / 退出過帳產生應付(退出為負數)。須在呼叫端的交易內執行;金額為 0 時不產生。
func CreatePayable(ctx context.Context, q *db.Queries, p db.InsertPayableParams) error {
	if p.Amount.IsZero() && p.BaseAmount.IsZero() {
		return nil
	}
	return q.InsertPayable(ctx, p)
}

// RemovePayable 反過帳時移除來源單據產生的應付;已沖帳則不可移除。沒有應付(金額為 0)時不做事。
func RemovePayable(ctx context.Context, q *db.Queries, sourceType string, sourceID int64) error {
	p, err := q.LockPayableBySource(ctx, db.LockPayableBySourceParams{SourceType: sourceType, SourceID: sourceID})
	if database.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !p.PaidAmount.IsZero() {
		return errPayableSettled.WithDetails(map[string]string{"source_no": p.SourceNo})
	}
	// 草稿 / 待審 / 已核准的付款單已引用此應付時也不可移除
	if no, err := q.PayableSettlementNo(ctx, &p.ID); err == nil {
		return errPayableSettled.WithDetails(map[string]string{"source_no": p.SourceNo, "settlement_no": no})
	} else if !database.IsNoRows(err) {
		return err
	}
	// 剩下的只會是已作廢付款單的明細(單據本身保留,明細隨應付移除)
	if err := q.DeleteSettlementLinesByPayable(ctx, &p.ID); err != nil {
		return err
	}
	return q.DeletePayable(ctx, p.ID)
}

type Module struct {
	store *database.Store
}

func New(store *database.Store) *Module { return &Module{store: store} }

// Register 掛上 /finance 路由;r 須已套用 auth.Authenticate。
func (m *Module) Register(r *gin.RouterGroup) {
	g := r.Group("/finance")
	g.GET("/payables", auth.Require(permission.PayableRead), m.listPayables)
	g.GET("/receivables", auth.Require(permission.ReceivableRead), m.listReceivables)
	// 對帳單 / 帳齡:邊別(side)決定所需權限,於 handler 內判斷
	report := auth.Require(permission.ReceivableRead, permission.PayableRead)
	g.GET("/statements", report, m.statement)
	g.GET("/aging", report, m.aging)
	m.registerSettlements(g, "/collections", sides[SideReceipt])
	m.registerSettlements(g, "/payments", sides[SidePayment])
}

type payableDTO struct {
	ID           int64           `json:"id"`
	SupplierID   int64           `json:"supplier_id"`
	SupplierCode string          `json:"supplier_code"`
	SupplierName string          `json:"supplier_name"`
	SourceType   string          `json:"source_type"`
	SourceID     int64           `json:"source_id"`
	SourceNo     string          `json:"source_no"`
	DocDate      string          `json:"doc_date"`
	DueDate      string          `json:"due_date"`
	Currency     string          `json:"currency"`
	ExchangeRate decimal.Decimal `json:"exchange_rate"`
	Amount       decimal.Decimal `json:"amount"`
	BaseAmount   decimal.Decimal `json:"base_amount"`
	PaidAmount   decimal.Decimal `json:"paid_amount"`
	Balance      decimal.Decimal `json:"balance"`
	CreatedAt    time.Time       `json:"created_at"`
}

// sumMeta 分頁資訊加上篩選結果的本位幣合計。
type sumMeta struct {
	page.Meta
	BaseAmountSum decimal.Decimal `json:"base_amount_sum"`
}

func optionalDate(c *gin.Context, name string) (*time.Time, error) {
	s := c.Query(name)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, apperr.Validation(map[string]string{name: "日期格式須為 YYYY-MM-DD"})
	}
	return &t, nil
}

// listPayables 應付帳款明細;open_only=true 只列未沖清。meta 另附篩選結果的本位幣合計。
func (m *Module) listPayables(c *gin.Context) {
	ctx := c.Request.Context()
	supplierID, err := httpx.QueryInt64(c, "supplier_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	from, err := optionalDate(c, "from")
	if err != nil {
		response.Error(c, err)
		return
	}
	to, err := optionalDate(c, "to")
	if err != nil {
		response.Error(c, err)
		return
	}
	openOnly, err := httpx.QueryBool(c, "open_only")
	if err != nil {
		response.Error(c, err)
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	companyID := authctx.ActorFrom(ctx).CompanyID
	keyword := httpx.QueryString(c, "keyword")
	open := openOnly != nil && *openOnly
	rows, err := m.store.ListPayables(ctx, db.ListPayablesParams{
		CompanyID: companyID, SupplierID: supplierID, Keyword: keyword, FromDate: from, ToDate: to, OpenOnly: open,
		Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	sum, err := m.store.CountPayables(ctx, db.CountPayablesParams{
		CompanyID: companyID, SupplierID: supplierID, Keyword: keyword, FromDate: from, ToDate: to, OpenOnly: open,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]payableDTO, len(rows))
	for i, r := range rows {
		out[i] = payableDTO{
			ID: r.ID, SupplierID: r.SupplierID, SupplierCode: r.SupplierCode, SupplierName: r.SupplierName,
			SourceType: r.SourceType, SourceID: r.SourceID, SourceNo: r.SourceNo,
			DocDate: r.DocDate.Format(time.DateOnly), DueDate: r.DueDate.Format(time.DateOnly),
			Currency: r.Currency, ExchangeRate: r.ExchangeRate, Amount: r.Amount, BaseAmount: r.BaseAmount,
			PaidAmount: r.PaidAmount, Balance: r.Amount.Sub(r.PaidAmount), CreatedAt: r.CreatedAt,
		}
	}
	response.List(c, out, sumMeta{Meta: pg.Meta(sum.Total), BaseAmountSum: sum.BaseAmountSum})
}
