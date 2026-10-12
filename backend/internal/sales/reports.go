package sales

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

// 銷售報表(D73):依客戶 / 料品排行,或逐筆明細。只計已過帳的出貨與銷貨退回(退回以負數),
// 金額為本位幣未稅;依負責業務套用資料範圍。毛利只計「已月結成本的月份」(成本 = 基本單位數量 × 該月平均成本)。

const maxReportExportRows = 20000

type salesTotalsDTO struct {
	DeliveryAmount decimal.Decimal  `json:"delivery_amount"`
	ReturnAmount   decimal.Decimal  `json:"return_amount"`
	NetAmount      decimal.Decimal  `json:"net_amount"`
	CostedAmount   decimal.Decimal  `json:"costed_amount"` // 已有成本的銷售額(毛利的基準)
	CostAmount     decimal.Decimal  `json:"cost_amount"`
	Margin         decimal.Decimal  `json:"margin"`
	MarginPct      *decimal.Decimal `json:"margin_pct"`
	UncostedAmount decimal.Decimal  `json:"uncosted_amount"` // 尚未月結成本、不在毛利內的銷售額
}

type salesGroupRow struct {
	ID             int64            `json:"id"`
	Code           string           `json:"code"`
	Name           string           `json:"name"`
	UnitName       string           `json:"unit_name,omitempty"`
	Docs           int64            `json:"docs,omitempty"`
	DeliveryQty    *decimal.Decimal `json:"delivery_qty,omitempty"`
	ReturnQty      *decimal.Decimal `json:"return_qty,omitempty"`
	DeliveryAmount decimal.Decimal  `json:"delivery_amount"`
	ReturnAmount   decimal.Decimal  `json:"return_amount"`
	NetAmount      decimal.Decimal  `json:"net_amount"`
	SharePct       *decimal.Decimal `json:"share_pct"`
	CostedAmount   decimal.Decimal  `json:"costed_amount"`
	CostAmount     decimal.Decimal  `json:"cost_amount"`
	Margin         decimal.Decimal  `json:"margin"`
	MarginPct      *decimal.Decimal `json:"margin_pct"`
}

type salesDetailRow struct {
	DocID        int64            `json:"doc_id"`
	DocNo        string           `json:"doc_no"`
	DocType      string           `json:"doc_type"`
	Date         string           `json:"date"`
	CustomerCode string           `json:"customer_code"`
	CustomerName string           `json:"customer_name"`
	ItemCode     string           `json:"item_code"`
	ItemName     string           `json:"item_name"`
	UnitName     string           `json:"unit_name"`
	Qty          decimal.Decimal  `json:"qty"`
	Amount       decimal.Decimal  `json:"amount"`
	Cost         *decimal.Decimal `json:"cost"`
	Margin       *decimal.Decimal `json:"margin"`
}

type salesReportDTO struct {
	Group  string          `json:"group"`
	From   string          `json:"from"`
	To     string          `json:"to"`
	Rows   []salesGroupRow `json:"rows"`
	Totals salesTotalsDTO  `json:"totals"`
}

// pct 比率(%)四捨五入到 2 位;分母為 0 回傳 nil。
func pct(num, den decimal.Decimal) *decimal.Decimal {
	if den.IsZero() {
		return nil
	}
	p := num.Div(den).Mul(decimal.NewFromInt(100)).Round(2)
	return &p
}

func (t *salesTotalsDTO) finish() {
	t.NetAmount = t.DeliveryAmount.Sub(t.ReturnAmount)
	t.Margin = t.CostedAmount.Sub(t.CostAmount)
	t.MarginPct = pct(t.Margin, t.CostedAmount)
	t.UncostedAmount = t.NetAmount.Sub(t.CostedAmount)
}

