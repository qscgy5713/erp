package production

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/money"
	"erp/internal/shared/response"
)

// MRP 需求計算(D89、D98):唯讀的建議,不產生任何單據。
//
// 依交期分成週期(第 0 期 = 今天起第一週,含沒填日期與已逾期的;最後一期含更晚的),由上層(成品)往下層(材料)逐階計算,
// 每一期:預計庫存 = 上期預計庫存 + 該期預計入庫(採購單、未完工工單)− 該期毛需求(訂單未出貨、未完工工單領料、上層建議生產帶下來的需求);
// 預計庫存低於安全庫存就在該期建議補貨:數量依最小訂購 / 生產量與批量倍數進位。
// 有 BOM 的料品建議「生產」(並在「最晚下單日」把材料需求帶到下一階),沒有 BOM 的建議「採購」。
// 最晚下單日 = 該期開始日 − 前置時間;已經來不及(早於今天)的標示為 late,材料需求從今天算起。

// mrpParam 料品的計畫參數(沒設定全為 0)。
type mrpParam struct {
	LeadDays int
	MinQty   decimal.Decimal
	Multiple decimal.Decimal
}

// mrpBucket 單一料品在一個週期的計算結果。
type mrpBucket struct {
	Start     string          `json:"start"`     // 該期開始日
	Gross     decimal.Decimal `json:"gross"`     // 毛需求
	Receipts  decimal.Decimal `json:"receipts"`  // 預計入庫(採購單 + 工單)
	Planned   decimal.Decimal `json:"planned"`   // 建議補貨量(計畫入庫)
	Projected decimal.Decimal `json:"projected"` // 期末預計庫存(含建議補貨)
}

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
	SuggestedQty decimal.Decimal `json:"suggested_qty"` // 建議數量(各期建議補貨合計,已依批量進位)
	// 計畫參數與時程
	LeadTimeDays  int             `json:"lead_time_days"`
	MinOrderQty   decimal.Decimal `json:"min_order_qty"`
	OrderMultiple decimal.Decimal `json:"order_multiple"`
	FirstNeed     *string         `json:"first_need"` // 最早需要補貨的週期開始日
	ReleaseBy     *string         `json:"release_by"` // 最晚下單日(最早的一筆)
	Late          bool            `json:"late"`       // 前置時間來不及:即使今天下單也趕不上
	Buckets       []mrpBucket     `json:"buckets"`
	// 缺料時的替代料提示(只供參考,不影響淨需求與建議量)
	Substitutes []mrpSubstitute `json:"substitutes"`
}

// mrpSubstitute 主料的替代料:FreeQty 為替代料自己的需求都扣掉後的剩餘供給,Covers 為它能頂替多少主料。
type mrpSubstitute struct {
	ItemID   int64           `json:"item_id"`
	ItemCode string          `json:"item_code"`
	ItemName string          `json:"item_name"`
	Ratio    decimal.Decimal `json:"ratio"`
	FreeQty  decimal.Decimal `json:"free_qty"`
	Covers   decimal.Decimal `json:"covers"`
}

type subEdge struct {
	mainID, subID int64
	ratio         decimal.Decimal
}

// attachSubstitutes 對「淨需求 > 0」的料品附上替代料提示。替代料可用量 = 現有 + 採購在途 + 工單在途
// − 它自己的毛需求(須在 mrpCompute 之後才是定案值);同一替代料被多個主料共用時各自顯示,不互相扣。
func attachSubstitutes(rows []mrpRow, edges []subEdge) {
	byID := map[int64]*mrpRow{}
	for i := range rows {
		byID[rows[i].ItemID] = &rows[i]
	}
	for _, e := range edges {
		main, sub := byID[e.mainID], byID[e.subID]
		if main == nil || sub == nil || !main.Net.IsPositive() {
			continue
		}
		free := sub.OnHand.Add(sub.IncomingPO).Add(sub.IncomingWO).
			Sub(sub.SalesDemand).Sub(sub.WoDemand).Sub(sub.PlanDemand).Sub(sub.SafetyStock)
		if !free.IsPositive() {
			continue
		}
		main.Substitutes = append(main.Substitutes, mrpSubstitute{ItemID: sub.ItemID, ItemCode: sub.ItemCode, ItemName: sub.ItemName,
			Ratio: e.ratio, FreeQty: free, Covers: free.Div(e.ratio).Round(money.QuantityPlaces)})
	}
}

