// Package xlsxout 把報表輸出成 Excel 下載:標題列、表頭與資料列(數字欄套用千分位格式)。
package xlsxout

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
)

const contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// Num 把 decimal 轉成 Excel 數字儲存格。
func Num(d decimal.Decimal) float64 { f, _ := d.Float64(); return f }

// Build 產生 Excel:titles 為最上方的說明列,header 為表頭,rows 為資料(字串、數字或 nil)。
func Build(sheet string, titles, header []string, rows [][]any) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	_ = f.SetSheetName("Sheet1", sheet)
	head, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"#F2F2F2"}, Pattern: 1}})
	numFmt := "#,##0.00##"
	num, _ := f.NewStyle(&excelize.Style{CustomNumFmt: &numFmt})
	row := 1
	for _, t := range titles {
		_ = f.SetCellValue(sheet, cell(1, row), t)
		row++
	}
	row++ // 空一列
	for i, h := range header {
		_ = f.SetCellValue(sheet, cell(i+1, row), h)
	}
	_ = f.SetCellStyle(sheet, cell(1, row), cell(len(header), row), head)
	row++
	for _, r := range rows {
		for i, v := range r {
			if v == nil {
				continue
			}
			c := cell(i+1, row)
			_ = f.SetCellValue(sheet, c, v)
			if _, ok := v.(float64); ok {
				_ = f.SetCellStyle(sheet, c, c, num)
			}
		}
		row++
	}
	for i := range header {
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, col, col, 16)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func cell(col, row int) string {
	s, _ := excelize.CoordinatesToCellName(col, row)
	return s
}

// Send 回傳 Excel 下載(檔名以 RFC 5987 編碼)。
func Send(c *gin.Context, filename string, data []byte) {
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	c.Data(http.StatusOK, contentType, data)
}
