package gl

import (
	"sort"
	"strings"

	"github.com/shopspring/decimal"
)

// 財報的科目分類規則(D59):以科目類別加上代號前綴判斷,沿用台灣常用科目編碼——
//
//	1 資產:11 開頭為流動資產,其餘為非流動資產
//	2 負債:21 開頭為流動負債,其餘為非流動負債
//	3 權益
//	4 收入:49 開頭為營業外收入,其餘為營業收入
//	5 成本:營業成本
//	6 費用:營業費用;7 開頭為營業外費損;8 開頭為所得稅費用
//
// 自訂科目只要依這個編碼慣例命名就會落在正確的區段。

// 損益表區段
const (
	secRevenue    = "revenue"     // 營業收入
	secCost       = "cost"        // 營業成本
	secOpex       = "opex"        // 營業費用
	secNonOpIn    = "nonop_in"    // 營業外收入
	secNonOpOut   = "nonop_out"   // 營業外費損
	secTax        = "tax"         // 所得稅費用
	secCurAsset   = "cur_asset"   // 流動資產
	secFixedAsset = "fixed_asset" // 非流動資產
	secCurLiab    = "cur_liab"    // 流動負債
	secFixedLiab  = "fixed_liab"  // 非流動負債
	secEquity     = "equity"      // 權益
)

// sectionOf 回傳科目所屬的報表區段;類別不明時回傳空字串(不列入報表)。
func sectionOf(acctType, code string) string {
	switch acctType {
	case "revenue":
		if strings.HasPrefix(code, "49") {
			return secNonOpIn
		}
		return secRevenue
	case "cost":
		return secCost
	case "expense":
		switch {
		case strings.HasPrefix(code, "7"):
			return secNonOpOut
		case strings.HasPrefix(code, "8"):
			return secTax
		}
		return secOpex
	case "asset":
		if strings.HasPrefix(code, "11") {
			return secCurAsset
		}
		return secFixedAsset
	case "liability":
		if strings.HasPrefix(code, "21") {
			return secCurLiab
		}
		return secFixedLiab
	case "equity":
		return secEquity
	}
	return ""
}

// activity 單一科目的期間借貸合計。
type activity struct {
	code, name, acctType string
	debit, credit        decimal.Decimal
}

// natural 依科目正常餘額方向取得「正」的金額:資產、成本、費用為借方餘額;負債、權益、收入為貸方餘額。
func natural(a activity) decimal.Decimal {
	switch a.acctType {
	case "asset", "cost", "expense":
		return a.debit.Sub(a.credit)
	}
	return a.credit.Sub(a.debit)
}

// StmtLine 報表的一行。Kind: header 區段標題 / account 科目 / total 小計或合計。
type StmtLine struct {
	Kind   string          `json:"kind"`
	Level  int             `json:"level"` // 縮排層數
	Code   string          `json:"code,omitempty"`
	Label  string          `json:"label"`
	Amount decimal.Decimal `json:"amount"`
	Prev   decimal.Decimal `json:"prev"` // 比較期間(去年同期 / 去年同日)金額
}

// secAccount / group 把科目依區段分組並保持代號順序;cur 與 prev 兩期的科目聯集後逐科目對齊。
type secAccount struct {
	code, name string
	cur, prev  decimal.Decimal
}

func group(cur, prev []activity) map[string][]secAccount {
	idx := map[string]*secAccount{}
	order := map[string][]string{}
	add := func(list []activity, isPrev bool) {
		for _, a := range list {
			sec := sectionOf(a.acctType, a.code)
			if sec == "" {
				continue
			}
			sa, ok := idx[a.code]
			if !ok {
				sa = &secAccount{code: a.code, name: a.name}
				idx[a.code] = sa
				order[sec] = append(order[sec], a.code)
			}
			if isPrev {
				sa.prev = natural(a)
			} else {
				sa.cur = natural(a)
			}
		}
	}
	add(cur, false)
	add(prev, true)
	out := map[string][]secAccount{}
	for sec, codes := range order {
		sort.Strings(codes)
		for _, c := range codes {
			sa := idx[c]
			if sa.cur.IsZero() && sa.prev.IsZero() {
				continue // 沒有餘額的科目不列
			}
			out[sec] = append(out[sec], *sa)
		}
	}
	return out
}

// stmt 逐段組出報表行,並記錄各區段小計供計算合計。
type stmt struct {
	lines []StmtLine
	sums  map[string][2]decimal.Decimal // 區段 → {本期, 比較期}
	g     map[string][]secAccount
}

func newStmt(cur, prev []activity) *stmt {
	return &stmt{g: group(cur, prev), sums: map[string][2]decimal.Decimal{}}
}

