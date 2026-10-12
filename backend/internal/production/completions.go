package production

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/inventory"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/money"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/system/docno"
)

// 工單分批完工(D94):已核准的工單可分多次回報完工,回報即過帳(領料 + 成品入庫同一交易)。

const (
	completionDocType = "work_order_completion"
	srcCompIssue      = "wo_completion_issue"
	srcCompReceipt    = "wo_completion_receipt"
)

var (
	errHasCompletions = apperr.Conflict("PRD-005", "這張工單已有完工回報,請先作廢全部完工回報;分批完工的工單不能用一次完工、反過帳、取消核准或作廢")
	errNotApproved    = apperr.Conflict("PRD-006", "工單須為「已核准」才能完工回報")
	errCompNotPosted  = apperr.Conflict("PRD-007", "這筆完工回報已作廢")
	errNothingDone    = apperr.Conflict("PRD-008", "還沒有任何完工回報,不能結案;要整張作廢請用「作廢」")
)

type compLineDTO struct {
	LineNo   int32           `json:"line_no"`
	ItemID   int64           `json:"item_id"`
	ItemCode string          `json:"item_code"`
	ItemName string          `json:"item_name"`
	UnitName string          `json:"unit_name"`
	Qty      decimal.Decimal `json:"qty"`
	LotNo    string          `json:"lot_no"`
	BinCode  string          `json:"bin_code"`
	Lots     []lotUsedDTO    `json:"lots"` // 實際領用的批號
}

type completionDTO struct {
	ID             int64           `json:"id"`
	DocNo          string          `json:"doc_no"`
	DocDate        string          `json:"doc_date"`
	Qty            decimal.Decimal `json:"qty"`
	ProcessingCost decimal.Decimal `json:"processing_cost"`
	OutputLotNo    string          `json:"output_lot_no"`
	OutputExpiry   *string         `json:"output_expiry_date"`
	OutputBinCode  string          `json:"output_bin_code"`
	Note           string          `json:"note"`
	Status         string          `json:"status"` // posted / voided
	CreatedByName  *string         `json:"created_by_name"`
	CreatedAt      time.Time       `json:"created_at"`
	VoidedByName   *string         `json:"voided_by_name"`
	VoidedAt       *time.Time      `json:"voided_at"`
	Lines          []compLineDTO   `json:"lines"`
	OutputLots     []lotUsedDTO    `json:"output_lots"` // 成品實際入庫的批號
}

// loadCompletions 工單的完工回報(含已作廢,供查看),與累計有效完工量。
func loadCompletions(ctx context.Context, q *db.Queries, workOrderID int64) ([]completionDTO, decimal.Decimal, error) {
	rows, err := q.ListCompletions(ctx, workOrderID)
	if err != nil {
		return nil, decimal.Zero, err
	}
	out := make([]completionDTO, 0, len(rows))
	done := decimal.Zero
	for _, r := range rows {
		d := completionDTO{ID: r.ID, DocNo: r.DocNo, DocDate: r.DocDate.Format(time.DateOnly), Qty: r.Qty, ProcessingCost: r.ProcessingCost,
			OutputLotNo: r.OutputLotNo, OutputExpiry: dateStr(r.OutputExpiry), OutputBinCode: r.OutputBinCode, Note: r.Note, Status: r.Status,
			CreatedByName: r.CreatedByName, CreatedAt: r.CreatedAt, VoidedByName: r.VoidedByName, VoidedAt: r.VoidedAt,
			Lines: []compLineDTO{}, OutputLots: []lotUsedDTO{}}
		lines, err := q.ListCompletionLines(ctx, r.ID)
		if err != nil {
			return nil, decimal.Zero, err
		}
		for _, l := range lines {
			d.Lines = append(d.Lines, compLineDTO{LineNo: l.LineNo, ItemID: l.ItemID, ItemCode: l.ItemCode, ItemName: l.ItemName,
				UnitName: l.UnitName, Qty: l.Qty, LotNo: l.LotNo, BinCode: l.BinCode, Lots: []lotUsedDTO{}})
		}
		if r.Status == "posted" {
			done = done.Add(r.Qty)
			used, err := q.ListCompletionLots(ctx, r.ID)
			if err != nil {
				return nil, decimal.Zero, err
			}
			for _, u := range used {
				lot := lotUsedDTO{LotNo: u.LotNo, ExpiryDate: dateStr(u.ExpiryDate), Qty: u.Qty.Abs()}
				if u.SourceType == srcCompReceipt {
					d.OutputLots = append(d.OutputLots, lot)
					continue
				}
				for i := range d.Lines {
					if d.Lines[i].ItemID == u.ItemID {
						d.Lines[i].Lots = append(d.Lines[i].Lots, lot)
						break
					}
				}
			}
		}
		out = append(out, d)
	}
	return out, done, nil
}

