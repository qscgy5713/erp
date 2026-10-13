package production

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/finance"
	"erp/internal/gl"
	"erp/internal/inventory"
	"erp/internal/masterdata"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/system/docno"
	"erp/internal/trade"
)

// 委外加工(D100):發料給加工廠(轉入該廠的委外倉)→ 收回成品(扣委外倉材料、成品入庫、加工費轉應付)。

const (
	scOrderType = "subcontract_order"

	kindIssue   = "issue"
	kindReceive = "receive"
	kindReturn  = "return"

	srcScIssue       = "sc_issue"            // 發料:發料倉 → 委外倉(調撥)
	srcScReturn      = "sc_return"           // 退料:委外倉 → 回料倉(調撥)
	srcScRecvIssue   = "sc_receive_issue"    // 收回時耗用的材料(自委外倉扣)
	srcScRecvReceipt = "sc_receive_receipt"  // 收回的成品入庫
	srcScFee         = "subcontract_receive" // 加工費的應付與傳票來源

	maxScLines = 100
)

// 各類型交易的單號規則與稽核用中文名
var scKinds = map[string]struct{ numbering, label string }{
	kindIssue:   {"subcontract_issue", "發料"},
	kindReceive: {"subcontract_receive", "收回"},
	kindReturn:  {"subcontract_return", "退料"},
}

var (
	errScNotOpen     = apperr.Conflict("SUB-001", "委外單不是「進行中」,不能再發料、收回或修改")
	errScTxnVoided   = apperr.Conflict("SUB-002", "這筆委外異動已作廢")
	errScHasTxns     = apperr.Conflict("SUB-003", "委外單已有發料 / 收回紀錄,請先作廢全部異動才能作廢整張單")
	errScNothingBack = apperr.Conflict("SUB-004", "還沒有任何收回紀錄,不能結案;要整張作廢請用「作廢」")
	errScHeldLeft    = apperr.Conflict("SUB-005", "還有材料在加工廠,請先退料(或收回耗用)再結案")
	errScHeldNeg     = apperr.Conflict("SUB-006", "作廢後加工廠的材料會變成負數(材料已被收回耗用或退料),請先作廢後續的收回 / 退料")
)

// ---- DTO ----

type scLineDTO struct {
	LineNo         int32           `json:"line_no"`
	ItemID         int64           `json:"item_id"`
	ItemCode       string          `json:"item_code"`
	ItemName       string          `json:"item_name"`
	UnitName       string          `json:"unit_name"`
	ItemLotControl string          `json:"item_lot_control"`
	Qty            decimal.Decimal `json:"qty"`          // 預計用量
	IssuedQty      decimal.Decimal `json:"issued_qty"`   // 累計發料(發料 − 退料)
	ConsumedQty    decimal.Decimal `json:"consumed_qty"` // 累計耗用(收回時實際耗料)
	HeldQty        decimal.Decimal `json:"held_qty"`     // 目前還在加工廠
	Note           string          `json:"note"`
}

type scTxnLineDTO struct {
	LineNo   int32           `json:"line_no"`
	ItemID   int64           `json:"item_id"`
	ItemCode string          `json:"item_code"`
	ItemName string          `json:"item_name"`
	UnitName string          `json:"unit_name"`
	Qty      decimal.Decimal `json:"qty"`
	LotNo    string          `json:"lot_no"`
	BinCode  string          `json:"bin_code"`
	Lots     []lotUsedDTO    `json:"lots"` // 收回:實際耗用的批號
}

type scTxnDTO struct {
	ID            int64           `json:"id"`
	Kind          string          `json:"kind"`
	DocNo         string          `json:"doc_no"`
	DocDate       string          `json:"doc_date"`
	Qty           decimal.Decimal `json:"qty"`
	ProcessingFee decimal.Decimal `json:"processing_fee"`
	OutputLotNo   string          `json:"output_lot_no"`
	OutputExpiry  *string         `json:"output_expiry_date"`
	OutputBinCode string          `json:"output_bin_code"`
	Note          string          `json:"note"`
	Status        string          `json:"status"`
	CreatedByName *string         `json:"created_by_name"`
	CreatedAt     time.Time       `json:"created_at"`
	VoidedByName  *string         `json:"voided_by_name"`
	VoidedAt      *time.Time      `json:"voided_at"`
	Lines         []scTxnLineDTO  `json:"lines"`
	OutputLots    []lotUsedDTO    `json:"output_lots"`
}

