package masterdata

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
)

// BaseCurrency 本位幣(第一期單一公司,固定新台幣)。
const BaseCurrency = "TWD"

var (
	errRateDup        = apperr.Conflict("FX-001", "該幣別在此日期已有匯率")
	errRateNotFound   = apperr.New(http.StatusUnprocessableEntity, "FX-002", "找不到該日期(含之前)的匯率,請先設定匯率")
	errTaxCodeDup     = apperr.Conflict("TAX-001", "稅別代碼已存在")
	errTermCodeDup    = apperr.Conflict("TERM-001", "付款條件代碼已存在")
	errBaseCurrencyFX = fieldErr("currency", "本位幣不需設定匯率")
)

// ---- 幣別 ----

type currencyDTO struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Symbol    string `json:"symbol"`
	Decimals  int16  `json:"decimals"`
	IsActive  bool   `json:"is_active"`
	IsBase    bool   `json:"is_base"`
	SortOrder int32  `json:"sort_order"`
}

func toCurrencyDTO(c db.Currency) currencyDTO {
	return currencyDTO{Code: c.Code, Name: c.Name, Symbol: c.Symbol, Decimals: c.Decimals,
		IsActive: c.IsActive, IsBase: c.Code == BaseCurrency, SortOrder: c.SortOrder}
}

func (m *Module) listCurrencies(c *gin.Context) {
	rows, err := m.store.ListCurrencies(c.Request.Context())
	out := make([]currencyDTO, len(rows))
	for i, r := range rows {
		out[i] = toCurrencyDTO(r)
	}
	reply(c, http.StatusOK, out, err)
}

type currencyActiveInput struct {
	IsActive bool `json:"is_active"`
}

func (m *Module) setCurrencyActive(c *gin.Context) {
	in, ok := bind[currencyActiveInput](c)
	if !ok {
		return
	}
	code := strings.ToUpper(c.Param("code"))
	if code == BaseCurrency && !in.IsActive {
		response.Error(c, fieldErr("is_active", "本位幣不可停用"))
		return
	}
	ctx := c.Request.Context()
	var out db.Currency
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetCurrency(ctx, code)
		if err != nil {
			return notFoundOr(err)
		}
		if out, err = q.SetCurrencyActive(ctx, db.SetCurrencyActiveParams{Code: code, IsActive: in.IsActive}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "currency", Summary: "修改幣別 " + code,
			Before: toCurrencyDTO(before), After: toCurrencyDTO(out),
		})
	})
	reply(c, http.StatusOK, toCurrencyDTO(out), err)
}

// ---- 匯率 ----