type bomChild struct {
	itemID int64
	qty    func(parentQty decimal.Decimal) decimal.Decimal
}

// mrpInput 計算輸入;各需求 / 供給為「料品 → 每期數量」(長度 Buckets)。
type mrpInput struct {
	Today   time.Time // 今天(當地日期,UTC 午夜表示)
	Buckets int       // 週期數,每期 7 天
	Sales   map[int64][]decimal.Decimal
	Wo      map[int64][]decimal.Decimal
	Po      map[int64][]decimal.Decimal
	WoIn    map[int64][]decimal.Decimal
	OnHand  map[int64]decimal.Decimal
	Params  map[int64]mrpParam
	Bom     map[int64][]bomChild // 成品 → 材料;不在 bom 內的料品視為採購件
}

// bucketOf 日期落在第幾期:今天以前(逾期)與沒填日期為 0,超過範圍的併入最後一期。
func bucketOf(d, today time.Time, n int) int {
	days := int(d.Sub(today).Hours() / 24)
	if days < 0 {
		return 0
	}
	return min(days/7, n-1)
}

func bucketStart(today time.Time, t int) time.Time {
	if t == 0 {
		return today
	}
	return today.AddDate(0, 0, 7*t)
}

// lotSize 把需求量進位成實際可下單的量:不足最小量補到最小量,再進位到批量的整數倍。
func lotSize(need decimal.Decimal, p mrpParam) decimal.Decimal {
	q := need.Round(money.QuantityPlaces)
	if p.MinQty.IsPositive() && q.LessThan(p.MinQty) {
		q = p.MinQty
	}
	if p.Multiple.IsPositive() {
		q = q.Div(p.Multiple).Ceil().Mul(p.Multiple)
	}
	return q
}

func sumQ(xs []decimal.Decimal) decimal.Decimal {
	t := decimal.Zero
	for _, x := range xs {
		t = t.Add(x)
	}
	return t
}

func bucketsOr(m map[int64][]decimal.Decimal, id int64, n int) []decimal.Decimal {
	if v, ok := m[id]; ok && len(v) == n {
		return v
	}
	return make([]decimal.Decimal, n)
}

