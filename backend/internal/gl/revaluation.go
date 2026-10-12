package gl

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/masterdata"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/money"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
)

// 外幣期末重估(D86):
//   - 把截止日(月底)的外幣應收 / 應付未沖餘額,以當日匯率重新換算,與帳面本位幣金額的差額記入未實現兌換損益;
//   - 重估傳票於下一次重估時自動沖回(沖銷傳票日期為新期間的第一天),再依新匯率重新估算;
//   - 只能依期間順序重估,可撤銷最近一次(連同當次沖回上期的傳票)。
// 收付款時的已實現損益仍以帳款入帳金額為基準(D81),因此沖回後不會重複認列。

var periodRe = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

type revalRow struct {
	Currency   string          `json:"currency"`
	Rate       decimal.Decimal `json:"rate"`
	ArForeign  decimal.Decimal `json:"ar_foreign"`
	ArCarrying decimal.Decimal `json:"ar_carrying"`
	ArRevalued decimal.Decimal `json:"ar_revalued"`
	ApForeign  decimal.Decimal `json:"ap_foreign"`
	ApCarrying decimal.Decimal `json:"ap_carrying"`
	ApRevalued decimal.Decimal `json:"ap_revalued"`
}

type revalResult struct {
	Period string
	AsOf   time.Time
	Rows   []revalRow
	ArDiff decimal.Decimal // 應收:重估後 − 帳面(正 = 增加)
	ApDiff decimal.Decimal // 應付:重估後 − 帳面(正 = 負債增加)
}

func parsePeriod(s string) (start, end time.Time, err error) {
	if !periodRe.MatchString(s) {
		return start, end, apperr.Validation(map[string]string{"period": "期間格式須為 YYYY-MM"})
	}
	start, _ = time.Parse("2006-01", s)
	return start, start.AddDate(0, 1, -1), nil
}

func computeReval(ctx context.Context, q *db.Queries, companyID int64, period string) (revalResult, error) {
	_, end, err := parsePeriod(period)
	if err != nil {
		return revalResult{}, err
	}
	res := revalResult{Period: period, AsOf: end}
	ar, err := q.FxOpenReceivables(ctx, db.FxOpenReceivablesParams{CompanyID: companyID, AsOf: end, BaseCurrency: masterdata.BaseCurrency})
	if err != nil {
		return res, err
	}
	ap, err := q.FxOpenPayables(ctx, db.FxOpenPayablesParams{CompanyID: companyID, AsOf: end, BaseCurrency: masterdata.BaseCurrency})
	if err != nil {
		return res, err
	}
	byCur := map[string]*revalRow{}
	var order []string
	get := func(cur string) (*revalRow, error) {
		if r := byCur[cur]; r != nil {
			return r, nil
		}
		rate, err := masterdata.RateOn(ctx, q, companyID, cur, end)
		if err != nil {
			return nil, err
		}
		r := &revalRow{Currency: cur, Rate: rate}
		byCur[cur] = r
		order = append(order, cur)
		return r, nil
	}
	for _, x := range ar {
		r, err := get(x.Currency)
		if err != nil {
			return res, err
		}
		r.ArForeign, r.ArCarrying = x.ForeignBalance, money.Amount(x.Carrying)
		r.ArRevalued = money.Amount(x.ForeignBalance.Mul(r.Rate))
		res.ArDiff = res.ArDiff.Add(r.ArRevalued.Sub(r.ArCarrying))
	}
	for _, x := range ap {
		r, err := get(x.Currency)
		if err != nil {
			return res, err
		}
		r.ApForeign, r.ApCarrying = x.ForeignBalance, money.Amount(x.Carrying)
		r.ApRevalued = money.Amount(x.ForeignBalance.Mul(r.Rate))
		res.ApDiff = res.ApDiff.Add(r.ApRevalued.Sub(r.ApCarrying))
	}
	for _, c := range order {
		res.Rows = append(res.Rows, *byCur[c])
	}
	return res, nil
}