type scOrderDTO struct {
	ID                 int64           `json:"id"`
	DocNo              string          `json:"doc_no"`
	DocDate            string          `json:"doc_date"`
	Status             string          `json:"status"`
	SupplierID         int64           `json:"supplier_id"`
	SupplierCode       string          `json:"supplier_code"`
	SupplierName       string          `json:"supplier_name"`
	ItemID             int64           `json:"item_id"`
	ItemCode           string          `json:"item_code"`
	ItemName           string          `json:"item_name"`
	UnitName           string          `json:"unit_name"`
	ItemLotControl     string          `json:"item_lot_control"`
	PlanQty            decimal.Decimal `json:"plan_qty"`
	ReceivedQty        decimal.Decimal `json:"received_qty"`
	UnitFee            decimal.Decimal `json:"unit_fee"`
	IssueWarehouseID   int64           `json:"issue_warehouse_id"`
	IssueWarehouse     string          `json:"issue_warehouse_name"`
	ReceiveWarehouseID int64           `json:"receive_warehouse_id"`
	ReceiveWarehouse   string          `json:"receive_warehouse_name"`
	ScWarehouseID      int64           `json:"sc_warehouse_id"`
	ScWarehouse        string          `json:"sc_warehouse_name"`
	DueDate            *string         `json:"due_date"`
	Note               string          `json:"note"`
	CreatedByName      *string         `json:"created_by_name"`
	Lines              []scLineDTO     `json:"lines"`
	Txns               []scTxnDTO      `json:"txns"`
	CanClose           bool            `json:"can_close"` // 有收回紀錄且材料都已退回或耗用
	Version            int32           `json:"version"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

func loadScOrder(ctx context.Context, q *db.Queries, companyID, id int64) (scOrderDTO, error) {
	o, err := q.GetSubcontractOrder(ctx, db.GetSubcontractOrderParams{ID: id, CompanyID: companyID})
	if database.IsNoRows(err) {
		return scOrderDTO{}, apperr.ErrNotFound
	}
	if err != nil {
		return scOrderDTO{}, err
	}
	received, err := q.SubcontractReceivedQty(ctx, id)
	if err != nil {
		return scOrderDTO{}, err
	}
	out := scOrderDTO{
		ID: o.ID, DocNo: o.DocNo, DocDate: o.DocDate.Format(time.DateOnly), Status: o.Status, SupplierID: o.SupplierID,
		SupplierCode: o.SupplierCode, SupplierName: o.SupplierName, ItemID: o.ItemID, ItemCode: o.ItemCode, ItemName: o.ItemName,
		UnitName: o.UnitName, ItemLotControl: o.ItemLotControl, PlanQty: o.PlanQty, ReceivedQty: received, UnitFee: o.UnitFee,
		IssueWarehouseID: o.IssueWarehouseID, IssueWarehouse: o.IssueWarehouseName, ReceiveWarehouseID: o.ReceiveWarehouseID,
		ReceiveWarehouse: o.ReceiveWarehouseName, ScWarehouseID: o.ScWarehouseID, ScWarehouse: o.ScWarehouseName,
		DueDate: dateStr(o.DueDate), Note: o.Note, CreatedByName: o.CreatedByName, Lines: []scLineDTO{}, Txns: []scTxnDTO{},
		Version: o.Version, UpdatedAt: o.UpdatedAt,
	}
	lines, err := q.ListSubcontractOrderLines(ctx, id)
	if err != nil {
		return scOrderDTO{}, err
	}
	held := decimal.Zero
	for _, l := range lines {
		h := l.IssuedQty.Sub(l.ConsumedQty)
		held = held.Add(h.Abs())
		out.Lines = append(out.Lines, scLineDTO{LineNo: l.LineNo, ItemID: l.ItemID, ItemCode: l.ItemCode, ItemName: l.ItemName,
			UnitName: l.UnitName, ItemLotControl: l.ItemLotControl, Qty: l.Qty, IssuedQty: l.IssuedQty, ConsumedQty: l.ConsumedQty,
			HeldQty: h, Note: l.Note})
	}
	// 不在預計用量裡的材料(發了額外的料)也要算進「還在加工廠」
	heldRows, err := q.SubcontractHeldQty(ctx, id)
	if err != nil {
		return scOrderDTO{}, err
	}
	planned := map[int64]bool{}
	for _, l := range lines {
		planned[l.ItemID] = true
	}
	extra := false
	for _, h := range heldRows {
		if !planned[h.ItemID] && !h.Qty.IsZero() {
			extra = true
		}
	}
	out.CanClose = o.Status == "open" && received.IsPositive() && held.IsZero() && !extra
	txns, err := q.ListSubcontractTxns(ctx, id)
	if err != nil {
		return scOrderDTO{}, err
	}
	for _, t := range txns {
		d := scTxnDTO{ID: t.ID, Kind: t.Kind, DocNo: t.DocNo, DocDate: t.DocDate.Format(time.DateOnly), Qty: t.Qty,
			ProcessingFee: t.ProcessingFee, OutputLotNo: t.OutputLotNo, OutputExpiry: dateStr(t.OutputExpiry),
			OutputBinCode: t.OutputBinCode, Note: t.Note, Status: t.Status, CreatedByName: t.CreatedByName, CreatedAt: t.CreatedAt,
			VoidedByName: t.VoidedByName, VoidedAt: t.VoidedAt, Lines: []scTxnLineDTO{}, OutputLots: []lotUsedDTO{}}
		tl, err := q.ListSubcontractTxnLines(ctx, t.ID)
		if err != nil {
			return scOrderDTO{}, err
		}
		for _, l := range tl {
			d.Lines = append(d.Lines, scTxnLineDTO{LineNo: l.LineNo, ItemID: l.ItemID, ItemCode: l.ItemCode, ItemName: l.ItemName,
				UnitName: l.UnitName, Qty: l.Qty, LotNo: l.LotNo, BinCode: l.BinCode, Lots: []lotUsedDTO{}})
		}
		if t.Kind == kindReceive && t.Status == "posted" {
			used, err := q.ListSubcontractTxnLots(ctx, t.ID)
			if err != nil {
				return scOrderDTO{}, err
			}
			for _, u := range used {
				lot := lotUsedDTO{LotNo: u.LotNo, ExpiryDate: dateStr(u.ExpiryDate), Qty: u.Qty.Abs()}
				if u.SourceType == srcScRecvReceipt {
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
		out.Txns = append(out.Txns, d)
	}
	return out, nil
}

// ---- 列表與單筆 ----

type scListDTO struct {
	ID           int64           `json:"id"`
	DocNo        string          `json:"doc_no"`
	DocDate      string          `json:"doc_date"`
	Status       string          `json:"status"`
	SupplierCode string          `json:"supplier_code"`
	SupplierName string          `json:"supplier_name"`
	ItemCode     string          `json:"item_code"`
	ItemName     string          `json:"item_name"`
	UnitName     string          `json:"unit_name"`
	PlanQty      decimal.Decimal `json:"plan_qty"`
	ReceivedQty  decimal.Decimal `json:"received_qty"`
	UnitFee      decimal.Decimal `json:"unit_fee"`
	DueDate      *string         `json:"due_date"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

func (m *Module) listSubcontracts(c *gin.Context) {
	ctx := c.Request.Context()
	supplierID, err := httpx.QueryInt64(c, "supplier_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	status := httpx.QueryString(c, "status")
	if status != nil && *status != "open" && *status != "closed" && *status != "voided" {
		response.Error(c, fieldErr("status", "狀態不正確"))
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	keyword := httpx.QueryString(c, "keyword")
	rows, err := m.store.ListSubcontractOrders(ctx, db.ListSubcontractOrdersParams{CompanyID: companyID, Status: status,
		SupplierID: supplierID, Keyword: keyword, Lim: pg.Limit(), Off: pg.Offset()})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountSubcontractOrders(ctx, db.CountSubcontractOrdersParams{CompanyID: companyID, Status: status,
		SupplierID: supplierID, Keyword: keyword})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]scListDTO, len(rows))
	for i, r := range rows {
		out[i] = scListDTO{ID: r.ID, DocNo: r.DocNo, DocDate: r.DocDate.Format(time.DateOnly), Status: r.Status,
			SupplierCode: r.SupplierCode, SupplierName: r.SupplierName, ItemCode: r.ItemCode, ItemName: r.ItemName, UnitName: r.UnitName,
			PlanQty: r.PlanQty, ReceivedQty: r.ReceivedQty, UnitFee: r.UnitFee, DueDate: dateStr(r.DueDate), UpdatedAt: r.UpdatedAt}
	}
	response.List(c, out, pg.Meta(total))
}

func (m *Module) getSubcontract(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	dto, err := loadScOrder(c.Request.Context(), m.store.Queries, actor(c).CompanyID, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 驗證 ----

func feeOK(f decimal.Decimal) bool {
	return !f.IsNegative() && f.Exponent() >= -2 && !f.GreaterThan(decimal.NewFromInt(maxProcessing))
}

// checkMaterials 驗證材料明細:商品類、啟用、不是成品自己、數量格式、不重複(同批號)。回傳逐行錯誤。
func checkMaterials(ctx context.Context, q *db.Queries, companyID, outItem int64, lines []woLineInput, withLot bool) (map[string]string, error) {
	ids := make([]int64, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.ItemID)
	}
	items, err := q.ListProductionItems(ctx, db.ListProductionItemsParams{CompanyID: companyID, Ids: ids})
	if err != nil {
		return nil, err
	}
	byID := map[int64]db.ListProductionItemsRow{}
	for _, it := range items {
		byID[it.ID] = it
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
		case l.ItemID == outItem:
			fields[key] = "材料不可是成品自己"
		case !qtyOK(l.Qty):
			fields[key] = "數量須大於 0,最多 4 位小數"
		}
		dup := fmt.Sprintf("%d", l.ItemID)
		if withLot {
			dup += "/" + strings.ToUpper(l.LotNo)
		}
		if seen[dup] && fields[key] == "" {
			fields[key] = "同一材料重複,請合併數量"
		}
		seen[dup] = true
	}
	return fields, nil
}

// checkMoves 發料 / 退料 / 收回的明細驗證與批號、儲位整理。
//   - 發料:fromWH 發料倉(儲位為來源儲位),toWH 委外倉
//   - 退料:fromWH 委外倉,toWH 回料倉(明細的儲位為回料倉的入庫儲位)
//   - 收回:fromWH 委外倉(toWH 為 nil);out 為成品入庫的批號、效期與儲位
//
// 有批號管理的材料批號可空白(先到期先出)。
type outInput struct {
	itemID, whID int64
	lotNo, bin   string
	expiry       *time.Time
}

type outChecked struct {
	lotNo  string
	expiry *time.Time
	bin    string
}

func checkMoves(ctx context.Context, q *db.Queries, companyID, outItem int64, lines []woLineInput, fromWH int64, toWH *int64, binIsDest bool, out *outInput) ([]woLineInput, outChecked, error) {
	var oc outChecked
	if len(lines) == 0 && out == nil {
		return nil, oc, fieldErr("lines", "請輸入材料明細")
	}
	if len(lines) > maxScLines {
		return nil, oc, fieldErr("lines", fmt.Sprintf("材料明細最多 %d 筆", maxScLines))
	}
	fields, err := checkMaterials(ctx, q, companyID, outItem, lines, true)
	if err != nil {
		return nil, oc, err
	}
	if len(fields) > 0 {
		return nil, oc, apperr.Validation(fields)
	}
	lotIn := make([]inventory.LotInput, 0, len(lines)+1)
	for _, l := range lines {
		lotIn = append(lotIn, inventory.LotInput{ItemID: l.ItemID, LotNo: l.LotNo})
	}
	if out != nil {
		lotIn = append(lotIn, inventory.LotInput{ItemID: out.itemID, LotNo: out.lotNo, Expiry: out.expiry, Inbound: true})
	}
	checked, lotErrs, err := inventory.CheckLotInputs(ctx, q, companyID, lotIn)
	if err != nil {
		return nil, oc, err
	}
	for i, msg := range lotErrs {
		if i == len(lines) {
			fields["output_lot_no"] = msg
		} else {
			fields[fmt.Sprintf("lines.%d", i)] = msg
		}
	}
	if len(fields) > 0 {
		return nil, oc, apperr.Validation(fields)
	}
	for i := range lines {
		lines[i].LotNo = checked[i].LotNo
	}
	if out != nil {
		oc.lotNo, oc.expiry = checked[len(lines)].LotNo, checked[len(lines)].Expiry
	}
	bins := make([]inventory.BinInput, 0, len(lines)+1)
	for _, l := range lines {
		b := inventory.BinInput{ItemID: l.ItemID, WarehouseID: fromWH}
		switch {
		case toWH != nil && binIsDest:
			b.ToWarehouseID, b.ToBinCode = toWH, l.BinCode
		case toWH != nil:
			b.BinCode, b.ToWarehouseID = l.BinCode, toWH
		case out != nil:
			b.BinCode = "" // 委外倉不使用儲位
		}
		bins = append(bins, b)
	}
	if out != nil {
		bins = append(bins, inventory.BinInput{ItemID: out.itemID, WarehouseID: out.whID, BinCode: out.bin, Inbound: true})
	}
	codes, toCodes, binErrs, err := inventory.CheckBinInputs(ctx, q, companyID, bins)
	if err != nil {
		return nil, oc, err
	}
	for i, msg := range binErrs {
		if out != nil && i == len(lines) {
			fields["output_bin_code"] = msg
		} else {
			fields[fmt.Sprintf("lines.%d", i)] = msg
		}
	}
	if len(fields) > 0 {
		return nil, oc, apperr.Validation(fields)
	}
	for i := range lines {
		if binIsDest {
			lines[i].BinCode = toCodes[i]
		} else {
			lines[i].BinCode = codes[i]
		}
	}
	if out != nil {
		oc.bin = codes[len(lines)]
	}
	return lines, oc, nil
}

// normalWarehouses 檢查倉庫存在、啟用且是一般倉(委外倉不能當發料倉或收回倉)。
func normalWarehouses(ctx context.Context, q *db.Queries, companyID int64, issueID, receiveID int64) error {
	whs, err := q.ListWarehouseFlags(ctx, db.ListWarehouseFlagsParams{CompanyID: companyID, Ids: []int64{issueID, receiveID}})
	if err != nil {
		return err
	}
	ok := map[int64]bool{}
	for _, w := range whs {
		ok[w.ID] = w.IsActive && w.Kind == "normal"
	}
	if !ok[issueID] {
		return fieldErr("issue_warehouse_id", "發料倉不存在、已停用或是委外倉")
	}
	if !ok[receiveID] {
		return fieldErr("receive_warehouse_id", "收回倉不存在、已停用或是委外倉")
	}
	return nil
}

// supplierWarehouse 取得(沒有就建立)加工廠的委外倉。
func supplierWarehouse(ctx context.Context, q *db.Queries, companyID, actorID int64, sup db.Supplier) (db.Warehouse, error) {
	w, err := q.SubcontractWarehouseOf(ctx, db.SubcontractWarehouseOfParams{CompanyID: companyID, SupplierID: &sup.ID})
	if err == nil {
		if !w.IsActive {
			return w, fieldErr("supplier_id", "這個加工廠的委外倉已停用,請先到倉庫維護啟用")
		}
		return w, nil
	}
	if !database.IsNoRows(err) {
		return w, err
	}
	code := "WP-" + sup.Code
	if utf8.RuneCountInString(code) > 20 {
		code = string([]rune(code)[:20])
	}
	name := "委外倉-" + sup.Name
	if utf8.RuneCountInString(name) > 100 {
		name = string([]rune(name)[:100])
	}
	w, err = q.CreateSubcontractWarehouse(ctx, db.CreateSubcontractWarehouseParams{CompanyID: companyID, Code: code, Name: name,
		SupplierID: &sup.ID, ActorID: &actorID})
	if database.IsUniqueViolation(err, "warehouses_company_code_key") {
		return w, fieldErr("supplier_id", "委外倉代碼 "+code+" 已被其他倉庫使用,請先改掉那個倉庫的代碼")
	}
	return w, err
}

// ---- 建立 / 修改 ----

type scInput struct {
	DocDate            string          `json:"doc_date" binding:"required"`
	SupplierID         int64           `json:"supplier_id" binding:"required"`
	ItemID             int64           `json:"item_id" binding:"required"`
	PlanQty            decimal.Decimal `json:"plan_qty"`
	UnitFee            decimal.Decimal `json:"unit_fee"`
	IssueWarehouseID   int64           `json:"issue_warehouse_id" binding:"required"`
	ReceiveWarehouseID int64           `json:"receive_warehouse_id" binding:"required"`
	DueDate            *string         `json:"due_date"`
	Note               string          `json:"note" binding:"max=1000"`
	Lines              []woLineInput   `json:"lines" binding:"dive"` // 預計用量;空白 = 依 BOM 展開(僅建立時)
}

type scPrepared struct {
	date  time.Time
	due   *time.Time
	lines []woLineInput
}

func prepareScOrder(ctx context.Context, q *db.Queries, companyID int64, in *scInput, generate bool) (scPrepared, error) {
	var p scPrepared
	var err error
	if p.date, err = trade.ParseDate("doc_date", in.DocDate); err != nil {
		return p, err
	}
	if p.due, err = trade.OptionalInputDate("due_date", in.DueDate); err != nil {
		return p, err
	}
	if !qtyOK(in.PlanQty) {
		return p, fieldErr("plan_qty", "數量須大於 0,最多 4 位小數")
	}
	if in.UnitFee.IsNegative() || in.UnitFee.Exponent() < -4 || in.UnitFee.GreaterThan(decimal.NewFromInt(maxProcessing)) {
		return p, fieldErr("unit_fee", "加工費單價須為 0 以上,最多 4 位小數")
	}
	if err := normalWarehouses(ctx, q, companyID, in.IssueWarehouseID, in.ReceiveWarehouseID); err != nil {
		return p, err
	}
	its, err := q.ListProductionItems(ctx, db.ListProductionItemsParams{CompanyID: companyID, Ids: []int64{in.ItemID}})
	if err != nil {
		return p, err
	}
	if len(its) != 1 || its[0].ItemType != "goods" || !its[0].IsActive {
		return p, fieldErr("item_id", "成品須為啟用的商品類料品")
	}
	lines := in.Lines
	if len(lines) == 0 {
		if !generate {
			return p, fieldErr("lines", "請輸入預計材料用量")
		}
		exp, err := explodeLines(ctx, q, companyID, in.ItemID, in.PlanQty, p.date)
		if err != nil {
			return p, err
		}
		for _, l := range exp {
			lines = append(lines, woLineInput{ItemID: l.ItemID, Qty: l.Qty})
		}
	}
	if len(lines) > maxScLines {
		return p, fieldErr("lines", fmt.Sprintf("材料明細最多 %d 筆", maxScLines))
	}
	fields, err := checkMaterials(ctx, q, companyID, in.ItemID, lines, false)
	if err != nil {
		return p, err
	}
	if len(fields) > 0 {
		return p, apperr.Validation(fields)
	}
	p.lines = lines
	return p, nil
}

func saveScLines(ctx context.Context, q *db.Queries, id int64, lines []woLineInput) error {
	if err := q.DeleteSubcontractOrderLines(ctx, id); err != nil {
		return err
	}
	for i, l := range lines {
		if err := q.AddSubcontractOrderLine(ctx, db.AddSubcontractOrderLineParams{OrderID: id, LineNo: int32(i + 1), ItemID: l.ItemID,
			Qty: l.Qty, Note: l.Note}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) createSubcontract(c *gin.Context) {
	var in scInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	var out scOrderDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		sup, err := q.GetSupplier(ctx, db.GetSupplierParams{ID: in.SupplierID, CompanyID: a.CompanyID})
		if database.IsNoRows(err) || (err == nil && !sup.IsActive) {
			return fieldErr("supplier_id", "加工廠不存在或已停用")
		}
		if err != nil {
			return err
		}
		p, err := prepareScOrder(ctx, q, a.CompanyID, &in, true)
		if err != nil {
			return err
		}
		wh, err := supplierWarehouse(ctx, q, a.CompanyID, a.UserID, sup)
		if err != nil {
			return err
		}
		no, err := docno.Next(ctx, q, a.CompanyID, scOrderType, p.date)
		if err != nil {
			return err
		}
		o, err := q.CreateSubcontractOrder(ctx, db.CreateSubcontractOrderParams{CompanyID: a.CompanyID, DocNo: no, DocDate: p.date,
			SupplierID: sup.ID, ItemID: in.ItemID, PlanQty: in.PlanQty, UnitFee: in.UnitFee, IssueWarehouseID: in.IssueWarehouseID,
			ReceiveWarehouseID: in.ReceiveWarehouseID, ScWarehouseID: wh.ID, DueDate: p.due, Note: in.Note, ActorID: &a.UserID})
		if err != nil {
			return err
		}
		if err := saveScLines(ctx, q, o.ID, p.lines); err != nil {
			return err
		}
		if out, err = loadScOrder(ctx, q, a.CompanyID, o.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: "create", EntityType: scOrderType, EntityID: &o.ID,
			Summary: "新增委外加工單 " + o.DocNo + "(" + sup.Name + ")"})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, out)
}

type scUpdateInput struct {
	scInput
	Version int32 `json:"version" binding:"required"`
}

func (m *Module) updateSubcontract(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in scUpdateInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	var out scOrderDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockSubcontractOrder(ctx, db.LockSubcontractOrderParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Version != in.Version {
			return apperr.ErrVersionConflict
		}
		if cur.Status != "open" {
			return errScNotOpen
		}
		if in.SupplierID != cur.SupplierID {
			return fieldErr("supplier_id", "建立後不可更換加工廠")
		}
		if in.ItemID != cur.ItemID {
			return fieldErr("item_id", "建立後不可更換成品")
		}
		n, err := q.CountSubcontractTxns(ctx, id)
		if err != nil {
			return err
		}
		if n > 0 && (in.IssueWarehouseID != cur.IssueWarehouseID || in.ReceiveWarehouseID != cur.ReceiveWarehouseID) {
			return fieldErr("issue_warehouse_id", "已有發料 / 收回紀錄,不可更換倉庫")
		}
		p, err := prepareScOrder(ctx, q, a.CompanyID, &in.scInput, false)
		if err != nil {
			return err
		}
		received, err := q.SubcontractReceivedQty(ctx, id)
		if err != nil {
			return err
		}
		if in.PlanQty.LessThan(received) {
			return fieldErr("plan_qty", "預計數量不可少於已收回的 "+received.String())
		}
		// 已發出去而還沒耗用或退回的材料不可從預計用量拿掉,否則畫面上看不到它還在加工廠
		held, err := heldByItem(ctx, q, id)
		if err != nil {
			return err
		}
		inPlan := map[int64]bool{}
		for _, l := range p.lines {
			inPlan[l.ItemID] = true
		}
		for item, qty := range held {
			if !qty.IsZero() && !inPlan[item] {
				return fieldErr("lines", "還有材料在加工廠(已發料未耗用或退回),不可從預計用量移除,請先退料")
			}
		}
		if _, err := q.UpdateSubcontractOrderHeader(ctx, db.UpdateSubcontractOrderHeaderParams{ID: id, CompanyID: a.CompanyID,
			Version: in.Version, DocDate: p.date, PlanQty: in.PlanQty, UnitFee: in.UnitFee, IssueWarehouseID: in.IssueWarehouseID,
			ReceiveWarehouseID: in.ReceiveWarehouseID, DueDate: p.due, Note: in.Note, ActorID: a.UserID}); err != nil {
			if database.IsNoRows(err) {
				return apperr.ErrVersionConflict
			}
			return err
		}
		if err := saveScLines(ctx, q, id, p.lines); err != nil {
			return err
		}
		if out, err = loadScOrder(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: "update", EntityType: scOrderType, EntityID: &id,
			Summary: "修改委外加工單 " + cur.DocNo})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, out)
}

