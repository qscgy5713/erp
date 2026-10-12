package production

import (
	"context"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/money"
	"erp/internal/shared/response"
)

// MRP 需求計算(D89):唯讀的建議,不產生任何單據。
//
//	淨需求 = 銷售未出貨 + 未完工工單的領料 + 上層建議生產帶下來的需求 + 安全庫存
//	         − 現有庫存 − 採購在途 − 工單在途
//
// 淨需求 > 0 時:有 BOM 的料品建議「生產」(並依 BOM 展開到下一階的需求),沒有 BOM 的建議「採購」。
// 由上層(成品)往下層(材料)逐階計算。

type mrpRow struct {
	ItemID       int64           `json:"item_id"`
	ItemCode     string          `json:"item_code"`
	ItemName     string          `json:"item_name"`
	UnitName     string          `json:"unit_name"`
	Level        int             `json:"level"`         // 0 = 最上層成品
	SalesDemand  decimal.Decimal `json:"sales_demand"`  // 訂單未出貨
	WoDemand     decimal.Decimal `json:"wo_demand"`     // 未完工工單領料
	PlanDemand   decimal.Decimal `json:"plan_demand"`   // 上層建議生產帶下來
	SafetyStock  decimal.Decimal `json:"safety_stock"`  //
	OnHand       decimal.Decimal `json:"on_hand"`       //
	IncomingPO   decimal.Decimal `json:"incoming_po"`   // 採購在途
	IncomingWO   decimal.Decimal `json:"incoming_wo"`   // 工單在途
	Net          decimal.Decimal `json:"net"`           // 淨需求(≤ 0 代表夠用)
	Action       string          `json:"action"`        // make 生產 / buy 採購 / none 夠用
	SuggestedQty decimal.Decimal `json:"suggested_qty"` // 建議數量(= max(淨需求, 0))
}

type bomChild struct {
	itemID int64
	qty    func(parentQty decimal.Decimal) decimal.Decimal
}

// mrpCompute 純計算,方便測試:bom 為「成品 → 材料」;不在 bom 內的料品視為採購件。
func mrpCompute(items []db.MrpItemsRow, sales, wo, po, woIn, onHand map[int64]decimal.Decimal, bom map[int64][]bomChild) []mrpRow {
	byID := map[int64]*mrpRow{}
	for _, it := range items {
		byID[it.ID] = &mrpRow{ItemID: it.ID, ItemCode: it.Code, ItemName: it.Name, UnitName: it.UnitName,
			SalesDemand: sales[it.ID], WoDemand: wo[it.ID], SafetyStock: it.SafetyStock, OnHand: onHand[it.ID],
			IncomingPO: po[it.ID], IncomingWO: woIn[it.ID]}
	}
	// 低階碼:料品在所有 BOM 路徑中的最深層數,才能確定「所有上層都算完」才輪到它
	level := map[int64]int{}
	var depth func(id int64, d int, path map[int64]bool)
	depth = func(id int64, d int, path map[int64]bool) {
		if path[id] {
			return // BOM 不可循環(建立時已擋),這裡只是防呆
		}
		if d > level[id] {
			level[id] = d
		} else if _, seen := level[id]; seen {
			return
		}
		path[id] = true
		for _, ch := range bom[id] {
			depth(ch.itemID, d+1, path)
		}
		delete(path, id)
	}
	hasParent := map[int64]bool{}
	for _, chs := range bom {
		for _, ch := range chs {
			hasParent[ch.itemID] = true
		}
	}
	for id := range byID {
		if !hasParent[id] {
			depth(id, 0, map[int64]bool{})
		}
	}
	order := make([]*mrpRow, 0, len(byID))
	for id, r := range byID {
		r.Level = level[id]
		order = append(order, r)
	}
	slices.SortFunc(order, func(a, b *mrpRow) int {
		if a.Level != b.Level {
			return a.Level - b.Level
		}
		if a.ItemCode < b.ItemCode {
			return -1
		}
		if a.ItemCode > b.ItemCode {
			return 1
		}
		return 0
	})
	for _, r := range order {
		gross := r.SalesDemand.Add(r.WoDemand).Add(r.PlanDemand).Add(r.SafetyStock)
		r.Net = gross.Sub(r.OnHand).Sub(r.IncomingPO).Sub(r.IncomingWO)
		r.Action, r.SuggestedQty = "none", decimal.Zero
		if r.Net.IsPositive() {
			r.SuggestedQty = r.Net.Round(money.QuantityPlaces)
			r.Action = "buy"
			if chs, ok := bom[r.ItemID]; ok {
				r.Action = "make"
				for _, ch := range chs {
					if c, ok := byID[ch.itemID]; ok {
						c.PlanDemand = c.PlanDemand.Add(ch.qty(r.SuggestedQty))
					}
				}
			}
		}
	}
	return order2rows(order)
}

