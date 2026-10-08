package inventory

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/auth"
	"erp/internal/db"
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
)

const (
	TypeAdjustment = "adjustment"
	TypeTransfer   = "transfer"
	TypeCount      = "count"
	maxLines       = 500
)

// 單據類型 → 單號規則、流水帳來源類型、中文名稱
var docTypes = map[string]struct{ numbering, source, label string }{
	TypeAdjustment: {"stock_adjustment", "stock_adjustment", "庫存調整單"},
	TypeTransfer:   {"stock_transfer", "stock_transfer", "調撥單"},
	TypeCount:      {"stock_count", "stock_count", "盤點單"},
}

var (
	errNoLines        = apperr.New(http.StatusUnprocessableEntity, "STK-001", "單據沒有明細")
	errNotCounted     = apperr.New(http.StatusUnprocessableEntity, "STK-002", "還有未盤點的明細,請填入實盤數量")
	errEditNotAllowed = apperr.New(http.StatusConflict, "STK-003", "只有草稿可以修改")
	errWarehouseFixed = fieldErr("warehouse_id", "建立後不可變更倉庫")
	// 刪掉快照中的料品等於不盤它,盤虧就會被藏起來
	errCountLineRemoved = apperr.New(http.StatusUnprocessableEntity, "STK-006", "盤點明細只能新增,不可刪除")
)

func fieldErr(field, msg string) *apperr.Error {
	return apperr.Validation(map[string]string{field: msg})
}

type Module struct {
	store *database.Store
}

func New(store *database.Store) *Module { return &Module{store: store} }

// Register 掛上 /inventory 路由;r 須已套用 auth.Authenticate。
func (m *Module) Register(r *gin.RouterGroup) {
	g := r.Group("/inventory")
	read := auth.Require(permission.InventoryRead, permission.InventoryWrite, permission.InventoryApprove, permission.InventoryPost)
	g.GET("/balances", read, m.listBalances)
	g.GET("/movement-summary", read, m.movementSummary)
	g.GET("/items/:id/ledger", read, m.itemLedger)
	g.GET("/documents", read, m.listDocuments)
	g.GET("/documents/:id", read, m.getDocument)
	g.POST("/documents", auth.Require(permission.InventoryWrite), m.createDocument)
	g.PUT("/documents/:id", auth.Require(permission.InventoryWrite), m.updateDocument)
	// 各動作的權限在 handler 內依動作判斷
	g.POST("/documents/:id/actions/:action", read, m.documentAction)
}

func actor(c *gin.Context) *authctx.Actor { return authctx.ActorFrom(c.Request.Context()) }

// ---- DTO ----

type lineDTO struct {
	ID           int64            `json:"id"`
	LineNo       int32            `json:"line_no"`
	ItemID       int64            `json:"item_id"`
	ItemCode     string           `json:"item_code"`
	ItemName     string           `json:"item_name"`
	ItemSpec     string           `json:"item_spec"`
	UnitID       int64            `json:"unit_id"`
	UnitName     string           `json:"unit_name"`
	BaseUnitName string           `json:"base_unit_name"`
	Qty          *decimal.Decimal `json:"qty"`
	Factor       decimal.Decimal  `json:"factor"`
	BaseQty      *decimal.Decimal `json:"base_qty"`
	SystemQty    *decimal.Decimal `json:"system_qty"`
	DiffQty      *decimal.Decimal `json:"diff_qty,omitempty"` // 盤點差異 = 實盤 − 帳面
	Note         string           `json:"note"`
}