// heldByItem 委外單目前還在加工廠的材料量。
func heldByItem(ctx context.Context, q *db.Queries, orderID int64) (map[int64]decimal.Decimal, error) {
	rows, err := q.SubcontractHeldQty(ctx, orderID)
	if err != nil {
		return nil, err
	}
	out := map[int64]decimal.Decimal{}
	for _, r := range rows {
		out[r.ItemID] = r.Qty
	}
	return out, nil
}

func allZero(m map[int64]decimal.Decimal) bool {
	for _, v := range m {
		if !v.IsZero() {
			return false
		}
	}
	return true
}

// autoClose 累計收回達預計數量、且材料都已耗用或退回時自動結案。
func autoClose(ctx context.Context, q *db.Queries, o db.SubcontractOrder, actorID int64) error {
	if o.Status != "open" {
		return nil
	}
	received, err := q.SubcontractReceivedQty(ctx, o.ID)
	if err != nil || received.LessThan(o.PlanQty) {
		return err
	}
	held, err := heldByItem(ctx, q, o.ID)
	if err != nil || !allZero(held) {
		return err
	}
	return q.SetSubcontractOrderStatus(ctx, db.SetSubcontractOrderStatusParams{ID: o.ID, Status: "closed", ActorID: actorID})
}

// ---- 發料 / 退料 ----

