// Package trade 為採購與銷售單據共用的規則:單頭驗證(倉庫、幣別、匯率、稅別、付款條件)、
// 明細計價(D33)與訂單類單據的狀態轉換(D30)。往來對象(供應商 / 客戶)由各模組自行驗證。
package trade

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/masterdata"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/docstate"
	"erp/internal/shared/money"
)

const MaxLines = 500

func FieldErr(field, msg string) *apperr.Error {
	return apperr.Validation(map[string]string{field: msg})
}

func ParseDate(field, s string) (time.Time, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return t, FieldErr(field, "日期格式須為 YYYY-MM-DD")
	}
	return t, nil
}

// OptionalDate 解析選填的查詢參數日期。
func OptionalDate(c *gin.Context, name string) (*time.Time, error) {
	s := c.Query(name)
	if s == "" {
		return nil, nil
	}
	t, err := ParseDate(name, s)
	return &t, err
}

// OptionalInputDate 解析選填的輸入日期(空字串視為未填)。
func OptionalInputDate(field string, s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	t, err := ParseDate(field, *s)
	return &t, err
}

func DateString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
}

// ---- 單頭 ----

// HeaderInput 單頭共用欄位(不含往來對象)。
type HeaderInput struct {
	DocDate       string           `json:"doc_date" binding:"required"`
	WarehouseID   int64            `json:"warehouse_id" binding:"required"`
	Currency      string           `json:"currency" binding:"required,len=3"`
	ExchangeRate  *decimal.Decimal `json:"exchange_rate"` // 未指定時取單據日期的匯率
	TaxTypeID     int64            `json:"tax_type_id" binding:"required"`
	PaymentTermID *int64           `json:"payment_term_id"`
	Note          string           `json:"note" binding:"max=2000"`
	Version       int32            `json:"version"`
}

// Header 已驗證的單頭。
type Header struct {
	Date     time.Time
	Rate     decimal.Decimal // 匯率
	TaxRate  decimal.Decimal
	Decimals int32 // 原幣金額小數位
}

// CheckHeader 驗證倉庫、幣別、匯率、稅別、付款條件,並帶出匯率與稅率快照。
func CheckHeader(ctx context.Context, q *db.Queries, companyID int64, in *HeaderInput) (Header, error) {
	var h Header
	var err error
	in.Note = strings.TrimSpace(in.Note)
	in.Currency = strings.ToUpper(in.Currency)
	if h.Date, err = ParseDate("doc_date", in.DocDate); err != nil {
		return h, err
	}
	whs, err := q.ListWarehouseFlags(ctx, db.ListWarehouseFlagsParams{CompanyID: companyID, Ids: []int64{in.WarehouseID}})
	if err != nil {
		return h, err
	}
	if len(whs) != 1 || !whs[0].IsActive {
		return h, FieldErr("warehouse_id", "倉庫不存在或已停用")
	}
	cur, err := q.GetCurrency(ctx, in.Currency)
	if database.IsNoRows(err) || (err == nil && !cur.IsActive) {
		return h, FieldErr("currency", "幣別不存在或已停用")
	} else if err != nil {
		return h, err
	}
	h.Decimals = int32(cur.Decimals)
	switch {
	case in.Currency == masterdata.BaseCurrency:
		h.Rate = decimal.NewFromInt(1)
	case in.ExchangeRate != nil:
		if !in.ExchangeRate.IsPositive() || in.ExchangeRate.Exponent() < -money.RatePlaces {
			return h, FieldErr("exchange_rate", fmt.Sprintf("匯率須大於 0,最多 %d 位小數", money.RatePlaces))
		}
		h.Rate = *in.ExchangeRate
	default:
		if h.Rate, err = masterdata.RateOn(ctx, q, companyID, in.Currency, h.Date); err != nil {
			return h, err
		}
	}
	tax, err := q.TaxTypeForDoc(ctx, db.TaxTypeForDocParams{ID: in.TaxTypeID, CompanyID: companyID})
	if database.IsNoRows(err) || (err == nil && !tax.IsActive) {
		return h, FieldErr("tax_type_id", "稅別不存在或已停用")
	} else if err != nil {
		return h, err
	}
	h.TaxRate = tax.Rate
	if in.PaymentTermID != nil {
		t, err := q.GetPaymentTerm(ctx, db.GetPaymentTermParams{ID: *in.PaymentTermID, CompanyID: companyID})
		if database.IsNoRows(err) || (err == nil && !t.IsActive) {
			return h, FieldErr("payment_term_id", "付款條件不存在或已停用")
		} else if err != nil {
			return h, err
		}
	}
	return h, nil
}

// DueDate 依付款條件計算到期日;未指定付款條件時為單據日期。
func DueDate(ctx context.Context, q *db.Queries, companyID int64, termID *int64, docDate time.Time) (time.Time, error) {
	if termID == nil {
		return docDate, nil
	}
	term, err := q.GetPaymentTerm(ctx, db.GetPaymentTermParams{ID: *termID, CompanyID: companyID})
	if err != nil {
		return docDate, err
	}
	return masterdata.DueDate(term, docDate), nil
}

// ---- 明細與金額 ----

// LineInput 明細共用欄位(來源單據參照由各模組自行定義)。
type LineInput struct {
	ItemID    int64           `json:"item_id" binding:"required"`
	UnitID    int64           `json:"unit_id" binding:"required"`
	Qty       decimal.Decimal `json:"qty"`
	UnitPrice decimal.Decimal `json:"unit_price"`
	Note      string          `json:"note" binding:"max=255"`
}