type documentDTO struct {
	ID              int64      `json:"id"`
	DocType         string     `json:"doc_type"`
	DocNo           string     `json:"doc_no"`
	DocDate         string     `json:"doc_date"`
	WarehouseID     int64      `json:"warehouse_id"`
	WarehouseName   string     `json:"warehouse_name"`
	ToWarehouseID   *int64     `json:"to_warehouse_id"`
	ToWarehouseName *string    `json:"to_warehouse_name"`
	CategoryID      *int64     `json:"category_id"`
	CategoryName    *string    `json:"category_name"`
	Status          string     `json:"status"`
	Note            string     `json:"note"`
	CreatedByName   *string    `json:"created_by_name"`
	SubmittedByName *string    `json:"submitted_by_name"`
	SubmittedAt     *time.Time `json:"submitted_at"`
	ApprovedByName  *string    `json:"approved_by_name"`
	ApprovedAt      *time.Time `json:"approved_at"`
	PostedByName    *string    `json:"posted_by_name"`
	PostedAt        *time.Time `json:"posted_at"`
	Lines           []lineDTO  `json:"lines"`
	Version         int32      `json:"version"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (m *Module) loadDocument(ctx context.Context, q *db.Queries, companyID, id int64) (documentDTO, error) {
	d, err := q.GetStockDocument(ctx, db.GetStockDocumentParams{ID: id, CompanyID: companyID})
	if database.IsNoRows(err) {
		return documentDTO{}, apperr.ErrNotFound
	}
	if err != nil {
		return documentDTO{}, err
	}
	rows, err := q.ListStockDocumentLines(ctx, id)
	if err != nil {
		return documentDTO{}, err
	}
	lines := make([]lineDTO, len(rows))
	for i, r := range rows {
		l := lineDTO{
			ID: r.ID, LineNo: r.LineNo, ItemID: r.ItemID, ItemCode: r.ItemCode, ItemName: r.ItemName,
			ItemSpec: r.ItemSpec, UnitID: r.UnitID, UnitName: r.UnitName, BaseUnitName: r.BaseUnitName,
			Qty: r.Qty, Factor: r.Factor, BaseQty: r.BaseQty, SystemQty: r.SystemQty, Note: r.Note,
		}
		if d.DocType == TypeCount && r.BaseQty != nil && r.SystemQty != nil {
			diff := r.BaseQty.Sub(*r.SystemQty)
			l.DiffQty = &diff
		}
		lines[i] = l
	}
	return documentDTO{
		ID: d.ID, DocType: d.DocType, DocNo: d.DocNo, DocDate: d.DocDate.Format(time.DateOnly),
		WarehouseID: d.WarehouseID, WarehouseName: d.WarehouseName, ToWarehouseID: d.ToWarehouseID,
		ToWarehouseName: d.ToWarehouseName, CategoryID: d.CategoryID, CategoryName: d.CategoryName,
		Status: d.Status, Note: d.Note, CreatedByName: d.CreatedByName, SubmittedByName: d.SubmittedByName,
		SubmittedAt: d.SubmittedAt, ApprovedByName: d.ApprovedByName, ApprovedAt: d.ApprovedAt,
		PostedByName: d.PostedByName, PostedAt: d.PostedAt, Lines: lines, Version: d.Version, UpdatedAt: d.UpdatedAt,
	}, nil
}

// ---- 列表與單筆 ----

type documentListDTO struct {
	ID              int64     `json:"id"`
	DocType         string    `json:"doc_type"`
	DocNo           string    `json:"doc_no"`
	DocDate         string    `json:"doc_date"`
	WarehouseName   string    `json:"warehouse_name"`
	ToWarehouseName *string   `json:"to_warehouse_name"`
	Status          string    `json:"status"`
	Note            string    `json:"note"`
	LineCount       int64     `json:"line_count"`
	CreatedByName   *string   `json:"created_by_name"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func optionalDate(c *gin.Context, name string) (*time.Time, error) {
	s := c.Query(name)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, fieldErr(name, "日期格式須為 YYYY-MM-DD")
	}
	return &t, nil
}

