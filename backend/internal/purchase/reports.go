package purchase

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/httpx"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/shared/xlsxout"
	"erp/internal/trade"
)

// 採購報表(D73):依供應商 / 料品統計進貨,或逐筆明細。只計已過帳的進貨與進貨退出(退出以負數),
// 金額為本位幣未稅。

const maxReportExportRows = 20000

type purchaseGroupRow struct {
	ID            int64            `json:"id"`
	Code          string           `json:"code"`
	Name          string           `json:"name"`
	UnitName      string           `json:"unit_name,omitempty"`
	Docs          int64            `json:"docs,omitempty"`
	ReceiptQty    *decimal.Decimal `json:"receipt_qty,omitempty"`
	ReturnQty     *decimal.Decimal `json:"return_qty,omitempty"`
	ReceiptAmount decimal.Decimal  `json:"receipt_amount"`
	ReturnAmount  decimal.Decimal  `json:"return_amount"`
	NetAmount     decimal.Decimal  `json:"net_amount"`
	SharePct      *decimal.Decimal `json:"share_pct"`
	AvgPrice      *decimal.Decimal `json:"avg_price,omitempty"` // 料品:淨進貨額 ÷ 淨數量(基本單位)
}

type purchaseTotalsDTO struct {
	ReceiptAmount decimal.Decimal `json:"receipt_amount"`
	ReturnAmount  decimal.Decimal `json:"return_amount"`
	NetAmount     decimal.Decimal `json:"net_amount"`
}

type purchaseDetailRow struct {
	DocID        int64           `json:"doc_id"`
	DocNo        string          `json:"doc_no"`
	DocType      string          `json:"doc_type"`
	Date         string          `json:"date"`
	SupplierCode string          `json:"supplier_code"`
	SupplierName string          `json:"supplier_name"`
	ItemCode     string          `json:"item_code"`
	ItemName     string          `json:"item_name"`
	UnitName     string          `json:"unit_name"`
	Qty          decimal.Decimal `json:"qty"`
	Amount       decimal.Decimal `json:"amount"`
}

type purchaseReportDTO struct {
	Group  string             `json:"group"`
	From   string             `json:"from"`
	To     string             `json:"to"`
	Rows   []purchaseGroupRow `json:"rows"`
	Totals purchaseTotalsDTO  `json:"totals"`
}

func pct(num, den decimal.Decimal) *decimal.Decimal {
	if den.IsZero() {
		return nil
	}
	p := num.Div(den).Mul(decimal.NewFromInt(100)).Round(2)
	return &p
}

