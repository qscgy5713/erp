package gl

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
)

var tst = time.FixedZone("TST", 8*3600)

// yearAgo 去年同日;2/29 在平年沒有對應日期,取 2/28(AddDate 會變成 3/1)。
func yearAgo(d time.Time) time.Time {
	if d.Month() == time.February && d.Day() == 29 {
		return time.Date(d.Year()-1, time.February, 28, 0, 0, 0, 0, time.UTC)
	}
	return d.AddDate(-1, 0, 0)
}

func toActivities(rows []db.AccountActivityRow) []activity {
	out := make([]activity, len(rows))
	for i, r := range rows {
		out[i] = activity{code: r.Code, name: r.Name, acctType: r.AcctType, debit: r.Debit, credit: r.Credit}
	}
	return out
}

func (m *Module) activities(ctx context.Context, companyID int64, from *time.Time, to time.Time, excludeYearEnd bool) ([]activity, error) {
	rows, err := m.store.AccountActivity(ctx, db.AccountActivityParams{
		CompanyID: companyID, FromDate: from, ToDate: to, ExcludeYearEnd: excludeYearEnd,
	})
	if err != nil {
		return nil, err
	}
	return toActivities(rows), nil
}

type statementDTO struct {
	Title    string     `json:"title"`
	From     string     `json:"from,omitempty"`
	To       string     `json:"to"`
	Compare  bool       `json:"compare"`
	PrevFrom string     `json:"prev_from,omitempty"`
	PrevTo   string     `json:"prev_to,omitempty"`
	Balanced *bool      `json:"balanced,omitempty"`
	Lines    []StmtLine `json:"lines"`
}

// incomeStatement GET /gl/reports/income-statement?from=&to=&compare=true&format=xlsx
// 損益表:期間內已過帳傳票的收入、成本、費用(排除年度結帳傳票);compare=true 另列去年同期。
func (m *Module) incomeStatement(c *gin.Context) {
	from, to, err := dateRange(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	compare := c.Query("compare") == "true"
	ctx := c.Request.Context()
	companyID := actor(c).CompanyID
	cur, err := m.activities(ctx, companyID, &from, to, true)
	if err != nil {
		response.Error(c, err)
		return
	}
	dto := statementDTO{Title: "損益表", From: from.Format(time.DateOnly), To: to.Format(time.DateOnly), Compare: compare}
	var prev []activity
	if compare {
		pf, pt := yearAgo(from), yearAgo(to)
		dto.PrevFrom, dto.PrevTo = pf.Format(time.DateOnly), pt.Format(time.DateOnly)
		if prev, err = m.activities(ctx, companyID, &pf, pt, true); err != nil {
			response.Error(c, err)
			return
		}
	}
	dto.Lines = IncomeStatement(cur, prev)
	m.respondStatement(c, dto)
}

// balanceSheet GET /gl/reports/balance-sheet?as_of=&compare=true&format=xlsx
// 資產負債表:截至某日的累計餘額(含年度結帳傳票);compare=true 另列去年同日。
func (m *Module) balanceSheet(c *gin.Context) {
	asOf, err := time.Parse(time.DateOnly, c.Query("as_of"))
	if err != nil {
		response.Error(c, fieldErr("as_of", "請指定截止日期(YYYY-MM-DD)"))
		return
	}
	compare := c.Query("compare") == "true"
	ctx := c.Request.Context()
	companyID := actor(c).CompanyID
	cur, err := m.activities(ctx, companyID, nil, asOf, false)
	if err != nil {
		response.Error(c, err)
		return
	}
	dto := statementDTO{Title: "資產負債表", To: asOf.Format(time.DateOnly), Compare: compare}
	var prev []activity
	if compare {
		pt := yearAgo(asOf)
		dto.PrevTo = pt.Format(time.DateOnly)
		if prev, err = m.activities(ctx, companyID, nil, pt, false); err != nil {
			response.Error(c, err)
			return
		}
	}
	lines, balanced := BalanceSheet(cur, prev)
	dto.Lines, dto.Balanced = lines, &balanced
	m.respondStatement(c, dto)
}

func (m *Module) respondStatement(c *gin.Context, dto statementDTO) {
	if c.Query("format") != "xlsx" {
		response.OK(c, dto)
		return
	}
	data, err := statementXLSX(dto)
	if err != nil {
		response.Error(c, err)
		return
	}
	name := dto.Title + "_" + dto.To + ".xlsx"
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+urlEscape(name))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

func urlEscape(s string) string {
	var b bytes.Buffer
	for _, by := range []byte(s) {
		if (by >= 'a' && by <= 'z') || (by >= 'A' && by <= 'Z') || (by >= '0' && by <= '9') || by == '.' || by == '-' || by == '_' {
			b.WriteByte(by)
		} else {
			fmt.Fprintf(&b, "%%%02X", by)
		}
	}
	return b.String()
}

// statementXLSX 匯出報表:標題、期間、各行(區段標題與合計粗體,金額千分位)。
func statementXLSX(dto statementDTO) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	const sheet = "報表"
	_ = f.SetSheetName("Sheet1", sheet)
	title, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}})
	head, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"#F2F2F2"}, Pattern: 1}})
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, NumFmt: 3, Border: []excelize.Border{{Type: "top", Color: "#999999", Style: 1}}})
	num, _ := f.NewStyle(&excelize.Style{NumFmt: 3})

	period := "截至 " + dto.To
	if dto.From != "" {
		period = dto.From + " ~ " + dto.To
	}
	_ = f.SetCellValue(sheet, "A1", dto.Title)
	_ = f.SetCellStyle(sheet, "A1", "A1", title)
	_ = f.SetCellValue(sheet, "A2", period+"(新台幣)")
	cols := []string{"科目代號", "項目", "金額"}
	if dto.Compare {
		prevLabel := "去年同期"
		if dto.From == "" {
			prevLabel = "去年同日"
		}
		cols = append(cols, prevLabel)
	}
	for i, h := range cols {
		cell, _ := excelize.CoordinatesToCellName(i+1, 4)
		_ = f.SetCellValue(sheet, cell, h)
	}
	endCol, _ := excelize.CoordinatesToCellName(len(cols), 4)
	_ = f.SetCellStyle(sheet, "A4", endCol, head)
	row := 5
	for _, l := range dto.Lines {
		indent := ""
		for range l.Level {
			indent += "    "
		}
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), l.Code)
		_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row), indent+l.Label)
		if l.Kind != "header" {
			amt, _ := l.Amount.Float64()
			_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", row), amt)
			if dto.Compare {
				pv, _ := l.Prev.Float64()
				_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", row), pv)
			}
		}
		last, _ := excelize.CoordinatesToCellName(len(cols), row)
		switch l.Kind {
		case "header":
			_ = f.SetCellStyle(sheet, fmt.Sprintf("A%d", row), last, head)
		case "total":
			_ = f.SetCellStyle(sheet, fmt.Sprintf("A%d", row), last, bold)
		default:
			_ = f.SetCellStyle(sheet, fmt.Sprintf("C%d", row), last, num)
		}
		row++
	}
	_ = f.SetColWidth(sheet, "A", "A", 12)
	_ = f.SetColWidth(sheet, "B", "B", 42)
	_ = f.SetColWidth(sheet, "C", "D", 16)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---- 年度結帳 ----

