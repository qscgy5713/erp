package masterdata

import (
	"testing"
	"time"

	"erp/internal/db"
)

func TestDueDate(t *testing.T) {
	d := func(s string) time.Time { tt, _ := time.Parse(time.DateOnly, s); return tt }
	cases := []struct {
		term    db.PaymentTerm
		docDate string
		want    string
	}{
		{db.PaymentTerm{NetDays: 0}, "2026-10-07", "2026-10-07"},                    // 貨到付款
		{db.PaymentTerm{NetDays: 30}, "2026-10-07", "2026-11-06"},                   // 單據日後 30 天
		{db.PaymentTerm{IsMonthEnd: true}, "2026-10-07", "2026-10-31"},              // 月結當月底
		{db.PaymentTerm{IsMonthEnd: true, NetDays: 30}, "2026-10-07", "2026-11-30"}, // 月結 30 天
		{db.PaymentTerm{IsMonthEnd: true}, "2026-12-15", "2026-12-31"},              // 跨年
		{db.PaymentTerm{IsMonthEnd: true}, "2028-02-03", "2028-02-29"},              // 閏年二月
		{db.PaymentTerm{IsMonthEnd: true, NetDays: 60}, "2026-01-31", "2026-04-01"},
	}
	for _, tc := range cases {
		if got := DueDate(tc.term, d(tc.docDate)).Format(time.DateOnly); got != tc.want {
			t.Errorf("%+v %s: got %s want %s", tc.term, tc.docDate, got, tc.want)
		}
	}
}
