package production

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/inventory"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/system/docno"
	"erp/internal/trade"
)

// 在製品(WIP,D101):已核准的工單可在完工前分批領料,材料轉入公司唯一的在製品倉;
// 完工回報時優先從在製品倉扣(不足的部分才從領料倉扣,與 D94 相同)。

const (
	srcWipIssue  = "wo_wip_issue"  // 領料:領料倉 → 在製品倉(調撥)
	srcWipReturn = "wo_wip_return" // 退料:在製品倉 → 領料倉

	wipIssueType  = "work_order_wip_issue"
	wipReturnType = "work_order_wip_return"
)

var (
	errWipLeft    = apperr.Conflict("PRD-009", "還有材料在在製品倉,請先退料或完工耗用再結案")
	errWipNeg     = apperr.Conflict("PRD-010", "作廢後在製品倉的材料會變成負數(材料已被完工耗用或退料),請先作廢後續的完工回報 / 退料")
	errHasWipTxns = apperr.Conflict("PRD-011", "這張工單已有領料 / 退料紀錄,請先作廢全部領料紀錄;有在製品的工單不能用一次完工、反過帳、取消核准或作廢")
)

type wipTxnLineDTO struct {
	LineNo   int32           `json:"line_no"`
	ItemID   int64           `json:"item_id"`
	ItemCode string          `json:"item_code"`
	ItemName string          `json:"item_name"`
	UnitName string          `json:"unit_name"`
	Qty      decimal.Decimal `json:"qty"`
	LotNo    string          `json:"lot_no"`
	BinCode  string          `json:"bin_code"`
}

type wipTxnDTO struct {
	ID            int64           `json:"id"`
	Kind          string          `json:"kind"` // issue / return
	DocNo         string          `json:"doc_no"`
	DocDate       string          `json:"doc_date"`
	Note          string          `json:"note"`
	Status        string          `json:"status"`
	CreatedByName *string         `json:"created_by_name"`
	CreatedAt     time.Time       `json:"created_at"`
	VoidedByName  *string         `json:"voided_by_name"`
	VoidedAt      *time.Time      `json:"voided_at"`
	Lines         []wipTxnLineDTO `json:"lines"`
}

type wipHeldDTO struct {
	ItemID   int64           `json:"item_id"`
	ItemCode string          `json:"item_code"`
	ItemName string          `json:"item_name"`
	UnitName string          `json:"unit_name"`
	Qty      decimal.Decimal `json:"qty"`
}

// wipDTO 工單的在製品:目前還在在製品倉的材料與領料 / 退料紀錄。
type wipDTO struct {
	Held []wipHeldDTO `json:"held"`
	Txns []wipTxnDTO  `json:"txns"`
}

func loadWip(ctx context.Context, q *db.Queries, workOrderID int64) (wipDTO, error) {
	out := wipDTO{Held: []wipHeldDTO{}, Txns: []wipTxnDTO{}}
	held, err := q.WipHeldQty(ctx, workOrderID)
	if err != nil {
		return out, err
	}
	for _, h := range held {
		if !h.Qty.IsZero() {
			out.Held = append(out.Held, wipHeldDTO{ItemID: h.ItemID, ItemCode: h.ItemCode, ItemName: h.ItemName, UnitName: h.UnitName, Qty: h.Qty})
		}
	}
	txns, err := q.ListWipTxns(ctx, workOrderID)
	if err != nil {
		return out, err
	}
	for _, t := range txns {
		d := wipTxnDTO{ID: t.ID, Kind: t.Kind, DocNo: t.DocNo, DocDate: t.DocDate.Format(time.DateOnly), Note: t.Note, Status: t.Status,
			CreatedByName: t.CreatedByName, CreatedAt: t.CreatedAt, VoidedByName: t.VoidedByName, VoidedAt: t.VoidedAt, Lines: []wipTxnLineDTO{}}
		lines, err := q.ListWipTxnLines(ctx, t.ID)
		if err != nil {
			return out, err
		}
		for _, l := range lines {
			d.Lines = append(d.Lines, wipTxnLineDTO{LineNo: l.LineNo, ItemID: l.ItemID, ItemCode: l.ItemCode, ItemName: l.ItemName,
				UnitName: l.UnitName, Qty: l.Qty, LotNo: l.LotNo, BinCode: l.BinCode})
		}
		out.Txns = append(out.Txns, d)
	}
	return out, nil
}

