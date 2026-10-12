package gl

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/shared/money"
	"erp/internal/shared/response"
)

// 現金流量表(直接法,D74):現金 = 對照表 settle.cash / settle.bank 的科目;每張傳票的現金增減依
// 「對方科目」的金額比例分攤,再依對方科目分類——
//   - 營業活動:收入 / 成本 / 費用、流動資產(11xx)、流動負債(21xx)
//   - 投資活動:其他資產(12xx 以後,例如設備、投資)
//   - 融資活動:權益(3xxx)、非流動負債(22xx 以後,例如借款)
func cashSection(acctType, code string) int {
	switch acctType {
	case "revenue", "cost", "expense":
		return 0
	case "asset":
		if strings.HasPrefix(code, "11") {
			return 0
		}
		return 1
	case "liability":
		if strings.HasPrefix(code, "21") {
			return 0
		}
		return 2
	}
	return 2
}

var cashSectionLabels = [3]struct{ head, total string }{
	{"營業活動之現金流量", "營業活動淨現金流入(出)"},
	{"投資活動之現金流量", "投資活動淨現金流入(出)"},
	{"融資活動之現金流量", "融資活動淨現金流入(出)"},
}

func (m *Module) cashFlowFor(c *gin.Context, from, to time.Time) (map[int][]db.CashFlowAllocationRow, decimal.Decimal, decimal.Decimal, error) {
	ctx := c.Request.Context()
	companyID := actor(c).CompanyID
	rows, err := m.store.CashFlowAllocation(ctx, db.CashFlowAllocationParams{CompanyID: companyID, FromDate: from, ToDate: to})
	if err != nil {
		return nil, decimal.Zero, decimal.Zero, err
	}
	opening, err := m.store.CashBalanceAsOf(ctx, db.CashBalanceAsOfParams{CompanyID: companyID, AsOf: from.AddDate(0, 0, -1)})
	if err != nil {
		return nil, decimal.Zero, decimal.Zero, err
	}
	closing, err := m.store.CashBalanceAsOf(ctx, db.CashBalanceAsOfParams{CompanyID: companyID, AsOf: to})
	if err != nil {
		return nil, decimal.Zero, decimal.Zero, err
	}
	by := map[int][]db.CashFlowAllocationRow{}
	for _, r := range rows {
		s := cashSection(r.AcctType, r.Code)
		by[s] = append(by[s], r)
	}
	return by, opening, closing, nil
}

// buildCashFlow 把本期與比較期(prev 可為 nil)的分攤結果對齊成報表行。
func buildCashFlow(cur, prev map[int][]db.CashFlowAllocationRow, curOpen, curClose, prevOpen, prevClose decimal.Decimal) ([]StmtLine, decimal.Decimal) {
	var lines []StmtLine
	var netCur, netPrev decimal.Decimal
	for s := 0; s < 3; s++ {
		type pair struct {
			code, name string
			cur, prev  decimal.Decimal
		}
		order := []string{}
		byCode := map[string]*pair{}
		add := func(rows []db.CashFlowAllocationRow, isPrev bool) {
			for _, r := range rows {
				p, ok := byCode[r.Code]
				if !ok {
					p = &pair{code: r.Code, name: r.Name}
					byCode[r.Code] = p
					order = append(order, r.Code)
				}
				if isPrev {
					p.prev = p.prev.Add(money.Amount(r.Amount))
				} else {
					p.cur = p.cur.Add(money.Amount(r.Amount))
				}
			}
		}
		add(cur[s], false)
		add(prev[s], true)
		// 依科目代號排序
		for i := 1; i < len(order); i++ {
			for j := i; j > 0 && order[j] < order[j-1]; j-- {
				order[j], order[j-1] = order[j-1], order[j]
			}
		}
		lines = append(lines, StmtLine{Kind: "header", Label: cashSectionLabels[s].head})
		var sc, sp decimal.Decimal
		for _, code := range order {
			p := byCode[code]
			lines = append(lines, StmtLine{Kind: "account", Level: 1, Code: p.code, Label: p.name, Amount: p.cur, Prev: p.prev})
			sc, sp = sc.Add(p.cur), sp.Add(p.prev)
		}
		lines = append(lines, StmtLine{Kind: "total", Label: cashSectionLabels[s].total, Amount: sc, Prev: sp})
		netCur, netPrev = netCur.Add(sc), netPrev.Add(sp)
	}
	lines = append(lines,
		StmtLine{Kind: "total", Label: "本期現金淨增減", Amount: netCur, Prev: netPrev},
		StmtLine{Kind: "account", Label: "期初現金及銀行存款", Amount: money.Amount(curOpen), Prev: money.Amount(prevOpen)},
		StmtLine{Kind: "total", Label: "期末現金及銀行存款", Amount: money.Amount(curClose), Prev: money.Amount(prevClose)},
	)
	return lines, netCur
}

