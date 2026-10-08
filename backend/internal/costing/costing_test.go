package costing

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func eq(t *testing.T, name string, got decimal.Decimal, want string) {
	t.Helper()
	if !got.Equal(d(want)) {
		t.Errorf("%s = %s, want %s", name, got, want)
	}
}

func TestComputeWeightedAverage(t *testing.T) {
	// 期初 24 個 @100,本月進 120 個共 13200 → 平均 (2400+13200)/144 = 108.333333
	r := compute(opening{qty: d("24"), value: d("2400"), avg: d("100")},
		activity{purchaseQty: d("120"), purchaseValue: d("13200"), salesQty: d("-36"), adjustQty: d("-6")})
	eq(t, "avg", r.avg, "108.333333")
	eq(t, "cogs", r.cogs, "3900")    // 36 × 108.333333 = 3899.99999 → 3900
	eq(t, "adjust", r.adjust, "650") // 損失
	eq(t, "closingQty", r.closingQty, "102")
	eq(t, "closingV", r.closingV, "11050")
}

func TestComputeGainAndReturns(t *testing.T) {
	// 盤盈(正)使損益為負;銷貨退回(正)抵銷銷貨成本
	r := compute(opening{qty: d("10"), value: d("1000"), avg: d("100")},
		activity{salesQty: d("5"), adjustQty: d("3")})
	eq(t, "avg", r.avg, "100") // 沒有進貨:沿用期初平均
	eq(t, "cogs", r.cogs, "-500")
	eq(t, "adjust", r.adjust, "-300")
	eq(t, "closingQty", r.closingQty, "18")
}

func TestComputeIdleCarriesOpeningExactly(t *testing.T) {
	r := compute(opening{qty: d("7"), value: d("333.3333"), avg: d("47.619043")}, activity{})
	eq(t, "closingV", r.closingV, "333.3333") // 不因捨入漂移
	eq(t, "avg", r.avg, "47.619043")
	eq(t, "cogs", r.cogs, "0")
}

func TestComputeFallbacks(t *testing.T) {
	// 期初為負庫存、本月進貨後分母仍不為正:沿用期初平均
	r := compute(opening{qty: d("-10"), value: d("-1000"), avg: d("100")}, activity{purchaseQty: d("4"), purchaseValue: d("480")})
	eq(t, "avg", r.avg, "100")
	// 沒有期初平均、分母不為正:用本月進貨單價
	r = compute(opening{qty: d("-4")}, activity{purchaseQty: d("2"), purchaseValue: d("300")})
	eq(t, "avg", r.avg, "150")
}

func TestPair(t *testing.T) {
	if got := pair(d("0"), "a", "b"); got != nil {
		t.Fatalf("0 不應有分錄: %v", got)
	}
	got := pair(d("-500"), "cost.cogs", "cost.inventory")
	if len(got) != 2 || got[0].Key != "cost.inventory" || !got[0].Debit.Equal(d("500")) || got[1].Key != "cost.cogs" || !got[1].Credit.Equal(d("500")) {
		t.Fatalf("負數應借貸對調: %+v", got)
	}
}
