package finance

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
)

var errReceivableSettled = apperr.New(http.StatusConflict, "FIN-002", "應收帳款已有收款沖帳,請先取消沖帳")

// CreateReceivable 由出貨 / 退回過帳產生應收(退回為負數)。須在呼叫端的交易內執行;金額為 0 時不產生。
func CreateReceivable(ctx context.Context, q *db.Queries, p db.InsertReceivableParams) error {
	if p.Amount.IsZero() && p.BaseAmount.IsZero() {
		return nil
	}
	return q.InsertReceivable(ctx, p)
}

// RemoveReceivable 反過帳時移除來源單據產生的應收;已沖帳則不可移除。
func RemoveReceivable(ctx context.Context, q *db.Queries, sourceType string, sourceID int64) error {
	r, err := q.LockReceivableBySource(ctx, db.LockReceivableBySourceParams{SourceType: sourceType, SourceID: sourceID})
	if database.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !r.PaidAmount.IsZero() {
		return errReceivableSettled.WithDetails(map[string]string{"source_no": r.SourceNo})
	}
	return q.DeleteReceivable(ctx, r.ID)
}

type receivableDTO struct {
	ID           int64           `json:"id"`
	CustomerID   int64           `json:"customer_id"`
	CustomerCode string          `json:"customer_code"`
	CustomerName string          `json:"customer_name"`
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

// listReceivables 應收帳款明細;依客戶負責業務套用資料範圍。meta 另附本位幣合計。
func (m *Module) listReceivables(c *gin.Context) {
	ctx := c.Request.Context()
	customerID, err := httpx.QueryInt64(c, "customer_id")
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
	a := authctx.ActorFrom(ctx)
	deptID, userID := a.ScopeFilter()
	keyword := httpx.QueryString(c, "keyword")
	open := openOnly != nil && *openOnly
	rows, err := m.store.ListReceivables(ctx, db.ListReceivablesParams{
		CompanyID: a.CompanyID, CustomerID: customerID, Keyword: keyword, FromDate: from, ToDate: to, OpenOnly: open,
		ScopeUserID: userID, ScopeDeptID: deptID, Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	sum, err := m.store.CountReceivables(ctx, db.CountReceivablesParams{
		CompanyID: a.CompanyID, CustomerID: customerID, Keyword: keyword, FromDate: from, ToDate: to, OpenOnly: open,
		ScopeUserID: userID, ScopeDeptID: deptID,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]receivableDTO, len(rows))
	for i, r := range rows {
		out[i] = receivableDTO{
			ID: r.ID, CustomerID: r.CustomerID, CustomerCode: r.CustomerCode, CustomerName: r.CustomerName,
			SourceType: r.SourceType, SourceID: r.SourceID, SourceNo: r.SourceNo,
			DocDate: r.DocDate.Format(time.DateOnly), DueDate: r.DueDate.Format(time.DateOnly),
			Currency: r.Currency, ExchangeRate: r.ExchangeRate, Amount: r.Amount, BaseAmount: r.BaseAmount,
			PaidAmount: r.PaidAmount, Balance: r.Amount.Sub(r.PaidAmount), CreatedAt: r.CreatedAt,
		}
	}
	response.List(c, out, sumMeta{Meta: pg.Meta(sum.Total), BaseAmountSum: sum.BaseAmountSum})
}