// wipHeld 工單目前在在製品倉的材料量(依料品)。
func wipHeld(ctx context.Context, q *db.Queries, workOrderID int64) (map[int64]decimal.Decimal, error) {
	rows, err := q.WipHeldQty(ctx, workOrderID)
	if err != nil {
		return nil, err
	}
	out := map[int64]decimal.Decimal{}
	for _, r := range rows {
		out[r.ItemID] = r.Qty
	}
	return out, nil
}

// wipWarehouseID 取得(沒有就建立)公司唯一的在製品倉。
func wipWarehouseID(ctx context.Context, q *db.Queries, companyID, actorID int64) (int64, error) {
	w, err := q.WipWarehouse(ctx, companyID)
	if err == nil {
		return w.ID, nil
	}
	if !database.IsNoRows(err) {
		return 0, err
	}
	w, err = q.CreateWipWarehouse(ctx, db.CreateWipWarehouseParams{CompanyID: companyID, ActorID: &actorID})
	if database.IsUniqueViolation(err, "warehouses_company_code_key") {
		return 0, fieldErr("lines", "在製品倉代碼 WIP 已被其他倉庫使用,請先改掉那個倉庫的代碼")
	}
	return w.ID, err
}

// closeIfDone 累計完工達計畫數量、且材料都已耗用或退回時,工單自動轉為「已過帳」(結案)。
func closeIfDone(ctx context.Context, q *db.Queries, cur db.WorkOrder, actorID int64) error {
	if cur.Status != "approved" {
		return nil
	}
	done, err := q.CompletedQty(ctx, cur.ID)
	if err != nil || done.LessThan(cur.PlanQty) {
		return err
	}
	held, err := wipHeld(ctx, q, cur.ID)
	if err != nil || !allZero(held) {
		return err
	}
	_, err = q.SetWorkOrderStatus(ctx, db.SetWorkOrderStatusParams{ID: cur.ID, CompanyID: cur.CompanyID, Status: "posted", Action: "post",
		ActorID: actorID, Version: cur.Version})
	return err
}

type wipInput struct {
	DocDate string        `json:"doc_date" binding:"required"`
	Note    string        `json:"note" binding:"max=255"`
	Lines   []woLineInput `json:"lines" binding:"dive"`
}

func (m *Module) postWip(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := httpx.ParamID(c, "id")
		if err != nil {
			response.Error(c, err)
			return
		}
		var in wipInput
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
			date, err := trade.ParseDate("doc_date", in.DocDate)
			if err != nil {
				return err
			}
			if date.Before(cur.DocDate) {
				return fieldErr("doc_date", "日期不可早於工單日期")
			}
			wip, err := wipWarehouseID(ctx, q, a.CompanyID, a.UserID)
			if err != nil {
				return err
			}
			from, to, src, numbering, label := cur.MaterialWarehouseID, wip, srcWipIssue, wipIssueType, "領料"
			if kind == kindReturn {
				from, to, src, numbering, label = wip, cur.MaterialWarehouseID, srcWipReturn, wipReturnType, "退料"
			}
			lines, _, err := checkMoves(ctx, q, a.CompanyID, cur.ItemID, in.Lines, from, &to, kind == kindReturn, nil)
			if err != nil {
				return err
			}
			if kind == kindReturn {
				held, err := wipHeld(ctx, q, id)
				if err != nil {
					return err
				}
				fields := map[string]string{}
				left := map[int64]decimal.Decimal{}
				for i, l := range lines {
					h, ok := left[l.ItemID]
					if !ok {
						h = held[l.ItemID]
					}
					if l.Qty.GreaterThan(h) {
						fields[fmt.Sprintf("lines.%d", i)] = "退料量超過還在在製品倉的 " + h.String()
					}
					left[l.ItemID] = h.Sub(l.Qty)
				}
				if len(fields) > 0 {
					return apperr.Validation(fields)
				}
			}
			no, err := docno.Next(ctx, q, a.CompanyID, numbering, date)
			if err != nil {
				return err
			}
			txn, err := q.CreateWipTxn(ctx, db.CreateWipTxnParams{CompanyID: a.CompanyID, WorkOrderID: id, Kind: kind, DocNo: no,
				DocDate: date, Note: in.Note, ActorID: &a.UserID})
			if err != nil {
				return err
			}
			moves := make([]inventory.Movement, 0, len(lines))
			for i, l := range lines {
				if err := q.AddWipTxnLine(ctx, db.AddWipTxnLineParams{TxnID: txn.ID, LineNo: int32(i + 1), ItemID: l.ItemID,
					Qty: l.Qty, LotNo: l.LotNo, BinCode: l.BinCode}); err != nil {
					return err
				}
				mv := inventory.Movement{ItemID: l.ItemID, WarehouseID: from, Qty: l.Qty.Neg(), LotNo: l.LotNo, TransferTo: &to}
				if kind == kindReturn {
					mv.ToBinCode = l.BinCode
				} else {
					mv.BinCode = l.BinCode
				}
				moves = append(moves, mv)
			}
			opt := inventory.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
			if err := inventory.Post(ctx, q, opt, inventory.Source{Type: src, ID: txn.ID, No: txn.DocNo, DocDate: txn.DocDate}, moves); err != nil {
				return err
			}
			if kind == kindReturn {
				if err := closeIfDone(ctx, q, cur, a.UserID); err != nil {
					return err
				}
			}
			if out, err = loadOrder(ctx, q, a.CompanyID, id); err != nil {
				return err
			}
			return audit.Record(ctx, q, audit.Entry{Action: "wip_" + kind, EntityType: "work_order", EntityID: &id,
				Summary: "工單 " + cur.DocNo + label + " " + txn.DocNo})
		})
		if err != nil {
			response.Error(c, err)
			return
		}
		response.Created(c, out)
	}
}

