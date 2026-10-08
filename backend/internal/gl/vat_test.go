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