// mrpCompute 純計算,方便測試。
func mrpCompute(items []db.MrpItemsRow, in mrpInput) []mrpRow {
	n := max(in.Buckets, 1)
	byID := map[int64]*mrpRow{}
	plan := map[int64][]decimal.Decimal{} // 上層建議生產帶下來的各期需求
	for _, it := range items {
		sales, wo, po, woIn := bucketsOr(in.Sales, it.ID, n), bucketsOr(in.Wo, it.ID, n), bucketsOr(in.Po, it.ID, n), bucketsOr(in.WoIn, it.ID, n)
		p := in.Params[it.ID]
		byID[it.ID] = &mrpRow{ItemID: it.ID, ItemCode: it.Code, ItemName: it.Name, UnitName: it.UnitName,
			SalesDemand: sumQ(sales), WoDemand: sumQ(wo), SafetyStock: it.SafetyStock, OnHand: in.OnHand[it.ID],
			IncomingPO: sumQ(po), IncomingWO: sumQ(woIn),
			LeadTimeDays: p.LeadDays, MinOrderQty: p.MinQty, OrderMultiple: p.Multiple}
		plan[it.ID] = make([]decimal.Decimal, n)
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
		for _, ch := range in.Bom[id] {
			depth(ch.itemID, d+1, path)
		}
		delete(path, id)
	}
	hasParent := map[int64]bool{}
	for _, chs := range in.Bom {
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
		sales, wo, po, woIn := bucketsOr(in.Sales, r.ItemID, n), bucketsOr(in.Wo, r.ItemID, n), bucketsOr(in.Po, r.ItemID, n), bucketsOr(in.WoIn, r.ItemID, n)
		p := in.Params[r.ItemID]
		r.PlanDemand = sumQ(plan[r.ItemID])
		r.Action, r.SuggestedQty = "none", decimal.Zero
		r.Buckets = make([]mrpBucket, n)
		proj, totalNeed := r.OnHand, decimal.Zero
		var relMin *time.Time
		for t := range n {
			gross := sales[t].Add(wo[t]).Add(plan[r.ItemID][t])
			rec := po[t].Add(woIn[t])
			avail := proj.Add(rec).Sub(gross)
			planned := decimal.Zero
			if avail.LessThan(r.SafetyStock) {
				need := r.SafetyStock.Sub(avail)
				planned = lotSize(need, p)
				totalNeed = totalNeed.Add(need)
				avail = avail.Add(planned)
				// 最晚下單日;來不及就從今天算起,並標示 late
				start := bucketStart(in.Today, t)
				rel := start.AddDate(0, 0, -p.LeadDays)
				if rel.Before(in.Today) {
					rel, r.Late = in.Today, true
				}
				if relMin == nil || rel.Before(*relMin) {
					rm := rel
					relMin = &rm
				}
				if r.FirstNeed == nil {
					fs := start.Format(time.DateOnly)
					r.FirstNeed = &fs
				}
				r.SuggestedQty = r.SuggestedQty.Add(planned)
				if chs, ok := in.Bom[r.ItemID]; ok {
					rb := bucketOf(rel, in.Today, n)
					for _, ch := range chs {
						if _, ok := byID[ch.itemID]; ok {
							plan[ch.itemID][rb] = plan[ch.itemID][rb].Add(ch.qty(planned))
						}
					}
				}
			}
			proj = avail
			r.Buckets[t] = mrpBucket{Start: bucketStart(in.Today, t).Format(time.DateOnly), Gross: gross, Receipts: rec, Planned: planned, Projected: avail}
		}
		if relMin != nil {
			rs := relMin.Format(time.DateOnly)
			r.ReleaseBy = &rs
		}
		// 淨需求:有補貨需求時為各期缺口合計;否則為總量淨額(負數 = 有剩餘)
		r.Net = r.SalesDemand.Add(r.WoDemand).Add(r.PlanDemand).Add(r.SafetyStock).Sub(r.OnHand).Sub(r.IncomingPO).Sub(r.IncomingWO)
		if totalNeed.IsPositive() {
			r.Net = totalNeed
			r.Action = "buy"
			if _, ok := in.Bom[r.ItemID]; ok {
				r.Action = "make"
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

// phased 把 (料品, 日期, 數量) 依日期分到各期。
func phased[T any](rows []T, today time.Time, n int, get func(T) (int64, time.Time, decimal.Decimal)) map[int64][]decimal.Decimal {
	m := map[int64][]decimal.Decimal{}
	for _, r := range rows {
		id, due, q := get(r)
		if m[id] == nil {
			m[id] = make([]decimal.Decimal, n)
		}
		b := bucketOf(due, today, n)
		m[id][b] = m[id][b].Add(q)
	}
	return m
}

func qtyMap[T any](rows []T, get func(T) (int64, decimal.Decimal)) map[int64]decimal.Decimal {
	m := map[int64]decimal.Decimal{}
	for _, r := range rows {
		id, q := get(r)
		m[id] = q
	}
	return m
}

// mrp GET /production/mrp?shortage_only=true&weeks=8 需求計算;預設只列需補的料品;shortage_only=false 列出所有有需求、庫存或在途的料品。
func (m *Module) mrp(c *gin.Context) {
	ctx := c.Request.Context()
	companyID := actor(c).CompanyID
	q := m.store.Queries
	weeks := 8
	if w := c.Query("weeks"); w != "" {
		v, err := strconv.Atoi(w)
		if err != nil || v < 1 || v > 26 {
			response.Error(c, apperr.Validation(map[string]string{"weeks": "週數須為 1 到 26"}))
			return
		}
		weeks = v
	}
	now := time.Now().In(time.FixedZone("TST", 8*3600))
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	items, err := q.MrpItems(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	sales, err := q.MrpSalesDemand(ctx, db.MrpSalesDemandParams{CompanyID: companyID, Today: today})
	if err != nil {
		response.Error(c, err)
		return
	}
	woD, err := q.MrpWorkOrderDemand(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	po, err := q.MrpPurchaseSupply(ctx, db.MrpPurchaseSupplyParams{CompanyID: companyID, Today: today})
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
	params, err := q.MrpParams(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	pm := map[int64]mrpParam{}
	for _, p := range params {
		pm[p.ItemID] = mrpParam{LeadDays: int(p.LeadTimeDays), MinQty: p.MinOrderQty, Multiple: p.OrderMultiple}
	}
	bom, bomIDs, err := loadBomGraph(ctx, q, companyID, now)
	if err != nil {
		response.Error(c, err)
		return
	}
	rows := mrpCompute(items, mrpInput{
		Today: today, Buckets: weeks,
		Sales:  phased(sales, today, weeks, func(r db.MrpSalesDemandRow) (int64, time.Time, decimal.Decimal) { return r.ItemID, r.Due, r.Qty }),
		Wo:     phased(woD, today, weeks, func(r db.MrpWorkOrderDemandRow) (int64, time.Time, decimal.Decimal) { return r.ItemID, r.Due, r.Qty }),
		Po:     phased(po, today, weeks, func(r db.MrpPurchaseSupplyRow) (int64, time.Time, decimal.Decimal) { return r.ItemID, r.Due, r.Qty }),
		WoIn:   phased(woS, today, weeks, func(r db.MrpWorkOrderSupplyRow) (int64, time.Time, decimal.Decimal) { return r.ItemID, r.Due, r.Qty }),
		OnHand: qtyMap(onHand, func(r db.MrpOnHandRow) (int64, decimal.Decimal) { return r.ItemID, r.Qty }),
		Params: pm, Bom: bom,
	})
	subRows, err := q.MrpSubstituteEdges(ctx, bomIDs)
	if err != nil {
		response.Error(c, err)
		return
	}
	edges := make([]subEdge, len(subRows))
	for i, r := range subRows {
		edges[i] = subEdge{r.MainID, r.SubID, r.Ratio}
	}
	attachSubstitutes(rows, edges)
	out := []mrpRow{}
	for _, r := range rows {
		busy := r.SalesDemand.IsPositive() || r.WoDemand.IsPositive() || r.PlanDemand.IsPositive() ||
			r.SafetyStock.IsPositive() || r.OnHand.IsPositive() || r.IncomingPO.IsPositive() || r.IncomingWO.IsPositive()
		if r.Substitutes == nil {
			r.Substitutes = []mrpSubstitute{}
		}
		if r.Action != "none" || (c.Query("shortage_only") == "false" && busy) {
			out = append(out, r)
		}
	}
	response.OK(c, out)
}

// loadBomGraph 今天適用的各成品 BOM(已生效、啟用中、生效日最晚的一份)展開成「成品 → 材料」,
// 並回傳這些 BOM 的 id(查替代料用)。
func loadBomGraph(ctx context.Context, q *db.Queries, companyID int64, on time.Time) (map[int64][]bomChild, []int64, error) {
	boms, err := q.MrpActiveBoms(ctx, db.MrpActiveBomsParams{CompanyID: companyID, OnDate: on})
	if err != nil {
		return nil, nil, err
	}
	out := map[int64][]bomChild{}
	ids := make([]int64, 0, len(boms))
	for _, b := range boms {
		ids = append(ids, b.ID)
		lines, err := q.ListBomLines(ctx, b.ID)
		if err != nil {
			if database.IsNoRows(err) {
				continue
			}
			return nil, nil, err
		}
		for _, l := range lines {
			yield, bomQty, scrap := b.YieldQty, l.Qty, l.ScrapPct
			out[b.ItemID] = append(out[b.ItemID], bomChild{itemID: l.ItemID, qty: func(parent decimal.Decimal) decimal.Decimal {
				return ExplodeQty(bomQty, yield, parent, scrap)
			}})
		}
	}
	return out, ids, nil
}