// purchaseReport GET /purchase/reports?group=supplier|item|detail&from=&to=[&supplier_id=&item_id=][&format=xlsx]
func (m *Module) purchaseReport(c *gin.Context) {
	from, to, err := trade.RequiredRange(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	group := c.Query("group")
	if group != "supplier" && group != "item" && group != "detail" {
		response.Error(c, fieldErr("group", "group 須為 supplier、item 或 detail"))
		return
	}
	ctx := c.Request.Context()
	companyID := actor(c).CompanyID
	xlsx := c.Query("format") == "xlsx"
	period := fmt.Sprintf("%s ~ %s", from.Format(time.DateOnly), to.Format(time.DateOnly))

	if group == "detail" {
		supplierID, err := httpx.QueryInt64(c, "supplier_id")
		if err != nil {
			response.Error(c, err)
			return
		}
		itemID, err := httpx.QueryInt64(c, "item_id")
		if err != nil {
			response.Error(c, err)
			return
		}
		pg := page.Parse(c.Query("page"), c.Query("size"))
		lim, off := pg.Limit(), pg.Offset()
		cnt, err := m.store.CountPurchaseReportDetail(ctx, db.CountPurchaseReportDetailParams{CompanyID: companyID,
			FromDate: from, ToDate: to, SupplierID: supplierID, ItemID: itemID})
		if err != nil {
			response.Error(c, err)
			return
		}
		if xlsx {
			if cnt > maxReportExportRows {
				response.Error(c, fieldErr("to", fmt.Sprintf("明細超過 %d 筆,請縮小期間或加上供應商 / 料品條件", maxReportExportRows)))
				return
			}
			lim, off = int32(maxReportExportRows), 0
		}
		rows, err := m.store.PurchaseReportDetail(ctx, db.PurchaseReportDetailParams{CompanyID: companyID,
			FromDate: from, ToDate: to, SupplierID: supplierID, ItemID: itemID, Lim: lim, Off: off})
		if err != nil {
			response.Error(c, err)
			return
		}
		out := make([]purchaseDetailRow, len(rows))
		for i, r := range rows {
			out[i] = purchaseDetailRow{DocID: r.DocID, DocNo: r.DocNo, DocType: r.DocType, Date: r.DocDate.Format(time.DateOnly),
				SupplierCode: r.SupplierCode, SupplierName: r.SupplierName, ItemCode: r.ItemCode, ItemName: r.ItemName,
				UnitName: r.UnitName, Qty: r.Qty, Amount: r.Amount}
		}
		if xlsx {
			data := make([][]any, len(out))
			for i, r := range out {
				typ := "進貨"
				if r.DocType == "return" {
					typ = "進貨退出"
				}
				data[i] = []any{r.Date, typ, r.DocNo, r.SupplierCode + " " + r.SupplierName, r.ItemCode + " " + r.ItemName, r.UnitName,
					xlsxout.Num(r.Qty), xlsxout.Num(r.Amount)}
			}
			b, err := xlsxout.Build("採購明細", []string{"採購明細(已過帳,本位幣未稅)", "期間 " + period},
				[]string{"日期", "類別", "單號", "供應商", "料品", "單位", "數量(基本單位)", "進貨額"}, data)
			if err != nil {
				response.Error(c, err)
				return
			}
			xlsxout.Send(c, "採購明細_"+period+".xlsx", b)
			return
		}
		response.List(c, out, pg.Meta(cnt))
		return
	}

	rows := []purchaseGroupRow{}
	var totals purchaseTotalsDTO
	add := func(r purchaseGroupRow) {
		r.NetAmount = r.ReceiptAmount.Sub(r.ReturnAmount)
		if r.ReceiptQty != nil && r.ReturnQty != nil {
			if netQty := r.ReceiptQty.Sub(*r.ReturnQty); !netQty.IsZero() {
				p := r.NetAmount.Div(netQty).Round(4)
				r.AvgPrice = &p
			}
		}
		rows = append(rows, r)
		totals.ReceiptAmount = totals.ReceiptAmount.Add(r.ReceiptAmount)
		totals.ReturnAmount = totals.ReturnAmount.Add(r.ReturnAmount)
	}
	if group == "supplier" {
		rs, err := m.store.PurchaseReportBySupplier(ctx, db.PurchaseReportBySupplierParams{CompanyID: companyID, FromDate: from, ToDate: to})
		if err != nil {
			response.Error(c, err)
			return
		}
		for _, r := range rs {
			add(purchaseGroupRow{ID: r.SupplierID, Code: r.SupplierCode, Name: r.SupplierName, Docs: r.ReceiptDocs,
				ReceiptAmount: r.ReceiptAmount, ReturnAmount: r.ReturnAmount})
		}
	} else {
		rs, err := m.store.PurchaseReportByItem(ctx, db.PurchaseReportByItemParams{CompanyID: companyID, FromDate: from, ToDate: to})
		if err != nil {
			response.Error(c, err)
			return
		}
		for _, r := range rs {
			rq, tq := r.ReceiptQty, r.ReturnQty
			add(purchaseGroupRow{ID: r.ItemID, Code: r.ItemCode, Name: r.ItemName, UnitName: r.UnitName, ReceiptQty: &rq, ReturnQty: &tq,
				ReceiptAmount: r.ReceiptAmount, ReturnAmount: r.ReturnAmount})
		}
	}
	totals.NetAmount = totals.ReceiptAmount.Sub(totals.ReturnAmount)
	for i := range rows {
		rows[i].SharePct = pct(rows[i].NetAmount, totals.NetAmount)
	}
	if xlsx {
		label := "供應商"
		if group == "item" {
			label = "料品"
		}
		header := []string{"排名", label + "代號", label + "名稱", "進貨額", "退出額", "淨進貨額", "占比 %"}
		if group == "item" {
			header = append(header, "進貨數量", "退出數量", "單位", "平均單價")
		} else {
			header = append(header, "進貨單數")
		}
		numOrNil := func(p *decimal.Decimal) any {
			if p == nil {
				return nil
			}
			return xlsxout.Num(*p)
		}
		data := make([][]any, 0, len(rows)+1)
		for i, r := range rows {
			line := []any{i + 1, r.Code, r.Name, xlsxout.Num(r.ReceiptAmount), xlsxout.Num(r.ReturnAmount), xlsxout.Num(r.NetAmount), numOrNil(r.SharePct)}
			if group == "item" {
				line = append(line, xlsxout.Num(*r.ReceiptQty), xlsxout.Num(*r.ReturnQty), r.UnitName, numOrNil(r.AvgPrice))
			} else {
				line = append(line, r.Docs)
			}
			data = append(data, line)
		}
		data = append(data, []any{"合計", "", "", xlsxout.Num(totals.ReceiptAmount), xlsxout.Num(totals.ReturnAmount), xlsxout.Num(totals.NetAmount)})
		b, err := xlsxout.Build("採購統計", []string{"採購統計(依" + label + ",已過帳,本位幣未稅)", "期間 " + period}, header, data)
		if err != nil {
			response.Error(c, err)
			return
		}
		xlsxout.Send(c, "採購統計_"+label+"_"+period+".xlsx", b)
		return
	}
	response.OK(c, purchaseReportDTO{Group: group, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly), Rows: rows, Totals: totals})
}
