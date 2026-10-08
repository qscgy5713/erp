package gl

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"

	"erp/internal/db"
	"erp/internal/shared/response"
)

type vatDetail struct {
	DocNo     string `json:"doc_no"`
	DocType   string `json:"doc_type"`
	Date      string `json:"date"`
	InvoiceNo string `json:"invoice_no"`
	TaxKind   string `json:"tax_kind"`
	Partner   string `json:"partner"`
	PartnerID string `json:"partner_tax_id"`
	Untaxed   string `json:"untaxed"`
	Tax       string `json:"tax"`
}

type vatDTO struct {
	Year      int         `json:"year"`
	Period    int         `json:"period"` // 1–6,每期兩個月(1 = 1–2 月)
	From      string      `json:"from"`
	To        string      `json:"to"`
	Summary   VatSummary  `json:"summary"`
	Issues    []VatIssue  `json:"issues"`
	SalesRows []vatDetail `json:"sales"`
	BuyRows   []vatDetail `json:"purchases"`
}

// vat401 GET /gl/reports/vat401?year=2026&period=5&format=xlsx
// 營業稅申報書(401)資料:依申報期別(雙月)彙總銷項與進項,並列出明細與待處理單據。
func (m *Module) vat401(c *gin.Context) {
	year, err1 := strconv.Atoi(c.Query("year"))
	period, err2 := strconv.Atoi(c.Query("period"))
	if err1 != nil || year < 2000 || year > 2100 {
		response.Error(c, fieldErr("year", "請指定年度"))
		return
	}
	if err2 != nil || period < 1 || period > 6 {
		response.Error(c, fieldErr("period", "申報期別為 1–6(每期兩個月)"))
		return
	}
	from := time.Date(year, time.Month(period*2-1), 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, time.Month(period*2+1), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	ctx := c.Request.Context()
	companyID := actor(c).CompanyID

	sRows, err := m.store.VatSalesDocs(ctx, db.VatSalesDocsParams{CompanyID: companyID, FromDate: from, ToDate: to})
	if err != nil {
		response.Error(c, err)
		return
	}
	pRows, err := m.store.VatPurchaseDocs(ctx, db.VatPurchaseDocsParams{CompanyID: companyID, FromDate: from, ToDate: to})
	if err != nil {
		response.Error(c, err)
		return
	}
	sales := make([]VatSale, len(sRows))
	buys := make([]VatPurchase, len(pRows))
	dto := vatDTO{Year: year, Period: period, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly),
		SalesRows: []vatDetail{}, BuyRows: []vatDetail{}}
	for i, r := range sRows {
		sales[i] = VatSale{DocNo: r.DocNo, DocType: r.DocType, InvoiceNo: r.InvoiceNo, TaxKind: r.TaxKind,
			PartnerCode: r.PartnerCode, PartnerName: r.PartnerName, PartnerTaxID: r.PartnerTaxID,
			Date: r.EffDate.Format(time.DateOnly), Untaxed: r.BaseUntaxed, Tax: r.BaseTax}
		u, t := signed(r.DocType, r.BaseUntaxed), signed(r.DocType, r.BaseTax)
		dto.SalesRows = append(dto.SalesRows, vatDetail{DocNo: r.DocNo, DocType: r.DocType, Date: r.EffDate.Format(time.DateOnly),
			InvoiceNo: r.InvoiceNo, TaxKind: r.TaxKind, Partner: r.PartnerName, PartnerID: r.PartnerTaxID, Untaxed: u.String(), Tax: t.String()})
	}
	for i, r := range pRows {
		buys[i] = VatPurchase{DocNo: r.DocNo, DocType: r.DocType, InvoiceNo: r.InvoiceNo, TaxKind: r.TaxKind,
			PartnerCode: r.PartnerCode, PartnerName: r.PartnerName, PartnerTaxID: r.PartnerTaxID,
			Date: r.DocDate.Format(time.DateOnly), Untaxed: r.BaseUntaxed, Tax: r.BaseTax, Goods: r.GoodsAmount, Expense: r.ExpenseAmount}
		u, t := signed(r.DocType, r.BaseUntaxed), signed(r.DocType, r.BaseTax)
		dto.BuyRows = append(dto.BuyRows, vatDetail{DocNo: r.DocNo, DocType: r.DocType, Date: r.DocDate.Format(time.DateOnly),
			InvoiceNo: r.InvoiceNo, TaxKind: r.TaxKind, Partner: r.PartnerName, PartnerID: r.PartnerTaxID, Untaxed: u.String(), Tax: t.String()})
	}
	dto.Summary, dto.Issues = BuildVat(sales, buys)

	if c.Query("format") != "xlsx" {
		response.OK(c, dto)
		return
	}
	data, err := vatXLSX(dto)
	if err != nil {
		response.Error(c, err)
		return
	}
	name := fmt.Sprintf("營業稅401_%d年第%d期.xlsx", year, period)
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+urlEscape(name))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

