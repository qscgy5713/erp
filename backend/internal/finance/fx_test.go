package finance

import (
	"testing"

	"github.com/shopspring/decimal"

	"erp/internal/db"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestSettleEntriesFx(t *testing.T) {
	cases := []struct {
		name     string
		cfg      sideCfg
		base     string
		cash     string
		fxKey    string
		fxDebit  string
		fxCredit string
	}{
		{"收款多收→利益", sides[SideReceipt], "3000", "3100", "fx.gain", "0", "100"},
		{"收款少收→損失", sides[SideReceipt], "3000", "2900", "fx.loss", "100", "0"},
		{"付款多付→損失", sides[SidePayment], "3000", "3100", "fx.loss", "100", "0"},
		{"付款少付→利益", sides[SidePayment], "3000", "2900", "fx.gain", "0", "100"},
	}
	for _, c := range cases {
		es := settleEntries(c.cfg, db.Settlement{Method: "transfer"}, d(c.base), d(c.cash))
		var dr, cr decimal.Decimal
		var fx bool
		for _, e := range es {
			dr, cr = dr.Add(e.Debit), cr.Add(e.Credit)
			if e.Key == c.fxKey {
				fx = e.Debit.Equal(d(c.fxDebit)) && e.Credit.Equal(d(c.fxCredit))
			}
		}
		if !dr.Equal(cr) || !fx {
			t.Errorf("%s:借 %s 貸 %s,匯兌分錄正確=%v(%+v)", c.name, dr, cr, fx, es)
		}
	}
	n := 0
	for _, e := range settleEntries(sides[SideReceipt], db.Settlement{Method: "cash"}, d("500"), d("500")) {
		if !e.Debit.IsZero() || !e.Credit.IsZero() {
			n++
		}
	}
	if n != 2 {
		t.Errorf("無差額不應有匯兌分錄,有效分錄 %d 筆", n)
	}
}
