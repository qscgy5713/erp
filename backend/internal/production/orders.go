package production

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/inventory"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/docstate"
	"erp/internal/shared/money"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/system/docno"
	"erp/internal/system/permission"
	"erp/internal/trade"
)

const (
	docType       = "work_order"
	srcIssue      = "work_order_issue"
	srcReceipt    = "work_order_receipt"
	maxWoLines    = 100
	maxProcessing = 99999999999
)

var errNotDraft = apperr.New(http.StatusConflict, "PRD-004", "只有草稿可以修改")

type lotUsedDTO struct {
	LotNo      string          `json:"lot_no"`
	ExpiryDate *string         `json:"expiry_date"`
	Qty        decimal.Decimal `json:"qty"`
}

type woLineDTO struct {
	LineNo         int32           `json:"line_no"`
	ItemID         int64           `json:"item_id"`
	ItemCode       string          `json:"item_code"`
	ItemName       string          `json:"item_name"`
	UnitName       string          `json:"unit_name"`
	Qty            decimal.Decimal `json:"qty"`
	LotNo          string          `json:"lot_no"`
	Note           string          `json:"note"`
	ItemLotControl string          `json:"item_lot_control"`
	OnHand         decimal.Decimal `json:"on_hand"` // 領料倉的現有量(提示用)
	Lots           []lotUsedDTO    `json:"lots"`    // 已完工:實際領用的批號
}

