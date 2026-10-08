package costing

import (
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/gl"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
)

var periodPattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

var tst = time.FixedZone("TST", 8*3600)

func currentMonth() string { return time.Now().In(tst).Format("2006-01") }

// monthRange 回傳月份的第一天與最後一天。
func monthRange(period string) (time.Time, time.Time) {
	start, _ := time.Parse("2006-01", period)
	return start, start.AddDate(0, 1, -1)
}

type closingDTO struct {
	Period         string          `json:"period"`
	Status         string          `json:"status"` // costed 已月結 / pending 未月結
	ItemCount      int32           `json:"item_count"`
	CogsAmount     decimal.Decimal `json:"cogs_amount"`
	AdjustAmount   decimal.Decimal `json:"adjust_amount"`
	InventoryValue decimal.Decimal `json:"inventory_value"`
	ClosedByName   *string         `json:"closed_by_name"`
	ClosedAt       *time.Time      `json:"closed_at"`
}

// listClosings 近 12 個已結束的月份(含已月結的更早月份),附各月結果。本月尚未結束,不列入。
func (m *Module) listClosings(c *gin.Context) {
	rows, err := m.store.ListCostClosings(c.Request.Context(), actor(c).CompanyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	stored := map[string]db.ListCostClosingsRow{}
	for _, r := range rows {
		stored[r.Period] = r
	}
	now := time.Now().In(tst)
	cur := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	seen := map[string]bool{}
	out := []closingDTO{}
	for i := 1; i <= 12; i++ {
		p := cur.AddDate(0, -i, 0).Format("2006-01")
		seen[p] = true
		d := closingDTO{Period: p, Status: "pending"}
		if r, ok := stored[p]; ok {
			d = toClosingDTO(r)
		}
		out = append(out, d)
	}
	for _, r := range rows {
		if !seen[r.Period] {
			out = append(out, toClosingDTO(r))
		}
	}
	response.OK(c, out)
}

func toClosingDTO(r db.ListCostClosingsRow) closingDTO {
	return closingDTO{
		Period: r.Period, Status: "costed", ItemCount: r.ItemCount, CogsAmount: r.CogsAmount,
		AdjustAmount: r.AdjustAmount, InventoryValue: r.InventoryValue, ClosedByName: r.ClosedByName, ClosedAt: &r.ClosedAt,
	}
}

type itemCostDTO struct {
	ItemID        int64           `json:"item_id"`
	ItemCode      string          `json:"item_code"`
	ItemName      string          `json:"item_name"`
	UnitName      string          `json:"unit_name"`
	OpeningQty    decimal.Decimal `json:"opening_qty"`
	OpeningValue  decimal.Decimal `json:"opening_value"`
	PurchaseQty   decimal.Decimal `json:"purchase_qty"`
	PurchaseValue decimal.Decimal `json:"purchase_value"`
	SalesQty      decimal.Decimal `json:"sales_qty"`
	AdjustQty     decimal.Decimal `json:"adjust_qty"`
	AvgCost       decimal.Decimal `json:"avg_cost"`
	CogsAmount    decimal.Decimal `json:"cogs_amount"`
	AdjustAmount  decimal.Decimal `json:"adjust_amount"`
	ClosingQty    decimal.Decimal `json:"closing_qty"`
	ClosingValue  decimal.Decimal `json:"closing_value"`
}

// listItems GET /costing/closings/:period/items?keyword=&page=&size= 各料品的成本計算明細。
func (m *Module) listItems(c *gin.Context) {
	ctx := c.Request.Context()
	period := c.Param("period")
	if !periodPattern.MatchString(period) {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	cl, err := m.store.GetCostClosing(ctx, db.GetCostClosingParams{CompanyID: actor(c).CompanyID, Period: period})
	if database.IsNoRows(err) {
		response.Error(c, apperr.ErrNotFound)
		return
	} else if err != nil {
		response.Error(c, err)
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	keyword := httpx.QueryString(c, "keyword")
	rows, err := m.store.ListItemCostsForClosing(ctx, db.ListItemCostsForClosingParams{
		ClosingID: cl.ID, Keyword: keyword, Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountItemCostsForClosing(ctx, db.CountItemCostsForClosingParams{ClosingID: cl.ID, Keyword: keyword})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]itemCostDTO, len(rows))
	for i, r := range rows {
		out[i] = itemCostDTO{
			ItemID: r.ItemID, ItemCode: r.ItemCode, ItemName: r.ItemName, UnitName: r.UnitName,
			OpeningQty: r.OpeningQty, OpeningValue: r.OpeningValue, PurchaseQty: r.PurchaseQty,
			PurchaseValue: r.PurchaseValue, SalesQty: r.SalesQty, AdjustQty: r.AdjustQty, AvgCost: r.AvgCost,
			CogsAmount: r.CogsAmount, AdjustAmount: r.AdjustAmount, ClosingQty: r.ClosingQty, ClosingValue: r.ClosingValue,
		}
	}
	response.List(c, out, pg.Meta(total))
}

// run POST /costing/closings/:period/run 月結:計算平均成本、回寫流水帳、拋銷貨成本與存貨損益傳票。
// 只能對已結束的月份、且須由最早有庫存異動的月份依序進行;以排他鎖與各庫存異動的共享鎖序列化。
func (m *Module) run(c *gin.Context) {
	period := c.Param("period")
	if !periodPattern.MatchString(period) {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto closingDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		if period >= currentMonth() {
			return apperr.New(422, "CST-002", "只能對已結束的月份月結成本")
		}
		if err := q.LockCompanyForPeriodChange(ctx, a.CompanyID); err != nil {
			return err
		}
		if _, err := q.GetCostClosing(ctx, db.GetCostClosingParams{CompanyID: a.CompanyID, Period: period}); err == nil {
			return errAlreadyClosed
		} else if !database.IsNoRows(err) {
			return err
		}
		start, end := monthRange(period)
		if earlier, err := q.UncostedEarlierMonth(ctx, db.UncostedEarlierMonthParams{CompanyID: a.CompanyID, StartDate: start}); err == nil {
			return apperr.Conflict("CST-003", fmt.Sprintf("請先月結 %s(月結須依序進行)", earlier))
		} else if !database.IsNoRows(err) {
			return err
		}

		// 期初:最近一次月結的期末
		open := map[int64]opening{}
		if prev, err := q.LatestCostClosingBefore(ctx, db.LatestCostClosingBeforeParams{CompanyID: a.CompanyID, Period: period}); err == nil {
			items, err := q.ListItemCostsRaw(ctx, prev.ID)
			if err != nil {
				return err
			}
			for _, it := range items {
				if !it.ClosingQty.IsZero() || !it.ClosingValue.IsZero() {
					open[it.ItemID] = opening{qty: it.ClosingQty, value: it.ClosingValue, avg: it.AvgCost}
				}
			}
		} else if !database.IsNoRows(err) {
			return err
		}
		aggs, err := q.CostPeriodAggregates(ctx, db.CostPeriodAggregatesParams{CompanyID: a.CompanyID, StartDate: start, EndDate: end})
		if err != nil {
			return err
		}
		act := map[int64]activity{}
		for _, g := range aggs {
			act[g.ItemID] = activity{
				purchaseQty: g.PurchaseQty, purchaseValue: g.PurchaseValue, salesQty: g.SalesQty,
				adjustQty: g.AdjustQty.Add(g.OtherQty),
			}
		}
		ids := map[int64]bool{}
		for id := range open {
			ids[id] = true
		}
		for id := range act {
			ids[id] = true
		}
		sorted := make([]int64, 0, len(ids))
		for id := range ids {
			sorted = append(sorted, id)
		}
		slices.Sort(sorted)

		type row struct {
			id int64
			o  opening
			a  activity
			r  result
		}
		rows := make([]row, 0, len(sorted))
		var cogs, adjust, inv decimal.Decimal
		for _, id := range sorted {
			r := row{id: id, o: open[id], a: act[id]}
			r.r = compute(r.o, r.a)
			rows = append(rows, r)
			cogs, adjust, inv = cogs.Add(r.r.cogs), adjust.Add(r.r.adjust), inv.Add(r.r.closingV)
		}

		cl, err := q.InsertCostClosing(ctx, db.InsertCostClosingParams{
			CompanyID: a.CompanyID, Period: period, ItemCount: int32(len(rows)), CogsAmount: cogs,
			AdjustAmount: adjust, InventoryValue: inv, ClosedBy: &a.UserID,
		})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if err := q.InsertItemCost(ctx, db.InsertItemCostParams{
				ClosingID: cl.ID, ItemID: r.id, OpeningQty: r.o.qty, OpeningValue: r.o.value,
				PurchaseQty: r.a.purchaseQty, PurchaseValue: r.a.purchaseValue, SalesQty: r.a.salesQty,
				AdjustQty: r.a.adjustQty, AvgCost: r.r.avg, CogsAmount: r.r.cogs, AdjustAmount: r.r.adjust,
				ClosingQty: r.r.closingQty, ClosingValue: r.r.closingV,
			}); err != nil {
				return err
			}
			if !r.a.idle() {
				avg := r.r.avg
				if err := q.WritebackUnitCost(ctx, db.WritebackUnitCostParams{
					CompanyID: a.CompanyID, ItemID: r.id, StartDate: start, EndDate: end, AvgCost: &avg,
				}); err != nil {
					return err
				}
			}
		}

		// 傳票:銷貨成本(借 銷貨成本 貸 存貨)、存貨盤損益(損失借 盤損 貸 存貨,盈餘相反)。日期為月底
		opt := gl.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
		src := gl.Source{Type: "cost_closing", ID: cl.ID, No: period, Date: end}
		src.Desc = "月結銷貨成本 " + period
		if err := gl.PostSource(ctx, q, opt, src, pair(cogs, "cost.cogs", "cost.inventory")); err != nil {
			return err
		}
		src.Desc = "月結存貨盤損益 " + period
		if err := gl.PostSource(ctx, q, opt, src, pair(adjust, "cost.adjustment", "cost.inventory")); err != nil {
			return err
		}

		dto = closingDTO{
			Period: period, Status: "costed", ItemCount: cl.ItemCount, CogsAmount: cl.CogsAmount,
			AdjustAmount: cl.AdjustAmount, InventoryValue: cl.InventoryValue, ClosedAt: &cl.ClosedAt,
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: "close", EntityType: "cost_closing", EntityID: &cl.ID, Summary: "月結成本 " + period, After: dto,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// pair 金額為正時 借 debitKey 貸 creditKey;為負時方向相反;為 0 時沒有分錄。
func pair(amount decimal.Decimal, debitKey, creditKey string) []gl.Entry {
	if amount.IsZero() {
		return nil
	}
	if amount.IsNegative() {
		debitKey, creditKey, amount = creditKey, debitKey, amount.Neg()
	}
	return []gl.Entry{{Key: debitKey, Debit: amount}, {Key: creditKey, Credit: amount}}
}

// cancel POST /costing/closings/:period/cancel 取消月結(只能取消最新的一個月):
// 沖銷該月的成本傳票、刪除計算結果並解除庫存鎖定,之後可重新月結。
func (m *Module) cancel(c *gin.Context) {
	period := c.Param("period")
	if !periodPattern.MatchString(period) {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		if err := q.LockCompanyForPeriodChange(ctx, a.CompanyID); err != nil {
			return err
		}
		cl, err := q.GetCostClosing(ctx, db.GetCostClosingParams{CompanyID: a.CompanyID, Period: period})
		if database.IsNoRows(err) {
			return errNotClosed
		} else if err != nil {
			return err
		}
		latest, err := q.LatestCostClosing(ctx, a.CompanyID)
		if err != nil {
			return err
		}
		if latest.Period != period {
			return apperr.Conflict("CST-005", fmt.Sprintf("只能取消最新的月結(%s),請依序取消", latest.Period))
		}
		if err := gl.ReverseSource(ctx, q, gl.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}, "cost_closing", cl.ID); err != nil {
			return err
		}
		if err := q.DeleteCostClosing(ctx, cl.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: "cancel", EntityType: "cost_closing", EntityID: &cl.ID, Summary: "取消月結成本 " + period,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, gin.H{"period": period, "status": "pending"})
}