func order2rows(order []*mrpRow) []mrpRow {
	out := make([]mrpRow, len(order))
	for i, r := range order {
		out[i] = *r
	}
	return out
}

func qtyMap[T any](rows []T, get func(T) (int64, decimal.Decimal)) map[int64]decimal.Decimal {
	m := map[int64]decimal.Decimal{}
	for _, r := range rows {
		id, q := get(r)
		m[id] = q
	}
	return m
}

// mrp GET /production/mrp?shortage_only=true 需求計算;預設只列需補的料品;shortage_only=false 列出所有有需求、庫存或在途的料品。
func (m *Module) mrp(c *gin.Context) {
	ctx := c.Request.Context()
	companyID := actor(c).CompanyID
	q := m.store.Queries
	items, err := q.MrpItems(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	sales, err := q.MrpSalesDemand(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	woD, err := q.MrpWorkOrderDemand(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	po, err := q.MrpPurchaseSupply(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	woS, err := q.MrpWorkOrderSupply(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	onHand, err := q.MrpOnHand(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	bom, err := loadBomGraph(ctx, q, companyID, time.Now().In(time.FixedZone("TST", 8*3600)))
	if err != nil {
		response.Error(c, err)
		return
	}
	rows := mrpCompute(items,
		qtyMap(sales, func(r db.MrpSalesDemandRow) (int64, decimal.Decimal) { return r.ItemID, r.Qty }),
		qtyMap(woD, func(r db.MrpWorkOrderDemandRow) (int64, decimal.Decimal) { return r.ItemID, r.Qty }),
		qtyMap(po, func(r db.MrpPurchaseSupplyRow) (int64, decimal.Decimal) { return r.ItemID, r.Qty }),
		qtyMap(woS, func(r db.MrpWorkOrderSupplyRow) (int64, decimal.Decimal) { return r.ItemID, r.Qty }),
		qtyMap(onHand, func(r db.MrpOnHandRow) (int64, decimal.Decimal) { return r.ItemID, r.Qty }),
		bom)
	out := []mrpRow{}
	for _, r := range rows {
		busy := r.SalesDemand.IsPositive() || r.WoDemand.IsPositive() || r.PlanDemand.IsPositive() ||
			r.SafetyStock.IsPositive() || r.OnHand.IsPositive() || r.IncomingPO.IsPositive() || r.IncomingWO.IsPositive()
		if r.Action != "none" || (c.Query("shortage_only") == "false" && busy) {
			out = append(out, r)
		}
	}
	response.OK(c, out)
}

// loadBomGraph 今天適用的各成品 BOM(已生效、啟用中、生效日最晚的一份)展開成「成品 → 材料」。
func loadBomGraph(ctx context.Context, q *db.Queries, companyID int64, on time.Time) (map[int64][]bomChild, error) {
	boms, err := q.MrpActiveBoms(ctx, db.MrpActiveBomsParams{CompanyID: companyID, OnDate: on})
	if err != nil {
		return nil, err
	}
	out := map[int64][]bomChild{}
	for _, b := range boms {
		lines, err := q.ListBomLines(ctx, b.ID)
		if err != nil {
			if database.IsNoRows(err) {
				continue
			}
			return nil, err
		}
		for _, l := range lines {
			yield, bomQty, scrap := b.YieldQty, l.Qty, l.ScrapPct
			out[b.ItemID] = append(out[b.ItemID], bomChild{itemID: l.ItemID, qty: func(parent decimal.Decimal) decimal.Decimal {
				return ExplodeQty(bomQty, yield, parent, scrap)
			}})
		}
	}
	return out, nil
}
