package gl

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"erp/internal/db"
	"erp/internal/shared/apperr"
	"erp/internal/shared/response"
)

type mediaPreviewDTO struct {
	Year       int             `json:"year"`
	Period     int             `json:"period"`
	FileName   string          `json:"file_name"`
	Count      int             `json:"count"`
	Totals     MediaTotals     `json:"totals"`
	Excluded   []MediaExcluded `json:"excluded"`
	Covered    string          `json:"covered"` // 本版涵蓋範圍說明
	TaxRegNo   string          `json:"tax_reg_no"`
	CompanyTax string          `json:"company_tax_id"`
}

const mediaCovered = "銷項:三聯式(31)、二聯式(32),應稅與免稅;進項:格式 21 / 22 / 25 的應稅進貨及費用(扣抵代號 1)。" +
	"不含退回折讓、零稅率、免稅進貨、固定資產、彙總登錄與作廢發票。"

var errMediaCompany = apperr.New(http.StatusUnprocessableEntity, "GL-040",
	"請先到「系統管理 → 公司資料」設定統一編號(8 碼)與稅籍編號(9 碼),媒體申報檔需要這兩項")

// vat401Media GET /gl/reports/vat401/media?year=&period=[&format=txt]
// 預設回傳預覽(筆數、金額與未納入清單);format=txt 下載申報檔(檔名為統一編號.TXT,每筆 81 字元,CRLF 換行)。
func (m *Module) vat401Media(c *gin.Context) {
	year, period, from, to, err := vatPeriod(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	companyID := actor(c).CompanyID
	co, err := m.store.GetCompany(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	taxID := ""
	if co.TaxID != nil {
		taxID = strings.TrimSpace(*co.TaxID)
	}
	if len(taxID) != 8 || len(co.TaxRegNo) != 9 {
		response.Error(c, errMediaCompany)
		return
	}
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
	docs := make([]MediaDoc, 0, len(sRows)+len(pRows))
	for _, r := range sRows {
		docs = append(docs, MediaDoc{Side: "sales", DocNo: r.DocNo, DocType: r.DocType, Date: r.EffDate, InvoiceNo: r.InvoiceNo,
			TaxKind: r.TaxKind, PartnerTaxID: r.PartnerTaxID, Untaxed: r.BaseUntaxed, Tax: r.BaseTax})
	}
	for _, r := range pRows {
		docs = append(docs, MediaDoc{Side: "purchase", DocNo: r.DocNo, DocType: r.DocType, Date: r.EffDate, InvoiceNo: r.InvoiceNo,
			InvoiceKind: r.InvoiceKind, TaxKind: r.TaxKind, PartnerTaxID: r.PartnerTaxID, Untaxed: r.BaseUntaxed, Tax: r.BaseTax})
	}
	lines, excluded, totals := BuildMedia(co.TaxRegNo, docs)
	name := taxID + ".TXT"
	if c.Query("format") != "txt" {
		response.OK(c, mediaPreviewDTO{Year: year, Period: period, FileName: name, Count: len(lines), Totals: totals,
			Excluded: nonNil(excluded), Covered: mediaCovered, TaxRegNo: co.TaxRegNo, CompanyTax: taxID})
		return
	}
	if len(lines) == 0 {
		response.Error(c, apperr.New(http.StatusUnprocessableEntity, "GL-041", "這個期別沒有可納入申報檔的資料"))
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	c.Data(http.StatusOK, "text/plain; charset=us-ascii", []byte(strings.Join(lines, "\r\n")+"\r\n"))
}

func nonNil(x []MediaExcluded) []MediaExcluded {
	if x == nil {
		return []MediaExcluded{}
	}
	return x
}