type exchangeRateDTO struct {
	ID        int64           `json:"id"`
	Currency  string          `json:"currency"`
	RateDate  string          `json:"rate_date"`
	Rate      decimal.Decimal `json:"rate"`
	Version   int32           `json:"version"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func toRateDTO(r db.ExchangeRate) exchangeRateDTO {
	return exchangeRateDTO{ID: r.ID, Currency: r.Currency, RateDate: r.RateDate.Format(dateLayout),
		Rate: r.Rate, Version: r.Version, UpdatedAt: r.UpdatedAt}
}

func optionalDate(c *gin.Context, name string) (*time.Time, error) {
	s := c.Query(name)
	if s == "" {
		return nil, nil
	}
	t, err := parseDate(name, s)
	return &t, err
}

func (m *Module) listExchangeRates(c *gin.Context) {
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
	ctx := c.Request.Context()
	pg := page.Parse(c.Query("page"), c.Query("size"))
	var currency *string
	if s := httpx.QueryString(c, "currency"); s != nil {
		v := strings.ToUpper(*s)
		currency = &v
	}
	companyID := actor(c).CompanyID
	rows, err := m.store.ListExchangeRates(ctx, db.ListExchangeRatesParams{
		CompanyID: companyID, Currency: currency, FromDate: from, ToDate: to, Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountExchangeRates(ctx, db.CountExchangeRatesParams{
		CompanyID: companyID, Currency: currency, FromDate: from, ToDate: to,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]exchangeRateDTO, len(rows))
	for i, r := range rows {
		out[i] = toRateDTO(r)
	}
	response.List(c, out, pg.Meta(total))
}

// RateOn 取得指定日期適用的匯率(該日或之前最近一筆);本位幣固定為 1。
// 供開單時換算本位幣金額使用。
func RateOn(ctx context.Context, q *db.Queries, companyID int64, currency string, on time.Time) (decimal.Decimal, error) {
	if currency == BaseCurrency {
		return decimal.NewFromInt(1), nil
	}
	r, err := q.RateOn(ctx, db.RateOnParams{CompanyID: companyID, Currency: currency, OnDate: on})
	if database.IsNoRows(err) {
		return decimal.Zero, errRateNotFound.WithDetails(map[string]string{
			"currency": currency, "date": on.Format(dateLayout),
		})
	}
	if err != nil {
		return decimal.Zero, fmt.Errorf("讀取匯率: %w", err)
	}
	return r.Rate, nil
}

// lookupExchangeRate GET /exchange-rates/lookup?currency=USD&date=2026-10-07
func (m *Module) lookupExchangeRate(c *gin.Context) {
	currency := strings.ToUpper(c.Query("currency"))
	on, err := parseDate("date", c.Query("date"))
	if err != nil {
		response.Error(c, err)
		return
	}
	rate, err := RateOn(c.Request.Context(), m.store.Queries, actor(c).CompanyID, currency, on)
	reply(c, http.StatusOK, gin.H{"currency": currency, "date": on.Format(dateLayout), "rate": rate}, err)
}

type exchangeRateInput struct {
	Currency string          `json:"currency" binding:"required,len=3"`
	RateDate string          `json:"rate_date" binding:"required"`
	Rate     decimal.Decimal `json:"rate"`
}

func validRate(rate decimal.Decimal) error {
	if !rate.IsPositive() {
		return fieldErr("rate", "匯率須大於 0")
	}
	if rate.Exponent() < -6 {
		return fieldErr("rate", "最多 6 位小數")
	}
	return nil
}

func (m *Module) createExchangeRate(c *gin.Context) {
	in, ok := bind[exchangeRateInput](c)
	if !ok {
		return
	}
	in.Currency = strings.ToUpper(in.Currency)
	if in.Currency == BaseCurrency {
		response.Error(c, errBaseCurrencyFX)
		return
	}
	date, err := parseDate("rate_date", in.RateDate)
	if err == nil {
		err = validRate(in.Rate)
	}
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var out db.ExchangeRate
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		if _, err := q.GetCurrency(ctx, in.Currency); err != nil {
			if database.IsNoRows(err) {
				return fieldErr("currency", "幣別不存在")
			}
			return err
		}
		var err error
		out, err = q.CreateExchangeRate(ctx, db.CreateExchangeRateParams{
			CompanyID: a.CompanyID, Currency: in.Currency, RateDate: date, Rate: in.Rate, CreatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(err, "exchange_rates_company_currency_date_key", errRateDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "exchange_rate", EntityID: &out.ID,
			Summary: fmt.Sprintf("新增匯率 %s %s = %s", out.Currency, in.RateDate, out.Rate), After: toRateDTO(out),
		})
	})
	reply(c, http.StatusCreated, toRateDTO(out), err)
}

type updateRateInput struct {
	Rate    decimal.Decimal `json:"rate"`
	Version int32           `json:"version" binding:"required"`
}

// updateExchangeRate 只能改匯率值;幣別或日期打錯請刪除重建。
func (m *Module) updateExchangeRate(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	in, ok := bind[updateRateInput](c)
	if !ok {
		return
	}
	if err := validRate(in.Rate); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var out db.ExchangeRate
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetExchangeRate(ctx, db.GetExchangeRateParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return notFoundOr(err)
		}
		out, err = q.UpdateExchangeRate(ctx, db.UpdateExchangeRateParams{
			ID: id, CompanyID: a.CompanyID, Rate: in.Rate, Version: in.Version, UpdatedBy: &a.UserID,
		})
		if err != nil {
			return versionConflictOr(err)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "exchange_rate", EntityID: &id,
			Summary: fmt.Sprintf("修改匯率 %s %s", out.Currency, out.RateDate.Format(dateLayout)),
			Before:  toRateDTO(before), After: toRateDTO(out),
		})
	})
	reply(c, http.StatusOK, toRateDTO(out), err)
}

func (m *Module) deleteExchangeRate(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetExchangeRate(ctx, db.GetExchangeRateParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return notFoundOr(err)
		}
		if _, err := q.DeleteExchangeRate(ctx, db.DeleteExchangeRateParams{ID: id, CompanyID: a.CompanyID}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Delete, EntityType: "exchange_rate", EntityID: &id,
			Summary: fmt.Sprintf("刪除匯率 %s %s", before.Currency, before.RateDate.Format(dateLayout)),
			Before:  toRateDTO(before),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

// ---- 稅別 ----

type taxTypeDTO struct {
	ID       int64           `json:"id"`
	Code     string          `json:"code"`
	Name     string          `json:"name"`
	Kind     string          `json:"kind"`
	Rate     decimal.Decimal `json:"rate"`
	IsActive bool            `json:"is_active"`
	Version  int32           `json:"version"`
}

func toTaxTypeDTO(t db.TaxType) taxTypeDTO {
	return taxTypeDTO{ID: t.ID, Code: t.Code, Name: t.Name, Kind: t.Kind, Rate: t.Rate, IsActive: t.IsActive, Version: t.Version}
}

func (m *Module) listTaxTypes(c *gin.Context) {
	rows, err := m.store.ListTaxTypes(c.Request.Context(), actor(c).CompanyID)
	out := make([]taxTypeDTO, len(rows))
	for i, r := range rows {
		out[i] = toTaxTypeDTO(r)
	}
	reply(c, http.StatusOK, out, err)
}

type taxTypeInput struct {
	Code     string          `json:"code" binding:"required,max=10"`
	Name     string          `json:"name" binding:"required,max=50"`
	Kind     string          `json:"kind" binding:"required,oneof=taxable zero exempt"`
	Rate     decimal.Decimal `json:"rate"`
	IsActive bool            `json:"is_active"`
	Version  int32           `json:"version"`
}

func (in *taxTypeInput) normalize() error {
	in.Code = normCode(in.Code)
	in.Name = strings.TrimSpace(in.Name)
	if in.Kind != "taxable" && !in.Rate.IsZero() {
		return fieldErr("rate", "零稅率與免稅的稅率須為 0")
	}
	if in.Rate.IsNegative() || in.Rate.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		return fieldErr("rate", "稅率須介於 0 與 1 之間(例:5% 填 0.05)")
	}
	if in.Rate.Exponent() < -4 {
		return fieldErr("rate", "最多 4 位小數")
	}
	return nil
}

func (m *Module) createTaxType(c *gin.Context) {
	in, ok := bind[taxTypeInput](c)
	if !ok {
		return
	}
	if err := in.normalize(); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var out db.TaxType
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		var err error
		out, err = q.CreateTaxType(ctx, db.CreateTaxTypeParams{
			CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, Kind: in.Kind, Rate: in.Rate, CreatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(err, "tax_types_company_code_key", errTaxCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "tax_type", EntityID: &out.ID,
			Summary: "新增稅別 " + out.Code + " " + out.Name, After: toTaxTypeDTO(out),
		})
	})
	reply(c, http.StatusCreated, toTaxTypeDTO(out), err)
}

func (m *Module) updateTaxType(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	in, ok := bind[taxTypeInput](c)
	if !ok {
		return
	}
	if err := in.normalize(); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var out db.TaxType
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetTaxType(ctx, db.GetTaxTypeParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return notFoundOr(err)
		}
		out, err = q.UpdateTaxType(ctx, db.UpdateTaxTypeParams{
			ID: id, CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, Kind: in.Kind, Rate: in.Rate,
			IsActive: in.IsActive, Version: in.Version, UpdatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(versionConflictOr(err), "tax_types_company_code_key", errTaxCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "tax_type", EntityID: &id,
			Summary: "修改稅別 " + out.Code + " " + out.Name, Before: toTaxTypeDTO(before), After: toTaxTypeDTO(out),
		})
	})
	reply(c, http.StatusOK, toTaxTypeDTO(out), err)
}

// ---- 付款條件 ----

type paymentTermDTO struct {
	ID         int64  `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	IsMonthEnd bool   `json:"is_month_end"`
	NetDays    int32  `json:"net_days"`
	IsActive   bool   `json:"is_active"`
	Version    int32  `json:"version"`
}