// revalEntries 應收增加 → 借 應收;應付增加 → 貸 應付;淨額為未實現利益(貸)或損失(借)。
func revalEntries(arDiff, apDiff decimal.Decimal) []Entry {
	var out []Entry
	if arDiff.IsPositive() {
		out = append(out, Entry{Key: "sales.receivable", Debit: arDiff})
	} else if arDiff.IsNegative() {
		out = append(out, Entry{Key: "sales.receivable", Credit: arDiff.Neg()})
	}
	if apDiff.IsPositive() {
		out = append(out, Entry{Key: "purchase.payable", Credit: apDiff})
	} else if apDiff.IsNegative() {
		out = append(out, Entry{Key: "purchase.payable", Debit: apDiff.Neg()})
	}
	net := arDiff.Sub(apDiff) // 正 = 利益
	if net.IsPositive() {
		out = append(out, Entry{Key: "fx.unrealized_gain", Credit: net})
	} else if net.IsNegative() {
		out = append(out, Entry{Key: "fx.unrealized_loss", Debit: net.Neg()})
	}
	return out
}

// swapSides 借貸對調(沖回時沿用原科目,損益才會在同一個科目內抵銷)。
func swapSides(es []Entry) []Entry {
	out := make([]Entry, len(es))
	for i, e := range es {
		e.Debit, e.Credit = e.Credit, e.Debit
		out[i] = e
	}
	return out
}

type revalHistoryDTO struct {
	ID        int64           `json:"id"`
	Period    string          `json:"period"`
	Status    string          `json:"status"`
	ArDiff    decimal.Decimal `json:"ar_diff"`
	ApDiff    decimal.Decimal `json:"ap_diff"`
	VoucherID *int64          `json:"voucher_id"`
}

// revaluationPreview GET /gl/revaluation?period=YYYY-MM 試算(不寫入)與最近的重估紀錄。
func (m *Module) revaluationPreview(c *gin.Context) {
	ctx := c.Request.Context()
	cid := actor(c).CompanyID
	period := c.Query("period")
	out := gin.H{}
	if period != "" {
		res, err := computeReval(ctx, m.store.Queries, cid, period)
		if err != nil {
			response.Error(c, err)
			return
		}
		rows := res.Rows
		if rows == nil {
			rows = []revalRow{}
		}
		out["period"], out["as_of"] = period, res.AsOf.Format(time.DateOnly)
		out["rows"], out["ar_diff"], out["ap_diff"], out["net_gain"] = rows, res.ArDiff, res.ApDiff, res.ArDiff.Sub(res.ApDiff)
	}
	hist, err := m.store.ListFxRevaluations(ctx, cid)
	if err != nil {
		response.Error(c, err)
		return
	}
	h := make([]revalHistoryDTO, len(hist))
	for i, r := range hist {
		h[i] = revalHistoryDTO{r.ID, r.Period, r.Status, r.ArDiff, r.ApDiff, r.VoucherID}
	}
	out["history"] = h
	response.OK(c, out)
}

var (
	errRevalOrder = func(latest string) *apperr.Error {
		return apperr.Conflict("GL-070", fmt.Sprintf("只能依期間順序重估:最近一次重估為 %s,請從其後的期間開始;若要重做請先撤銷", latest))
	}
	errRevalFuture = apperr.Conflict("GL-071", "期間尚未結束,不可重估")
	errRevalLatest = apperr.Conflict("GL-072", "只能撤銷最近一次的重估")
)