// section 輸出一個區段(標題 + 科目 + 小計),金額為科目的正常餘額方向;由合計行決定加或減。
// show 為 false 且區段沒有科目時整段省略,但小計仍記為 0 以便計算合計。
func (s *stmt) section(key, header, totalLabel string, show bool) {
	accts := s.g[key]
	var cur, prev decimal.Decimal
	for _, a := range accts {
		cur, prev = cur.Add(a.cur), prev.Add(a.prev)
	}
	s.sums[key] = [2]decimal.Decimal{cur, prev}
	if len(accts) == 0 && !show {
		return
	}
	s.lines = append(s.lines, StmtLine{Kind: "header", Label: header})
	for _, a := range accts {
		s.lines = append(s.lines, StmtLine{Kind: "account", Level: 1, Code: a.code, Label: a.name, Amount: a.cur, Prev: a.prev})
	}
	s.lines = append(s.lines, StmtLine{Kind: "total", Label: totalLabel, Amount: cur, Prev: prev})
}

// total 輸出由區段小計加減而成的合計行(例如營業毛利 = 營業收入 − 營業成本)。
func (s *stmt) total(label string, plus []string, minus []string) [2]decimal.Decimal {
	var v [2]decimal.Decimal
	for _, k := range plus {
		v[0], v[1] = v[0].Add(s.sums[k][0]), v[1].Add(s.sums[k][1])
	}
	for _, k := range minus {
		v[0], v[1] = v[0].Sub(s.sums[k][0]), v[1].Sub(s.sums[k][1])
	}
	s.lines = append(s.lines, StmtLine{Kind: "total", Label: label, Amount: v[0], Prev: v[1]})
	return v
}

// IncomeStatement 損益表。cur / prev 為本期與比較期的科目活動(已排除年度結帳傳票)。
// 營業收入已含銷貨退回及折讓(借方餘額的收入科目自然為負);營業外費損與所得稅以正數列示、在合計時減除。
func IncomeStatement(cur, prev []activity) []StmtLine {
	s := newStmt(cur, prev)
	s.section(secRevenue, "營業收入", "營業收入淨額", true)
	s.section(secCost, "營業成本", "營業成本合計", true)
	gross := s.total("營業毛利", []string{secRevenue}, []string{secCost})
	s.section(secOpex, "營業費用", "營業費用合計", true)
	s.sums["gross"] = gross
	opIncome := s.total("營業淨利", []string{"gross"}, []string{secOpex})
	s.sums["op_income"] = opIncome
	s.section(secNonOpIn, "營業外收入", "營業外收入合計", false)
	s.section(secNonOpOut, "營業外費損", "營業外費損合計", false)
	pretax := s.total("稅前淨利", []string{"op_income", secNonOpIn}, []string{secNonOpOut})
	s.sums["pretax"] = pretax
	s.section(secTax, "所得稅費用", "所得稅費用合計", false)
	s.total("本期淨利(損)", []string{"pretax"}, []string{secTax})
	return s.lines
}

// BalanceSheet 資產負債表。cur / prev 為截至兩個日期的累計科目活動(含所有年度結帳傳票)。
// 尚未年度結帳的損益科目餘額以「未結轉損益」列在權益,所以不論有沒有年結,資產 = 負債 + 權益 都成立。
// 回傳報表行與是否平衡。
func BalanceSheet(cur, prev []activity) ([]StmtLine, bool) {
	s := newStmt(cur, prev)
	s.section(secCurAsset, "流動資產", "流動資產合計", false)
	s.section(secFixedAsset, "非流動資產", "非流動資產合計", false)
	assets := s.total("資產總計", []string{secCurAsset, secFixedAsset}, nil)
	s.sums["assets"] = assets

	s.section(secCurLiab, "流動負債", "流動負債合計", false)
	s.section(secFixedLiab, "非流動負債", "非流動負債合計", false)
	liab := s.total("負債總計", []string{secCurLiab, secFixedLiab}, nil)
	s.sums["liab"] = liab

	s.section(secEquity, "權益", "權益(科目)合計", false)
	// 未結轉損益 = 收入 − 成本 − 費用(全部損益科目的累計淨額)
	var open [2]decimal.Decimal
	for i, list := range [][]activity{cur, prev} {
		for _, a := range list {
			switch a.acctType {
			case "revenue":
				open[i] = open[i].Add(a.credit.Sub(a.debit))
			case "cost", "expense":
				open[i] = open[i].Sub(a.debit.Sub(a.credit))
			}
		}
	}
	if !open[0].IsZero() || !open[1].IsZero() {
		s.lines = append(s.lines, StmtLine{Kind: "account", Level: 1, Label: "未結轉損益(尚未年度結帳的累計損益)", Amount: open[0], Prev: open[1]})
	}
	s.sums["equity_all"] = [2]decimal.Decimal{s.sums[secEquity][0].Add(open[0]), s.sums[secEquity][1].Add(open[1])}
	s.lines = append(s.lines, StmtLine{Kind: "total", Label: "權益總計", Amount: s.sums["equity_all"][0], Prev: s.sums["equity_all"][1]})
	both := s.total("負債及權益總計", []string{"liab", "equity_all"}, nil)

	balanced := assets[0].Equal(both[0]) && assets[1].Equal(both[1])
	return s.lines, balanced
}
