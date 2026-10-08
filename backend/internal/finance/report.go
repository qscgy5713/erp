package finance

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/masterdata"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/money"
	"erp/internal/shared/response"
	"erp/internal/system/permission"
)

// 對帳單與帳齡分析:side 為 receivable(客戶)或 payable(供應商);權限依邊別判斷。

func ledgerSide(c *gin.Context) (receivable bool, err error) {
	switch c.Query("side") {
	case "receivable":
		return true, nil
	case "payable":
		return false, nil
	}
	return false, fieldErr("side", "side 須為 receivable 或 payable")
}

func (m *Module) requireReport(c *gin.Context, a *authctx.Actor, receivable bool) bool {
	perm := permission.PayableRead
	if receivable {
		perm = permission.ReceivableRead
	}
	if !a.Can(perm) {
		response.Error(c, apperr.ErrForbidden)
		return false
	}
	return true
}

type statementRowDTO struct {
	Date    string          `json:"date"`
	Kind    string          `json:"kind"` // goods_receipt / purchase_return / delivery / sales_return / receipt / payment
	DocNo   string          `json:"doc_no"`
	RefID   int64           `json:"ref_id"`
	Delta   decimal.Decimal `json:"delta"` // 帳款增加為正,收付款 / 退回為負
	Balance decimal.Decimal `json:"balance"`
}

type statementDTO struct {
	PartnerID   int64             `json:"partner_id"`
	PartnerCode string            `json:"partner_code"`
	PartnerName string            `json:"partner_name"`
	Currency    string            `json:"currency"`
	From        string            `json:"from"`
	To          string            `json:"to"`
	Opening     decimal.Decimal   `json:"opening"`
	Closing     decimal.Decimal   `json:"closing"`
	Rows        []statementRowDTO `json:"rows"`
}