type scMaterialInput struct {
	DocDate string        `json:"doc_date" binding:"required"`
	Note    string        `json:"note" binding:"max=255"`
	Lines   []woLineInput `json:"lines" binding:"dive"`
}

func (m *Module) postScMaterial(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := httpx.ParamID(c, "id")
		if err != nil {
			response.Error(c, err)
			return
		}
		var in scMaterialInput
		if err := httpx.BindJSON(c, &in); err != nil {
			response.Error(c, err)
			return
		}
		a := actor(c)
		ctx := c.Request.Context()
		var out scOrderDTO
		err = m.store.InTx(ctx, func(q *db.Queries) error {
			cur, err := q.LockSubcontractOrder(ctx, db.LockSubcontractOrderParams{ID: id, CompanyID: a.CompanyID})
			if database.IsNoRows(err) {
				return apperr.ErrNotFound
			}
			if err != nil {
				return err
			}
			if cur.Status != "open" {
				return errScNotOpen
			}
			date, err := trade.ParseDate("doc_date", in.DocDate)
			if err != nil {
				return err
			}
			if date.Before(cur.DocDate) {
				return fieldErr("doc_date", "日期不可早於委外單日期")
			}
			from, to, src := cur.IssueWarehouseID, cur.ScWarehouseID, srcScIssue
			if kind == kindReturn {
				from, to, src = cur.ScWarehouseID, cur.IssueWarehouseID, srcScReturn
			}
			lines, _, err := checkMoves(ctx, q, a.CompanyID, cur.ItemID, in.Lines, from, &to, kind == kindReturn, nil)
			if err != nil {
				return err
			}
			if kind == kindReturn {
				held, err := heldByItem(ctx, q, id)
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
						fields[fmt.Sprintf("lines.%d", i)] = "退料量超過還在加工廠的 " + h.String()
					}
					left[l.ItemID] = h.Sub(l.Qty)
				}
				if len(fields) > 0 {
					return apperr.Validation(fields)
				}
			}
			no, err := docno.Next(ctx, q, a.CompanyID, scKinds[kind].numbering, date)
			if err != nil {
				return err
			}
			txn, err := q.CreateSubcontractTxn(ctx, db.CreateSubcontractTxnParams{CompanyID: a.CompanyID, OrderID: id, Kind: kind,
				DocNo: no, DocDate: date, Note: in.Note, ActorID: &a.UserID})
			if err != nil {
				return err
			}
			moves := make([]inventory.Movement, 0, len(lines))
			for i, l := range lines {
				if err := q.AddSubcontractTxnLine(ctx, db.AddSubcontractTxnLineParams{TxnID: txn.ID, LineNo: int32(i + 1), ItemID: l.ItemID,
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
				if err := autoClose(ctx, q, cur, a.UserID); err != nil {
					return err
				}
			}
			if out, err = loadScOrder(ctx, q, a.CompanyID, id); err != nil {
				return err
			}
			return audit.Record(ctx, q, audit.Entry{Action: kind, EntityType: scOrderType, EntityID: &id,
				Summary: "委外單 " + cur.DocNo + scKinds[kind].label + " " + txn.DocNo})
		})
		if err != nil {
			response.Error(c, err)
			return
		}
		response.Created(c, out)
	}
}