// salesReport GET /sales/reports?group=customer|item|detail&from=&to=[&customer_id=&item_id=][&format=xlsx]
func (m *Module) salesReport(c *gin.Context) {
	from, to, err := trade.RequiredRange(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	group := c.Query("group")
	if group != "customer" && group != "item" && group != "detail" {
		response.Error(c, fieldErr("group", "group 須為 customer、item 或 detail"))
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	deptID, userID := a.ScopeFilter()
	xlsx := c.Query("format") == "xlsx"
	period := fmt.Sprintf("%s ~ %s", from.Format(time.DateOnly), to.Format(time.DateOnly))

	if group == "detail" {
		customerID, err := httpx.QueryInt64(c, "customer_id")
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
		cnt, err := m.store.CountSalesReportDetail(ctx, db.CountSalesReportDetailParams{CompanyID: a.CompanyID,
			FromDate: from, ToDate: to, CustomerID: customerID, ItemID: itemID, ScopeUserID: userID, ScopeDeptID: deptID})
		if err != nil {
			response.Error(c, err)
			return
		}
		if xlsx {
			if cnt > maxReportExportRows {
				response.Error(c, fieldErr("to", fmt.Sprintf("明細超過 %d 筆,請縮小期間或加上客戶 / 料品條件", maxReportExportRows)))
				return
			}
			lim, off = int32(maxReportExportRows), 0
		}
		rows, err := m.store.SalesReportDetail(ctx, db.SalesReportDetailParams{CompanyID: a.CompanyID,
			FromDate: from, ToDate: to, CustomerID: customerID, ItemID: itemID, ScopeUserID: userID, ScopeDeptID: deptID, Lim: lim, Off: off})
		if err != nil {
			response.Error(c, err)
			return
		}
		out := make([]salesDetailRow, len(rows))
		for i, r := range rows {
			d := salesDetailRow{DocID: r.DocID, DocNo: r.DocNo, DocType: r.DocType, Date: r.DocDate.Format(time.DateOnly),
				CustomerCode: r.CustomerCode, CustomerName: r.CustomerName, ItemCode: r.ItemCode, ItemName: r.ItemName,
				UnitName: r.UnitName, Qty: r.Qty, Amount: r.Amount}
			if r.HasCost {
				cost := r.Cost
				margin := r.Amount.Sub(cost)
				d.Cost, d.Margin = &cost, &margin
			}
			out[i] = d
		}
		if xlsx {
			data := make([][]any, len(out))
			for i, r := range out {
				typ := "出貨"
				if r.DocType == "return" {
					typ = "銷貨退回"
				}
				var cost, margin any
				if r.Cost != nil {
					cost, margin = xlsxout.Num(*r.Cost), xlsxout.Num(*r.Margin)
				}
				data[i] = []any{r.Date, typ, r.DocNo, r.CustomerCode + " " + r.CustomerName, r.ItemCode + " " + r.ItemName, r.UnitName,
					xlsxout.Num(r.Qty), xlsxout.Num(r.Amount), cost, margin}
			}
			b, err := xlsxout.Build("銷售明細", []string{"銷售明細(已過帳,本位幣未稅)", "期間 " + period},
				[]string{"日期", "類別", "單號", "客戶", "料品", "單位", "數量(基本單位)", "銷售額", "成本", "毛利"}, data)
			if err != nil {
				response.Error(c, err)
				return
			}
			xlsxout.Send(c, "銷售明細_"+period+".xlsx", b)
			return
		}
		response.List(c, out, pg.Meta(cnt))
		return
	}

	var rows []salesGroupRow
	var totals salesTotalsDTO
	add := func(r salesGroupRow) {
		r.NetAmount = r.DeliveryAmount.Sub(r.ReturnAmount)
		r.Margin = r.CostedAmount.Sub(r.CostAmount)
		r.MarginPct = pct(r.Margin, r.CostedAmount)
		rows = append(rows, r)
		totals.DeliveryAmount = totals.DeliveryAmount.Add(r.DeliveryAmount)
		totals.ReturnAmount = totals.ReturnAmount.Add(r.ReturnAmount)
		totals.CostedAmount = totals.CostedAmount.Add(r.CostedAmount)
		totals.CostAmount = totals.CostAmount.Add(r.CostAmount)
	}
	if group == "customer" {
		rs, err := m.store.SalesReportByCustomer(ctx, db.SalesReportByCustomerParams{CompanyID: a.CompanyID,
			FromDate: from, ToDate: to, ScopeUserID: userID, ScopeDeptID: deptID})
		if err != nil {
			response.Error(c, err)
			return
		}
		for _, r := range rs {
			add(salesGroupRow{ID: r.CustomerID, Code: r.CustomerCode, Name: r.CustomerName, Docs: r.DeliveryDocs,
				DeliveryAmount: r.DeliveryAmount, ReturnAmount: r.ReturnAmount, CostedAmount: r.CostedAmount, CostAmount: r.CostAmount})
		}
	} else {
		rs, err := m.store.SalesReportByItem(ctx, db.SalesReportByItemParams{CompanyID: a.CompanyID,
			FromDate: from, ToDate: to, ScopeUserID: userID, ScopeDeptID: deptID})
		if err != nil {
			response.Error(c, err)
			return
		}
		for _, r := range rs {
			dq, rq := r.DeliveryQty, r.ReturnQty
			add(salesGroupRow{ID: r.ItemID, Code: r.ItemCode, Name: r.ItemName, UnitName: r.UnitName, DeliveryQty: &dq, ReturnQty: &rq,
				DeliveryAmount: r.DeliveryAmount, ReturnAmount: r.ReturnAmount, CostedAmount: r.CostedAmount, CostAmount: r.CostAmount})
		}
	}
	totals.finish()
	for i := range rows {
		rows[i].SharePct = pct(rows[i].NetAmount, totals.NetAmount)
	}
	if rows == nil {
		rows = []salesGroupRow{}
	}
	if xlsx {
		label := "客戶"
		if group == "item" {
			label = "料品"
		}
		header := []string{"排名", label + "代號", label + "名稱", "出貨額", "退回額", "淨銷售額", "占比 %", "已月結成本的銷售額", "成本", "毛利", "毛利率 %"}
		if group == "item" {
			header = append(header, "出貨數量", "退回數量", "單位")
		} else {
			header = append(header, "出貨單數")
		}
		data := make([][]any, 0, len(rows)+1)
		numOrNil := func(p *decimal.Decimal) any {
			if p == nil {
				return nil
			}
			return xlsxout.Num(*p)
		}
		for i, r := range rows {
			line := []any{i + 1, r.Code, r.Name, xlsxout.Num(r.DeliveryAmount), xlsxout.Num(r.ReturnAmount), xlsxout.Num(r.NetAmount),
				numOrNil(r.SharePct), xlsxout.Num(r.CostedAmount), xlsxout.Num(r.CostAmount), xlsxout.Num(r.Margin), numOrNil(r.MarginPct)}
			if group == "item" {
				line = append(line, xlsxout.Num(*r.DeliveryQty), xlsxout.Num(*r.ReturnQty), r.UnitName)
			} else {
				line = append(line, r.Docs)
			}
			data = append(data, line)
		}
		data = append(data, []any{"合計", "", "", xlsxout.Num(totals.DeliveryAmount), xlsxout.Num(totals.ReturnAmount), xlsxout.Num(totals.NetAmount),
			nil, xlsxout.Num(totals.CostedAmount), xlsxout.Num(totals.CostAmount), xlsxout.Num(totals.Margin), numOrNil(totals.MarginPct)})
		title := "銷售排行(依" + label + ",已過帳,本位幣未稅)"
		b, err := xlsxout.Build("銷售排行", []string{title, "期間 " + period, "毛利只計已月結成本的月份;尚未月結的銷售額 " + totals.UncostedAmount.String()}, header, data)
		if err != nil {
			response.Error(c, err)
			return
		}
		xlsxout.Send(c, "銷售排行_"+label+"_"+period+".xlsx", b)
		return
	}
	response.OK(c, salesReportDTO{Group: group, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly), Rows: rows, Totals: totals})
}