// runRevaluation POST /gl/revaluation/:period
func (m *Module) runRevaluation(c *gin.Context) {
	period := c.Param("period")
	start, end, err := parsePeriod(period)
	if err != nil {
		response.Error(c, err)
		return
	}
	if end.After(time.Now()) {
		response.Error(c, errRevalFuture)
		return
	}
	a := actor(c)
	opt := Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
	var created revalHistoryDTO
	err = m.store.InTx(c.Request.Context(), func(q *db.Queries) error {
		ctx := c.Request.Context()
		prev, err := q.LatestActiveFxRevaluation(ctx, a.CompanyID)
		hasPrev := err == nil
		if err != nil && !database.IsNoRows(err) {
			return err
		}
		if hasPrev && prev.Period >= period {
			return errRevalOrder(prev.Period)
		}
		res, err := computeReval(ctx, q, a.CompanyID, period)
		if err != nil {
			return err
		}
		row, err := q.CreateFxRevaluation(ctx, db.CreateFxRevaluationParams{
			CompanyID: a.CompanyID, Period: period, ArDiff: res.ArDiff, ApDiff: res.ApDiff, CreatedBy: &a.UserID,
		})
		if err != nil {
			return err
		}
		// 沖回上一次重估:以相反分錄另開傳票(日期為本期第一天),撤銷時各自沖銷
		var prevID *int64
		if hasPrev {
			prevID = &prev.ID
			if err := PostSource(ctx, q, opt, Source{
				Type: "fx_reval_rev", ID: prev.ID, No: prev.Period, Date: start, Desc: fmt.Sprintf("沖回 %s 外幣期末重估", prev.Period),
			}, swapSides(revalEntries(prev.ArDiff, prev.ApDiff))); err != nil {
				return err
			}
		}
		if err := PostSource(ctx, q, opt, Source{
			Type: "fx_reval", ID: row.ID, No: period, Date: end, Desc: fmt.Sprintf("%s 外幣期末重估", period),
		}, revalEntries(res.ArDiff, res.ApDiff)); err != nil {
			return err
		}
		var vid *int64
		if id, err := q.OpenVoucherBySourceID(ctx, db.OpenVoucherBySourceIDParams{CompanyID: a.CompanyID, SourceType: "fx_reval", SourceID: &row.ID}); err == nil {
			vid = &id
		} else if !database.IsNoRows(err) {
			return err
		}
		if err := q.SetFxRevaluationVouchers(ctx, db.SetFxRevaluationVouchersParams{ID: row.ID, VoucherID: vid, PrevRevaluationID: prevID}); err != nil {
			return err
		}
		created = revalHistoryDTO{row.ID, period, "active", res.ArDiff, res.ApDiff, vid}
		return audit.Record(ctx, q, audit.Entry{
			Action: "create", EntityType: "fx_revaluation", EntityID: &row.ID,
			Summary: fmt.Sprintf("%s 外幣期末重估:應收差額 %s、應付差額 %s", period, res.ArDiff, res.ApDiff),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, created)
}

// undoRevaluation POST /gl/revaluation/:period/undo 撤銷最近一次重估:沖銷該次傳票,並還原當次沖回上期的動作。
func (m *Module) undoRevaluation(c *gin.Context) {
	period := c.Param("period")
	if _, _, err := parsePeriod(period); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	opt := Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
	err := m.store.InTx(c.Request.Context(), func(q *db.Queries) error {
		ctx := c.Request.Context()
		latest, err := q.LatestActiveFxRevaluation(ctx, a.CompanyID)
		if database.IsNoRows(err) || (err == nil && latest.Period != period) {
			return errRevalLatest
		}
		if err != nil {
			return err
		}
		if err := ReverseSource(ctx, q, opt, "fx_reval", latest.ID); err != nil {
			return err
		}
		if latest.PrevRevaluationID != nil {
			if err := ReverseSource(ctx, q, opt, "fx_reval_rev", *latest.PrevRevaluationID); err != nil {
				return err
			}
		}
		if err := q.SetFxRevaluationStatus(ctx, db.SetFxRevaluationStatusParams{ID: latest.ID, Status: "undone"}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: "undo", EntityType: "fx_revaluation", EntityID: &latest.ID, Summary: period + " 外幣期末重估撤銷",
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
