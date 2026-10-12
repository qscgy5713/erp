package production

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"erp/internal/db"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

var today = time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)

// in0 把數量全放在第 0 期(沒填日期 / 已逾期的需求與供給)。
func in0(m map[int64]string) map[int64][]decimal.Decimal {
	out := map[int64][]decimal.Decimal{}
	for id, q := range m {
		out[id] = []decimal.Decimal{d(q), {}, {}, {}}
	}
	return out
}

func TestMrpCompute(t *testing.T) {
	// 成品 F(id 1) = 2×半成品 S(id 2);S = 3×材料 M(id 3);另有共用材料 M 直接用在 F:1×M
	items := []db.MrpItemsRow{
		{ID: 1, Code: "F", SafetyStock: d("0")},
		{ID: 2, Code: "S", SafetyStock: d("0")},
		{ID: 3, Code: "M", SafetyStock: d("10")},
	}
	mul := func(n string) func(decimal.Decimal) decimal.Decimal {
		return func(p decimal.Decimal) decimal.Decimal { return p.Mul(d(n)) }
	}
	bom := map[int64][]bomChild{
		1: {{itemID: 2, qty: mul("2")}, {itemID: 3, qty: mul("1")}},
		2: {{itemID: 3, qty: mul("3")}},
	}
	rows := mrpCompute(items, mrpInput{
		Today: today, Buckets: 4,
		Sales:  in0(map[int64]string{1: "10"}),                   // 訂單未出 10 個 F
		Po:     in0(map[int64]string{3: "5"}),                    // 材料採購在途 5
		WoIn:   in0(map[int64]string{2: "4"}),                    // S 工單在途 4
		OnHand: map[int64]decimal.Decimal{1: d("3"), 3: d("20")}, // F 庫存 3、M 庫存 20
		Bom:    bom,
	})
	by := map[string]mrpRow{}
	for _, r := range rows {
		by[r.ItemCode] = r
	}
	// F:淨需求 10 − 3 = 7 → 生產 7
	if f := by["F"]; f.Action != "make" || !f.SuggestedQty.Equal(d("7")) || f.Level != 0 {
		t.Fatalf("F = %+v", f)
	}
	// S:上層帶下 14,在途 4 → 淨 10 → 生產 10
	if s := by["S"]; s.Action != "make" || !s.PlanDemand.Equal(d("14")) || !s.SuggestedQty.Equal(d("10")) || s.Level != 1 {
		t.Fatalf("S = %+v", s)
	}
	// M:F 帶下 7、S 帶下 30、安全庫存 10 → 47;庫存 20 + 在途 5 → 淨 22 → 採購;層級在 S 之後(2)
	if m := by["M"]; m.Action != "buy" || !m.PlanDemand.Equal(d("37")) || !m.SuggestedQty.Equal(d("22")) || m.Level != 2 {
		t.Fatalf("M = %+v", m)
	}
}

func TestAttachSubstitutes(t *testing.T) {
	// 主料 M(1) 缺 10;替代料 A(2) 庫存 30、自己有 5 的銷售需求,ratio 2(1 個 M 要 2 個 A);
	// B(3) 沒有剩餘;C(4) 是夠用主料的替代料(不附提示)
	rows := []mrpRow{
		{ItemID: 1, ItemCode: "M", Net: d("10")},
		{ItemID: 2, ItemCode: "A", OnHand: d("30"), SalesDemand: d("5")},
		{ItemID: 3, ItemCode: "B", OnHand: d("4"), SafetyStock: d("4")},
		{ItemID: 4, ItemCode: "C", OnHand: d("9")},
		{ItemID: 5, ItemCode: "N", Net: d("-3")},
	}
	attachSubstitutes(rows, []subEdge{
		{1, 2, d("2")}, {1, 3, d("1")}, {5, 4, d("1")}, {1, 99, d("1")},
	})
	if len(rows[0].Substitutes) != 1 {
		t.Fatalf("M 替代料 = %+v", rows[0].Substitutes)
	}
	if s := rows[0].Substitutes[0]; s.ItemCode != "A" || !s.FreeQty.Equal(d("25")) || !s.Covers.Equal(d("12.5")) {
		t.Fatalf("A = %+v", s)
	}
	if len(rows[4].Substitutes) != 0 {
		t.Fatalf("夠用的主料不該有提示: %+v", rows[4].Substitutes)
	}
}

