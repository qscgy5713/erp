package purchase

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/docstate"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/system/docno"
	"erp/internal/system/permission"
)

var errOrderEditNotAllowed = apperr.New(http.StatusConflict, "PUR-001", "只有草稿可以修改")

type orderLineDTO struct {
	ID           int64           `json:"id"`
	LineNo       int32           `json:"line_no"`
	ItemID       int64           `json:"item_id"`
	ItemCode     string          `json:"item_code"`
	ItemName     string          `json:"item_name"`
	ItemSpec     string          `json:"item_spec"`
	ItemType     string          `json:"item_type"`
	UnitID       int64           `json:"unit_id"`
	UnitName     string          `json:"unit_name"`
	BaseUnitName string          `json:"base_unit_name"`
	Qty          decimal.Decimal `json:"qty"`
	Factor       decimal.Decimal `json:"factor"`
	BaseQty      decimal.Decimal `json:"base_qty"`
	UnitPrice    decimal.Decimal `json:"unit_price"`
	Amount       decimal.Decimal `json:"amount"`
	ReceivedQty  decimal.Decimal `json:"received_qty"`  // 已過帳進貨量(輸入單位)
	RemainingQty decimal.Decimal `json:"remaining_qty"` // 未交量
	Note         string          `json:"note"`
}