// ---- 收回 ----

type scPreviewLineDTO struct {
	ItemID   int64           `json:"item_id"`
	ItemCode string          `json:"item_code"`
	ItemName string          `json:"item_name"`
	UnitName string          `json:"unit_name"`
	Qty      decimal.Decimal `json:"qty"`      // 預設耗用量
	HeldQty  decimal.Decimal `json:"held_qty"` // 目前還在加工廠
}

type scPreviewDTO struct {
	Qty           decimal.Decimal    `json:"qty"`
	Remaining     decimal.Decimal    `json:"remaining"`
	ProcessingFee decimal.Decimal    `json:"processing_fee"`
	Lines         []scPreviewLineDTO `json:"lines"`
}

func (m *Module) subcontractReceivePreview(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	doc, err := loadScOrder(ctx, m.store.Queries, actor(c).CompanyID, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	remaining := doc.PlanQty.Sub(doc.ReceivedQty)
	qty := remaining
	if s := c.Query("qty"); s != "" {
		if qty, err = decimal.NewFromString(s); err != nil || !qtyOK(qty) {
			response.Error(c, fieldErr("qty", "數量須大於 0,最多 4 位小數"))
			return
		}
	}
	out := scPreviewDTO{Qty: qty, Remaining: remaining, ProcessingFee: doc.UnitFee.Mul(qty).Round(2), Lines: make([]scPreviewLineDTO, len(doc.Lines))}
	for i, l := range doc.Lines {
		out.Lines[i] = scPreviewLineDTO{ItemID: l.ItemID, ItemCode: l.ItemCode, ItemName: l.ItemName, UnitName: l.UnitName,
			Qty: prorate(l.Qty, qty, doc.PlanQty), HeldQty: l.HeldQty}
	}
	response.OK(c, out)
}

type scReceiveInput struct {
	DocDate       string           `json:"doc_date" binding:"required"`
	Qty           decimal.Decimal  `json:"qty"`
	ProcessingFee *decimal.Decimal `json:"processing_fee"` // 空白 = 預計單價 × 本次數量
	OutputLotNo   string           `json:"output_lot_no"`
	OutputBinCode string           `json:"output_bin_code"`
	OutputExpiry  *string          `json:"output_expiry_date"`
	Note          string           `json:"note" binding:"max=255"`
	Lines         []woLineInput    `json:"lines" binding:"dive"` // 實際耗料;空白 = 依預計用量比例
}

func (m *Module) receiveSubcontract(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in scReceiveInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	var out scOrderDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockSubcontractOrder(ctx, db.LockSubcontractOrderParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Status != "open" {
			return errScNotOpen
		}
		date, err := trade.ParseDate("doc_date", in.DocDate)
		if err != nil {
			return err
		}
		if date.Before(cur.DocDate) {
			return fieldErr("doc_date", "收回日期不可早於委外單日期")
		}
		expiry, err := trade.OptionalInputDate("output_expiry_date", in.OutputExpiry)
		if err != nil {
			return err
		}
		received, err := q.SubcontractReceivedQty(ctx, id)
		if err != nil {
			return err
		}
		remaining := cur.PlanQty.Sub(received)
		if !qtyOK(in.Qty) {
			return fieldErr("qty", "本次收回數量須大於 0,最多 4 位小數")
		}
		if in.Qty.GreaterThan(remaining) {
			return fieldErr("qty", "本次收回數量不可超過尚未收回的數量 "+remaining.String())
		}
		fee := cur.UnitFee.Mul(in.Qty).Round(2)
		if in.ProcessingFee != nil {
			fee = *in.ProcessingFee
		}
		if !feeOK(fee) {
			return fieldErr("processing_fee", "加工費須為 0 以上,最多 2 位小數")
		}
		lines := in.Lines
		if len(lines) == 0 {
			planned, err := q.ListSubcontractOrderLines(ctx, id)
			if err != nil {
				return err
			}
			for _, l := range planned {
				lines = append(lines, woLineInput{ItemID: l.ItemID, Qty: prorate(l.Qty, in.Qty, cur.PlanQty)})
			}
		}
		lines, oc, err := checkMoves(ctx, q, a.CompanyID, cur.ItemID, lines, cur.ScWarehouseID, nil, false,
			&outInput{itemID: cur.ItemID, whID: cur.ReceiveWarehouseID, lotNo: in.OutputLotNo, bin: in.OutputBinCode, expiry: expiry})
		if err != nil {
			return err
		}
		held, err := heldByItem(ctx, q, id)
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
				fields[fmt.Sprintf("lines.%d", i)] = "耗用量超過還在加工廠的 " + h.String() + ",請先補發料"
			}
			left[l.ItemID] = h.Sub(l.Qty)
		}
		if len(fields) > 0 {
			return apperr.Validation(fields)
		}
		no, err := docno.Next(ctx, q, a.CompanyID, scKinds[kindReceive].numbering, date)
		if err != nil {
			return err
		}
		txn, err := q.CreateSubcontractTxn(ctx, db.CreateSubcontractTxnParams{CompanyID: a.CompanyID, OrderID: id, Kind: kindReceive,
			DocNo: no, DocDate: date, Qty: in.Qty, ProcessingFee: fee, OutputLotNo: oc.lotNo, OutputExpiry: oc.expiry,
			OutputBinCode: oc.bin, Note: in.Note, ActorID: &a.UserID})
		if err != nil {
			return err
		}
		moves := make([]inventory.Movement, 0, len(lines))
		for i, l := range lines {
			if err := q.AddSubcontractTxnLine(ctx, db.AddSubcontractTxnLineParams{TxnID: txn.ID, LineNo: int32(i + 1), ItemID: l.ItemID,
				Qty: l.Qty, LotNo: l.LotNo}); err != nil {
				return err
			}
			moves = append(moves, inventory.Movement{ItemID: l.ItemID, WarehouseID: cur.ScWarehouseID, Qty: l.Qty.Neg(), LotNo: l.LotNo})
		}
		opt := inventory.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
		if err := inventory.Post(ctx, q, opt, inventory.Source{Type: srcScRecvIssue, ID: txn.ID, No: txn.DocNo, DocDate: txn.DocDate}, moves); err != nil {
			return err
		}
		if err := inventory.Post(ctx, q, opt, inventory.Source{Type: srcScRecvReceipt, ID: txn.ID, No: txn.DocNo, DocDate: txn.DocDate}, []inventory.Movement{{
			ItemID: cur.ItemID, WarehouseID: cur.ReceiveWarehouseID, Qty: in.Qty, LotNo: oc.lotNo, Expiry: oc.expiry, BinCode: oc.bin,
		}}); err != nil {
			return err
		}
		if err := postScFee(ctx, q, a.CompanyID, a.UserID, cur, txn); err != nil {
			return err
		}
		if err := autoClose(ctx, q, cur, a.UserID); err != nil {
			return err
		}
		if out, err = loadScOrder(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: kindReceive, EntityType: scOrderType, EntityID: &id,
			Summary: "委外單 " + cur.DocNo + " 收回 " + txn.DocNo + "(" + in.Qty.String() + ",加工費 " + fee.String() + ")"})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, out)
}