// at 把數量放在指定的週期。
func at(n int, t int, q string) []decimal.Decimal {
	out := make([]decimal.Decimal, n)
	out[t] = d(q)
	return out
}

func TestMrpTimePhased(t *testing.T) {
	items := []db.MrpItemsRow{{ID: 1, Code: "X", SafetyStock: d("0")}}
	// 庫存 10,第 2 期(第 15 天起)要出 15,第 3 期才有採購 10 到貨:第 2 期缺 5
	rows := mrpCompute(items, mrpInput{
		Today: today, Buckets: 4,
		Sales:  map[int64][]decimal.Decimal{1: at(4, 2, "15")},
		Po:     map[int64][]decimal.Decimal{1: at(4, 3, "10")},
		OnHand: map[int64]decimal.Decimal{1: d("10")},
		Params: map[int64]mrpParam{1: {LeadDays: 7}},
	})
	x := rows[0]
	if x.Action != "buy" || !x.SuggestedQty.Equal(d("5")) || !x.Net.Equal(d("5")) {
		t.Fatalf("X = %+v", x)
	}
	if x.FirstNeed == nil || *x.FirstNeed != "2026-10-26" || x.ReleaseBy == nil || *x.ReleaseBy != "2026-10-19" || x.Late {
		t.Fatalf("時程 first=%v release=%v late=%v", x.FirstNeed, x.ReleaseBy, x.Late)
	}
	if !x.Buckets[1].Projected.Equal(d("10")) || !x.Buckets[2].Projected.Equal(d("0")) || !x.Buckets[3].Projected.Equal(d("10")) {
		t.Fatalf("各期預計庫存 = %+v", x.Buckets)
	}
	// 供給早於需求就不缺料:採購第 1 期到貨 10,足夠第 2 期的需求
	rows = mrpCompute(items, mrpInput{
		Today: today, Buckets: 4,
		Sales:  map[int64][]decimal.Decimal{1: at(4, 2, "15")},
		Po:     map[int64][]decimal.Decimal{1: at(4, 1, "10")},
		OnHand: map[int64]decimal.Decimal{1: d("10")},
	})
	if rows[0].Action != "none" || !rows[0].Net.Equal(d("-5")) {
		t.Fatalf("供給早於需求應夠用: %+v", rows[0])
	}
}

func TestMrpLotSizeAndLead(t *testing.T) {
	items := []db.MrpItemsRow{{ID: 1, Code: "F"}, {ID: 2, Code: "M"}}
	mul := func(p decimal.Decimal) decimal.Decimal { return p.Mul(d("2")) }
	in := mrpInput{
		Today: today, Buckets: 4,
		Sales:  map[int64][]decimal.Decimal{1: at(4, 2, "5")},
		Params: map[int64]mrpParam{1: {LeadDays: 7, MinQty: d("20"), Multiple: d("8")}, 2: {LeadDays: 30}},
		Bom:    map[int64][]bomChild{1: {{itemID: 2, qty: mul}}},
	}
	by := map[string]mrpRow{}
	for _, r := range mrpCompute(items, in) {
		by[r.ItemCode] = r
	}
	// F 缺 5 → 最小量 20 → 進位到 8 的倍數 24;第 2 期開始日 10/26 − 7 天 = 10/19 下單(第 1 期)
	f := by["F"]
	if !f.SuggestedQty.Equal(d("24")) || !f.Net.Equal(d("5")) || *f.ReleaseBy != "2026-10-19" {
		t.Fatalf("F = %+v", f)
	}
	// M 的需求 48 落在第 1 期(10/19 開始);前置 30 天來不及,從今天算起並標 late
	m := by["M"]
	if !m.Buckets[1].Gross.Equal(d("48")) || !m.SuggestedQty.Equal(d("48")) || !m.Late || *m.ReleaseBy != "2026-10-12" {
		t.Fatalf("M = %+v", m)
	}
}

func TestMrpHorizonAndBuckets(t *testing.T) {
	if bucketOf(today.AddDate(0, 0, -3), today, 4) != 0 || bucketOf(today.AddDate(0, 0, 6), today, 4) != 0 ||
		bucketOf(today.AddDate(0, 0, 7), today, 4) != 1 || bucketOf(today.AddDate(0, 0, 400), today, 4) != 3 {
		t.Fatal("bucketOf 邊界不正確")
	}
	if got := lotSize(d("0.00004"), mrpParam{}); !got.IsZero() {
		t.Fatalf("lotSize = %s", got)
	}
}
