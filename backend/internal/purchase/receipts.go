package purchase

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/finance"
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
	TypeReceipt = "receipt" // 進貨
	TypeReturn  = "return"  // 進貨退出
)

// 單據類型 → 單號規則、流水帳 / 應付來源類型、中文名稱
var receiptTypes = map[string]struct{ numbering, source, label string }{
	TypeReceipt: {"goods_receipt", finance.SourceGoodsReceipt, "進貨單"},
	TypeReturn:  {"purchase_return", finance.SourcePurchaseReturn, "進貨退出單"},
}

var (
	errReceiptEditNotAllowed = apperr.New(http.StatusConflict, "PUR-003", "只有草稿可以修改")
	errReceiptTypeFixed      = fieldErr("doc_type", "建立後不可變更單據類型")
)

type receiptLineDTO struct {
	ID              int64           `json:"id"`
	LineNo          int32           `json:"line_no"`
	ItemID          int64           `json:"item_id"`
	ItemCode        string          `json:"item_code"`
	ItemName        string          `json:"item_name"`
	ItemSpec        string          `json:"item_spec"`
	ItemType        string          `json:"item_type"`
	UnitID          int64           `json:"unit_id"`
	UnitName        string          `json:"unit_name"`
	BaseUnitName    string          `json:"base_unit_name"`
	Qty             decimal.Decimal `json:"qty"`
	Factor          decimal.Decimal `json:"factor"`
	BaseQty         decimal.Decimal `json:"base_qty"`
	UnitPrice       decimal.Decimal `json:"unit_price"`
	Amount          decimal.Decimal `json:"amount"`
	BaseAmount      decimal.Decimal `json:"base_amount"`
	PoLineID        *int64          `json:"po_line_id"`
	PoNo            *string         `json:"po_no"`
	ReceiptLineID   *int64          `json:"receipt_line_id"`
	SourceReceiptNo *string         `json:"source_receipt_no"`
	Note            string          `json:"note"`
}

