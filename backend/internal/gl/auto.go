package gl

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/system/docno"
)

// 拋轉規則的 key(D46):業務事件 → 科目。預設值由 migration 預載,可在畫面調整。
var MappingKeys = []struct{ Key, Label string }{
	{"purchase.inventory", "進貨:存貨(商品類明細)"},
	{"purchase.expense", "進貨:費用(服務 / 費用類明細)"},
	{"purchase.input_tax", "進貨:進項稅額"},
	{"purchase.payable", "進貨:應付帳款"},
	{"sales.receivable", "銷貨:應收帳款"},
	{"sales.revenue", "銷貨:銷貨收入"},
	{"sales.output_tax", "銷貨:銷項稅額"},
	{"sales.return", "銷貨退回:銷貨退回及折讓"},
	{"cost.cogs", "月結:銷貨成本"},
	{"cost.inventory", "月結:存貨"},
	{"cost.adjustment", "月結:存貨盤損(盈)"},
	{"settle.cash", "收付款:現金"},
	{"settle.bank", "收付款:銀行存款(匯款、支票、其他)"},
}

func mappingLabel(key string) string {
	for _, k := range MappingKeys {
		if k.Key == key {
			return k.Label
		}
	}
	return key
}

// 收付款方式 → 拋轉 key
func SettleKey(method string) string {
	if method == "cash" {
		return "settle.cash"
	}
	return "settle.bank"
}

// Entry 一筆拋轉分錄:以拋轉規則的 key 指定科目,金額為本位幣。
type Entry struct {
	Key        string
	Debit      decimal.Decimal
	Credit     decimal.Decimal
	CustomerID *int64
	SupplierID *int64
}

// Source 產生傳票的業務單據。
type Source struct {
	Type string // goods_receipt、purchase_return、delivery、sales_return、collection、payment
	ID   int64
	No   string
	Date time.Time
	Desc string
}

type Options struct {
	CompanyID int64
	ActorID   *int64
}

func periodOf(d time.Time) string { return d.Format("2006-01") }

// CheckPeriodOpen 該日期所屬會計期間已關帳時回傳錯誤。須在寫入的交易內呼叫;
// 取共享鎖,與關帳(排他鎖)序列化,避免檢查通過後該期才被關帳。
func CheckPeriodOpen(ctx context.Context, q *db.Queries, companyID int64, date time.Time) error {
	if err := q.LockCompanyForPeriod(ctx, companyID); err != nil {
		return err
	}
	p, err := q.GetPeriod(ctx, db.GetPeriodParams{CompanyID: companyID, Period: periodOf(date)})
	if database.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if p.Status == "closed" {
		return apperr.New(http.StatusConflict, errPeriodClosed,
			fmt.Sprintf("會計期間 %s 已關帳,不可過帳 / 反過帳或新增傳票;如需更正請先重開該期間", p.Period))
	}
	return nil
}

type account struct {
	id int64
}

func resolve(ctx context.Context, q *db.Queries, companyID int64, entries []Entry) (map[string]account, error) {
	rows, err := q.ListAccountMappings(ctx, companyID)
	if err != nil {
		return nil, err
	}
	byKey := map[string]db.ListAccountMappingsRow{}
	for _, r := range rows {
		byKey[r.MapKey] = r
	}
	out := map[string]account{}
	for _, e := range entries {
		if _, done := out[e.Key]; done {
			continue
		}
		r, ok := byKey[e.Key]
		switch {
		case !ok:
			return nil, errNoMapping.WithDetails(map[string]string{"key": e.Key, "label": mappingLabel(e.Key)})
		case !r.IsActive || !r.IsPostable:
			return nil, apperr.New(http.StatusUnprocessableEntity, "GL-003",
				fmt.Sprintf("拋轉科目「%s」(%s %s)已停用或不是明細科目,請至拋轉規則調整", mappingLabel(e.Key), r.AccountCode, r.AccountName))
		}
		out[e.Key] = account{id: r.AccountID}
	}
	return out, nil
}