var (
	errYearNotEnded   = apperr.New(http.StatusUnprocessableEntity, "GL-030", "年度尚未結束,不可年度結帳")
	errYearClosed     = apperr.New(http.StatusConflict, "GL-031", "此年度已年度結帳")
	errNothingToClose = apperr.New(http.StatusUnprocessableEntity, "GL-032", "此年度沒有損益科目餘額,不需要年度結帳")
	errYearNotClosed  = apperr.New(http.StatusConflict, "GL-033", "此年度尚未年度結帳")
)

type yearDTO struct {
	Year      int             `json:"year"`
	Status    string          `json:"status"` // closed 已年結 / open 可年結 / not_ended 年度尚未結束
	VoucherNo string          `json:"voucher_no,omitempty"`
	Profit    decimal.Decimal `json:"profit"` // 該年度損益(排除年結傳票)
}

// yearProfit 該年度損益(收入 − 成本 − 費用),排除年度結帳傳票。
func (m *Module) yearActivity(ctx context.Context, q *db.Queries, companyID int64, year int) ([]db.AccountActivityRow, error) {
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	return q.AccountActivity(ctx, db.AccountActivityParams{CompanyID: companyID, FromDate: &from, ToDate: to, ExcludeYearEnd: true})
}

func profitOf(rows []db.AccountActivityRow) decimal.Decimal {
	p := decimal.Zero
	for _, r := range rows {
		switch r.AcctType {
		case "revenue":
			p = p.Add(r.Credit.Sub(r.Debit))
		case "cost", "expense":
			p = p.Sub(r.Debit.Sub(r.Credit))
		}
	}
	return p
}

