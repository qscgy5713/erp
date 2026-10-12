package finance

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
)

var statementKindLabels = map[string]string{
	"goods_receipt": "進貨", "purchase_return": "進貨退出", "delivery": "出貨", "sales_return": "銷貨退回",
	"receipt": "收款", "payment": "付款",
}

func floatOf(d decimal.Decimal) float64 { f, _ := d.Float64(); return f }

// statementXLSX 對帳單 Excel:對象資料、期初、明細(日期、類別、單號、增減、餘額)與期末。
func statementXLSX(d statementDTO, receivable bool) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	head, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"#F2F2F2"}, Pattern: 1}})
	num, _ := f.NewStyle(&excelize.Style{NumFmt: 4}) // #,##0.00
	const sheet = "對帳單"
	_ = f.SetSheetName("Sheet1", sheet)
	put := func(row int, vals ...any) {
		for i, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(i+1, row)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}
	side := "客戶"
	if !receivable {
		side = "供應商"
	}
	put(1, fmt.Sprintf("%s對帳單", side))
	put(2, side, fmt.Sprintf("%s %s", d.PartnerCode, d.PartnerName), "幣別", d.Currency)
	put(3, "期間", fmt.Sprintf("%s ~ %s", d.From, d.To))
	put(5, "日期", "類別", "單號", "增減", "餘額")
	_ = f.SetCellStyle(sheet, "A5", "E5", head)
	put(6, "期初餘額", "", "", "", floatOf(d.Opening))
	row := 7
	for _, r := range d.Rows {
		kind := statementKindLabels[r.Kind]
		if kind == "" {
			kind = r.Kind
		}
		put(row, r.Date, kind, r.DocNo, floatOf(r.Delta), floatOf(r.Balance))
		row++
	}
	put(row, "期末餘額", "", "", "", floatOf(d.Closing))
	_ = f.SetCellStyle(sheet, "D6", fmt.Sprintf("E%d", row), num)
	_ = f.SetColWidth(sheet, "A", "A", 14)
	_ = f.SetColWidth(sheet, "B", "B", 12)
	_ = f.SetColWidth(sheet, "C", "C", 22)
	_ = f.SetColWidth(sheet, "D", "E", 16)
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeStatementXLSX 回傳 Excel 檔案(format=xlsx)。
func writeStatementXLSX(c *gin.Context, d statementDTO, receivable bool) error {
	data, err := statementXLSX(d, receivable)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("對帳單_%s_%s_%s.xlsx", d.PartnerCode, d.From, d.To)
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
	return nil
}