type woDTO struct {
	ID                  int64           `json:"id"`
	DocNo               string          `json:"doc_no"`
	DocDate             string          `json:"doc_date"`
	Status              string          `json:"status"`
	ItemID              int64           `json:"item_id"`
	ItemCode            string          `json:"item_code"`
	ItemName            string          `json:"item_name"`
	UnitName            string          `json:"unit_name"`
	ItemLotControl      string          `json:"item_lot_control"`
	PlanQty             decimal.Decimal `json:"plan_qty"`
	WarehouseID         int64           `json:"warehouse_id"`
	WarehouseName       string          `json:"warehouse_name"`
	MaterialWarehouseID int64           `json:"material_warehouse_id"`
	MaterialWarehouse   string          `json:"material_warehouse_name"`
	ProcessingCost      decimal.Decimal `json:"processing_cost"`
	OutputLotNo         string          `json:"output_lot_no"`
	OutputExpiry        *string         `json:"output_expiry_date"`
	DueDate             *string         `json:"due_date"`
	Note                string          `json:"note"`
	CreatedByName       *string         `json:"created_by_name"`
	SubmittedByName     *string         `json:"submitted_by_name"`
	SubmittedAt         *time.Time      `json:"submitted_at"`
	ApprovedByName      *string         `json:"approved_by_name"`
	ApprovedAt          *time.Time      `json:"approved_at"`
	PostedByName        *string         `json:"posted_by_name"`
	PostedAt            *time.Time      `json:"posted_at"`
	Lines               []woLineDTO     `json:"lines"`
	OutputLots          []lotUsedDTO    `json:"output_lots"` // 已完工:成品實際入庫的批號
	Version             int32           `json:"version"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

func dateStr(t *time.Time) *string { return trade.DateString(t) }

func loadOrder(ctx context.Context, q *db.Queries, companyID, id int64) (woDTO, error) {
	w, err := q.GetWorkOrder(ctx, db.GetWorkOrderParams{ID: id, CompanyID: companyID})
	if database.IsNoRows(err) {
		return woDTO{}, apperr.ErrNotFound
	}
	if err != nil {
		return woDTO{}, err
	}
	rows, err := q.ListWorkOrderLines(ctx, db.ListWorkOrderLinesParams{WorkOrderID: id, MaterialWarehouseID: w.MaterialWarehouseID})
	if err != nil {
		return woDTO{}, err
	}
	out := woDTO{
		ID: w.ID, DocNo: w.DocNo, DocDate: w.DocDate.Format(time.DateOnly), Status: w.Status, ItemID: w.ItemID,
		ItemCode: w.ItemCode, ItemName: w.ItemName, UnitName: w.UnitName, ItemLotControl: w.ItemLotControl,
		PlanQty: w.PlanQty, WarehouseID: w.WarehouseID, WarehouseName: w.WarehouseName,
		MaterialWarehouseID: w.MaterialWarehouseID, MaterialWarehouse: w.MaterialWarehouseName,
		ProcessingCost: w.ProcessingCost, OutputLotNo: w.OutputLotNo, OutputExpiry: dateStr(w.OutputExpiry),
		DueDate: dateStr(w.DueDate), Note: w.Note, CreatedByName: w.CreatedByName, SubmittedByName: w.SubmittedByName,
		SubmittedAt: w.SubmittedAt, ApprovedByName: w.ApprovedByName, ApprovedAt: w.ApprovedAt, PostedByName: w.PostedByName,
		PostedAt: w.PostedAt, Lines: make([]woLineDTO, len(rows)), OutputLots: []lotUsedDTO{}, Version: w.Version, UpdatedAt: w.UpdatedAt,
	}
	for i, l := range rows {
		out.Lines[i] = woLineDTO{LineNo: l.LineNo, ItemID: l.ItemID, ItemCode: l.ItemCode, ItemName: l.ItemName, UnitName: l.UnitName,
			Qty: l.Qty, LotNo: l.LotNo, Note: l.Note, ItemLotControl: l.ItemLotControl, OnHand: l.OnHand, Lots: []lotUsedDTO{}}
	}
	if w.Status == "posted" {
		used, err := q.ListWorkOrderLots(ctx, id)
		if err != nil {
			return woDTO{}, err
		}
		for _, u := range used {
			d := lotUsedDTO{LotNo: u.LotNo, ExpiryDate: dateStr(u.ExpiryDate), Qty: u.Qty.Abs()}
			if u.SourceType == srcReceipt {
				out.OutputLots = append(out.OutputLots, d)
				continue
			}
			for i := range out.Lines {
				if out.Lines[i].ItemID == u.ItemID {
					out.Lines[i].Lots = append(out.Lines[i].Lots, d)
					break
				}
			}
		}
	}
	return out, nil
}

// ---- 列表與單筆 ----

type woListDTO struct {
	ID             int64           `json:"id"`
	DocNo          string          `json:"doc_no"`
	DocDate        string          `json:"doc_date"`
	Status         string          `json:"status"`
	ItemCode       string          `json:"item_code"`
	ItemName       string          `json:"item_name"`
	UnitName       string          `json:"unit_name"`
	PlanQty        decimal.Decimal `json:"plan_qty"`
	DueDate        *string         `json:"due_date"`
	ProcessingCost decimal.Decimal `json:"processing_cost"`
	WarehouseName  string          `json:"warehouse_name"`
	CreatedByName  *string         `json:"created_by_name"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func (m *Module) listOrders(c *gin.Context) {
	ctx := c.Request.Context()
	from, err := trade.OptionalDate(c, "from")
	if err != nil {
		response.Error(c, err)
		return
	}
	to, err := trade.OptionalDate(c, "to")
	if err != nil {
		response.Error(c, err)
		return
	}
	itemID, err := httpx.QueryInt64(c, "item_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	status := httpx.QueryString(c, "status")
	if status != nil && !docstate.Valid(docstate.Status(*status)) {
		response.Error(c, fieldErr("status", "狀態不正確"))
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	keyword := httpx.QueryString(c, "keyword")
	rows, err := m.store.ListWorkOrders(ctx, db.ListWorkOrdersParams{CompanyID: companyID, Status: status, ItemID: itemID,
		FromDate: from, ToDate: to, Keyword: keyword, Lim: pg.Limit(), Off: pg.Offset()})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountWorkOrders(ctx, db.CountWorkOrdersParams{CompanyID: companyID, Status: status, ItemID: itemID,
		FromDate: from, ToDate: to, Keyword: keyword})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]woListDTO, len(rows))
	for i, r := range rows {
		out[i] = woListDTO{ID: r.ID, DocNo: r.DocNo, DocDate: r.DocDate.Format(time.DateOnly), Status: r.Status, ItemCode: r.ItemCode,
			ItemName: r.ItemName, UnitName: r.UnitName, PlanQty: r.PlanQty, DueDate: dateStr(r.DueDate), ProcessingCost: r.ProcessingCost,
			WarehouseName: r.WarehouseName, CreatedByName: r.CreatedByName, UpdatedAt: r.UpdatedAt}
	}
	response.List(c, out, pg.Meta(total))
}

func (m *Module) getOrder(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	dto, err := loadOrder(c.Request.Context(), m.store.Queries, actor(c).CompanyID, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 依 BOM 展開 ----

// ExplodeQty 成品數量 planQty 需要的材料量 = BOM 用量 × planQty ÷ yield,捨入到 4 位小數,但至少 0.0001。
func ExplodeQty(bomQty, yield, planQty decimal.Decimal) decimal.Decimal {
	q := bomQty.Mul(planQty).Div(yield).Round(money.QuantityPlaces)
	if !q.IsPositive() {
		return decimal.New(1, -money.QuantityPlaces)
	}
	return q
}

type explodeLineDTO struct {
	ItemID   int64           `json:"item_id"`
	ItemCode string          `json:"item_code"`
	ItemName string          `json:"item_name"`
	UnitName string          `json:"unit_name"`
	Qty      decimal.Decimal `json:"qty"`
}

func explodeLines(ctx context.Context, q *db.Queries, companyID, itemID int64, planQty decimal.Decimal) ([]explodeLineDTO, error) {
	bom, err := q.GetBomByItem(ctx, db.GetBomByItemParams{CompanyID: companyID, ItemID: itemID})
	if database.IsNoRows(err) {
		return nil, fieldErr("item_id", "這個成品還沒有 BOM,請先建立 BOM 或自行輸入領料明細")
	}
	if err != nil {
		return nil, err
	}
	if !bom.IsActive {
		return nil, fieldErr("item_id", "這個成品的 BOM 已停用")
	}
	rows, err := q.ListBomLines(ctx, bom.ID)
	if err != nil {
		return nil, err
	}
	out := make([]explodeLineDTO, len(rows))
	for i, l := range rows {
		out[i] = explodeLineDTO{ItemID: l.ItemID, ItemCode: l.ItemCode, ItemName: l.ItemName, UnitName: l.UnitName,
			Qty: ExplodeQty(l.Qty, bom.YieldQty, planQty)}
	}
	return out, nil
}

// explode GET /production/work-orders/explode?item_id=&qty= 依 BOM 計算材料需求(開單預覽)。
func (m *Module) explode(c *gin.Context) {
	itemID, err := httpx.QueryInt64(c, "item_id")
	if err != nil || itemID == nil {
		response.Error(c, fieldErr("item_id", "請指定成品"))
		return
	}
	qty, err := decimal.NewFromString(c.Query("qty"))
	if err != nil || !qtyOK(qty) {
		response.Error(c, fieldErr("qty", "數量須大於 0,最多 4 位小數"))
		return
	}
	lines, err := explodeLines(c.Request.Context(), m.store.Queries, actor(c).CompanyID, *itemID, qty)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, lines)
}

// ---- 建立與修改(草稿) ----

type woLineInput struct {
	ItemID int64           `json:"item_id" binding:"required"`
	Qty    decimal.Decimal `json:"qty"`
	LotNo  string          `json:"lot_no"`
	Note   string          `json:"note" binding:"max=255"`
}

type woInput struct {
	DocDate             string          `json:"doc_date" binding:"required"`
	ItemID              int64           `json:"item_id" binding:"required"`
	PlanQty             decimal.Decimal `json:"plan_qty"`
	WarehouseID         int64           `json:"warehouse_id" binding:"required"`
	MaterialWarehouseID int64           `json:"material_warehouse_id" binding:"required"`
	ProcessingCost      decimal.Decimal `json:"processing_cost"`
	OutputLotNo         string          `json:"output_lot_no"`
	OutputExpiry        *string         `json:"output_expiry_date"`
	DueDate             *string         `json:"due_date"`
	Note                string          `json:"note" binding:"max=2000"`
	Lines               []woLineInput   `json:"lines" binding:"dive"`
	Version             int32           `json:"version"`
}

type preparedOrder struct {
	date   time.Time
	due    *time.Time
	expiry *time.Time
	lotNo  string
	lines  []woLineInput
}

func prepareOrder(ctx context.Context, q *db.Queries, companyID int64, in *woInput, generate bool) (preparedOrder, error) {
	var p preparedOrder
	date, err := trade.ParseDate("doc_date", in.DocDate)
	if err != nil {
		return p, err
	}
	p.date = date
	if p.due, err = trade.OptionalInputDate("due_date", in.DueDate); err != nil {
		return p, err
	}
	if p.expiry, err = trade.OptionalInputDate("output_expiry_date", in.OutputExpiry); err != nil {
		return p, err
	}
	if !qtyOK(in.PlanQty) {
		return p, fieldErr("plan_qty", "完工數量須大於 0,最多 4 位小數")
	}
	if in.ProcessingCost.IsNegative() || in.ProcessingCost.Exponent() < -2 || in.ProcessingCost.GreaterThan(decimal.NewFromInt(maxProcessing)) {
		return p, fieldErr("processing_cost", "加工費須為 0 以上,最多 2 位小數")
	}
	whs, err := q.ListWarehouseFlags(ctx, db.ListWarehouseFlagsParams{CompanyID: companyID, Ids: []int64{in.WarehouseID, in.MaterialWarehouseID}})
	if err != nil {
		return p, err
	}
	active := map[int64]bool{}
	for _, w := range whs {
		active[w.ID] = w.IsActive
	}
	if !active[in.WarehouseID] {
		return p, fieldErr("warehouse_id", "成品入庫倉不存在或已停用")
	}
	if !active[in.MaterialWarehouseID] {
		return p, fieldErr("material_warehouse_id", "領料倉不存在或已停用")
	}
	lines := in.Lines
	if len(lines) == 0 {
		if !generate {
			return p, fieldErr("lines", "請輸入領料明細")
		}
		exp, err := explodeLines(ctx, q, companyID, in.ItemID, in.PlanQty)
		if err != nil {
			return p, err
		}
		for _, l := range exp {
			lines = append(lines, woLineInput{ItemID: l.ItemID, Qty: l.Qty})
		}
	}
	if len(lines) > maxWoLines {
		return p, fieldErr("lines", fmt.Sprintf("領料明細最多 %d 筆", maxWoLines))
	}
	ids := []int64{in.ItemID}
	for _, l := range lines {
		ids = append(ids, l.ItemID)
	}
	items, err := q.ListProductionItems(ctx, db.ListProductionItemsParams{CompanyID: companyID, Ids: ids})
	if err != nil {
		return p, err
	}
	byID := map[int64]db.ListProductionItemsRow{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if it, ok := byID[in.ItemID]; !ok || it.ItemType != "goods" || !it.IsActive {
		return p, fieldErr("item_id", "成品須為啟用的商品類料品")
	}
	fields := map[string]string{}
	seen := map[string]bool{}
	for i := range lines {
		l := &lines[i]
		key := fmt.Sprintf("lines.%d", i)
		it, ok := byID[l.ItemID]
		l.LotNo = strings.TrimSpace(l.LotNo)
		switch {
		case !ok || it.ItemType != "goods" || !it.IsActive:
			fields[key] = "材料須為啟用的商品類料品"
		case l.ItemID == in.ItemID:
			fields[key] = "材料不可是成品自己"
		case !qtyOK(l.Qty):
			fields[key] = "用量須大於 0,最多 4 位小數"
		}
		dup := fmt.Sprintf("%d/%s", l.ItemID, strings.ToUpper(l.LotNo))
		if seen[dup] && fields[key] == "" {
			fields[key] = "同一材料(同批號)重複,請合併數量"
		}
		seen[dup] = true
	}
	if len(fields) > 0 {
		return p, apperr.Validation(fields)
	}
	// 批號:材料出庫可空白(先到期先出);成品入庫須批號(批號管理的料品),效期管理的新批號須效期
	inputs := make([]inventory.LotInput, 0, len(lines)+1)
	for _, l := range lines {
		inputs = append(inputs, inventory.LotInput{ItemID: l.ItemID, LotNo: l.LotNo})
	}
	inputs = append(inputs, inventory.LotInput{ItemID: in.ItemID, LotNo: in.OutputLotNo, Expiry: p.expiry, Inbound: true})
	checked, lotErrs, err := inventory.CheckLotInputs(ctx, q, companyID, inputs)
	if err != nil {
		return p, err
	}
	for i, msg := range lotErrs {
		if i == len(lines) {
			fields["output_lot_no"] = msg
		} else {
			fields[fmt.Sprintf("lines.%d", i)] = msg
		}
	}
	if len(fields) > 0 {
		return p, apperr.Validation(fields)
	}
	for i := range lines {
		lines[i].LotNo = checked[i].LotNo
	}
	p.lotNo, p.expiry = checked[len(lines)].LotNo, checked[len(lines)].Expiry
	p.lines = lines
	return p, nil
}

func saveOrderLines(ctx context.Context, q *db.Queries, id int64, lines []woLineInput) error {
	if err := q.DeleteWorkOrderLines(ctx, id); err != nil {
		return err
	}
	for i, l := range lines {
		if err := q.AddWorkOrderLine(ctx, db.AddWorkOrderLineParams{WorkOrderID: id, LineNo: int32(i + 1), ItemID: l.ItemID,
			Qty: l.Qty, LotNo: l.LotNo, Note: l.Note}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) createOrder(c *gin.Context) {
	var in woInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	var out woDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		p, err := prepareOrder(ctx, q, a.CompanyID, &in, true)
		if err != nil {
			return err
		}
		no, err := docno.Next(ctx, q, a.CompanyID, docType, p.date)
		if err != nil {
			return err
		}
		w, err := q.CreateWorkOrder(ctx, db.CreateWorkOrderParams{CompanyID: a.CompanyID, DocNo: no, DocDate: p.date, ItemID: in.ItemID,
			PlanQty: in.PlanQty, WarehouseID: in.WarehouseID, MaterialWarehouseID: in.MaterialWarehouseID,
			ProcessingCost: in.ProcessingCost, OutputLotNo: p.lotNo, OutputExpiry: p.expiry, DueDate: p.due, Note: in.Note, ActorID: &a.UserID})
		if err != nil {
			return err
		}
		if err := saveOrderLines(ctx, q, w.ID, p.lines); err != nil {
			return err
		}
		if out, err = loadOrder(ctx, q, a.CompanyID, w.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Create, EntityType: "work_order", EntityID: &w.ID,
			Summary: "新增工單 " + no, After: out})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, out)
}

func (m *Module) updateOrder(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in woInput
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
		if cur.Status != string(docstate.Draft) {
			return errNotDraft
		}
		before, err := loadOrder(ctx, q, a.CompanyID, id)
		if err != nil {
			return err
		}
		p, err := prepareOrder(ctx, q, a.CompanyID, &in, false)
		if err != nil {
			return err
		}
		if _, err := q.UpdateWorkOrderHeader(ctx, db.UpdateWorkOrderHeaderParams{ID: id, CompanyID: a.CompanyID, DocDate: p.date,
			ItemID: in.ItemID, PlanQty: in.PlanQty, WarehouseID: in.WarehouseID, MaterialWarehouseID: in.MaterialWarehouseID,
			ProcessingCost: in.ProcessingCost, OutputLotNo: p.lotNo, OutputExpiry: p.expiry, DueDate: p.due, Note: in.Note,
			ActorID: &a.UserID, Version: in.Version}); database.IsNoRows(err) {
			return apperr.ErrVersionConflict
		} else if err != nil {
			return err
		}
		if err := saveOrderLines(ctx, q, id, p.lines); err != nil {
			return err
		}
		if out, err = loadOrder(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Update, EntityType: "work_order", EntityID: &id,
			Summary: "修改工單 " + cur.DocNo, Before: before, After: out})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, out)
}

// ---- 狀態動作 ----

type actionInput struct {
	Version int32 `json:"version" binding:"required"`
}

func actionPermission(action docstate.Action, status docstate.Status) (string, bool) {
	switch action {
	case docstate.Submit:
		return permission.WorkOrderWrite, true
	case docstate.Approve, docstate.Reject, docstate.Unapprove:
		return permission.WorkOrderApprove, true
	case docstate.Post, docstate.Unpost:
		return permission.WorkOrderPost, true
	case docstate.Void:
		if status == docstate.Draft {
			return permission.WorkOrderWrite, true
		}
		return permission.WorkOrderApprove, true
	}
	return "", false
}

func (m *Module) orderAction(c *gin.Context) {
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
	action := docstate.Action(c.Param("action"))
	ctx := c.Request.Context()
	a := actor(c)
	var dto woDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockWorkOrder(ctx, db.LockWorkOrderParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		perm, ok := actionPermission(action, docstate.Status(cur.Status))
		if !ok {
			return apperr.ErrNotFound
		}
		if !a.Can(perm) {
			return apperr.ErrForbidden
		}
		if cur.Version != in.Version {
			return apperr.ErrVersionConflict
		}
		next, err := trade.PostingTransition(docstate.Status(cur.Status), action)
		if err != nil {
			return err
		}
		doc, err := loadOrder(ctx, q, a.CompanyID, id)
		if err != nil {
			return err
		}
		if err := applyAction(ctx, q, a, cur, doc, action); err != nil {
			return err
		}
		if _, err := q.SetWorkOrderStatus(ctx, db.SetWorkOrderStatusParams{ID: id, CompanyID: a.CompanyID, Status: string(next),
			Action: string(action), ActorID: a.UserID, Version: in.Version}); err != nil {
			return err
		}
		if dto, err = loadOrder(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		label := trade.ActionLabels[action]
		switch action {
		case docstate.Post:
			label = "完工"
		case docstate.Unpost:
			label = "反完工"
		}
		return audit.Record(ctx, q, audit.Entry{Action: string(action), EntityType: "work_order", EntityID: &id,
			Summary: label + "工單 " + cur.DocNo, Before: map[string]string{"status": cur.Status}, After: map[string]string{"status": string(next)}})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// applyAction 動作的附帶檢查與庫存處理:完工 = 在同一交易內領料(扣材料)並成品入庫;反完工沖銷這兩批分錄。
func applyAction(ctx context.Context, q *db.Queries, a *authctx.Actor, cur db.WorkOrder, doc woDTO, action docstate.Action) error {
	opt := inventory.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
	issue := inventory.Source{Type: srcIssue, ID: cur.ID, No: cur.DocNo, DocDate: cur.DocDate}
	receipt := inventory.Source{Type: srcReceipt, ID: cur.ID, No: cur.DocNo, DocDate: cur.DocDate}
	switch action {
	case docstate.Submit:
		if len(doc.Lines) == 0 {
			return fieldErr("lines", "請輸入領料明細")
		}
	case docstate.Post:
		moves := make([]inventory.Movement, 0, len(doc.Lines))
		for _, l := range doc.Lines {
			moves = append(moves, inventory.Movement{ItemID: l.ItemID, WarehouseID: cur.MaterialWarehouseID, Qty: l.Qty.Neg(), LotNo: l.LotNo})
		}
		if err := inventory.Post(ctx, q, opt, issue, moves); err != nil {
			return err
		}
		return inventory.Post(ctx, q, opt, receipt, []inventory.Movement{{
			ItemID: cur.ItemID, WarehouseID: cur.WarehouseID, Qty: cur.PlanQty, LotNo: cur.OutputLotNo, Expiry: cur.OutputExpiry,
		}})
	case docstate.Unpost:
		// 先沖銷成品入庫(成品已被領用或出貨就會因庫存不足被擋),再沖銷領料
		if err := inventory.Reverse(ctx, q, opt, receipt); err != nil {
			return err
		}
		return inventory.Reverse(ctx, q, opt, issue)
	}
	return nil
}
