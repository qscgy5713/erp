package gl

import (
	"testing"

	"github.com/shopspring/decimal"
)

func act(code, name, typ, d, c string) activity {
	return activity{code: code, name: name, acctType: typ, debit: decimal.RequireFromString(d), credit: decimal.RequireFromString(c)}
}

func find(lines []StmtLine, label string) StmtLine {
	for _, l := range lines {
		if l.Label == label {
			return l
		}
	}
	return StmtLine{Label: "(找不到 " + label + ")"}
}

func TestSectionOf(t *testing.T) {
	cases := []struct{ typ, code, want string }{
		{"revenue", "4101", secRevenue}, {"revenue", "4901", secNonOpIn},
		{"cost", "5101", secCost},
		{"expense", "6102", secOpex}, {"expense", "7101", secNonOpOut}, {"expense", "8101", secTax},
		{"asset", "1101", secCurAsset}, {"asset", "1501", secFixedAsset},
		{"liability", "2101", secCurLiab}, {"liability", "2501", secFixedLiab},
		{"equity", "3101", secEquity}, {"other", "9", ""},
	}
	for _, c := range cases {
		if got := sectionOf(c.typ, c.code); got != c.want {
			t.Errorf("sectionOf(%s,%s)=%s want %s", c.typ, c.code, got, c.want)
		}
	}
}

func TestIncomeStatement(t *testing.T) {
	cur := []activity{
		act("4101", "銷貨收入", "revenue", "0", "10000"),
		act("4102", "銷貨退回及折讓", "revenue", "500", "0"), // 借方餘額的收入科目 → 負數
		act("5101", "銷貨成本", "cost", "6000", "0"),
		act("6102", "租金支出", "expense", "1000", "0"),
		act("4901", "利息收入", "revenue", "0", "100"),
		act("7101", "利息費用", "expense", "200", "0"),
		act("8101", "所得稅費用", "expense", "300", "0"),
		act("1101", "現金", "asset", "9999", "0"), // 資產科目不應出現在損益表
	}
	prev := []activity{act("4101", "銷貨收入", "revenue", "0", "4000")}
	lines := IncomeStatement(cur, prev)
	want := map[string]string{
		"營業收入淨額": "9500", "營業毛利": "3500", "營業淨利": "2500", "稅前淨利": "2400", "本期淨利(損)": "2100",
	}
	for label, v := range want {
		if got := find(lines, label).Amount; !got.Equal(decimal.RequireFromString(v)) {
			t.Errorf("%s = %s, want %s", label, got, v)
		}
	}
	if got := find(lines, "營業收入淨額").Prev; !got.Equal(decimal.NewFromInt(4000)) {
		t.Errorf("比較期營業收入 = %s", got)
	}
	if got := find(lines, "現金").Label; got == "現金" {
		t.Error("資產科目不應出現在損益表")
	}
	// 沒有營業外與稅的資料時省略該區段
	if l := IncomeStatement(cur[:4], nil); find(l, "營業外費損").Kind != "" {
		t.Error("空區段應省略")
	}
}

func TestBalanceSheet(t *testing.T) {
	// 股本 5000 現金入帳;賣出 1000 收現(未年結)→ 資產 6000 = 權益 5000 + 未結轉損益 1000
	cur := []activity{
		act("1101", "現金", "asset", "6000", "0"),
		act("3101", "股本", "equity", "0", "5000"),
		act("4101", "銷貨收入", "revenue", "0", "1000"),
	}
	lines, ok := BalanceSheet(cur, nil)
	if !ok {
		t.Fatal("資產負債表應平衡")
	}
	if got := find(lines, "權益總計").Amount; !got.Equal(decimal.NewFromInt(6000)) {
		t.Errorf("權益總計 = %s", got)
	}
	if find(lines, "未結轉損益(尚未年度結帳的累計損益)").Amount.IsZero() {
		t.Error("應列出未結轉損益")
	}
	// 年結後:收入結清、保留盈餘 1000,仍平衡且不再有未結轉損益
	closed := []activity{
		act("1101", "現金", "asset", "6000", "0"),
		act("3101", "股本", "equity", "0", "5000"),
		act("3201", "保留盈餘", "equity", "0", "1000"),
		act("4101", "銷貨收入", "revenue", "1000", "1000"),
	}
	lines, ok = BalanceSheet(closed, nil)
	if !ok || find(lines, "未結轉損益(尚未年度結帳的累計損益)").Kind != "" {
		t.Errorf("年結後應平衡且無未結轉損益 ok=%v", ok)
	}
	// 不平衡的資料要被偵測出來
	if _, ok := BalanceSheet([]activity{act("1101", "現金", "asset", "10", "0")}, nil); ok {
		t.Error("不平衡應回報 false")
	}
}