func toTermDTO(t db.PaymentTerm) paymentTermDTO {
	return paymentTermDTO{ID: t.ID, Code: t.Code, Name: t.Name, IsMonthEnd: t.IsMonthEnd, NetDays: t.NetDays,
		IsActive: t.IsActive, Version: t.Version}
}

// DueDate 依付款條件計算到期日:月結以單據日所在月底起算,否則以單據日起算。
func DueDate(t db.PaymentTerm, docDate time.Time) time.Time {
	base := docDate
	if t.IsMonthEnd {
		base = time.Date(docDate.Year(), docDate.Month()+1, 0, 0, 0, 0, 0, docDate.Location())
	}
	return base.AddDate(0, 0, int(t.NetDays))
}

func (m *Module) listPaymentTerms(c *gin.Context) {
	rows, err := m.store.ListPaymentTerms(c.Request.Context(), actor(c).CompanyID)
	out := make([]paymentTermDTO, len(rows))
	for i, r := range rows {
		out[i] = toTermDTO(r)
	}
	reply(c, http.StatusOK, out, err)
}

type paymentTermInput struct {
	Code       string `json:"code" binding:"required,max=20"`
	Name       string `json:"name" binding:"required,max=50"`
	IsMonthEnd bool   `json:"is_month_end"`
	NetDays    int32  `json:"net_days" binding:"min=0,max=365"`
	IsActive   bool   `json:"is_active"`
	Version    int32  `json:"version"`
}

