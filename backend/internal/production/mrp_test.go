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