// Priced 驗證、換算基本單位並計算金額後的結果。
type Priced struct {
	Note       string
	Factor     decimal.Decimal
	BaseQty    decimal.Decimal
	Amount     decimal.Decimal // 原幣
	BaseAmount decimal.Decimal // 本位幣
}

type Totals struct {
	Untaxed, Tax, Total             decimal.Decimal // 原幣
	BaseUntaxed, BaseTax, BaseTotal decimal.Decimal // 本位幣
}

// PriceLines 驗證明細(料品、單位、數量、單價)並計算金額,結果與輸入依索引對應。
// 行金額 = 數量 × 單價,依幣別小數位捨入;本位幣金額 = 行金額 × 匯率,捨入到元;
// 稅額依單頭合計計算;本位幣未稅 = 各行本位幣金額合計(D33)。
func PriceLines(ctx context.Context, q *db.Queries, companyID int64, h Header, lines []LineInput) ([]Priced, Totals, error) {
	var t Totals
	if len(lines) == 0 {
		return nil, t, FieldErr("lines", "請輸入明細")
	}
	if len(lines) > MaxLines {
		return nil, t, FieldErr("lines", fmt.Sprintf("明細最多 %d 筆", MaxLines))
	}
	ids := make([]int64, len(lines))
	for i, l := range lines {
		ids[i] = l.ItemID
	}
	items, err := q.ListTradeItems(ctx, db.ListTradeItemsParams{CompanyID: companyID, Ids: ids})
	if err != nil {
		return nil, t, err
	}
	itemByID := map[int64]db.ListTradeItemsRow{}
	for _, it := range items {
		itemByID[it.ID] = it
	}
	factors, err := q.ListItemUnitFactors(ctx, ids)
	if err != nil {
		return nil, t, err
	}
	factorOf := map[[2]int64]decimal.Decimal{}
	for _, f := range factors {
		factorOf[[2]int64{f.ItemID, f.UnitID}] = f.Factor
	}

	fields := map[string]string{}
	out := make([]Priced, len(lines))
	for i, l := range lines {
		key := fmt.Sprintf("lines.%d", i)
		it, ok := itemByID[l.ItemID]
		switch {
		case !ok:
			fields[key] = "料品不存在"
			continue
		case !it.IsActive:
			fields[key] = it.Code + " 已停用"
			continue
		}
		factor, ok := factorOf[[2]int64{l.ItemID, l.UnitID}]
		switch {
		case !ok:
			fields[key] = it.Code + " 沒有此單位"
			continue
		case !l.Qty.IsPositive():
			fields[key] = "數量須大於 0"
			continue
		case l.Qty.Exponent() < -money.QuantityPlaces:
			fields[key] = fmt.Sprintf("數量最多 %d 位小數", money.QuantityPlaces)
			continue
		case l.UnitPrice.IsNegative():
			fields[key] = "單價不可為負"
			continue
		case l.UnitPrice.Exponent() < -money.UnitPricePlaces:
			fields[key] = fmt.Sprintf("單價最多 %d 位小數", money.UnitPricePlaces)
			continue
		}
		base := l.Qty.Mul(factor).Round(money.QuantityPlaces)
		if base.IsZero() {
			fields[key] = "換算成基本單位後為 0"
			continue
		}
		amount := money.Round(l.Qty.Mul(l.UnitPrice), h.Decimals)
		p := Priced{
			Note: strings.TrimSpace(l.Note), Factor: factor, BaseQty: base, Amount: amount,
			BaseAmount: money.Amount(amount.Mul(h.Rate)),
		}
		out[i] = p
		t.Untaxed = t.Untaxed.Add(p.Amount)
		t.BaseUntaxed = t.BaseUntaxed.Add(p.BaseAmount)
	}
	if len(fields) > 0 {
		return nil, t, apperr.Validation(fields)
	}
	t.Tax = money.Round(t.Untaxed.Mul(h.TaxRate), h.Decimals)
	t.Total = t.Untaxed.Add(t.Tax)
	t.BaseTax = money.Amount(t.Tax.Mul(h.Rate))
	t.BaseTotal = t.BaseUntaxed.Add(t.BaseTax)
	return out, t, nil
}

// ---- 狀態 ----

var ActionLabels = map[docstate.Action]string{
	docstate.Submit: "送審", docstate.Reject: "退回", docstate.Approve: "核准", docstate.Unapprove: "取消核准",
	docstate.Post: "過帳", docstate.Unpost: "反過帳", docstate.Void: "作廢", docstate.Close: "結案",
	docstate.Reopen: "重開",
}

// OrderTransition 訂單類單據(採購單、報價單、銷售訂單)不過帳:
// 核准後可結案(剩餘數量不再交貨),結案可重開回「已核准」(D30)。
func OrderTransition(from docstate.Status, action docstate.Action) (docstate.Status, error) {
	switch action {
	case docstate.Post, docstate.Unpost:
		return from, docstate.ErrInvalidTransition
	case docstate.Reopen:
		if from == docstate.Closed {
			return docstate.Approved, nil
		}
		return from, docstate.ErrInvalidTransition
	}
	return docstate.Transition(from, action)
}

// PostingTransition 會過帳的單據(進貨 / 出貨及其退回):不使用結案 / 重開。
func PostingTransition(from docstate.Status, action docstate.Action) (docstate.Status, error) {
	if action == docstate.Close || action == docstate.Reopen {
		return from, docstate.ErrInvalidTransition
	}
	return docstate.Transition(from, action)
}
