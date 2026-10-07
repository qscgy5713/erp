package money

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestAmountRoundsHalfAwayFromZero(t *testing.T) {
	cases := map[string]string{"10.5": "11", "10.49": "10", "-10.5": "-11", "0.5": "1"}
	for in, want := range cases {
		if got := Amount(d(in)); !got.Equal(d(want)) {
			t.Errorf("Amount(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestTax(t *testing.T) {
	if got := Tax(d("1234"), TaxRate5); !got.Equal(d("62")) { // 61.7 → 62
		t.Fatalf("Tax = %s", got)
	}
}

func TestSplitTaxIncluded(t *testing.T) {
	for _, in := range []string{"105", "100", "1", "99999"} {
		u, tax := SplitTaxIncluded(d(in), TaxRate5)
		if !u.Add(tax).Equal(d(in)) {
			t.Errorf("%s: 未稅 %s + 稅 %s 不等於含稅", in, u, tax)
		}
	}
	u, tax := SplitTaxIncluded(d("105"), TaxRate5)
	if !u.Equal(d("100")) || !tax.Equal(d("5")) {
		t.Fatalf("105 → %s + %s", u, tax)
	}
}