// postScFee 加工費轉應付與傳票:借 費用(月結時由「加工費轉出」轉入成品存貨)貸 應付帳款。以本位幣、含稅金額記帳。
func postScFee(ctx context.Context, q *db.Queries, companyID, actorID int64, o db.SubcontractOrder, t db.SubcontractTxn) error {
	if !t.ProcessingFee.IsPositive() {
		return nil
	}
	sup, err := q.GetSupplier(ctx, db.GetSupplierParams{ID: o.SupplierID, CompanyID: companyID})
	if err != nil {
		return err
	}
	due, err := trade.DueDate(ctx, q, companyID, sup.PaymentTermID, t.DocDate)
	if err != nil {
		return err
	}
	if err := finance.CreatePayable(ctx, q, db.InsertPayableParams{CompanyID: companyID, SupplierID: o.SupplierID, SourceType: srcScFee,
		SourceID: t.ID, SourceNo: t.DocNo, DocDate: t.DocDate, DueDate: due, Currency: masterdata.BaseCurrency,
		ExchangeRate: decimal.NewFromInt(1), Amount: t.ProcessingFee, BaseAmount: t.ProcessingFee, CreatedBy: &actorID}); err != nil {
		return err
	}
	return gl.PostSource(ctx, q, gl.Options{CompanyID: companyID, ActorID: &actorID},
		gl.Source{Type: srcScFee, ID: t.ID, No: t.DocNo, Date: t.DocDate, Desc: "委外加工費 " + t.DocNo},
		[]gl.Entry{
			{Key: "purchase.expense", Debit: t.ProcessingFee},
			{Key: "purchase.payable", Credit: t.ProcessingFee, SupplierID: &o.SupplierID},
		})
}

