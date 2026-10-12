package gl

import (
	"github.com/shopspring/decimal"
)

// 營業稅 401 申報彙總(D60)。所有金額為本位幣;進貨退出、銷貨退回以負數沖減。

type VatSale struct {
	DocNo, DocType, InvoiceNo, TaxKind     string
	PartnerCode, PartnerName, PartnerTaxID string
	Date                                   string
	Untaxed, Tax                           decimal.Decimal
}

type VatPurchase struct {
	DocNo, DocType, InvoiceNo, TaxKind     string
	PartnerCode, PartnerName, PartnerTaxID string
	Date                                   string
	Untaxed, Tax, Goods, Expense           decimal.Decimal
}

// VatSummary 401 的核心欄位。
type VatSummary struct {
	// 銷項(有發票者)
	TaxableTriplicate decimal.Decimal `json:"taxable_triplicate"` // 應稅銷售額:買方有統一編號(三聯式)
	TaxableDuplicate  decimal.Decimal `json:"taxable_duplicate"`  // 應稅銷售額:買方無統編(二聯式)
	TaxableSales      decimal.Decimal `json:"taxable_sales"`
	ZeroSales         decimal.Decimal `json:"zero_sales"`
	ExemptSales       decimal.Decimal `json:"exempt_sales"`
	TotalSales        decimal.Decimal `json:"total_sales"`
	OutputTax         decimal.Decimal `json:"output_tax"`
	// 進項(有發票者)
	DeductibleGoods   decimal.Decimal `json:"deductible_goods"`   // 可扣抵進貨(應稅)
	DeductibleExpense decimal.Decimal `json:"deductible_expense"` // 可扣抵費用及其他(應稅)
	InputTax          decimal.Decimal `json:"input_tax"`          // 可扣抵進項稅額
	NonTaxPurchase    decimal.Decimal `json:"non_tax_purchase"`   // 零稅率 / 免稅進貨(無進項稅額)
	// 結果
	NetTax decimal.Decimal `json:"net_tax"` // 銷項稅額 − 進項稅額;正為應納、負為留抵
}

// VatIssue 待處理:不會計入申報、但可能漏登的單據。
type VatIssue struct {
	Kind    string          `json:"kind"` // sales_no_invoice / purchase_no_invoice
	DocNo   string          `json:"doc_no"`
	Date    string          `json:"date"`
	Partner string          `json:"partner"`
	Untaxed decimal.Decimal `json:"untaxed"`
	Tax     decimal.Decimal `json:"tax"`
}

func signed(docType string, v decimal.Decimal) decimal.Decimal {
	if docType == "return" {
		return v.Neg()
	}
	return v
}

// BuildVat 彙總銷項與進項。沒有發票號碼的單據不計入申報,改列為待處理。
// 進貨的稅額只有應稅(taxable)才可扣抵;零稅率 / 免稅進貨不會有稅額。
func BuildVat(sales []VatSale, purchases []VatPurchase) (VatSummary, []VatIssue) {
	var s VatSummary
	issues := []VatIssue{}
	for _, d := range sales {
		untaxed, tax := signed(d.DocType, d.Untaxed), signed(d.DocType, d.Tax)
		if d.InvoiceNo == "" {
			issues = append(issues, VatIssue{Kind: "sales_no_invoice", DocNo: d.DocNo, Date: d.Date, Partner: d.PartnerName, Untaxed: untaxed, Tax: tax})
			continue
		}
		switch d.TaxKind {
		case "taxable":
			s.TaxableSales = s.TaxableSales.Add(untaxed)
			if d.PartnerTaxID != "" {
				s.TaxableTriplicate = s.TaxableTriplicate.Add(untaxed)
			} else {
				s.TaxableDuplicate = s.TaxableDuplicate.Add(untaxed)
			}
			s.OutputTax = s.OutputTax.Add(tax)
		case "zero":
			s.ZeroSales = s.ZeroSales.Add(untaxed)
		default:
			s.ExemptSales = s.ExemptSales.Add(untaxed)
		}
	}
	s.TotalSales = s.TaxableSales.Add(s.ZeroSales).Add(s.ExemptSales)
	for _, d := range purchases {
		untaxed, tax := signed(d.DocType, d.Untaxed), signed(d.DocType, d.Tax)
		if d.InvoiceNo == "" {
			issues = append(issues, VatIssue{Kind: "purchase_no_invoice", DocNo: d.DocNo, Date: d.Date, Partner: d.PartnerName, Untaxed: untaxed, Tax: tax})
			continue
		}
		if d.TaxKind != "taxable" {
			s.NonTaxPurchase = s.NonTaxPurchase.Add(untaxed)
			continue
		}
		s.DeductibleGoods = s.DeductibleGoods.Add(signed(d.DocType, d.Goods))
		s.DeductibleExpense = s.DeductibleExpense.Add(signed(d.DocType, d.Expense))
		s.InputTax = s.InputTax.Add(tax)
	}
	s.NetTax = s.OutputTax.Sub(s.InputTax)
	return s, issues
}

// VatSettings 每期人工輸入的項目。
type VatSettings struct {
	PriorCredit decimal.Decimal `json:"prior_credit"` // 上期累積留抵稅額
	CustomsTax  decimal.Decimal `json:"customs_tax"`  // 海關代徵營業稅額(進口貨物,可扣抵)
	UseRatio    bool            `json:"use_ratio"`    // 兼營免稅:進項稅額按比例扣抵
}

// VatResult 申報稅額的計算結果。
type VatResult struct {
	DeductRatio   decimal.Decimal `json:"deduct_ratio"`   // 扣抵比例(%);未啟用或沒有免稅銷售時為 100
	InputTax      decimal.Decimal `json:"input_tax"`      // 進項稅額(未乘比例)
	CustomsTax    decimal.Decimal `json:"customs_tax"`    // 海關代徵
	DeductibleTax decimal.Decimal `json:"deductible_tax"` // 本期可扣抵稅額 = (進項 + 海關代徵) × 比例
	PriorCredit   decimal.Decimal `json:"prior_credit"`   // 上期累積留抵稅額
	TaxPayable    decimal.Decimal `json:"tax_payable"`    // 本期應納稅額(≥ 0)
	CarryCredit   decimal.Decimal `json:"carry_credit"`   // 本期累積留抵稅額(≥ 0),可留抵下期或申請退稅
}

// CalcVat 本期應納(留抵)稅額 = 銷項稅額 − (進項稅額 + 海關代徵) × 扣抵比例 − 上期累積留抵稅額。
// 扣抵比例 = (應稅銷售額 + 零稅率銷售額) ÷ 銷售額總計,只有勾選「按比例扣抵」且銷售額總計 > 0 時才套用。
func CalcVat(s VatSummary, st VatSettings) VatResult {
	r := VatResult{DeductRatio: decimal.NewFromInt(100), InputTax: s.InputTax, CustomsTax: st.CustomsTax, PriorCredit: st.PriorCredit}
	if st.UseRatio && s.TotalSales.IsPositive() {
		r.DeductRatio = s.TaxableSales.Add(s.ZeroSales).Div(s.TotalSales).Mul(decimal.NewFromInt(100)).Round(2)
	}
	r.DeductibleTax = s.InputTax.Add(st.CustomsTax).Mul(r.DeductRatio).Div(decimal.NewFromInt(100)).Round(0)
	net := s.OutputTax.Sub(r.DeductibleTax).Sub(st.PriorCredit)
	if net.IsPositive() {
		r.TaxPayable = net
	} else {
		r.CarryCredit = net.Neg()
	}
	return r
}