// voidWipTxn POST /production/work-orders/:id/wip-txns/:tid/void 作廢領料 / 退料:以反向分錄沖銷;已結案的工單退回已核准。
func (m *Module) voidWipTxn(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	tid, err := httpx.ParamID(c, "tid")
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
		t, err := q.LockWipTxn(ctx, db.LockWipTxnParams{ID: tid, WorkOrderID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if t.Status != "posted" {
			return errScTxnVoided
		}
		src := srcWipIssue
		if t.Kind == kindReturn {
			src = srcWipReturn
		}
		opt := inventory.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
		if err := inventory.Reverse(ctx, q, opt, inventory.Source{Type: src, ID: t.ID, No: t.DocNo, DocDate: t.DocDate}); err != nil {
			return err
		}
		if err := q.VoidWipTxn(ctx, db.VoidWipTxnParams{ID: tid, ActorID: &a.UserID}); err != nil {
			return err
		}
		held, err := wipHeld(ctx, q, id)
		if err != nil {
			return err
		}
		for _, h := range held {
			if h.IsNegative() {
				return errWipNeg
			}
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
		return audit.Record(ctx, q, audit.Entry{Action: "void_wip_" + t.Kind, EntityType: "work_order", EntityID: &id,
			Summary: "工單 " + cur.DocNo + " 作廢在製品" + map[string]string{kindIssue: "領料", kindReturn: "退料"}[t.Kind] + " " + t.DocNo})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, out)
}

type wipReportRow struct {
	WorkOrderID  int64           `json:"work_order_id"`
	DocNo        string          `json:"doc_no"`
	DocDate      string          `json:"doc_date"`
	ItemCode     string          `json:"item_code"`
	ItemName     string          `json:"item_name"`
	MaterialCode string          `json:"material_code"`
	MaterialName string          `json:"material_name"`
	UnitName     string          `json:"unit_name"`
	Qty          decimal.Decimal `json:"qty"`
	AvgCost      decimal.Decimal `json:"avg_cost"` // 最近一次月結的平均成本(沒有 = 0)
	Amount       decimal.Decimal `json:"amount"`
}

// wipReport GET /production/wip 在製品清單:進行中的工單還在在製品倉的材料。
func (m *Module) wipReport(c *gin.Context) {
	rows, err := m.store.ListWipReport(c.Request.Context(), actor(c).CompanyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]wipReportRow, len(rows))
	for i, r := range rows {
		out[i] = wipReportRow{WorkOrderID: r.WorkOrderID, DocNo: r.DocNo, DocDate: r.DocDate.Format(time.DateOnly), ItemCode: r.ItemCode,
			ItemName: r.ItemName, MaterialCode: r.MaterialCode, MaterialName: r.MaterialName, UnitName: r.UnitName, Qty: r.Qty,
			AvgCost: r.AvgCost, Amount: r.Qty.Mul(r.AvgCost).Round(2)}
	}
	response.OK(c, out)
}