// cashFlow GET /gl/reports/cash-flow?from=&to=&compare=true&format=xlsx
func (m *Module) cashFlow(c *gin.Context) {
	from, to, err := dateRange(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	compare := c.Query("compare") == "true"
	cur, curOpen, curClose, err := m.cashFlowFor(c, from, to)
	if err != nil {
		response.Error(c, err)
		return
	}
	dto := statementDTO{Title: "現金流量表", From: from.Format(time.DateOnly), To: to.Format(time.DateOnly), Compare: compare}
	var prev map[int][]db.CashFlowAllocationRow
	var prevOpen, prevClose decimal.Decimal
	if compare {
		pf, pt := yearAgo(from), yearAgo(to)
		dto.PrevFrom, dto.PrevTo = pf.Format(time.DateOnly), pt.Format(time.DateOnly)
		if prev, prevOpen, prevClose, err = m.cashFlowFor(c, pf, pt); err != nil {
			response.Error(c, err)
			return
		}
	}
	lines, net := buildCashFlow(cur, prev, curOpen, curClose, prevOpen, prevClose)
	dto.Lines = lines
	// 自我核對:淨增減應等於期末 − 期初(差異只可能來自四捨五入)
	if diff := net.Sub(money.Amount(curClose).Sub(money.Amount(curOpen))).Abs(); diff.GreaterThan(decimal.NewFromInt(1)) {
		dto.Note = "現金流量淨增減與現金科目餘額變動不一致(差 " + diff.String() + "),請確認現金對照科目設定與傳票。"
	}
	m.respondStatement(c, dto)
}

type deptPnlRow struct {
	DepartmentID *int64          `json:"department_id"`
	Code         string          `json:"code"`
	Name         string          `json:"name"`
	Revenue      decimal.Decimal `json:"revenue"`
	Cost         decimal.Decimal `json:"cost"`
	Expense      decimal.Decimal `json:"expense"`
	Profit       decimal.Decimal `json:"profit"`
}

// departmentPnl GET /gl/reports/department-pnl?from=&to=
// 部門別損益:收入 − 成本 − 費用,依傳票明細的輔助核算部門;沒填部門的歸在「未指定」。
func (m *Module) departmentPnl(c *gin.Context) {
	from, to, err := dateRange(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	rows, err := m.store.DepartmentPnl(c.Request.Context(), db.DepartmentPnlParams{CompanyID: actor(c).CompanyID, FromDate: from, ToDate: to})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := []*deptPnlRow{}
	idx := map[int64]*deptPnlRow{}
	var unassigned *deptPnlRow
	for _, r := range rows {
		var d *deptPnlRow
		if r.DepartmentID == nil {
			if unassigned == nil {
				unassigned = &deptPnlRow{Name: "未指定部門"}
			}
			d = unassigned
		} else if d = idx[*r.DepartmentID]; d == nil {
			code, name := "", ""
			if r.DepartmentCode != nil {
				code = *r.DepartmentCode
			}
			if r.DepartmentName != nil {
				name = *r.DepartmentName
			}
			d = &deptPnlRow{DepartmentID: r.DepartmentID, Code: code, Name: name}
			idx[*r.DepartmentID] = d
			out = append(out, d)
		}
		switch r.AcctType {
		case "revenue":
			d.Revenue = d.Revenue.Add(r.Amount)
		case "cost":
			d.Cost = d.Cost.Sub(r.Amount)
		case "expense":
			d.Expense = d.Expense.Sub(r.Amount)
		}
	}
	if unassigned != nil {
		out = append(out, unassigned)
	}
	total := deptPnlRow{Name: "合計"}
	for _, d := range out {
		d.Profit = d.Revenue.Sub(d.Cost).Sub(d.Expense)
		total.Revenue, total.Cost = total.Revenue.Add(d.Revenue), total.Cost.Add(d.Cost)
		total.Expense, total.Profit = total.Expense.Add(d.Expense), total.Profit.Add(d.Profit)
	}
	response.OK(c, gin.H{"from": from.Format(time.DateOnly), "to": to.Format(time.DateOnly), "rows": out, "total": total})
}