func (m *Module) listDocuments(c *gin.Context) {
	ctx := c.Request.Context()
	whID, err := httpx.QueryInt64(c, "warehouse_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	from, err := optionalDate(c, "from")
	if err != nil {
		response.Error(c, err)
		return
	}
	to, err := optionalDate(c, "to")
	if err != nil {
		response.Error(c, err)
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	docType, status, keyword := httpx.QueryString(c, "doc_type"), httpx.QueryString(c, "status"), httpx.QueryString(c, "keyword")
	rows, err := m.store.ListStockDocuments(ctx, db.ListStockDocumentsParams{
		CompanyID: companyID, DocType: docType, Status: status, WarehouseID: whID, Keyword: keyword,
		FromDate: from, ToDate: to, Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountStockDocuments(ctx, db.CountStockDocumentsParams{
		CompanyID: companyID, DocType: docType, Status: status, WarehouseID: whID, Keyword: keyword,
		FromDate: from, ToDate: to,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]documentListDTO, len(rows))
	for i, r := range rows {
		out[i] = documentListDTO{
			ID: r.ID, DocType: r.DocType, DocNo: r.DocNo, DocDate: r.DocDate.Format(time.DateOnly),
			WarehouseName: r.WarehouseName, ToWarehouseName: r.ToWarehouseName, Status: r.Status, Note: r.Note,
			LineCount: r.LineCount, CreatedByName: r.CreatedByName, UpdatedAt: r.UpdatedAt,
		}
	}
	response.List(c, out, pg.Meta(total))
}

func (m *Module) getDocument(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	dto, err := m.loadDocument(c.Request.Context(), m.store.Queries, actor(c).CompanyID, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 建立與修改(草稿) ----

type lineInput struct {
	ItemID int64            `json:"item_id" binding:"required"`
	UnitID int64            `json:"unit_id" binding:"required"`
	Qty    *decimal.Decimal `json:"qty"` // 盤點未盤可為 null
	Note   string           `json:"note" binding:"max=255"`
}

type documentInput struct {
	DocType       string      `json:"doc_type" binding:"required,oneof=adjustment transfer count"`
	DocDate       string      `json:"doc_date" binding:"required"`
	WarehouseID   int64       `json:"warehouse_id" binding:"required"`
	ToWarehouseID *int64      `json:"to_warehouse_id"`
	CategoryID    *int64      `json:"category_id"`
	Note          string      `json:"note" binding:"max=2000"`
	Lines         []lineInput `json:"lines" binding:"dive"`
	Version       int32       `json:"version"`
}

// preparedLine 已驗證並換算成基本單位的明細。
type preparedLine struct {
	lineInput
	factor    decimal.Decimal
	baseQty   *decimal.Decimal
	systemQty *decimal.Decimal
}

// prepareLines 驗證明細並換算基本單位數量。
// 調整:數量 ≠ 0(可負);調撥:數量 > 0;盤點:實盤 ≥ 0 或未盤(null),且只能用基本單位。
func prepareLines(ctx context.Context, q *db.Queries, companyID int64, docType string, lines []lineInput) ([]preparedLine, error) {
	if len(lines) > maxLines {
		return nil, fieldErr("lines", fmt.Sprintf("明細最多 %d 筆", maxLines))
	}
	ids := make([]int64, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.ItemID)
	}
	items, err := q.ListStockItems(ctx, db.ListStockItemsParams{CompanyID: companyID, Ids: ids})
	if err != nil {
		return nil, err
	}
	itemByID := map[int64]db.ListStockItemsRow{}
	for _, it := range items {
		itemByID[it.ID] = it
	}
	factors, err := q.ListItemUnitFactors(ctx, ids)
	if err != nil {
		return nil, err
	}
	factorOf := map[[2]int64]decimal.Decimal{}
	for _, f := range factors {
		factorOf[[2]int64{f.ItemID, f.UnitID}] = f.Factor
	}

	fields := map[string]string{}
	out := make([]preparedLine, len(lines))
	seenCount := map[int64]bool{}
	for i, l := range lines {
		key := fmt.Sprintf("lines.%d", i)
		l.Note = strings.TrimSpace(l.Note)
		it, ok := itemByID[l.ItemID]
		switch {
		case !ok:
			fields[key] = "料品不存在"
			continue
		case it.ItemType != "goods":
			fields[key] = it.Code + " 為服務類料品,不能有庫存異動"
			continue
		case !it.IsActive:
			fields[key] = it.Code + " 已停用"
			continue
		}
		factor, ok := factorOf[[2]int64{l.ItemID, l.UnitID}]
		if !ok {
			fields[key] = it.Code + " 沒有此單位"
			continue
		}
		p := preparedLine{lineInput: l, factor: factor}
		if docType == TypeCount {
			if l.UnitID != it.BaseUnitID {
				fields[key] = "盤點請以基本單位輸入"
				continue
			}
			if seenCount[l.ItemID] {
				fields[key] = it.Code + " 重複"
				continue
			}
			seenCount[l.ItemID] = true
		}
		if l.Qty != nil {
			qty := *l.Qty
			switch {
			case qty.Exponent() < -money.QuantityPlaces:
				fields[key] = fmt.Sprintf("數量最多 %d 位小數", money.QuantityPlaces)
				continue
			case docType == TypeAdjustment && qty.IsZero():
				fields[key] = "調整數量不可為 0"
				continue
			case docType == TypeTransfer && !qty.IsPositive():
				fields[key] = "調撥數量須大於 0"
				continue
			case docType == TypeCount && qty.IsNegative():
				fields[key] = "實盤數量不可為負"
				continue
			}
			base := qty.Mul(factor).Round(money.QuantityPlaces)
			if base.IsZero() && docType != TypeCount {
				fields[key] = "換算成基本單位後為 0"
				continue
			}
			p.baseQty = &base
		} else if docType != TypeCount {
			fields[key] = "請輸入數量"
			continue
		}
		out[i] = p
	}
	if len(fields) > 0 {
		return nil, apperr.Validation(fields)
	}
	return out, nil
}

func checkHeader(ctx context.Context, q *db.Queries, companyID int64, in *documentInput) (time.Time, error) {
	date, err := time.Parse(time.DateOnly, in.DocDate)
	if err != nil {
		return date, fieldErr("doc_date", "日期格式須為 YYYY-MM-DD")
	}
	ids := []int64{in.WarehouseID}
	if in.DocType == TypeTransfer {
		if in.ToWarehouseID == nil {
			return date, fieldErr("to_warehouse_id", "請選擇調入倉")
		}
		if *in.ToWarehouseID == in.WarehouseID {
			return date, fieldErr("to_warehouse_id", "調入倉不可與調出倉相同")
		}
		ids = append(ids, *in.ToWarehouseID)
	} else {
		in.ToWarehouseID = nil
	}
	if in.DocType != TypeCount {
		in.CategoryID = nil
	}
	whs, err := q.ListWarehouseFlags(ctx, db.ListWarehouseFlagsParams{CompanyID: companyID, Ids: ids})
	if err != nil {
		return date, err
	}
	active := 0
	for _, w := range whs {
		if w.IsActive {
			active++
		}
	}
	if active != len(ids) {
		return date, fieldErr("warehouse_id", "倉庫不存在或已停用")
	}
	return date, nil
}

func saveLines(ctx context.Context, q *db.Queries, docID int64, lines []preparedLine) error {
	if err := q.DeleteStockDocumentLines(ctx, docID); err != nil {
		return err
	}
	for i, l := range lines {
		if err := q.AddStockDocumentLine(ctx, db.AddStockDocumentLineParams{
			DocumentID: docID, LineNo: int32(i + 1), ItemID: l.ItemID, UnitID: l.UnitID, Qty: l.Qty,
			Factor: l.factor, BaseQty: l.baseQty, SystemQty: l.systemQty, Note: l.Note,
		}); err != nil {
			return err
		}
	}
	return nil
}

// countLines 盤點單:以建立當下的帳面數量為快照;已傳入的實盤數保留,新增的料品以目前現有量為帳面數。
func countLines(ctx context.Context, q *db.Queries, companyID, warehouseID int64, categoryID *int64, given []preparedLine, generate bool) ([]preparedLine, error) {
	if !generate {
		for i := range given {
			if given[i].systemQty != nil {
				continue
			}
			bal, err := q.GetBalanceQty(ctx, db.GetBalanceQtyParams{ItemID: given[i].ItemID, WarehouseID: warehouseID})
			if err != nil {
				return nil, err
			}
			given[i].systemQty = &bal
		}
		return given, nil
	}
	snap, err := q.CountSnapshot(ctx, db.CountSnapshotParams{CompanyID: companyID, WarehouseID: warehouseID, CategoryID: categoryID})
	if err != nil {
		return nil, err
	}
	out := make([]preparedLine, len(snap))
	for i, s := range snap {
		sys := s.Qty
		out[i] = preparedLine{
			lineInput: lineInput{ItemID: s.ItemID, UnitID: s.BaseUnitID},
			factor:    decimal.NewFromInt(1), systemQty: &sys,
		}
	}
	return out, nil
}

func (m *Module) createDocument(c *gin.Context) {
	var in documentInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	ctx := c.Request.Context()
	a := actor(c)
	var dto documentDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		date, err := checkHeader(ctx, q, a.CompanyID, &in)
		if err != nil {
			return err
		}
		var lines []preparedLine
		if in.DocType == TypeCount {
			// 盤點:明細由系統依倉庫現有量產生(建立即凍結該倉庫)
			if lines, err = countLines(ctx, q, a.CompanyID, in.WarehouseID, in.CategoryID, nil, true); err != nil {
				return err
			}
			if len(lines) == 0 {
				return apperr.New(http.StatusUnprocessableEntity, "STK-004", "此倉庫(分類)目前沒有可盤點的料品")
			}
			countNo, err := q.OpenCountDocNo(ctx, db.OpenCountDocNoParams{
				CompanyID: a.CompanyID, WarehouseIds: []int64{in.WarehouseID}, ExcludeID: 0,
			})
			if err == nil {
				return apperr.Conflict("STK-005", "此倉庫已有進行中的盤點單 "+countNo)
			} else if !database.IsNoRows(err) {
				return err
			}
		} else {
			if len(in.Lines) == 0 {
				return errNoLines
			}
			if lines, err = prepareLines(ctx, q, a.CompanyID, in.DocType, in.Lines); err != nil {
				return err
			}
		}
		no, err := docno.Next(ctx, q, a.CompanyID, docTypes[in.DocType].numbering, date)
		if err != nil {
			return err
		}
		d, err := q.CreateStockDocument(ctx, db.CreateStockDocumentParams{
			CompanyID: a.CompanyID, DocType: in.DocType, DocNo: no, DocDate: date, WarehouseID: in.WarehouseID,
			ToWarehouseID: in.ToWarehouseID, CategoryID: in.CategoryID, Note: in.Note, CreatedBy: &a.UserID,
		})
		if err != nil {
			return err
		}
		if err := saveLines(ctx, q, d.ID, lines); err != nil {
			return err
		}
		if dto, err = m.loadDocument(ctx, q, a.CompanyID, d.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "stock_document", EntityID: &d.ID,
			Summary: "新增" + docTypes[in.DocType].label + " " + no, After: dto,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto)
}

func (m *Module) updateDocument(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in documentInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	ctx := c.Request.Context()
	a := actor(c)
	var dto documentDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockStockDocument(ctx, db.LockStockDocumentParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Version != in.Version {
			return apperr.ErrVersionConflict
		}
		if !docstate.Editable(docstate.Status(cur.Status)) {
			return errEditNotAllowed
		}
		if in.DocType != cur.DocType || in.WarehouseID != cur.WarehouseID {
			return errWarehouseFixed
		}
		before, err := m.loadDocument(ctx, q, a.CompanyID, id)
		if err != nil {
			return err
		}
		in.CategoryID = cur.CategoryID
		date, err := checkHeader(ctx, q, a.CompanyID, &in)
		if err != nil {
			return err
		}
		if len(in.Lines) == 0 {
			return errNoLines
		}
		lines, err := prepareLines(ctx, q, a.CompanyID, in.DocType, in.Lines)
		if err != nil {
			return err
		}
		if in.DocType == TypeCount {
			// 保留原快照的帳面數;新加入的料品以目前現有量為帳面數
			snap := map[int64]*decimal.Decimal{}
			for _, l := range before.Lines {
				snap[l.ItemID] = l.SystemQty
			}
			kept := map[int64]bool{}
			for _, l := range lines {
				kept[l.ItemID] = true
			}
			for _, l := range before.Lines {
				if !kept[l.ItemID] {
					return errCountLineRemoved.WithDetails(map[string]string{"item": l.ItemCode + " " + l.ItemName})
				}
			}
			for i := range lines {
				lines[i].systemQty = snap[lines[i].ItemID]
			}
			if lines, err = countLines(ctx, q, a.CompanyID, cur.WarehouseID, nil, lines, false); err != nil {
				return err
			}
		}
		if _, err := q.UpdateStockDocumentHeader(ctx, db.UpdateStockDocumentHeaderParams{
			ID: id, CompanyID: a.CompanyID, DocDate: date, ToWarehouseID: in.ToWarehouseID, Note: in.Note,
			Version: in.Version, UpdatedBy: &a.UserID,
		}); err != nil {
			if database.IsNoRows(err) {
				return apperr.ErrVersionConflict
			}
			return err
		}
		if err := saveLines(ctx, q, id, lines); err != nil {
			return err
		}
		if dto, err = m.loadDocument(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "stock_document", EntityID: &id,
			Summary: "修改" + docTypes[cur.DocType].label + " " + cur.DocNo, Before: before, After: dto,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 狀態動作 ----

type actionInput struct {
	Version int32 `json:"version" binding:"required"`
}

var actionLabels = map[docstate.Action]string{
	docstate.Submit: "送審", docstate.Reject: "退回", docstate.Approve: "核准", docstate.Unapprove: "取消核准",
	docstate.Post: "過帳", docstate.Unpost: "反過帳", docstate.Void: "作廢",
}

// actionPermission 各動作所需權限;作廢草稿只需開單權限,作廢待審/已核准需核准權限。
func actionPermission(action docstate.Action, status docstate.Status) (string, bool) {
	switch action {
	case docstate.Submit:
		return permission.InventoryWrite, true
	case docstate.Approve, docstate.Reject, docstate.Unapprove:
		return permission.InventoryApprove, true
	case docstate.Post, docstate.Unpost:
		return permission.InventoryPost, true
	case docstate.Void:
		if status == docstate.Draft {
			return permission.InventoryWrite, true
		}
		return permission.InventoryApprove, true
	default:
		return "", false
	}
}

func (m *Module) documentAction(c *gin.Context) {
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
	var dto documentDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockStockDocument(ctx, db.LockStockDocumentParams{ID: id, CompanyID: a.CompanyID})
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
		next, err := docstate.Transition(docstate.Status(cur.Status), action)
		if err != nil {
			return err
		}
		before, err := m.loadDocument(ctx, q, a.CompanyID, id)
		if err != nil {
			return err
		}
		if err := m.applyAction(ctx, q, a, cur, before, action); err != nil {
			return err
		}
		if _, err := q.SetStockDocumentStatus(ctx, db.SetStockDocumentStatusParams{
			ID: id, CompanyID: a.CompanyID, Status: string(next), Action: string(action), ActorID: a.UserID,
			Version: in.Version,
		}); err != nil {
			return err
		}
		if dto, err = m.loadDocument(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: string(action), EntityType: "stock_document", EntityID: &id,
			Summary: actionLabels[action] + docTypes[cur.DocType].label + " " + cur.DocNo,
			Before:  map[string]string{"status": cur.Status}, After: map[string]string{"status": string(next)},
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// applyAction 動作的附帶檢查與庫存處理(狀態轉換本身由呼叫端處理)。
func (m *Module) applyAction(ctx context.Context, q *db.Queries, a *authctx.Actor, cur db.StockDocument, doc documentDTO, action docstate.Action) error {
	src := Source{Type: docTypes[cur.DocType].source, ID: cur.ID, No: cur.DocNo, DocDate: cur.DocDate}
	opt := Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
	if cur.DocType == TypeCount {
		opt.ExcludeCountDocID = cur.ID
	}
	switch action {
	case docstate.Submit:
		if len(doc.Lines) == 0 {
			return errNoLines
		}
		if cur.DocType == TypeCount {
			for _, l := range doc.Lines {
				if l.BaseQty == nil {
					return errNotCounted.WithDetails(map[string]string{"item": l.ItemCode + " " + l.ItemName})
				}
			}
		}
	case docstate.Post:
		moves := movementsOf(cur, doc)
		if len(moves) == 0 {
			// 盤點全部無差異:沒有異動也可過帳(完成盤點、解除凍結)
			if cur.DocType == TypeCount {
				return nil
			}
			return errNothingToPost
		}
		return Post(ctx, q, opt, src, moves)
	case docstate.Unpost:
		err := Reverse(ctx, q, opt, src)
		// 無差異的盤點單過帳時沒有分錄,反過帳也就沒有東西可沖銷
		if err == ErrNothingToReverse && cur.DocType == TypeCount {
			return nil
		}
		return err
	}
	return nil
}

// movementsOf 依單據類型產生庫存異動(基本單位)。
func movementsOf(cur db.StockDocument, doc documentDTO) []Movement {
	var moves []Movement
	for _, l := range doc.Lines {
		lineID := l.ID
		switch cur.DocType {
		case TypeAdjustment:
			moves = append(moves, Movement{ItemID: l.ItemID, WarehouseID: cur.WarehouseID, Qty: *l.BaseQty, SourceLineID: &lineID})
		case TypeTransfer:
			moves = append(moves,
				Movement{ItemID: l.ItemID, WarehouseID: cur.WarehouseID, Qty: l.BaseQty.Neg(), SourceLineID: &lineID},
				Movement{ItemID: l.ItemID, WarehouseID: *cur.ToWarehouseID, Qty: *l.BaseQty, SourceLineID: &lineID})
		case TypeCount:
			if l.DiffQty != nil && !l.DiffQty.IsZero() {
				moves = append(moves, Movement{ItemID: l.ItemID, WarehouseID: cur.WarehouseID, Qty: *l.DiffQty, SourceLineID: &lineID})
			}
		}
	}
	return moves
}