func vatXLSX(d vatDTO) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"#F2F2F2"}, Pattern: 1}})
	num, _ := f.NewStyle(&excelize.Style{NumFmt: 3})
	put := func(sheet string, row int, vals ...any) {
		for i, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(i+1, row)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}
	money := func(v interface{ Float64() (float64, bool) }) float64 { x, _ := v.Float64(); return x }

	const sum = "彙總"
	_ = f.SetSheetName("Sheet1", sum)
	put(sum, 1, fmt.Sprintf("營業稅申報資料(401) %d 年第 %d 期(%s ~ %s,新台幣)", d.Year, d.Period, d.From, d.To))
	s := d.Summary
	rows := []struct {
		label string
		v     any
		head  bool
	}{
		{"銷項(已登錄發票的已過帳出貨與銷貨退回,退回已沖減)", nil, true},
		{"應稅銷售額 — 三聯式(買方有統一編號)", money(s.TaxableTriplicate), false},
		{"應稅銷售額 — 二聯式(買方無統一編號)", money(s.TaxableDuplicate), false},
		{"應稅銷售額合計", money(s.TaxableSales), false},
		{"零稅率銷售額", money(s.ZeroSales), false},
		{"免稅銷售額", money(s.ExemptSales), false},
		{"銷售額總計", money(s.TotalSales), false},
		{"銷項稅額", money(s.OutputTax), false},
		{"進項(有供應商發票號碼的已過帳進貨與進貨退出)", nil, true},
		{"可扣抵進貨金額(應稅)", money(s.DeductibleGoods), false},
		{"可扣抵費用及其他金額(應稅)", money(s.DeductibleExpense), false},
		{"可扣抵進項稅額", money(s.InputTax), false},
		{"零稅率 / 免稅進貨金額", money(s.NonTaxPurchase), false},
		{"稅額計算", nil, true},
		{"本期應納(+)/ 留抵(−)稅額 = 銷項稅額 − 進項稅額", money(s.NetTax), false},
	}
	for i, r := range rows {
		put(sum, i+3, r.label, r.v)
		if r.head {
			_ = f.SetCellStyle(sum, fmt.Sprintf("A%d", i+3), fmt.Sprintf("B%d", i+3), bold)
		} else {
			_ = f.SetCellStyle(sum, fmt.Sprintf("B%d", i+3), fmt.Sprintf("B%d", i+3), num)
		}
	}
	r := len(rows) + 5
	put(sum, r, "注意:本表為依系統資料彙總的申報參考數字,不含上期留抵稅額、扣抵比例、海關代徵等項目,正式申報請由會計核對後填報。")
	_ = f.SetColWidth(sum, "A", "A", 62)
	_ = f.SetColWidth(sum, "B", "B", 18)

	detail := func(sheet string, list []vatDetail, partner string) {
		_, _ = f.NewSheet(sheet)
		put(sheet, 1, "單號", "類型", "日期", "發票號碼", "稅別", partner, "統一編號", "銷售額(未稅)", "稅額")
		_ = f.SetCellStyle(sheet, "A1", "I1", bold)
		for i, x := range list {
			typ := map[string]string{"delivery": "出貨", "receipt": "進貨", "return": "退回 / 退出"}[x.DocType]
			untaxed, _ := strconv.ParseFloat(x.Untaxed, 64)
			tax, _ := strconv.ParseFloat(x.Tax, 64)
			kind := map[string]string{"taxable": "應稅", "zero": "零稅率", "exempt": "免稅"}[x.TaxKind]
			put(sheet, i+2, x.DocNo, typ, x.Date, x.InvoiceNo, kind, x.Partner, x.PartnerID, untaxed, tax)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("H%d", i+2), fmt.Sprintf("I%d", i+2), num)
		}
		_ = f.SetColWidth(sheet, "A", "A", 20)
		_ = f.SetColWidth(sheet, "F", "F", 30)
		_ = f.SetColWidth(sheet, "H", "I", 16)
	}
	detail("銷項明細", d.SalesRows, "客戶")
	detail("進項明細", d.BuyRows, "供應商")

	const iss = "待處理"
	_, _ = f.NewSheet(iss)
	put(iss, 1, "問題", "單號", "日期", "對象", "金額(未稅)", "稅額")
	_ = f.SetCellStyle(iss, "A1", "F1", bold)
	for i, x := range d.Issues {
		label := map[string]string{"sales_no_invoice": "出貨 / 退回尚未登錄發票(未計入銷項)", "purchase_no_invoice": "進貨 / 退出沒有供應商發票號碼(未計入進項)"}[x.Kind]
		put(iss, i+2, label, x.DocNo, x.Date, x.Partner, money(x.Untaxed), money(x.Tax))
		_ = f.SetCellStyle(iss, fmt.Sprintf("E%d", i+2), fmt.Sprintf("F%d", i+2), num)
	}
	_ = f.SetColWidth(iss, "A", "A", 44)
	_ = f.SetColWidth(iss, "B", "D", 22)
	_ = f.SetColWidth(iss, "E", "F", 16)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
