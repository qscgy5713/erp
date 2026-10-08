// Package purchase 為採購:採購單、進貨單、進貨退出單。
// 進貨 / 退出過帳時在同一交易內呼叫 inventory.Post 異動庫存,並以 finance.CreatePayable 產生應付。
package purchase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/auth"
	"erp/internal/db"
	"erp/internal/masterdata"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/money"
	"erp/internal/system/permission"
)

const maxLines = 500

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

func fieldErr(field, msg string) *apperr.Error {
	return apperr.Validation(map[string]string{field: msg})
}

func parseDate(field, s string) (time.Time, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return t, fieldErr(field, "日期格式須為 YYYY-MM-DD")
	}
	return t, nil
}

func optionalDate(c *gin.Context, name string) (*time.Time, error) {
	s := c.Query(name)
	if s == "" {
		return nil, nil
	}
	t, err := parseDate(name, s)
	return &t, err
}

func dateString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
}

// ---- 單頭 ----

// headerInput 採購單與進貨 / 退出單共用的單頭欄位。
type headerInput struct {
	DocDate       string           `json:"doc_date" binding:"required"`
	SupplierID    int64            `json:"supplier_id" binding:"required"`
	WarehouseID   int64            `json:"warehouse_id" binding:"required"`
	Currency      string           `json:"currency" binding:"required,len=3"`
	ExchangeRate  *decimal.Decimal `json:"exchange_rate"` // 未指定時取單據日期的匯率
	TaxTypeID     int64            `json:"tax_type_id" binding:"required"`
	PaymentTermID *int64           `json:"payment_term_id"`
	Note          string           `json:"note" binding:"max=2000"`
	Version       int32            `json:"version"`
}

// header 已驗證的單頭。
type header struct {
	date     time.Time
	rate     decimal.Decimal // 匯率
	taxRate  decimal.Decimal
	decimals int32 // 原幣金額小數位
}

// checkHeader 驗證供應商、倉庫、幣別、匯率、稅別、付款條件,並帶出匯率與稅率快照。
func checkHeader(ctx context.Context, q *db.Queries, companyID int64, in *headerInput) (header, error) {
	var h header
	var err error
	in.Note = strings.TrimSpace(in.Note)
	in.Currency = strings.ToUpper(in.Currency)
	if h.date, err = parseDate("doc_date", in.DocDate); err != nil {
		return h, err
	}
	sup, err := q.SupplierForDoc(ctx, db.SupplierForDocParams{ID: in.SupplierID, CompanyID: companyID})
	if database.IsNoRows(err) || (err == nil && !sup.IsActive) {
		return h, fieldErr("supplier_id", "供應商不存在或已停用")
	} else if err != nil {
		return h, err
	}
	whs, err := q.ListWarehouseFlags(ctx, db.ListWarehouseFlagsParams{CompanyID: companyID, Ids: []int64{in.WarehouseID}})
	if err != nil {
		return h, err
	}
	if len(whs) != 1 || !whs[0].IsActive {
		return h, fieldErr("warehouse_id", "倉庫不存在或已停用")
	}
	cur, err := q.GetCurrency(ctx, in.Currency)
	if database.IsNoRows(err) || (err == nil && !cur.IsActive) {
		return h, fieldErr("currency", "幣別不存在或已停用")
	} else if err != nil {
		return h, err
	}
	h.decimals = int32(cur.Decimals)
	switch {
	case in.Currency == masterdata.BaseCurrency:
		h.rate = decimal.NewFromInt(1)
	case in.ExchangeRate != nil:
		if !in.ExchangeRate.IsPositive() || in.ExchangeRate.Exponent() < -money.RatePlaces {
			return h, fieldErr("exchange_rate", fmt.Sprintf("匯率須大於 0,最多 %d 位小數", money.RatePlaces))
		}
		h.rate = *in.ExchangeRate
	default:
		if h.rate, err = masterdata.RateOn(ctx, q, companyID, in.Currency, h.date); err != nil {
			return h, err
		}
	}
	tax, err := q.TaxTypeForDoc(ctx, db.TaxTypeForDocParams{ID: in.TaxTypeID, CompanyID: companyID})
	if database.IsNoRows(err) || (err == nil && !tax.IsActive) {
		return h, fieldErr("tax_type_id", "稅別不存在或已停用")
	} else if err != nil {
		return h, err
	}
	h.taxRate = tax.Rate
	if in.PaymentTermID != nil {
		t, err := q.GetPaymentTerm(ctx, db.GetPaymentTermParams{ID: *in.PaymentTermID, CompanyID: companyID})
		if database.IsNoRows(err) || (err == nil && !t.IsActive) {
			return h, fieldErr("payment_term_id", "付款條件不存在或已停用")
		} else if err != nil {
			return h, err
		}
	}
	return h, nil
}