// ---- 作廢異動 / 結案 / 作廢整張 ----

func (m *Module) voidScTxn(c *gin.Context) {
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
	var out scOrderDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockSubcontractOrder(ctx, db.LockSubcontractOrderParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		t, err := q.LockSubcontractTxn(ctx, db.LockSubcontractTxnParams{ID: tid, OrderID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if t.Status != "posted" {
			return errScTxnVoided
		}
		opt := inventory.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
		mk := func(typ string) inventory.Source {
			return inventory.Source{Type: typ, ID: t.ID, No: t.DocNo, DocDate: t.DocDate}
		}
		switch t.Kind {
		case kindIssue:
			err = inventory.Reverse(ctx, q, opt, mk(srcScIssue))
		case kindReturn:
			err = inventory.Reverse(ctx, q, opt, mk(srcScReturn))
		default:
			// 先沖成品入庫(成品已被領用或出貨就會因庫存不足被擋),再沖耗料與加工費
			if err = inventory.Reverse(ctx, q, opt, mk(srcScRecvReceipt)); err != nil {
				return err
			}
			if err = inventory.Reverse(ctx, q, opt, mk(srcScRecvIssue)); err != nil {
				return err
			}
			if t.ProcessingFee.IsPositive() {
				if err = finance.RemovePayable(ctx, q, srcScFee, t.ID); err != nil {
					return err
				}
				err = gl.ReverseSource(ctx, q, gl.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}, srcScFee, t.ID)
			}
		}
		if err != nil {
			return err
		}
		if err := q.VoidSubcontractTxn(ctx, db.VoidSubcontractTxnParams{ID: tid, ActorID: &a.UserID}); err != nil {
			return err
		}
		held, err := heldByItem(ctx, q, id)
		if err != nil {
			return err
		}
		for _, h := range held {
			if h.IsNegative() {
				return errScHeldNeg
			}
		}
		if cur.Status == "closed" {
			if err := q.SetSubcontractOrderStatus(ctx, db.SetSubcontractOrderStatusParams{ID: id, Status: "open", ActorID: a.UserID}); err != nil {
				return err
			}
		}
		if out, err = loadScOrder(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: "void_" + t.Kind, EntityType: scOrderType, EntityID: &id,
			Summary: "委外單 " + cur.DocNo + " 作廢" + scKinds[t.Kind].label + " " + t.DocNo})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, out)
}

