package gl

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestBuildVat(t *testing.T) {
	sales := []VatSale{
		{DocNo: "S1", DocType: "delivery", InvoiceNo: "AB1", TaxKind: "taxable", PartnerTaxID: "12345678", Untaxed: d("1000"), Tax: d("50")},
		{DocNo: "S2", DocType: "return", InvoiceNo: "AB2", TaxKind: "taxable", PartnerTaxID: "12345678", Untaxed: d("200"), Tax: d("10")},
		{DocNo: "S3", DocType: "delivery", InvoiceNo: "AB3", TaxKind: "taxable", Untaxed: d("500"), Tax: d("25")},
		{DocNo: "S4", DocType: "delivery", InvoiceNo: "AB4", TaxKind: "zero", Untaxed: d("700")},
		{DocNo: "S5", DocType: "delivery", InvoiceNo: "AB5", TaxKind: "exempt", Untaxed: d("300")},
		{DocNo: "S6", DocType: "delivery", TaxKind: "taxable", Untaxed: d("999"), Tax: d("50")}, // 沒發票:待處理
	}
	buys := []VatPurchase{
		{DocNo: "P1", DocType: "receipt", InvoiceNo: "X1", TaxKind: "taxable", Untaxed: d("600"), Tax: d("30"), Goods: d("400"), Expense: d("200")},
		{DocNo: "P2", DocType: "return", InvoiceNo: "X2", TaxKind: "taxable", Untaxed: d("100"), Tax: d("5"), Goods: d("100")},
		{DocNo: "P3", DocType: "receipt", InvoiceNo: "X3", TaxKind: "exempt", Untaxed: d("80")},
		{DocNo: "P4", DocType: "receipt", TaxKind: "taxable", Untaxed: d("1000"), Tax: d("50")}, // 沒發票:不可扣抵
	}
	s, issues := BuildVat(sales, buys)
	want := map[string]decimal.Decimal{
		"triplicate": d("800"), "duplicate": s.TaxableDuplicate, "taxable": d("1300"), "zero": d("700"), "exempt": d("300"),
		"total": d("2300"), "output": d("65"), "goods": d("300"), "expense": d("200"), "input": d("25"), "nontax": d("80"), "net": d("40"),
	}
	got := map[string]decimal.Decimal{
		"triplicate": s.TaxableTriplicate, "duplicate": s.TaxableDuplicate, "taxable": s.TaxableSales, "zero": s.ZeroSales,
		"exempt": s.ExemptSales, "total": s.TotalSales, "output": s.OutputTax, "goods": s.DeductibleGoods,
		"expense": s.DeductibleExpense, "input": s.InputTax, "nontax": s.NonTaxPurchase, "net": s.NetTax,
	}
	for k, w := range want {
		if !got[k].Equal(w) {
			t.Errorf("%s = %s, want %s", k, got[k], w)
		}
	}
	if !s.TaxableDuplicate.Equal(d("500")) {
		t.Errorf("二聯式 = %s", s.TaxableDuplicate)
	}
	if len(issues) != 2 || issues[0].DocNo != "S6" || issues[1].DocNo != "P4" {
		t.Errorf("issues = %+v", issues)
	}
}

func TestCalcVat(t *testing.T) {
	s := VatSummary{TaxableSales: d("1300"), ZeroSales: d("700"), ExemptSales: d("300"), TotalSales: d("2300"), OutputTax: d("65"), InputTax: d("25")}
	// 不套比例:65 − (25 + 10) − 5 = 25 應納
	r := CalcVat(s, VatSettings{PriorCredit: d("5"), CustomsTax: d("10")})
	if !r.TaxPayable.Equal(d("25")) || !r.CarryCredit.IsZero() || !r.DeductRatio.Equal(d("100")) {
		t.Fatalf("不套比例 = %+v", r)
	}
	// 套比例:(1300 + 700) / 2300 = 86.96%;可扣抵 round(35 × 0.8696) = 30;65 − 30 = 35 應納
	r = CalcVat(s, VatSettings{CustomsTax: d("10"), UseRatio: true})
	if !r.DeductRatio.Equal(d("86.96")) || !r.DeductibleTax.Equal(d("30")) || !r.TaxPayable.Equal(d("35")) {
		t.Fatalf("套比例 = %+v", r)
	}
	// 留抵:銷項小於扣抵 + 上期留抵
	r = CalcVat(VatSummary{OutputTax: d("10"), InputTax: d("40"), TotalSales: d("200"), TaxableSales: d("200")}, VatSettings{PriorCredit: d("3")})
	if !r.TaxPayable.IsZero() || !r.CarryCredit.Equal(d("33")) {
		t.Fatalf("留抵 = %+v", r)
	}
	// 沒有銷售額時不套比例(避免除以 0)
	r = CalcVat(VatSummary{InputTax: d("10")}, VatSettings{UseRatio: true})
	if !r.DeductRatio.Equal(d("100")) || !r.CarryCredit.Equal(d("10")) {
		t.Fatalf("無銷售 = %+v", r)
	}
}