type receiptDTO struct {
	ID              int64            `json:"id"`
	DocType         string           `json:"doc_type"`
	DocNo           string           `json:"doc_no"`
	DocDate         string           `json:"doc_date"`
	SupplierID      int64            `json:"supplier_id"`
	SupplierCode    string           `json:"supplier_code"`
	SupplierName    string           `json:"supplier_name"`
	WarehouseID     int64            `json:"warehouse_id"`
	WarehouseName   string           `json:"warehouse_name"`
	Currency        string           `json:"currency"`
	ExchangeRate    decimal.Decimal  `json:"exchange_rate"`
	TaxTypeID       int64            `json:"tax_type_id"`
	TaxTypeName     string           `json:"tax_type_name"`
	TaxRate         decimal.Decimal  `json:"tax_rate"`
	PaymentTermID   *int64           `json:"payment_term_id"`
	PaymentTermName *string          `json:"payment_term_name"`
	InvoiceNo       string           `json:"invoice_no"`
	UntaxedAmount   decimal.Decimal  `json:"untaxed_amount"`
	TaxAmount       decimal.Decimal  `json:"tax_amount"`
	TotalAmount     decimal.Decimal  `json:"total_amount"`
	BaseUntaxed     decimal.Decimal  `json:"base_untaxed"`
	BaseTax         decimal.Decimal  `json:"base_tax"`
	BaseTotal       decimal.Decimal  `json:"base_total"`
	Status          string           `json:"status"`
	Note            string           `json:"note"`
	CreatedByName   *string          `json:"created_by_name"`
	SubmittedByName *string          `json:"submitted_by_name"`
	SubmittedAt     *time.Time       `json:"submitted_at"`
	ApprovedByName  *string          `json:"approved_by_name"`
	ApprovedAt      *time.Time       `json:"approved_at"`
	PostedByName    *string          `json:"posted_by_name"`
	PostedAt        *time.Time       `json:"posted_at"`
	Lines           []receiptLineDTO `json:"lines"`
	Version         int32            `json:"version"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

func loadReceipt(ctx context.Context, q *db.Queries, companyID, id int64) (receiptDTO, error) {
	r, err := q.GetGoodsReceipt(ctx, db.GetGoodsReceiptParams{ID: id, CompanyID: companyID})
	if database.IsNoRows(err) {
		return receiptDTO{}, apperr.ErrNotFound
	}
	if err != nil {
		return receiptDTO{}, err
	}
	rows, err := q.ListGoodsReceiptLines(ctx, id)
	if err != nil {
		return receiptDTO{}, err
	}
	lines := make([]receiptLineDTO, len(rows))
	for i, l := range rows {
		lines[i] = receiptLineDTO{
			ID: l.ID, LineNo: l.LineNo, ItemID: l.ItemID, ItemCode: l.ItemCode, ItemName: l.ItemName,
			ItemSpec: l.ItemSpec, ItemType: l.ItemType, UnitID: l.UnitID, UnitName: l.UnitName,
			BaseUnitName: l.BaseUnitName, Qty: l.Qty, Factor: l.Factor, BaseQty: l.BaseQty, UnitPrice: l.UnitPrice,
			Amount: l.Amount, BaseAmount: l.BaseAmount, PoLineID: l.PoLineID, PoNo: l.PoNo,
			ReceiptLineID: l.ReceiptLineID, SourceReceiptNo: l.SourceReceiptNo, Note: l.Note,
		}
	}
	return receiptDTO{
		ID: r.ID, DocType: r.DocType, DocNo: r.DocNo, DocDate: r.DocDate.Format(time.DateOnly),
		SupplierID: r.SupplierID, SupplierCode: r.SupplierCode, SupplierName: r.SupplierName,
		WarehouseID: r.WarehouseID, WarehouseName: r.WarehouseName, Currency: r.Currency,
		ExchangeRate: r.ExchangeRate, TaxTypeID: r.TaxTypeID, TaxTypeName: r.TaxTypeName, TaxRate: r.TaxRate,
		PaymentTermID: r.PaymentTermID, PaymentTermName: r.PaymentTermName, InvoiceNo: r.InvoiceNo,
		UntaxedAmount: r.UntaxedAmount, TaxAmount: r.TaxAmount, TotalAmount: r.TotalAmount,
		BaseUntaxed: r.BaseUntaxed, BaseTax: r.BaseTax, BaseTotal: r.BaseTotal, Status: r.Status, Note: r.Note,
		CreatedByName: r.CreatedByName, SubmittedByName: r.SubmittedByName, SubmittedAt: r.SubmittedAt,
		ApprovedByName: r.ApprovedByName, ApprovedAt: r.ApprovedAt, PostedByName: r.PostedByName,
		PostedAt: r.PostedAt, Lines: lines, Version: r.Version, UpdatedAt: r.UpdatedAt,
	}, nil
}

// ---- 列表與單筆 ----

type receiptListDTO struct {
	ID            int64           `json:"id"`
	DocType       string          `json:"doc_type"`
	DocNo         string          `json:"doc_no"`
	DocDate       string          `json:"doc_date"`
	SupplierCode  string          `json:"supplier_code"`
	SupplierName  string          `json:"supplier_name"`
	WarehouseName string          `json:"warehouse_name"`
	Currency      string          `json:"currency"`
	TotalAmount   decimal.Decimal `json:"total_amount"`
	InvoiceNo     string          `json:"invoice_no"`
	Status        string          `json:"status"`
	Note          string          `json:"note"`
	CreatedByName *string         `json:"created_by_name"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func (m *Module) listReceipts(c *gin.Context) {
	ctx := c.Request.Context()
	supplierID, err := httpx.QueryInt64(c, "supplier_id")
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
	rows, err := m.store.ListGoodsReceipts(ctx, db.ListGoodsReceiptsParams{
		CompanyID: companyID, DocType: docType, Status: status, SupplierID: supplierID, Keyword: keyword,
		FromDate: from, ToDate: to, Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountGoodsReceipts(ctx, db.CountGoodsReceiptsParams{
		CompanyID: companyID, DocType: docType, Status: status, SupplierID: supplierID, Keyword: keyword,
		FromDate: from, ToDate: to,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]receiptListDTO, len(rows))
	for i, r := range rows {
		out[i] = receiptListDTO{
			ID: r.ID, DocType: r.DocType, DocNo: r.DocNo, DocDate: r.DocDate.Format(time.DateOnly),
			SupplierCode: r.SupplierCode, SupplierName: r.SupplierName, WarehouseName: r.WarehouseName,
			Currency: r.Currency, TotalAmount: r.TotalAmount, InvoiceNo: r.InvoiceNo, Status: r.Status, Note: r.Note,
			CreatedByName: r.CreatedByName, UpdatedAt: r.UpdatedAt,
		}
	}
	response.List(c, out, pg.Meta(total))
}

func (m *Module) getReceipt(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	dto, err := loadReceipt(c.Request.Context(), m.store.Queries, actor(c).CompanyID, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 來源單據檢查 ----

// refLine 檢查來源所需的明細欄位(開單時來自輸入,過帳時來自資料庫)。
type refLine struct {
	itemID, unitID          int64
	qty                     decimal.Decimal
	poLineID, receiptLineID *int64
}

// refDoc 單據層級的比對條件。
type refDoc struct {
	companyID  int64
	docType    string
	supplierID int64
	currency   string
	excludeID  int64 // 本單 id:計算已交 / 已退量時排除自己
}

// checkRefs 驗證明細引用的來源並檢查剩餘量,回傳各明細索引的錯誤訊息。
// 進貨引用「已核准」採購單明細,累計不可超過未交量;退出引用「已過帳」進貨明細,累計不可超過可退量(D32)。
// lock 為 true(過帳)時先依 id 順序鎖定來源單據再重新讀取剩餘量,避免兩張單同時過帳而超交 / 超退。
func checkRefs(ctx context.Context, q *db.Queries, d refDoc, lines []refLine, lock bool) (map[int]string, error) {
	errs := map[int]string{}
	var ids []int64
	for _, l := range lines {
		if d.docType == TypeReceipt && l.poLineID != nil {
			ids = append(ids, *l.poLineID)
		}
		if d.docType == TypeReturn && l.receiptLineID != nil {
			ids = append(ids, *l.receiptLineID)
		}
	}
	if len(ids) == 0 {
		return errs, nil
	}

	// 統一成同一種形狀:來源明細 → 單號、狀態、比對欄位、可用量
	type source struct {
		docID, supplierID, itemID, unitID int64
		docNo, docType, status, currency  string
		available                         decimal.Decimal
	}
	fetch := func() (map[int64]source, error) {
		out := map[int64]source{}
		if d.docType == TypeReceipt {
			rows, err := q.PoLineRefs(ctx, db.PoLineRefsParams{CompanyID: d.companyID, Ids: ids, ExcludeReceiptID: d.excludeID})
			if err != nil {
				return nil, err
			}
			for _, r := range rows {
				out[r.ID] = source{r.OrderID, r.SupplierID, r.ItemID, r.UnitID, r.DocNo, "", r.Status, r.Currency, r.Qty.Sub(r.ReceivedQty)}
			}
			return out, nil
		}
		rows, err := q.ReceiptLineRefs(ctx, db.ReceiptLineRefsParams{CompanyID: d.companyID, Ids: ids, ExcludeReceiptID: d.excludeID})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.ID] = source{r.ReceiptID, r.SupplierID, r.ItemID, r.UnitID, r.DocNo, r.DocType, r.Status, r.Currency, r.Qty.Sub(r.ReturnedQty)}
		}
		return out, nil
	}
	sources, err := fetch()
	if err != nil {
		return nil, err
	}
	if lock {
		docIDs := map[int64]bool{}
		for _, s := range sources {
			docIDs[s.docID] = true
		}
		sorted := slices.Sorted(maps.Keys(docIDs))
		if d.docType == TypeReceipt {
			_, err = q.LockPurchaseOrders(ctx, db.LockPurchaseOrdersParams{CompanyID: d.companyID, Ids: sorted})
		} else {
			_, err = q.LockGoodsReceipts(ctx, db.LockGoodsReceiptsParams{CompanyID: d.companyID, Ids: sorted})
		}
		if err != nil {
			return nil, err
		}
		if sources, err = fetch(); err != nil {
			return nil, err
		}
	}

	srcLabel, wantStatus := "採購單", string(docstate.Approved)
	if d.docType == TypeReturn {
		srcLabel, wantStatus = "進貨單", string(docstate.Posted)
	}
	used := map[int64]decimal.Decimal{}
	for i, l := range lines {
		ref := l.poLineID
		if d.docType == TypeReturn {
			ref = l.receiptLineID
		}
		if ref == nil {
			continue
		}
		s, ok := sources[*ref]
		switch {
		case !ok:
			errs[i] = "找不到來源" + srcLabel + "明細"
		case d.docType == TypeReturn && s.docType != TypeReceipt:
			errs[i] = "來源須為進貨單"
		case s.status != wantStatus:
			if d.docType == TypeReceipt {
				errs[i] = fmt.Sprintf("採購單 %s 未核准或已結案", s.docNo)
			} else {
				errs[i] = fmt.Sprintf("進貨單 %s 尚未過帳", s.docNo)
			}
		case s.supplierID != d.supplierID:
			errs[i] = fmt.Sprintf("%s %s 的供應商與本單不同", srcLabel, s.docNo)
		case s.currency != d.currency:
			errs[i] = fmt.Sprintf("%s %s 的幣別為 %s", srcLabel, s.docNo, s.currency)
		case s.itemID != l.itemID || s.unitID != l.unitID:
			errs[i] = fmt.Sprintf("料品或單位與%s %s 不同", srcLabel, s.docNo)
		default:
			used[*ref] = used[*ref].Add(l.qty)
			if used[*ref].GreaterThan(s.available) {
				what := "未交量"
				if d.docType == TypeReturn {
					what = "可退量"
				}
				errs[i] = fmt.Sprintf("超過%s %s 的%s %s", srcLabel, s.docNo, what, decimal.Max(s.available, decimal.Zero).String())
			}
		}
	}
	return errs, nil
}

// ---- 建立與修改(草稿) ----

type receiptInput struct {
	headerInput
	DocType   string      `json:"doc_type" binding:"required,oneof=receipt return"`
	InvoiceNo string      `json:"invoice_no" binding:"max=20"`
	Lines     []lineInput `json:"lines" binding:"dive"`
}

func prepareReceipt(ctx context.Context, q *db.Queries, companyID, excludeID int64, in *receiptInput) (trade.Header, []pricedLine, trade.Totals, error) {
	in.InvoiceNo = strings.TrimSpace(in.InvoiceNo)
	h, err := checkHeader(ctx, q, companyID, &in.headerInput)
	if err != nil {
		return h, nil, trade.Totals{}, err
	}
	refs := make([]refLine, len(in.Lines))
	for i := range in.Lines {
		// 進貨只能引用採購明細、退出只能引用進貨明細
		if in.DocType == TypeReceipt {
			in.Lines[i].ReceiptLineID = nil
		} else {
			in.Lines[i].PoLineID = nil
		}
		l := in.Lines[i]
		refs[i] = refLine{itemID: l.ItemID, unitID: l.UnitID, qty: l.Qty, poLineID: l.PoLineID, receiptLineID: l.ReceiptLineID}
	}
	lines, t, err := priceLines(ctx, q, companyID, h, in.Lines)
	if err != nil {
		return h, nil, t, err
	}
	errs, err := checkRefs(ctx, q, refDoc{companyID, in.DocType, in.SupplierID, in.Currency, excludeID}, refs, false)
	if err != nil {
		return h, nil, t, err
	}
	if len(errs) > 0 {
		fields := map[string]string{}
		for i, msg := range errs {
			fields[fmt.Sprintf("lines.%d", i)] = msg
		}
		return h, nil, t, apperr.Validation(fields)
	}
	return h, lines, t, nil
}

func saveReceiptLines(ctx context.Context, q *db.Queries, receiptID int64, lines []pricedLine) error {
	if err := q.DeleteGoodsReceiptLines(ctx, receiptID); err != nil {
		return err
	}
	for i, l := range lines {
		if err := q.AddGoodsReceiptLine(ctx, db.AddGoodsReceiptLineParams{
			ReceiptID: receiptID, LineNo: int32(i + 1), ItemID: l.ItemID, UnitID: l.UnitID, Qty: l.Qty,
			Factor: l.Factor, BaseQty: l.BaseQty, UnitPrice: l.UnitPrice, Amount: l.Amount, BaseAmount: l.BaseAmount,
			PoLineID: l.PoLineID, ReceiptLineID: l.ReceiptLineID, Note: l.Note,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) createReceipt(c *gin.Context) {
	var in receiptInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto receiptDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		h, lines, t, err := prepareReceipt(ctx, q, a.CompanyID, 0, &in)
		if err != nil {
			return err
		}
		typ := receiptTypes[in.DocType]
		no, err := docno.Next(ctx, q, a.CompanyID, typ.numbering, h.Date)
		if err != nil {
			return err
		}
		r, err := q.CreateGoodsReceipt(ctx, db.CreateGoodsReceiptParams{
			CompanyID: a.CompanyID, DocType: in.DocType, DocNo: no, DocDate: h.Date, SupplierID: in.SupplierID,
			WarehouseID: in.WarehouseID, Currency: in.Currency, ExchangeRate: h.Rate, TaxTypeID: in.TaxTypeID,
			TaxRate: h.TaxRate, PaymentTermID: in.PaymentTermID, InvoiceNo: in.InvoiceNo, UntaxedAmount: t.Untaxed,
			TaxAmount: t.Tax, TotalAmount: t.Total, BaseUntaxed: t.BaseUntaxed, BaseTax: t.BaseTax,
			BaseTotal: t.BaseTotal, Note: in.Note, CreatedBy: &a.UserID,
		})
		if err != nil {
			return err
		}
		if err := saveReceiptLines(ctx, q, r.ID, lines); err != nil {
			return err
		}
		if dto, err = loadReceipt(ctx, q, a.CompanyID, r.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "goods_receipt", EntityID: &r.ID,
			Summary: "新增" + typ.label + " " + no, After: dto,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto)
}

func (m *Module) updateReceipt(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in receiptInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto receiptDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockGoodsReceipt(ctx, db.LockGoodsReceiptParams{ID: id, CompanyID: a.CompanyID})
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
			return errReceiptEditNotAllowed
		}
		if in.DocType != cur.DocType {
			return errReceiptTypeFixed
		}
		before, err := loadReceipt(ctx, q, a.CompanyID, id)
		if err != nil {
			return err
		}
		h, lines, t, err := prepareReceipt(ctx, q, a.CompanyID, id, &in)
		if err != nil {
			return err
		}
		if _, err := q.UpdateGoodsReceiptHeader(ctx, db.UpdateGoodsReceiptHeaderParams{
			ID: id, CompanyID: a.CompanyID, DocDate: h.Date, SupplierID: in.SupplierID, WarehouseID: in.WarehouseID,
			Currency: in.Currency, ExchangeRate: h.Rate, TaxTypeID: in.TaxTypeID, TaxRate: h.TaxRate,
			PaymentTermID: in.PaymentTermID, InvoiceNo: in.InvoiceNo, UntaxedAmount: t.Untaxed, TaxAmount: t.Tax,
			TotalAmount: t.Total, BaseUntaxed: t.BaseUntaxed, BaseTax: t.BaseTax, BaseTotal: t.BaseTotal,
			Note: in.Note, Version: in.Version, UpdatedBy: &a.UserID,
		}); err != nil {
			if database.IsNoRows(err) {
				return apperr.ErrVersionConflict
			}
			return err
		}
		if err := saveReceiptLines(ctx, q, id, lines); err != nil {
			return err
		}
		if dto, err = loadReceipt(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "goods_receipt", EntityID: &id,
			Summary: "修改" + receiptTypes[cur.DocType].label + " " + cur.DocNo, Before: before, After: dto,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 狀態動作 ----

func receiptActionPermission(action docstate.Action, status docstate.Status) (string, bool) {
	switch action {
	case docstate.Submit:
		return permission.ReceiptWrite, true
	case docstate.Approve, docstate.Reject, docstate.Unapprove:
		return permission.ReceiptApprove, true
	case docstate.Post, docstate.Unpost:
		return permission.ReceiptPost, true
	case docstate.Void:
		if status == docstate.Draft {
			return permission.ReceiptWrite, true
		}
		return permission.ReceiptApprove, true
	default:
		// 進貨單不使用結案 / 重開
		return "", false
	}
}

func (m *Module) receiptAction(c *gin.Context) {
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
	var dto receiptDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockGoodsReceipt(ctx, db.LockGoodsReceiptParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		perm, ok := receiptActionPermission(action, docstate.Status(cur.Status))
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
		doc, err := loadReceipt(ctx, q, a.CompanyID, id)
		if err != nil {
			return err
		}
		if err := applyReceiptAction(ctx, q, a, cur, doc, action); err != nil {
			return err
		}
		if _, err := q.SetGoodsReceiptStatus(ctx, db.SetGoodsReceiptStatusParams{
			ID: id, CompanyID: a.CompanyID, Status: string(next), Action: string(action), ActorID: a.UserID,
			Version: in.Version,
		}); err != nil {
			return err
		}
		if dto, err = loadReceipt(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: string(action), EntityType: "goods_receipt", EntityID: &id,
			Summary: actionLabels[action] + receiptTypes[cur.DocType].label + " " + cur.DocNo,
			Before:  map[string]string{"status": cur.Status}, After: map[string]string{"status": string(next)},
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// applyReceiptAction 動作的附帶檢查、庫存與應付處理(狀態轉換本身由呼叫端處理)。
func applyReceiptAction(ctx context.Context, q *db.Queries, a *authctx.Actor, cur db.GoodsReceipt, doc receiptDTO, action docstate.Action) error {
	typ := receiptTypes[cur.DocType]
	src := inventory.Source{Type: typ.source, ID: cur.ID, No: cur.DocNo, DocDate: cur.DocDate}
	opt := inventory.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
	switch action {
	case docstate.Submit:
		if len(doc.Lines) == 0 {
			return fieldErr("lines", "請輸入明細")
		}
	case docstate.Post:
		if len(doc.Lines) == 0 {
			return fieldErr("lines", "請輸入明細")
		}
		refs := make([]refLine, len(doc.Lines))
		for i, l := range doc.Lines {
			refs[i] = refLine{itemID: l.ItemID, unitID: l.UnitID, qty: l.Qty, poLineID: l.PoLineID, receiptLineID: l.ReceiptLineID}
		}
		errs, err := checkRefs(ctx, q, refDoc{a.CompanyID, cur.DocType, cur.SupplierID, cur.Currency, cur.ID}, refs, true)
		if err != nil {
			return err
		}
		if len(errs) > 0 {
			msgs := make([]string, 0, len(errs))
			details := map[string]string{}
			for i, l := range doc.Lines {
				if msg, ok := errs[i]; ok {
					msgs = append(msgs, fmt.Sprintf("第 %d 行 %s:%s", l.LineNo, l.ItemCode, msg))
					details[fmt.Sprintf("lines.%d", i)] = msg
				}
			}
			return apperr.New(http.StatusUnprocessableEntity, "PUR-004", "無法過帳:"+strings.Join(msgs, ";")).WithDetails(details)
		}
		if moves := movementsOf(cur, doc); len(moves) > 0 {
			if err := inventory.Post(ctx, q, opt, src, moves); err != nil {
				return err
			}
		}
		due, err := trade.DueDate(ctx, q, a.CompanyID, cur.PaymentTermID, cur.DocDate)
		if err != nil {
			return err
		}
		amount, baseAmount := cur.TotalAmount, cur.BaseTotal
		if cur.DocType == TypeReturn {
			amount, baseAmount = amount.Neg(), baseAmount.Neg()
		}
		return finance.CreatePayable(ctx, q, db.InsertPayableParams{
			CompanyID: a.CompanyID, SupplierID: cur.SupplierID, SourceType: typ.source, SourceID: cur.ID,
			SourceNo: cur.DocNo, DocDate: cur.DocDate, DueDate: due, Currency: cur.Currency,
			ExchangeRate: cur.ExchangeRate, Amount: amount, BaseAmount: baseAmount, CreatedBy: &a.UserID,
		})
	case docstate.Unpost:
		if cur.DocType == TypeReceipt {
			no, err := q.ReceiptReturnNo(ctx, cur.ID)
			if err == nil {
				return apperr.Conflict("PUR-005", "進貨單已有退出單 "+no+",請先作廢退出單")
			}
			if !database.IsNoRows(err) {
				return err
			}
		}
		if err := finance.RemovePayable(ctx, q, typ.source, cur.ID); err != nil {
			return err
		}
		err := inventory.Reverse(ctx, q, opt, src)
		// 只有服務類明細的單據過帳時沒有庫存分錄
		if err == inventory.ErrNothingToReverse && len(movementsOf(cur, doc)) == 0 {
			return nil
		}
		return err
	}
	return nil
}

// movementsOf 商品類明細的庫存異動:進貨為正、退出為負;單位成本 = 本位幣金額 ÷ 基本單位數量(D36)。
func movementsOf(cur db.GoodsReceipt, doc receiptDTO) []inventory.Movement {
	var moves []inventory.Movement
	for _, l := range doc.Lines {
		if l.ItemType != "goods" {
			continue
		}
		lineID := l.ID
		cost := l.BaseAmount.Div(l.BaseQty).Round(money.UnitPricePlaces)
		qty := l.BaseQty
		if cur.DocType == TypeReturn {
			qty = qty.Neg()
		}
		moves = append(moves, inventory.Movement{
			ItemID: l.ItemID, WarehouseID: cur.WarehouseID, Qty: qty, UnitCost: &cost, SourceLineID: &lineID,
		})
	}
	return moves
}

// ---- 可退貨明細 ----

type returnableDTO struct {
	ReceiptLineID int64           `json:"receipt_line_id"`
	ReceiptID     int64           `json:"receipt_id"`
	DocNo         string          `json:"doc_no"`
	DocDate       string          `json:"doc_date"`
	WarehouseID   int64           `json:"warehouse_id"`
	LineNo        int32           `json:"line_no"`
	ItemID        int64           `json:"item_id"`
	ItemCode      string          `json:"item_code"`
	ItemName      string          `json:"item_name"`
	ItemSpec      string          `json:"item_spec"`
	UnitID        int64           `json:"unit_id"`
	UnitName      string          `json:"unit_name"`
	Qty           decimal.Decimal `json:"qty"`
	UnitPrice     decimal.Decimal `json:"unit_price"`
	ReturnedQty   decimal.Decimal `json:"returned_qty"`
	RemainingQty  decimal.Decimal `json:"remaining_qty"`
}

// returnableLines 指定供應商與幣別、已過帳且尚未退完的進貨明細(最多 200 筆)。
func (m *Module) returnableLines(c *gin.Context) {
	supplierID, err := httpx.QueryInt64(c, "supplier_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	currency := strings.ToUpper(c.Query("currency"))
	if supplierID == nil || currency == "" {
		response.Error(c, apperr.Validation(map[string]string{"supplier_id": "請指定供應商與幣別"}))
		return
	}
	rows, err := m.store.ReturnableReceiptLines(c.Request.Context(), db.ReturnableReceiptLinesParams{
		CompanyID: actor(c).CompanyID, SupplierID: *supplierID, Currency: currency,
		Keyword: httpx.QueryString(c, "keyword"),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]returnableDTO, len(rows))
	for i, r := range rows {
		out[i] = returnableDTO{
			ReceiptLineID: r.ReceiptLineID, ReceiptID: r.ReceiptID, DocNo: r.DocNo,
			DocDate: r.DocDate.Format(time.DateOnly), WarehouseID: r.WarehouseID, LineNo: r.LineNo, ItemID: r.ItemID,
			ItemCode: r.ItemCode, ItemName: r.ItemName, ItemSpec: r.ItemSpec, UnitID: r.UnitID, UnitName: r.UnitName,
			Qty: r.Qty, UnitPrice: r.UnitPrice, ReturnedQty: r.ReturnedQty, RemainingQty: r.RemainingQty,
		}
	}
	response.OK(c, out)
}