func (m *Module) createPaymentTerm(c *gin.Context) {
	in, ok := bind[paymentTermInput](c)
	if !ok {
		return
	}
	in.Code, in.Name = normCode(in.Code), strings.TrimSpace(in.Name)
	ctx := c.Request.Context()
	a := actor(c)
	var out db.PaymentTerm
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		var err error
		out, err = q.CreatePaymentTerm(ctx, db.CreatePaymentTermParams{
			CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, IsMonthEnd: in.IsMonthEnd, NetDays: in.NetDays,
			CreatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(err, "payment_terms_company_code_key", errTermCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "payment_term", EntityID: &out.ID,
			Summary: "新增付款條件 " + out.Code + " " + out.Name, After: toTermDTO(out),
		})
	})
	reply(c, http.StatusCreated, toTermDTO(out), err)
}

func (m *Module) updatePaymentTerm(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	in, ok := bind[paymentTermInput](c)
	if !ok {
		return
	}
	in.Code, in.Name = normCode(in.Code), strings.TrimSpace(in.Name)
	ctx := c.Request.Context()
	a := actor(c)
	var out db.PaymentTerm
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetPaymentTerm(ctx, db.GetPaymentTermParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return notFoundOr(err)
		}
		out, err = q.UpdatePaymentTerm(ctx, db.UpdatePaymentTermParams{
			ID: id, CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, IsMonthEnd: in.IsMonthEnd,
			NetDays: in.NetDays, IsActive: in.IsActive, Version: in.Version, UpdatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(versionConflictOr(err), "payment_terms_company_code_key", errTermCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "payment_term", EntityID: &id,
			Summary: "修改付款條件 " + out.Code + " " + out.Name, Before: toTermDTO(before), After: toTermDTO(out),
		})
	})
	reply(c, http.StatusOK, toTermDTO(out), err)
}