type orderDTO struct {
	ID              int64           `json:"id"`
	DocNo           string          `json:"doc_no"`
	DocDate         string          `json:"doc_date"`
	SupplierID      int64           `json:"supplier_id"`
	SupplierCode    string          `json:"supplier_code"`
	SupplierName    string          `json:"supplier_name"`
	WarehouseID     int64           `json:"warehouse_id"`
	WarehouseName   string          `json:"warehouse_name"`
	ExpectedDate    *string         `json:"expected_date"`
	Currency        string          `json:"currency"`
	ExchangeRate    decimal.Decimal `json:"exchange_rate"`
	TaxTypeID       int64           `json:"tax_type_id"`
	TaxTypeName     string          `json:"tax_type_name"`
	TaxRate         decimal.Decimal `json:"tax_rate"`
	PaymentTermID   *int64          `json:"payment_term_id"`
	PaymentTermName *string         `json:"payment_term_name"`
	UntaxedAmount   decimal.Decimal `json:"untaxed_amount"`
	TaxAmount       decimal.Decimal `json:"tax_amount"`
	TotalAmount     decimal.Decimal `json:"total_amount"`
	Status          string          `json:"status"`
	Note            string          `json:"note"`
	CreatedByName   *string         `json:"created_by_name"`
	SubmittedByName *string         `json:"submitted_by_name"`
	SubmittedAt     *time.Time      `json:"submitted_at"`
	ApprovedByName  *string         `json:"approved_by_name"`
	ApprovedAt      *time.Time      `json:"approved_at"`
	ClosedByName    *string         `json:"closed_by_name"`
	ClosedAt        *time.Time      `json:"closed_at"`
	Lines           []orderLineDTO  `json:"lines"`
	Version         int32           `json:"version"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func loadOrder(ctx context.Context, q *db.Queries, companyID, id int64) (orderDTO, error) {
	o, err := q.GetPurchaseOrder(ctx, db.GetPurchaseOrderParams{ID: id, CompanyID: companyID})
	if database.IsNoRows(err) {
		return orderDTO{}, apperr.ErrNotFound
	}
	if err != nil {
		return orderDTO{}, err
	}
	rows, err := q.ListPurchaseOrderLines(ctx, id)
	if err != nil {
		return orderDTO{}, err
	}
	lines := make([]orderLineDTO, len(rows))
	for i, r := range rows {
		lines[i] = orderLineDTO{
			ID: r.ID, LineNo: r.LineNo, ItemID: r.ItemID, ItemCode: r.ItemCode, ItemName: r.ItemName,
			ItemSpec: r.ItemSpec, ItemType: r.ItemType, UnitID: r.UnitID, UnitName: r.UnitName,
			BaseUnitName: r.BaseUnitName, Qty: r.Qty, Factor: r.Factor, BaseQty: r.BaseQty, UnitPrice: r.UnitPrice,
			Amount: r.Amount, ReceivedQty: r.ReceivedQty, RemainingQty: decimal.Max(r.Qty.Sub(r.ReceivedQty), decimal.Zero),
			Note: r.Note,
		}
	}
	return orderDTO{
		ID: o.ID, DocNo: o.DocNo, DocDate: o.DocDate.Format(time.DateOnly), SupplierID: o.SupplierID,
		SupplierCode: o.SupplierCode, SupplierName: o.SupplierName, WarehouseID: o.WarehouseID,
		WarehouseName: o.WarehouseName, ExpectedDate: dateString(o.ExpectedDate), Currency: o.Currency,
		ExchangeRate: o.ExchangeRate, TaxTypeID: o.TaxTypeID, TaxTypeName: o.TaxTypeName, TaxRate: o.TaxRate,
		PaymentTermID: o.PaymentTermID, PaymentTermName: o.PaymentTermName, UntaxedAmount: o.UntaxedAmount,
		TaxAmount: o.TaxAmount, TotalAmount: o.TotalAmount, Status: o.Status, Note: o.Note,
		CreatedByName: o.CreatedByName, SubmittedByName: o.SubmittedByName, SubmittedAt: o.SubmittedAt,
		ApprovedByName: o.ApprovedByName, ApprovedAt: o.ApprovedAt, ClosedByName: o.ClosedByName,
		ClosedAt: o.ClosedAt, Lines: lines, Version: o.Version, UpdatedAt: o.UpdatedAt,
	}, nil
}

// ---- 列表與單筆 ----

type orderListDTO struct {
	ID            int64           `json:"id"`
	DocNo         string          `json:"doc_no"`
	DocDate       string          `json:"doc_date"`
	ExpectedDate  *string         `json:"expected_date"`
	SupplierCode  string          `json:"supplier_code"`
	SupplierName  string          `json:"supplier_name"`
	Currency      string          `json:"currency"`
	TotalAmount   decimal.Decimal `json:"total_amount"`
	Status        string          `json:"status"`
	ReceiptState  string          `json:"receipt_state"` // none / partial / full
	Note          string          `json:"note"`
	CreatedByName *string         `json:"created_by_name"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func (m *Module) listOrders(c *gin.Context) {
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
	status, keyword := httpx.QueryString(c, "status"), httpx.QueryString(c, "keyword")
	rows, err := m.store.ListPurchaseOrders(ctx, db.ListPurchaseOrdersParams{
		CompanyID: companyID, Status: status, SupplierID: supplierID, Keyword: keyword, FromDate: from, ToDate: to,
		Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountPurchaseOrders(ctx, db.CountPurchaseOrdersParams{
		CompanyID: companyID, Status: status, SupplierID: supplierID, Keyword: keyword, FromDate: from, ToDate: to,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]orderListDTO, len(rows))
	for i, r := range rows {
		out[i] = orderListDTO{
			ID: r.ID, DocNo: r.DocNo, DocDate: r.DocDate.Format(time.DateOnly), ExpectedDate: dateString(r.ExpectedDate),
			SupplierCode: r.SupplierCode, SupplierName: r.SupplierName, Currency: r.Currency,
			TotalAmount: r.TotalAmount, Status: r.Status, ReceiptState: r.ReceiptState, Note: r.Note,
			CreatedByName: r.CreatedByName, UpdatedAt: r.UpdatedAt,
		}
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

// ---- 建立與修改(草稿) ----

type orderInput struct {
	headerInput
	ExpectedDate *string     `json:"expected_date"`
	Lines        []lineInput `json:"lines" binding:"dive"`
}

// prepareOrder 驗證單頭與明細;採購單明細不引用其他單據。
func prepareOrder(ctx context.Context, q *db.Queries, companyID int64, in *orderInput) (header, *time.Time, []pricedLine, totals, error) {
	h, err := checkHeader(ctx, q, companyID, &in.headerInput)
	if err != nil {
		return h, nil, nil, totals{}, err
	}
	var expected *time.Time
	if in.ExpectedDate != nil && *in.ExpectedDate != "" {
		t, err := parseDate("expected_date", *in.ExpectedDate)
		if err != nil {
			return h, nil, nil, totals{}, err
		}
		if t.Before(h.date) {
			return h, nil, nil, totals{}, fieldErr("expected_date", "預定交貨日不可早於採購日期")
		}
		expected = &t
	}
	for i := range in.Lines {
		in.Lines[i].PoLineID, in.Lines[i].ReceiptLineID = nil, nil
	}
	lines, t, err := priceLines(ctx, q, companyID, h, in.Lines)
	return h, expected, lines, t, err
}

func saveOrderLines(ctx context.Context, q *db.Queries, orderID int64, lines []pricedLine) error {
	if err := q.DeletePurchaseOrderLines(ctx, orderID); err != nil {
		return err
	}
	for i, l := range lines {
		if err := q.AddPurchaseOrderLine(ctx, db.AddPurchaseOrderLineParams{
			OrderID: orderID, LineNo: int32(i + 1), ItemID: l.ItemID, UnitID: l.UnitID, Qty: l.Qty,
			Factor: l.factor, BaseQty: l.baseQty, UnitPrice: l.UnitPrice, Amount: l.amount, Note: l.Note,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) createOrder(c *gin.Context) {
	var in orderInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto orderDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		h, expected, lines, t, err := prepareOrder(ctx, q, a.CompanyID, &in)
		if err != nil {
			return err
		}
		no, err := docno.Next(ctx, q, a.CompanyID, "purchase_order", h.date)
		if err != nil {
			return err
		}
		o, err := q.CreatePurchaseOrder(ctx, db.CreatePurchaseOrderParams{
			CompanyID: a.CompanyID, DocNo: no, DocDate: h.date, SupplierID: in.SupplierID, WarehouseID: in.WarehouseID,
			ExpectedDate: expected, Currency: in.Currency, ExchangeRate: h.rate, TaxTypeID: in.TaxTypeID,
			TaxRate: h.taxRate, PaymentTermID: in.PaymentTermID, UntaxedAmount: t.untaxed, TaxAmount: t.tax,
			TotalAmount: t.total, Note: in.Note, CreatedBy: &a.UserID,
		})
		if err != nil {
			return err
		}
		if err := saveOrderLines(ctx, q, o.ID, lines); err != nil {
			return err
		}
		if dto, err = loadOrder(ctx, q, a.CompanyID, o.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "purchase_order", EntityID: &o.ID, Summary: "新增採購單 " + no, After: dto,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto)
}

func (m *Module) updateOrder(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in orderInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto orderDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockPurchaseOrder(ctx, db.LockPurchaseOrderParams{ID: id, CompanyID: a.CompanyID})
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
			return errOrderEditNotAllowed
		}
		before, err := loadOrder(ctx, q, a.CompanyID, id)
		if err != nil {
			return err
		}
		h, expected, lines, t, err := prepareOrder(ctx, q, a.CompanyID, &in)
		if err != nil {
			return err
		}
		if _, err := q.UpdatePurchaseOrderHeader(ctx, db.UpdatePurchaseOrderHeaderParams{
			ID: id, CompanyID: a.CompanyID, DocDate: h.date, SupplierID: in.SupplierID, WarehouseID: in.WarehouseID,
			ExpectedDate: expected, Currency: in.Currency, ExchangeRate: h.rate, TaxTypeID: in.TaxTypeID,
			TaxRate: h.taxRate, PaymentTermID: in.PaymentTermID, UntaxedAmount: t.untaxed, TaxAmount: t.tax,
			TotalAmount: t.total, Note: in.Note, Version: in.Version, UpdatedBy: &a.UserID,
		}); err != nil {
			if database.IsNoRows(err) {
				return apperr.ErrVersionConflict
			}
			return err
		}
		if err := saveOrderLines(ctx, q, id, lines); err != nil {
			return err
		}
		if dto, err = loadOrder(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "purchase_order", EntityID: &id,
			Summary: "修改採購單 " + cur.DocNo, Before: before, After: dto,
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
	docstate.Post: "過帳", docstate.Unpost: "反過帳", docstate.Void: "作廢", docstate.Close: "結案",
	docstate.Reopen: "重開",
}

// orderTransition 採購單不過帳:核准後可結案(剩餘未交視為取消),結案可重開回「已核准」(D30)。
func orderTransition(from docstate.Status, action docstate.Action) (docstate.Status, error) {
	switch action {
	case docstate.Post, docstate.Unpost:
		return from, docstate.ErrInvalidTransition
	case docstate.Reopen:
		if from == docstate.Closed {
			return docstate.Approved, nil
		}
		return from, docstate.ErrInvalidTransition
	}
	return docstate.Transition(from, action)
}

func orderActionPermission(action docstate.Action, status docstate.Status) (string, bool) {
	switch action {
	case docstate.Submit:
		return permission.PurchaseOrderWrite, true
	case docstate.Approve, docstate.Reject, docstate.Unapprove, docstate.Close, docstate.Reopen:
		return permission.PurchaseOrderApprove, true
	case docstate.Void:
		if status == docstate.Draft {
			return permission.PurchaseOrderWrite, true
		}
		return permission.PurchaseOrderApprove, true
	default:
		return "", false
	}
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
	var dto orderDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockPurchaseOrder(ctx, db.LockPurchaseOrderParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		perm, ok := orderActionPermission(action, docstate.Status(cur.Status))
		if !ok {
			return apperr.ErrNotFound
		}
		if !a.Can(perm) {
			return apperr.ErrForbidden
		}
		if cur.Version != in.Version {
			return apperr.ErrVersionConflict
		}
		next, err := orderTransition(docstate.Status(cur.Status), action)
		if err != nil {
			return err
		}
		switch action {
		case docstate.Submit:
			doc, err := loadOrder(ctx, q, a.CompanyID, id)
			if err != nil {
				return err
			}
			if len(doc.Lines) == 0 {
				return fieldErr("lines", "請輸入明細")
			}
		case docstate.Unapprove, docstate.Void:
			// 已有進貨單(含草稿)時不可取消核准或作廢,否則進貨單會指向非核准的採購單
			no, err := q.PurchaseOrderReceiptNo(ctx, id)
			if err == nil {
				return apperr.Conflict("PUR-002", "採購單已有進貨單 "+no+",請先作廢進貨單;若不再進貨請改用結案")
			}
			if !database.IsNoRows(err) {
				return err
			}
		}
		if _, err := q.SetPurchaseOrderStatus(ctx, db.SetPurchaseOrderStatusParams{
			ID: id, CompanyID: a.CompanyID, Status: string(next), Action: string(action), ActorID: a.UserID,
			Version: in.Version,
		}); err != nil {
			return err
		}
		if dto, err = loadOrder(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: string(action), EntityType: "purchase_order", EntityID: &id,
			Summary: actionLabels[action] + "採購單 " + cur.DocNo,
			Before:  map[string]string{"status": cur.Status}, After: map[string]string{"status": string(next)},
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 未交貨清單 ----

type outstandingDTO struct {
	PoLineID     int64           `json:"po_line_id"`
	OrderID      int64           `json:"order_id"`
	DocNo        string          `json:"doc_no"`
	DocDate      string          `json:"doc_date"`
	ExpectedDate *string         `json:"expected_date"`
	SupplierID   int64           `json:"supplier_id"`
	SupplierCode string          `json:"supplier_code"`
	SupplierName string          `json:"supplier_name"`
	Currency     string          `json:"currency"`
	WarehouseID  int64           `json:"warehouse_id"`
	LineNo       int32           `json:"line_no"`
	ItemID       int64           `json:"item_id"`
	ItemCode     string          `json:"item_code"`
	ItemName     string          `json:"item_name"`
	ItemSpec     string          `json:"item_spec"`
	UnitID       int64           `json:"unit_id"`
	UnitName     string          `json:"unit_name"`
	Qty          decimal.Decimal `json:"qty"`
	UnitPrice    decimal.Decimal `json:"unit_price"`
	ReceivedQty  decimal.Decimal `json:"received_qty"`
	RemainingQty decimal.Decimal `json:"remaining_qty"`
}

// outstandingLines 已核准採購單中尚未交齊的明細;due_before 可查逾期(預定交貨日在此之前)。
func (m *Module) outstandingLines(c *gin.Context) {
	ctx := c.Request.Context()
	supplierID, err := httpx.QueryInt64(c, "supplier_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	dueBefore, err := optionalDate(c, "due_before")
	if err != nil {
		response.Error(c, err)
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	currency, keyword := httpx.QueryString(c, "currency"), httpx.QueryString(c, "keyword")
	rows, err := m.store.OutstandingPurchaseLines(ctx, db.OutstandingPurchaseLinesParams{
		CompanyID: companyID, SupplierID: supplierID, Currency: currency, Keyword: keyword, DueBefore: dueBefore,
		Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountOutstandingPurchaseLines(ctx, db.CountOutstandingPurchaseLinesParams{
		CompanyID: companyID, SupplierID: supplierID, Currency: currency, Keyword: keyword, DueBefore: dueBefore,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]outstandingDTO, len(rows))
	for i, r := range rows {
		out[i] = outstandingDTO{
			PoLineID: r.PoLineID, OrderID: r.OrderID, DocNo: r.DocNo, DocDate: r.DocDate.Format(time.DateOnly),
			ExpectedDate: dateString(r.ExpectedDate), SupplierID: r.SupplierID, SupplierCode: r.SupplierCode,
			SupplierName: r.SupplierName, Currency: r.Currency, WarehouseID: r.WarehouseID, LineNo: r.LineNo,
			ItemID: r.ItemID, ItemCode: r.ItemCode, ItemName: r.ItemName, ItemSpec: r.ItemSpec, UnitID: r.UnitID,
			UnitName: r.UnitName, Qty: r.Qty, UnitPrice: r.UnitPrice, ReceivedQty: r.ReceivedQty,
			RemainingQty: r.RemainingQty,
		}
	}
	response.List(c, out, pg.Meta(total))
}