type scActionInput struct {
	Version int32 `json:"version" binding:"required"`
}

// closeSubcontract 結案(短交):至少收回一次,且材料都已耗用或退回。
func (m *Module) closeSubcontract(c *gin.Context) {
	m.scStatusChange(c, "closed")
}

// voidSubcontract 作廢整張委外單:沒有任何有效的發料 / 收回 / 退料才行。
func (m *Module) voidSubcontract(c *gin.Context) {
	m.scStatusChange(c, "voided")
}

func (m *Module) scStatusChange(c *gin.Context, status string) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in scActionInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	var out scOrderDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockSubcontractOrder(ctx, db.LockSubcontractOrderParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Version != in.Version {
			return apperr.ErrVersionConflict
		}
		if cur.Status != "open" {
			return errScNotOpen
		}
		label := "短交結案"
		if status == "closed" {
			received, err := q.SubcontractReceivedQty(ctx, id)
			if err != nil {
				return err
			}
			if !received.IsPositive() {
				return errScNothingBack
			}
			held, err := heldByItem(ctx, q, id)
			if err != nil {
				return err
			}
			if !allZero(held) {
				return errScHeldLeft
			}
		} else {
			label = "作廢"
			n, err := q.CountSubcontractTxns(ctx, id)
			if err != nil {
				return err
			}
			if n > 0 {
				return errScHasTxns
			}
		}
		if err := q.SetSubcontractOrderStatus(ctx, db.SetSubcontractOrderStatusParams{ID: id, Status: status, ActorID: a.UserID}); err != nil {
			return err
		}
		if out, err = loadScOrder(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: map[string]string{"closed": "close", "voided": "void"}[status],
			EntityType: scOrderType, EntityID: &id, Summary: "委外單 " + cur.DocNo + " " + label})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, out)
}