// statement GET /finance/statements?side=&partner_id=&currency=&from=&to=
// 單一對象、單一幣別的對帳單:期初 + 各筆帳款與已過帳收付款 = 期末。
func (m *Module) statement(c *gin.Context) {
	ctx := c.Request.Context()
	a := authctx.ActorFrom(ctx)
	recv, err := ledgerSide(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	if !m.requireReport(c, a, recv) {
		return
	}
	partnerID, err := httpx.QueryInt64(c, "partner_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	currency := strings.ToUpper(c.Query("currency"))
	if partnerID == nil || currency == "" {
		response.Error(c, fieldErr("partner_id", "請指定對象與幣別"))
		return
	}
	from, err := time.Parse(time.DateOnly, c.Query("from"))
	if err != nil {
		response.Error(c, fieldErr("from", "請指定開始日期(YYYY-MM-DD)"))
		return
	}
	to, err := time.Parse(time.DateOnly, c.Query("to"))
	if err != nil || to.Before(from) {
		response.Error(c, fieldErr("to", "請指定不早於開始日期的結束日期"))
		return
	}

	out := statementDTO{PartnerID: *partnerID, Currency: currency, From: c.Query("from"), To: c.Query("to"), Rows: []statementRowDTO{}}
	type row struct {
		date  time.Time
		kind  string
		docNo string
		ref   int64
		delta decimal.Decimal
	}
	var rows []row
	if recv {
		cust, err := m.store.CustomerForDoc(ctx, db.CustomerForDocParams{ID: *partnerID, CompanyID: a.CompanyID})
		if database.IsNoRows(err) || (err == nil && !masterdata.CustomerVisible(a, cust.SalesUserID, cust.SalesDepartmentID)) {
			response.Error(c, apperr.ErrNotFound)
			return
		} else if err != nil {
			response.Error(c, err)
			return
		}
		out.PartnerCode, out.PartnerName = cust.Code, cust.Name
		if out.Opening, err = m.store.ReceivableStatementOpening(ctx, db.ReceivableStatementOpeningParams{
			CompanyID: a.CompanyID, PartnerID: *partnerID, Currency: currency, FromDate: from,
		}); err != nil {
			response.Error(c, err)
			return
		}
		rs, err := m.store.ReceivableStatement(ctx, db.ReceivableStatementParams{
			CompanyID: a.CompanyID, PartnerID: *partnerID, Currency: currency, FromDate: from, ToDate: to,
		})
		if err != nil {
			response.Error(c, err)
			return
		}
		for _, r := range rs {
			rows = append(rows, row{r.TxnDate, r.Kind, r.DocNo, r.RefID, r.Delta})
		}
	} else {
		sup, err := m.store.SupplierForDoc(ctx, db.SupplierForDocParams{ID: *partnerID, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			response.Error(c, apperr.ErrNotFound)
			return
		} else if err != nil {
			response.Error(c, err)
			return
		}
		out.PartnerCode, out.PartnerName = sup.Code, sup.Name
		if out.Opening, err = m.store.PayableStatementOpening(ctx, db.PayableStatementOpeningParams{
			CompanyID: a.CompanyID, PartnerID: *partnerID, Currency: currency, FromDate: from,
		}); err != nil {
			response.Error(c, err)
			return
		}
		rs, err := m.store.PayableStatement(ctx, db.PayableStatementParams{
			CompanyID: a.CompanyID, PartnerID: *partnerID, Currency: currency, FromDate: from, ToDate: to,
		})
		if err != nil {
			response.Error(c, err)
			return
		}
		for _, r := range rs {
			rows = append(rows, row{r.TxnDate, r.Kind, r.DocNo, r.RefID, r.Delta})
		}
	}
	bal := out.Opening
	for _, r := range rows {
		bal = bal.Add(r.delta)
		out.Rows = append(out.Rows, statementRowDTO{
			Date: r.date.Format(time.DateOnly), Kind: r.kind, DocNo: r.docNo, RefID: r.ref, Delta: r.delta, Balance: bal,
		})
	}
	out.Closing = bal
	response.OK(c, out)
}

type agingRowDTO struct {
	PartnerID   int64           `json:"partner_id"`
	PartnerCode string          `json:"partner_code"`
	PartnerName string          `json:"partner_name"`
	NotDue      decimal.Decimal `json:"not_due"`
	D1to30      decimal.Decimal `json:"d1_30"`
	D31to60     decimal.Decimal `json:"d31_60"`
	D61to90     decimal.Decimal `json:"d61_90"`
	D90Plus     decimal.Decimal `json:"d90_plus"`
	Total       decimal.Decimal `json:"total"`
}

// aging GET /finance/aging?side=&as_of=
// 依到期日分組的未沖餘額(本位幣):尚未到期、逾期 1–30 / 31–60 / 61–90 / 90 天以上。
// 餘額為「目前」未沖餘額(不回推歷史沖帳),as_of 只決定逾期天數與納入的單據日期。
func (m *Module) aging(c *gin.Context) {
	ctx := c.Request.Context()
	a := authctx.ActorFrom(ctx)
	recv, err := ledgerSide(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	if !m.requireReport(c, a, recv) {
		return
	}
	asOf := time.Now().In(time.FixedZone("TST", 8*3600))
	asOf = time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 0, 0, 0, 0, time.UTC)
	if s := c.Query("as_of"); s != "" {
		if asOf, err = time.Parse(time.DateOnly, s); err != nil {
			response.Error(c, fieldErr("as_of", "日期格式須為 YYYY-MM-DD"))
			return
		}
	}
	out := []agingRowDTO{}
	r := func(d decimal.Decimal) decimal.Decimal { return money.Amount(d) }
	if recv {
		deptID, userID := a.ScopeFilter()
		rows, err := m.store.ReceivableAging(ctx, db.ReceivableAgingParams{
			CompanyID: a.CompanyID, AsOf: asOf, ScopeUserID: userID, ScopeDeptID: deptID,
		})
		if err != nil {
			response.Error(c, err)
			return
		}
		for _, x := range rows {
			out = append(out, agingRowDTO{x.PartnerID, x.PartnerCode, x.PartnerName, r(x.NotDue), r(x.D130), r(x.D3160), r(x.D6190), r(x.D90Plus), r(x.Total)})
		}
	} else {
		rows, err := m.store.PayableAging(ctx, db.PayableAgingParams{CompanyID: a.CompanyID, AsOf: asOf})
		if err != nil {
			response.Error(c, err)
			return
		}
		for _, x := range rows {
			out = append(out, agingRowDTO{x.PartnerID, x.PartnerCode, x.PartnerName, r(x.NotDue), r(x.D130), r(x.D3160), r(x.D6190), r(x.D90Plus), r(x.Total)})
		}
	}
	response.OK(c, gin.H{"as_of": asOf.Format(time.DateOnly), "rows": out})
}