// PostSource 為業務單據產生已過帳傳票。須在呼叫端的交易內執行。
// 期間已關帳時回傳錯誤;所有分錄金額為 0 時不產生傳票;借貸不平衡視為程式錯誤而拒絕。
func PostSource(ctx context.Context, q *db.Queries, opt Options, src Source, entries []Entry) error {
	if err := CheckPeriodOpen(ctx, q, opt.CompanyID, src.Date); err != nil {
		return err
	}
	var lines []Entry
	debit, credit := decimal.Zero, decimal.Zero
	for _, e := range entries {
		if e.Debit.IsNegative() || e.Credit.IsNegative() || (e.Debit.IsPositive() && e.Credit.IsPositive()) {
			return fmt.Errorf("拋轉分錄金額不合法: %+v", e)
		}
		if e.Debit.IsZero() && e.Credit.IsZero() {
			continue
		}
		lines = append(lines, e)
		debit, credit = debit.Add(e.Debit), credit.Add(e.Credit)
	}
	if len(lines) == 0 {
		return nil
	}
	if !debit.Equal(credit) {
		return errUnbalanced.WithDetails(map[string]string{"debit": debit.String(), "credit": credit.String(), "source": src.No})
	}
	accts, err := resolve(ctx, q, opt.CompanyID, lines)
	if err != nil {
		return err
	}
	no, err := docno.Next(ctx, q, opt.CompanyID, "journal_voucher", src.Date)
	if err != nil {
		return err
	}
	v, err := q.CreateVoucher(ctx, db.CreateVoucherParams{
		CompanyID: opt.CompanyID, DocNo: no, VoucherDate: src.Date, SourceType: src.Type, SourceID: &src.ID,
		SourceNo: src.No, Description: src.Desc, Status: "posted", TotalAmount: debit, PostedBy: opt.ActorID,
		CreatedBy: opt.ActorID,
	})
	if err != nil {
		return err
	}
	for i, e := range lines {
		if err := q.AddVoucherLine(ctx, db.AddVoucherLineParams{
			VoucherID: v.ID, LineNo: int32(i + 1), AccountID: accts[e.Key].id, Debit: e.Debit, Credit: e.Credit,
			CustomerID: e.CustomerID, SupplierID: e.SupplierID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// ReverseSource 沖銷業務單據尚未沖銷的傳票(反過帳):產生借貸相反、沿用原日期的沖銷傳票。
// 單據沒有傳票(金額為 0)時不做事。須在呼叫端的交易內執行。
func ReverseSource(ctx context.Context, q *db.Queries, opt Options, srcType string, srcID int64) error {
	open, err := q.OpenVouchersBySource(ctx, db.OpenVouchersBySourceParams{CompanyID: opt.CompanyID, SourceType: srcType, SourceID: &srcID})
	if err != nil {
		return err
	}
	for _, orig := range open {
		if err := reverseVoucher(ctx, q, opt, orig, orig.VoucherDate); err != nil {
			return err
		}
	}
	return nil
}

// reverseVoucher 以 date 為日期產生 orig 的沖銷傳票(借貸對調)。
func reverseVoucher(ctx context.Context, q *db.Queries, opt Options, orig db.Voucher, date time.Time) error {
	if err := CheckPeriodOpen(ctx, q, opt.CompanyID, date); err != nil {
		return err
	}
	lines, err := q.ListVoucherLines(ctx, orig.ID)
	if err != nil {
		return err
	}
	no, err := docno.Next(ctx, q, opt.CompanyID, "journal_voucher", date)
	if err != nil {
		return err
	}
	rv, err := q.CreateVoucher(ctx, db.CreateVoucherParams{
		CompanyID: opt.CompanyID, DocNo: no, VoucherDate: date, SourceType: orig.SourceType, SourceID: orig.SourceID,
		SourceNo: orig.SourceNo, Description: "沖銷 " + orig.DocNo, Status: "posted", ReversalOf: &orig.ID,
		TotalAmount: orig.TotalAmount, PostedBy: opt.ActorID, CreatedBy: opt.ActorID,
	})
	if err != nil {
		return err
	}
	for _, l := range lines {
		if err := q.AddVoucherLine(ctx, db.AddVoucherLineParams{
			VoucherID: rv.ID, LineNo: l.LineNo, AccountID: l.AccountID, Debit: l.Credit, Credit: l.Debit,
			Description: l.Description, CustomerID: l.CustomerID, SupplierID: l.SupplierID, DepartmentID: l.DepartmentID,
		}); err != nil {
			return err
		}
	}
	return nil
}