// prorate 本次完工 qty 依工單計畫數量分攤的領料量(捨入 4 位,至少 0.0001)。
func prorate(lineQty, qty, plan decimal.Decimal) decimal.Decimal {
	q := lineQty.Mul(qty).Div(plan).Round(money.QuantityPlaces)
	if !q.IsPositive() {
		return decimal.New(1, -money.QuantityPlaces)
	}
	return q
}

func prorateCost(cost, qty, plan decimal.Decimal) decimal.Decimal {
	return cost.Mul(qty).Div(plan).Round(2)
}

type previewDTO struct {
	Qty            decimal.Decimal `json:"qty"`
	Remaining      decimal.Decimal `json:"remaining"`
	ProcessingCost decimal.Decimal `json:"processing_cost"`
	Lines          []woLineDTO     `json:"lines"`
}

// completionPreview GET /production/work-orders/:id/completion-preview?qty= 預設領料與加工費(開完工回報視窗用)。
func (m *Module) completionPreview(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	doc, err := loadOrder(ctx, m.store.Queries, actor(c).CompanyID, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	done, err := m.store.CompletedQty(ctx, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	remaining := doc.PlanQty.Sub(done)
	qty := remaining
	if s := c.Query("qty"); s != "" {
		if qty, err = decimal.NewFromString(s); err != nil || !qtyOK(qty) {
			response.Error(c, fieldErr("qty", "數量須大於 0,最多 4 位小數"))
			return
		}
	}
	out := previewDTO{Qty: qty, Remaining: remaining, ProcessingCost: prorateCost(doc.ProcessingCost, qty, doc.PlanQty), Lines: make([]woLineDTO, len(doc.Lines))}
	for i, l := range doc.Lines {
		l.Qty = prorate(l.Qty, qty, doc.PlanQty)
		l.Lots = []lotUsedDTO{}
		out.Lines[i] = l
	}
	response.OK(c, out)
}

type completionInput struct {
	DocDate        string           `json:"doc_date" binding:"required"`
	Qty            decimal.Decimal  `json:"qty"`
	ProcessingCost *decimal.Decimal `json:"processing_cost"` // 空白 = 工單加工費 × 本次數量 ÷ 計畫數量
	OutputLotNo    string           `json:"output_lot_no"`
	OutputBinCode  string           `json:"output_bin_code"`
	OutputExpiry   *string          `json:"output_expiry_date"`
	Note           string           `json:"note" binding:"max=255"`
	Lines          []woLineInput    `json:"lines" binding:"dive"` // 空白 = 依工單領料明細比例
}

// createCompletion POST /production/work-orders/:id/completions 完工回報(即過帳)。
func (m *Module) createCompletion(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in completionInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	var out woDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockWorkOrder(ctx, db.LockWorkOrderParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Status != "approved" {
			return errNotApproved
		}
		done, err := q.CompletedQty(ctx, id)
		if err != nil {
			return err
		}
		remaining := cur.PlanQty.Sub(done)
		if !qtyOK(in.Qty) {
			return fieldErr("qty", "本次完工數量須大於 0,最多 4 位小數")
		}
		if in.Qty.GreaterThan(remaining) {
			return fieldErr("qty", "本次完工數量不可超過尚未完工的數量 "+remaining.String())
		}
		cost := prorateCost(cur.ProcessingCost, in.Qty, cur.PlanQty)
		if in.ProcessingCost != nil {
			cost = *in.ProcessingCost
		}
		doc, err := loadOrder(ctx, q, a.CompanyID, id)
		if err != nil {
			return err
		}
		lines := in.Lines
		if len(lines) == 0 {
			for _, l := range doc.Lines {
				lines = append(lines, woLineInput{ItemID: l.ItemID, Qty: prorate(l.Qty, in.Qty, cur.PlanQty), LotNo: l.LotNo, BinCode: l.BinCode})
			}
		}
		// 重用工單的驗證(批號、儲位、材料資格、加工費格式);數量已在上面檢查過
		wi := woInput{DocDate: in.DocDate, ItemID: cur.ItemID, PlanQty: in.Qty, WarehouseID: cur.WarehouseID, MaterialWarehouseID: cur.MaterialWarehouseID,
			ProcessingCost: cost, OutputLotNo: in.OutputLotNo, OutputBinCode: in.OutputBinCode, OutputExpiry: in.OutputExpiry, Lines: lines}
		p, err := prepareOrder(ctx, q, a.CompanyID, &wi, false)
		if err != nil {
			return err
		}
		if p.date.Before(cur.DocDate) {
			return fieldErr("doc_date", "完工日期不可早於工單日期")
		}
		no, err := docno.Next(ctx, q, a.CompanyID, completionDocType, p.date)
		if err != nil {
			return err
		}
		comp, err := q.CreateCompletion(ctx, db.CreateCompletionParams{CompanyID: a.CompanyID, WorkOrderID: id, DocNo: no, DocDate: p.date,
			Qty: in.Qty, ProcessingCost: cost, OutputLotNo: p.lotNo, OutputExpiry: p.expiry, OutputBinCode: p.binNo, Note: in.Note, ActorID: &a.UserID})
		if err != nil {
			return err
		}
		moves := make([]inventory.Movement, 0, len(p.lines))
		for i, l := range p.lines {
			if err := q.AddCompletionLine(ctx, db.AddCompletionLineParams{CompletionID: comp.ID, LineNo: int32(i + 1), ItemID: l.ItemID,
				Qty: l.Qty, LotNo: l.LotNo, BinCode: l.BinCode}); err != nil {
				return err
			}
			moves = append(moves, inventory.Movement{ItemID: l.ItemID, WarehouseID: cur.MaterialWarehouseID, Qty: l.Qty.Neg(), LotNo: l.LotNo, BinCode: l.BinCode})
		}
		opt := inventory.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
		if err := inventory.Post(ctx, q, opt, inventory.Source{Type: srcCompIssue, ID: comp.ID, No: comp.DocNo, DocDate: comp.DocDate}, moves); err != nil {
			return err
		}
		if err := inventory.Post(ctx, q, opt, inventory.Source{Type: srcCompReceipt, ID: comp.ID, No: comp.DocNo, DocDate: comp.DocDate}, []inventory.Movement{{
			ItemID: cur.ItemID, WarehouseID: cur.WarehouseID, Qty: in.Qty, LotNo: p.lotNo, Expiry: p.expiry, BinCode: p.binNo,
		}}); err != nil {
			return err
		}
		if !in.Qty.LessThan(remaining) { // 累計達計畫數量 → 自動結案
			if _, err := q.SetWorkOrderStatus(ctx, db.SetWorkOrderStatusParams{ID: id, CompanyID: a.CompanyID, Status: "posted", Action: "post",
				ActorID: a.UserID, Version: cur.Version}); err != nil {
				return err
			}
		}
		if out, err = loadOrder(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: "complete", EntityType: "work_order", EntityID: &id,
			Summary: "工單 " + cur.DocNo + " 完工回報 " + comp.DocNo + "(" + in.Qty.String() + ")"})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, out)
}

// voidCompletion POST /production/work-orders/:id/completions/:cid/void 作廢完工回報:沖銷入庫與領料;已結案的工單退回已核准。
func (m *Module) voidCompletion(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	cid, err := httpx.ParamID(c, "cid")
	if err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	var out woDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockWorkOrder(ctx, db.LockWorkOrderParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		comp, err := q.LockCompletion(ctx, db.LockCompletionParams{ID: cid, WorkOrderID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if comp.Status != "posted" {
			return errCompNotPosted
		}
		opt := inventory.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
		// 先沖成品入庫(成品已被領用或出貨就會因庫存不足被擋),再沖領料
		if err := inventory.Reverse(ctx, q, opt, inventory.Source{Type: srcCompReceipt, ID: comp.ID, No: comp.DocNo, DocDate: comp.DocDate}); err != nil {
			return err
		}
		if err := inventory.Reverse(ctx, q, opt, inventory.Source{Type: srcCompIssue, ID: comp.ID, No: comp.DocNo, DocDate: comp.DocDate}); err != nil {
			return err
		}
		if err := q.VoidCompletion(ctx, db.VoidCompletionParams{ID: cid, ActorID: &a.UserID}); err != nil {
			return err
		}
		if cur.Status == "posted" {
			if _, err := q.SetWorkOrderStatus(ctx, db.SetWorkOrderStatusParams{ID: id, CompanyID: a.CompanyID, Status: "approved", Action: "unpost",
				ActorID: a.UserID, Version: cur.Version}); err != nil {
				return err
			}
		}
		if out, err = loadOrder(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: "void_completion", EntityType: "work_order", EntityID: &id,
			Summary: "工單 " + cur.DocNo + " 作廢完工回報 " + comp.DocNo})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, out)
}

// closeOrder POST /production/work-orders/:id/close 短交結案:至少有一筆完工回報、未達計畫數量也結案,剩餘計畫不再生產。
func (m *Module) closeOrder(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in actionInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	var out woDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockWorkOrder(ctx, db.LockWorkOrderParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Version != in.Version {
			return apperr.ErrVersionConflict
		}
		if cur.Status != "approved" {
			return errNotApproved
		}
		done, err := q.CompletedQty(ctx, id)
		if err != nil {
			return err
		}
		if !done.IsPositive() {
			return errNothingDone
		}
		if _, err := q.SetWorkOrderStatus(ctx, db.SetWorkOrderStatusParams{ID: id, CompanyID: a.CompanyID, Status: "posted", Action: "post",
			ActorID: a.UserID, Version: cur.Version}); err != nil {
			return err
		}
		if out, err = loadOrder(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: "close", EntityType: "work_order", EntityID: &id,
			Summary: "工單 " + cur.DocNo + " 短交結案(完工 " + done.String() + " / " + cur.PlanQty.String() + ")"})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, out)
}
