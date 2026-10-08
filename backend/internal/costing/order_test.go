package costing

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestCostOrder(t *testing.T) {
	use := func(item int64) materialUse { return materialUse{item: item, qty: decimal.NewFromInt(1)} }
	// 3 用 2、2 用 1:計算順序必須是 1、2、3,與料品 id 排序無關
	p := productionInputs{materials: map[int64][]materialUse{3: {use(2)}, 2: {use(1)}}}
	got, err := costOrder([]int64{1, 2, 3}, p)
	if err != nil || len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("order = %v err=%v", got, err)
	}
	p = productionInputs{materials: map[int64][]materialUse{1: {use(3)}, 3: {use(2)}, 2: {use(1)}}}
	if _, err := costOrder([]int64{1, 2, 3}, p); err == nil {
		t.Fatal("循環應回報錯誤")
	}
	// 材料不在當月清單內(沒有活動)不影響順序
	p = productionInputs{materials: map[int64][]materialUse{5: {use(9)}}}
	if got, err := costOrder([]int64{5}, p); err != nil || len(got) != 1 {
		t.Fatalf("order = %v err=%v", got, err)
	}
}

func TestComputeWithConsumption(t *testing.T) {
	d := decimal.RequireFromString
	// 材料:期初 60 個 6000;本月領料 20 個 → 平均 100,領料金額 2000,期末 40 個 4000,不計入銷貨成本
	r := compute(opening{qty: d("60"), value: d("6000"), avg: d("100")}, activity{consumeQty: d("-20")})
	if !r.avg.Equal(d("100")) || !r.consumeV.Equal(d("2000")) || !r.closingQty.Equal(d("40")) || !r.closingV.Equal(d("4000")) || !r.cogs.IsZero() {
		t.Fatalf("result = %+v", r)
	}
	// 成品:本月完工 10 個、成本 4500(併入進貨)→ 平均 450
	r = compute(opening{}, activity{purchaseQty: d("10"), purchaseValue: d("4500")})
	if !r.avg.Equal(d("450")) || !r.closingV.Equal(d("4500")) {
		t.Fatalf("result = %+v", r)
	}
	// 只有領料也不算閒置(整筆沿用期初的捷徑不可略過領料)
	if (activity{consumeQty: d("-1")}).idle() || (activity{produceQty: d("1")}).idle() {
		t.Fatal("有領料 / 完工不應視為閒置")
	}
}