// ---- 明細與金額 ----

type lineInput struct {
	ItemID        int64           `json:"item_id" binding:"required"`
	UnitID        int64           `json:"unit_id" binding:"required"`
	Qty           decimal.Decimal `json:"qty"`
	UnitPrice     decimal.Decimal `json:"unit_price"`
	PoLineID      *int64          `json:"po_line_id"`      // 進貨:來源採購明細
	ReceiptLineID *int64          `json:"receipt_line_id"` // 退出:來源進貨明細
	Note          string          `json:"note" binding:"max=255"`
}

// pricedLine 已驗證、換算基本單位並計算金額的明細。
type pricedLine struct {
	lineInput
	factor     decimal.Decimal
	baseQty    decimal.Decimal
	amount     decimal.Decimal // 原幣
	baseAmount decimal.Decimal // 本位幣
}

type totals struct {
	untaxed, tax, total             decimal.Decimal // 原幣
	baseUntaxed, baseTax, baseTotal decimal.Decimal // 本位幣
}

// priceLines 驗證明細(料品、單位、數量、單價)並計算金額。
// 行金額 = 數量 × 單價,依幣別小數位捨入;本位幣金額 = 行金額 × 匯率,捨入到元(D33)。
func priceLines(ctx context.Context, q *db.Queries, companyID int64, h header, lines []lineInput) ([]pricedLine, totals, error) {
	var t totals
	if len(lines) == 0 {
		return nil, t, fieldErr("lines", "請輸入明細")
	}
	if len(lines) > maxLines {
		return nil, t, fieldErr("lines", fmt.Sprintf("明細最多 %d 筆", maxLines))
	}
	ids := make([]int64, len(lines))
	for i, l := range lines {
		ids[i] = l.ItemID
	}
	items, err := q.ListPurchaseItems(ctx, db.ListPurchaseItemsParams{CompanyID: companyID, Ids: ids})
	if err != nil {
		return nil, t, err
	}
	itemByID := map[int64]db.ListPurchaseItemsRow{}
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
	out := make([]pricedLine, len(lines))
	for i, l := range lines {
		key := fmt.Sprintf("lines.%d", i)
		l.Note = strings.TrimSpace(l.Note)
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
		amount := money.Round(l.Qty.Mul(l.UnitPrice), h.decimals)
		p := pricedLine{lineInput: l, factor: factor, baseQty: base, amount: amount, baseAmount: money.Amount(amount.Mul(h.rate))}
		out[i] = p
		t.untaxed = t.untaxed.Add(p.amount)
		t.baseUntaxed = t.baseUntaxed.Add(p.baseAmount)
	}
	if len(fields) > 0 {
		return nil, t, apperr.Validation(fields)
	}
	// 稅額依單頭合計計算
	t.tax = money.Round(t.untaxed.Mul(h.taxRate), h.decimals)
	t.total = t.untaxed.Add(t.tax)
	t.baseTax = money.Amount(t.tax.Mul(h.rate))
	t.baseTotal = t.baseUntaxed.Add(t.baseTax)
	return out, t, nil
}
