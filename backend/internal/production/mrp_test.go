package production

import (
	"testing"

	"github.com/shopspring/decimal"

	"erp/internal/db"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

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
	rows := mrpCompute(items,
		map[int64]decimal.Decimal{1: d("10")}, // 訂單未出 10 個 F
		map[int64]decimal.Decimal{},
		map[int64]decimal.Decimal{3: d("5")},             // 材料採購在途 5
		map[int64]decimal.Decimal{2: d("4")},             // S 工單在途 4
		map[int64]decimal.Decimal{1: d("3"), 3: d("20")}, // F 庫存 3、M 庫存 20
		bom)
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
