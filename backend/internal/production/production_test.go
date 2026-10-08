package production

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestWouldCycle(t *testing.T) {
	// 現有:A → B、B → C(成品 → 材料)
	edges := map[int64][]int64{1: {2}, 2: {3}}
	cases := []struct {
		name     string
		parent   int64
		children []int64
		want     bool
	}{
		{"沒有循環", 4, []int64{1, 2, 3}, false},
		{"成品的材料直接用到成品", 3, []int64{1}, true}, // C 的 BOM 用 A,而 A → B → C
		{"間接循環", 3, []int64{2}, true},
		{"改 A 的材料為新料", 1, []int64{3, 5}, false}, // A → C 沒問題(C 不再用 A)
		{"自己用自己", 7, []int64{7}, true},
	}
	for _, c := range cases {
		if got := WouldCycle(edges, c.parent, c.children); got != c.want {
			t.Errorf("%s = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestExplodeQty(t *testing.T) {
	d := decimal.RequireFromString
	cases := []struct{ bom, yield, plan, scrap, want string }{
		{"2", "1", "10", "0", "20"},
		{"1", "3", "10", "0", "3.3333"}, // 捨入到 4 位
		{"5", "10", "7", "0", "3.5"},
		{"0.0001", "1000", "1", "0", "0.0001"}, // 太小仍至少 0.0001
		{"2", "1", "10", "5", "21"},            // 損耗 5%
		{"1", "3", "10", "2.5", "3.4167"},      // 損耗後再捨入
	}
	for _, c := range cases {
		if got := ExplodeQty(d(c.bom), d(c.yield), d(c.plan), d(c.scrap)); !got.Equal(d(c.want)) {
			t.Errorf("explode(%s,%s,%s,%s) = %s, want %s", c.bom, c.yield, c.plan, c.scrap, got, c.want)
		}
	}
}