// listYears GET /gl/year-end 有已過帳傳票的年度與年結狀態。
func (m *Module) listYears(c *gin.Context) {
	ctx := c.Request.Context()
	companyID := actor(c).CompanyID
	years, err := m.store.YearsWithVouchers(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	thisYear := time.Now().In(tst).Year()
	out := []yearDTO{}
	for _, y := range years {
		d := yearDTO{Year: int(y), Status: "open"}
		rows, err := m.yearActivity(ctx, m.store.Queries, companyID, d.Year)
		if err != nil {
			response.Error(c, err)
			return
		}
		d.Profit = profitOf(rows)
		if v, err := m.store.OpenYearEndVoucher(ctx, db.OpenYearEndVoucherParams{CompanyID: companyID, Year: int64(y)}); err == nil {
			d.Status, d.VoucherNo = "closed", v.DocNo
		} else if !database.IsNoRows(err) {
			response.Error(c, err)
			return
		} else if d.Year >= thisYear {
			d.Status = "not_ended"
		}
		out = append(out, d)
	}
	response.OK(c, out)
}

func yearParam(c *gin.Context) (int, error) {
	y, err := strconv.Atoi(c.Param("year"))
	if err != nil || y < 2000 || y > 2100 {
		return 0, apperr.ErrNotFound
	}
	return y, nil
}

// closeYear POST /gl/year-end/:year/close
// 年度結帳:產生一張 12/31 的結帳傳票,把該年度收入、成本、費用科目的餘額結清,差額(本期損益)轉入保留盈餘。
// 損益表排除這張傳票,所以結帳後仍看得到該年度的收入與費用;資產負債表的權益則由「未結轉損益」轉為保留盈餘。
func (m *Module) closeYear(c *gin.Context) {
	year, err := yearParam(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		if year >= time.Now().In(tst).Year() {
			return errYearNotEnded
		}
		if err := q.LockCompanyForPeriodChange(ctx, a.CompanyID); err != nil {
			return err
		}
		if _, err := q.OpenYearEndVoucher(ctx, db.OpenYearEndVoucherParams{CompanyID: a.CompanyID, Year: int64(year)}); err == nil {
			return errYearClosed
		} else if !database.IsNoRows(err) {
			return err
		}
		rows, err := m.yearActivity(ctx, q, a.CompanyID, year)
		if err != nil {
			return err
		}
		retained, err := resolve(ctx, q, a.CompanyID, []Entry{{Key: "year.retained"}})
		if err != nil {
			return err
		}
		var entries []RawEntry
		debit, credit := decimal.Zero, decimal.Zero
		for _, r := range rows {
			var net decimal.Decimal // 正為該科目的正常餘額方向
			switch r.AcctType {
			case "revenue":
				net = r.Credit.Sub(r.Debit)
			case "cost", "expense":
				net = r.Debit.Sub(r.Credit)
			default:
				continue
			}
			if net.IsZero() {
				continue
			}
			// 收入科目:貸方餘額用借方結清;成本 / 費用科目:借方餘額用貸方結清;餘額方向相反時反向
			closeWithDebit := (r.AcctType == "revenue") == net.IsPositive()
			e := RawEntry{AccountID: r.ID, Description: fmt.Sprintf("%d 年度結帳", year)}
			if closeWithDebit {
				e.Debit = net.Abs()
				debit = debit.Add(e.Debit)
			} else {
				e.Credit = net.Abs()
				credit = credit.Add(e.Credit)
			}
			entries = append(entries, e)
		}
		if len(entries) == 0 {
			return errNothingToClose
		}
		// 借方合計大於貸方 = 收入大於費用 = 獲利,差額貸記保留盈餘;反之虧損借記
		diff := debit.Sub(credit)
		// 損益剛好為零時借貸已平衡,不需要保留盈餘分錄(分錄不可借貸皆為 0)
		if !diff.IsZero() {
			re := RawEntry{AccountID: retained["year.retained"].id, Description: fmt.Sprintf("%d 年度損益轉入", year)}
			if diff.IsPositive() {
				re.Credit = diff
			} else {
				re.Debit = diff.Neg()
			}
			entries = append(entries, re)
		}
		src := Source{Type: "year_end", ID: int64(year), No: strconv.Itoa(year), Date: time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC),
			Desc: fmt.Sprintf("%d 年度結帳", year)}
		if err := PostAccounts(ctx, q, Options{CompanyID: a.CompanyID, ActorID: &a.UserID}, src, entries); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: "year_end", EntityType: "accounting_year", Summary: fmt.Sprintf("年度結帳 %d,本期損益 %s", year, diff.String()),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, gin.H{"year": year, "status": "closed"})
}

// undoYear POST /gl/year-end/:year/undo 沖銷年度結帳傳票。
func (m *Module) undoYear(c *gin.Context) {
	year, err := yearParam(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		if err := q.LockCompanyForPeriodChange(ctx, a.CompanyID); err != nil {
			return err
		}
		if _, err := q.OpenYearEndVoucher(ctx, db.OpenYearEndVoucherParams{CompanyID: a.CompanyID, Year: int64(year)}); database.IsNoRows(err) {
			return errYearNotClosed
		} else if err != nil {
			return err
		}
		if err := ReverseSource(ctx, q, Options{CompanyID: a.CompanyID, ActorID: &a.UserID}, "year_end", int64(year)); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: "undo_year_end", EntityType: "accounting_year", Summary: fmt.Sprintf("撤銷年度結帳 %d", year),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, gin.H{"year": year, "status": "open"})
}
