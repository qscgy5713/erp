package gl

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
)

// 報表只計已過帳的傳票。金額為本位幣;期初 / 期末為「借方 − 貸方」的淨額(正為借餘)。

func dateRange(c *gin.Context) (time.Time, time.Time, error) {
	from, err := time.Parse(time.DateOnly, c.Query("from"))
	if err != nil {
		return from, from, fieldErr("from", "請指定開始日期(YYYY-MM-DD)")
	}
	to, err := time.Parse(time.DateOnly, c.Query("to"))
	if err != nil {
		return from, to, fieldErr("to", "請指定結束日期(YYYY-MM-DD)")
	}
	if to.Before(from) {
		return from, to, fieldErr("to", "結束日期不可早於開始日期")
	}
	return from, to, nil
}

type trialRowDTO struct {
	AccountID    int64           `json:"account_id"`
	Code         string          `json:"code"`
	Name         string          `json:"name"`
	AcctType     string          `json:"acct_type"`
	Opening      decimal.Decimal `json:"opening"`
	PeriodDebit  decimal.Decimal `json:"period_debit"`
	PeriodCredit decimal.Decimal `json:"period_credit"`
	Closing      decimal.Decimal `json:"closing"`
}

// trialBalance GET /gl/reports/trial-balance?from=&to=
// 每個有異動的科目:期初、本期借貸、期末;最後附借貸總額核對(合計的期初與期末應為 0)。
func (m *Module) trialBalance(c *gin.Context) {
	from, to, err := dateRange(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	rows, err := m.store.TrialBalance(c.Request.Context(), db.TrialBalanceParams{
		CompanyID: actor(c).CompanyID, FromDate: from, ToDate: to,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]trialRowDTO, 0, len(rows))
	var open, debit, credit decimal.Decimal
	for _, r := range rows {
		closing := r.Opening.Add(r.PeriodDebit).Sub(r.PeriodCredit)
		if r.Opening.IsZero() && r.PeriodDebit.IsZero() && r.PeriodCredit.IsZero() {
			continue
		}
		out = append(out, trialRowDTO{
			AccountID: r.AccountID, Code: r.Code, Name: r.Name, AcctType: r.AcctType, Opening: r.Opening,
			PeriodDebit: r.PeriodDebit, PeriodCredit: r.PeriodCredit, Closing: closing,
		})
		open, debit, credit = open.Add(r.Opening), debit.Add(r.PeriodDebit), credit.Add(r.PeriodCredit)
	}
	response.OK(c, gin.H{
		"rows": out, "total_opening": open, "total_debit": debit, "total_credit": credit,
		"balanced": open.IsZero() && debit.Equal(credit),
	})
}

type ledgerRowDTO struct {
	VoucherID   int64           `json:"voucher_id"`
	DocNo       string          `json:"doc_no"`
	Date        string          `json:"date"`
	SourceType  string          `json:"source_type"`
	SourceNo    string          `json:"source_no"`
	Description string          `json:"description"`
	Debit       decimal.Decimal `json:"debit"`
	Credit      decimal.Decimal `json:"credit"`
	Balance     decimal.Decimal `json:"balance"`
}

// generalLedger GET /gl/reports/ledger?account_id=&from=&to=
// 單一科目的期初餘額與期間明細(含累計餘額)。
func (m *Module) generalLedger(c *gin.Context) {
	ctx := c.Request.Context()
	accountID, err := httpx.QueryInt64(c, "account_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	if accountID == nil {
		response.Error(c, fieldErr("account_id", "請選擇科目"))
		return
	}
	from, to, err := dateRange(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	companyID := actor(c).CompanyID
	acct, err := m.store.GetAccount(ctx, db.GetAccountParams{ID: *accountID, CompanyID: companyID})
	if database.IsNoRows(err) {
		response.Error(c, apperr.ErrNotFound)
		return
	} else if err != nil {
		response.Error(c, err)
		return
	}
	opening, err := m.store.GeneralLedgerOpening(ctx, db.GeneralLedgerOpeningParams{CompanyID: companyID, AccountID: *accountID, FromDate: from})
	if err != nil {
		response.Error(c, err)
		return
	}
	rows, err := m.store.GeneralLedger(ctx, db.GeneralLedgerParams{CompanyID: companyID, AccountID: *accountID, FromDate: from, ToDate: to})
	if err != nil {
		response.Error(c, err)
		return
	}
	bal := opening
	out := make([]ledgerRowDTO, len(rows))
	var debit, credit decimal.Decimal
	for i, r := range rows {
		bal = bal.Add(r.Debit).Sub(r.Credit)
		debit, credit = debit.Add(r.Debit), credit.Add(r.Credit)
		out[i] = ledgerRowDTO{
			VoucherID: r.VoucherID, DocNo: r.DocNo, Date: r.VoucherDate.Format(time.DateOnly), SourceType: r.SourceType,
			SourceNo: r.SourceNo, Description: r.Description, Debit: r.Debit, Credit: r.Credit, Balance: bal,
		}
	}
	response.OK(c, gin.H{
		"account_code": acct.Code, "account_name": acct.Name, "opening": opening, "total_debit": debit,
		"total_credit": credit, "closing": bal, "rows": out,
	})
}

type journalRowDTO struct {
	VoucherID          int64           `json:"voucher_id"`
	DocNo              string          `json:"doc_no"`
	Date               string          `json:"date"`
	SourceType         string          `json:"source_type"`
	SourceNo           string          `json:"source_no"`
	VoucherDescription string          `json:"voucher_description"`
	LineNo             int32           `json:"line_no"`
	AccountCode        string          `json:"account_code"`
	AccountName        string          `json:"account_name"`
	Debit              decimal.Decimal `json:"debit"`
	Credit             decimal.Decimal `json:"credit"`
	Description        string          `json:"description"`
}

// journal GET /gl/reports/journal?from=&to=&page=&size=  依日期與傳票號碼的分錄流水(日記帳)。
func (m *Module) journal(c *gin.Context) {
	ctx := c.Request.Context()
	from, to, err := dateRange(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	rows, err := m.store.Journal(ctx, db.JournalParams{CompanyID: companyID, FromDate: from, ToDate: to, Lim: pg.Limit(), Off: pg.Offset()})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountJournal(ctx, db.CountJournalParams{CompanyID: companyID, FromDate: from, ToDate: to})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]journalRowDTO, len(rows))
	for i, r := range rows {
		out[i] = journalRowDTO{
			VoucherID: r.VoucherID, DocNo: r.DocNo, Date: r.VoucherDate.Format(time.DateOnly), SourceType: r.SourceType,
			SourceNo: r.SourceNo, VoucherDescription: r.VoucherDescription, LineNo: r.LineNo, AccountCode: r.AccountCode,
			AccountName: r.AccountName, Debit: r.Debit, Credit: r.Credit, Description: r.Description,
		}
	}
	response.List(c, out, pg.Meta(total))
}
