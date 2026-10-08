package sales

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/approval"
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
	"erp/internal/trade"
)

const (
	TypeQuotation = "quotation"
	TypeOrder     = "order"
)

// 單據類型 → 單號規則、中文名稱
var orderTypes = map[string]struct{ numbering, label string }{
	TypeQuotation: {"sales_quotation", "報價單"},
	TypeOrder:     {"sales_order", "訂單"},
}

var (
	errOrderEditNotAllowed = apperr.New(http.StatusConflict, "SAL-001", "只有草稿可以修改")
	errOrderTypeFixed      = fieldErr("doc_type", "建立後不可變更單據類型")
)

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
	DeliveredQty decimal.Decimal `json:"delivered_qty"` // 已過帳出貨量(輸入單位)
	RemainingQty decimal.Decimal `json:"remaining_qty"` // 未出貨量
	Note         string          `json:"note"`
}

type orderDTO struct {
	ID              int64           `json:"id"`
	DocType         string          `json:"doc_type"`
	DocNo           string          `json:"doc_no"`
	DocDate         string          `json:"doc_date"`
	CustomerID      int64           `json:"customer_id"`
	CustomerCode    string          `json:"customer_code"`
	CustomerName    string          `json:"customer_name"`
	SalesUserID     *int64          `json:"sales_user_id"`
	SalesUserName   *string         `json:"sales_user_name"`
	WarehouseID     int64           `json:"warehouse_id"`
	WarehouseName   string          `json:"warehouse_name"`
	QuotationID     *int64          `json:"quotation_id"`
	QuotationNo     *string         `json:"quotation_no"`
	ValidUntil      *string         `json:"valid_until"`
	DeliveryDate    *string         `json:"delivery_date"`
	CustomerPoNo    string          `json:"customer_po_no"`
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

// loadOrder 讀取報價單 / 訂單並檢查資料範圍。
func loadOrder(ctx context.Context, q *db.Queries, a *authctx.Actor, id int64) (orderDTO, error) {
	o, err := q.GetSalesOrder(ctx, db.GetSalesOrderParams{ID: id, CompanyID: a.CompanyID})
	if database.IsNoRows(err) {
		return orderDTO{}, apperr.ErrNotFound
	}
	if err != nil {
		return orderDTO{}, err
	}
	if err := visible(a, o.SalesUserID, o.SalesDepartmentID); err != nil {
		return orderDTO{}, err
	}
	rows, err := q.ListSalesOrderLines(ctx, id)
	if err != nil {
		return orderDTO{}, err
	}
	lines := make([]orderLineDTO, len(rows))
	for i, r := range rows {
		lines[i] = orderLineDTO{
			ID: r.ID, LineNo: r.LineNo, ItemID: r.ItemID, ItemCode: r.ItemCode, ItemName: r.ItemName,
			ItemSpec: r.ItemSpec, ItemType: r.ItemType, UnitID: r.UnitID, UnitName: r.UnitName,
			BaseUnitName: r.BaseUnitName, Qty: r.Qty, Factor: r.Factor, BaseQty: r.BaseQty, UnitPrice: r.UnitPrice,
			Amount: r.Amount, DeliveredQty: r.DeliveredQty,
			RemainingQty: decimal.Max(r.Qty.Sub(r.DeliveredQty), decimal.Zero), Note: r.Note,
		}
	}
	return orderDTO{
		ID: o.ID, DocType: o.DocType, DocNo: o.DocNo, DocDate: o.DocDate.Format(time.DateOnly),
		CustomerID: o.CustomerID, CustomerCode: o.CustomerCode, CustomerName: o.CustomerName,
		SalesUserID: o.SalesUserID, SalesUserName: o.SalesUserName, WarehouseID: o.WarehouseID,
		WarehouseName: o.WarehouseName, QuotationID: o.QuotationID, QuotationNo: o.QuotationNo,
		ValidUntil: dateString(o.ValidUntil), DeliveryDate: dateString(o.DeliveryDate), CustomerPoNo: o.CustomerPoNo,
		Currency: o.Currency, ExchangeRate: o.ExchangeRate, TaxTypeID: o.TaxTypeID, TaxTypeName: o.TaxTypeName,
		TaxRate: o.TaxRate, PaymentTermID: o.PaymentTermID, PaymentTermName: o.PaymentTermName,
		UntaxedAmount: o.UntaxedAmount, TaxAmount: o.TaxAmount, TotalAmount: o.TotalAmount, Status: o.Status,
		Note: o.Note, CreatedByName: o.CreatedByName, SubmittedByName: o.SubmittedByName,
		SubmittedAt: o.SubmittedAt, ApprovedByName: o.ApprovedByName, ApprovedAt: o.ApprovedAt,
		ClosedByName: o.ClosedByName, ClosedAt: o.ClosedAt, Lines: lines, Version: o.Version, UpdatedAt: o.UpdatedAt,
	}, nil
}

// ---- 列表與單筆 ----

type orderListDTO struct {
	ID            int64           `json:"id"`
	DocType       string          `json:"doc_type"`
	DocNo         string          `json:"doc_no"`
	DocDate       string          `json:"doc_date"`
	ValidUntil    *string         `json:"valid_until"`
	DeliveryDate  *string         `json:"delivery_date"`
	CustomerPoNo  string          `json:"customer_po_no"`
	CustomerCode  string          `json:"customer_code"`
	CustomerName  string          `json:"customer_name"`
	SalesUserName *string         `json:"sales_user_name"`
	Currency      string          `json:"currency"`
	TotalAmount   decimal.Decimal `json:"total_amount"`
	Status        string          `json:"status"`
	ShipState     string          `json:"ship_state"` // 訂單:none / partial / full
	Converted     bool            `json:"converted"`  // 報價單:已轉訂單
	Note          string          `json:"note"`
	CreatedByName *string         `json:"created_by_name"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func (m *Module) listOrders(c *gin.Context) {
	ctx := c.Request.Context()
	customerID, err := httpx.QueryInt64(c, "customer_id")
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
	a := actor(c)
	deptID, userID := a.ScopeFilter()
	docType, status, keyword := httpx.QueryString(c, "doc_type"), httpx.QueryString(c, "status"), httpx.QueryString(c, "keyword")
	rows, err := m.store.ListSalesOrders(ctx, db.ListSalesOrdersParams{
		CompanyID: a.CompanyID, DocType: docType, Status: status, CustomerID: customerID, Keyword: keyword,
		FromDate: from, ToDate: to, ScopeUserID: userID, ScopeDeptID: deptID, Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountSalesOrders(ctx, db.CountSalesOrdersParams{
		CompanyID: a.CompanyID, DocType: docType, Status: status, CustomerID: customerID, Keyword: keyword,
		FromDate: from, ToDate: to, ScopeUserID: userID, ScopeDeptID: deptID,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]orderListDTO, len(rows))
	for i, r := range rows {
		out[i] = orderListDTO{
			ID: r.ID, DocType: r.DocType, DocNo: r.DocNo, DocDate: r.DocDate.Format(time.DateOnly),
			ValidUntil: dateString(r.ValidUntil), DeliveryDate: dateString(r.DeliveryDate), CustomerPoNo: r.CustomerPoNo,
			CustomerCode: r.CustomerCode, CustomerName: r.CustomerName, SalesUserName: r.SalesUserName,
			Currency: r.Currency, TotalAmount: r.TotalAmount, Status: r.Status, ShipState: r.ShipState,
			Converted: r.Converted, Note: r.Note, CreatedByName: r.CreatedByName, UpdatedAt: r.UpdatedAt,
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
	dto, err := loadOrder(c.Request.Context(), m.store.Queries, actor(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 建立與修改(草稿) ----

type orderInput struct {
	headerInput
	DocType      string      `json:"doc_type" binding:"required,oneof=quotation order"`
	QuotationID  *int64      `json:"quotation_id"`  // 訂單:來源報價單
	ValidUntil   *string     `json:"valid_until"`   // 報價:有效期限
	DeliveryDate *string     `json:"delivery_date"` // 訂單:預定出貨日
	CustomerPoNo string      `json:"customer_po_no" binding:"max=50"`
	Lines        []lineInput `json:"lines" binding:"dive"`
}

type preparedOrder struct {
	h            trade.Header
	salesUserID  *int64
	validUntil   *time.Time
	deliveryDate *time.Time
	lines        []pricedLine
	totals       trade.Totals
}

func prepareOrder(ctx context.Context, q *db.Queries, a *authctx.Actor, in *orderInput) (preparedOrder, error) {
	var p preparedOrder
	in.CustomerPoNo = strings.TrimSpace(in.CustomerPoNo)
	h, cust, err := checkHeader(ctx, q, a, &in.headerInput)
	if err != nil {
		return p, err
	}
	p.h, p.salesUserID = h, cust.SalesUserID
	// 報價單只用有效期限、訂單只用預定出貨日與來源報價單
	if in.DocType == TypeQuotation {
		in.DeliveryDate, in.QuotationID = nil, nil
		if p.validUntil, err = trade.OptionalInputDate("valid_until", in.ValidUntil); err != nil {
			return p, err
		}
		if p.validUntil != nil && p.validUntil.Before(h.Date) {
			return p, fieldErr("valid_until", "有效期限不可早於報價日期")
		}
	} else {
		in.ValidUntil = nil
		if p.deliveryDate, err = trade.OptionalInputDate("delivery_date", in.DeliveryDate); err != nil {
			return p, err
		}
		if p.deliveryDate != nil && p.deliveryDate.Before(h.Date) {
			return p, fieldErr("delivery_date", "預定出貨日不可早於訂單日期")
		}
		if in.QuotationID != nil {
			qt, err := q.GetSalesOrder(ctx, db.GetSalesOrderParams{ID: *in.QuotationID, CompanyID: a.CompanyID})
			switch {
			case database.IsNoRows(err) || (err == nil && (qt.DocType != TypeQuotation || visible(a, qt.SalesUserID, qt.SalesDepartmentID) != nil)):
				return p, fieldErr("quotation_id", "報價單不存在")
			case err != nil:
				return p, err
			case qt.Status != string(docstate.Approved):
				return p, fieldErr("quotation_id", "報價單 "+qt.DocNo+" 未核准或已結案")
			case qt.CustomerID != in.CustomerID:
				return p, fieldErr("quotation_id", "報價單 "+qt.DocNo+" 的客戶與本單不同")
			}
		}
	}
	for i := range in.Lines {
		in.Lines[i].SoLineID, in.Lines[i].DeliveryLineID = nil, nil
	}
	p.lines, p.totals, err = priceLines(ctx, q, a.CompanyID, h, in.Lines)
	return p, err
}

func saveOrderLines(ctx context.Context, q *db.Queries, orderID int64, lines []pricedLine) error {
	if err := q.DeleteSalesOrderLines(ctx, orderID); err != nil {
		return err
	}
	for i, l := range lines {
		if err := q.AddSalesOrderLine(ctx, db.AddSalesOrderLineParams{
			OrderID: orderID, LineNo: int32(i + 1), ItemID: l.ItemID, UnitID: l.UnitID, Qty: l.Qty,
			Factor: l.Factor, BaseQty: l.BaseQty, UnitPrice: l.UnitPrice, Amount: l.Amount, Note: l.Note,
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
		p, err := prepareOrder(ctx, q, a, &in)
		if err != nil {
			return err
		}
		typ := orderTypes[in.DocType]
		no, err := docno.Next(ctx, q, a.CompanyID, typ.numbering, p.h.Date)
		if err != nil {
			return err
		}
		o, err := q.CreateSalesOrder(ctx, db.CreateSalesOrderParams{
			CompanyID: a.CompanyID, DocType: in.DocType, DocNo: no, DocDate: p.h.Date, CustomerID: in.CustomerID,
			SalesUserID: p.salesUserID, WarehouseID: in.WarehouseID, QuotationID: in.QuotationID,
			ValidUntil: p.validUntil, DeliveryDate: p.deliveryDate, CustomerPoNo: in.CustomerPoNo,
			Currency: in.Currency, ExchangeRate: p.h.Rate, TaxTypeID: in.TaxTypeID, TaxRate: p.h.TaxRate,
			PaymentTermID: in.PaymentTermID, UntaxedAmount: p.totals.Untaxed, TaxAmount: p.totals.Tax,
			TotalAmount: p.totals.Total, Note: in.Note, CreatedBy: &a.UserID,
		})
		if err != nil {
			return err
		}
		if err := saveOrderLines(ctx, q, o.ID, p.lines); err != nil {
			return err
		}
		if dto, err = loadOrder(ctx, q, a, o.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "sales_order", EntityID: &o.ID, Summary: "新增" + typ.label + " " + no, After: dto,
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
		cur, err := q.LockSalesOrder(ctx, db.LockSalesOrderParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		before, err := loadOrder(ctx, q, a, id) // 含資料範圍檢查
		if err != nil {
			return err
		}
		if cur.Version != in.Version {
			return apperr.ErrVersionConflict
		}
		if !docstate.Editable(docstate.Status(cur.Status)) {
			return errOrderEditNotAllowed
		}
		if in.DocType != cur.DocType {
			return errOrderTypeFixed
		}
		p, err := prepareOrder(ctx, q, a, &in)
		if err != nil {
			return err
		}
		if _, err := q.UpdateSalesOrderHeader(ctx, db.UpdateSalesOrderHeaderParams{
			ID: id, CompanyID: a.CompanyID, DocDate: p.h.Date, CustomerID: in.CustomerID, SalesUserID: p.salesUserID,
			WarehouseID: in.WarehouseID, QuotationID: in.QuotationID, ValidUntil: p.validUntil,
			DeliveryDate: p.deliveryDate, CustomerPoNo: in.CustomerPoNo, Currency: in.Currency, ExchangeRate: p.h.Rate,
			TaxTypeID: in.TaxTypeID, TaxRate: p.h.TaxRate, PaymentTermID: in.PaymentTermID,
			UntaxedAmount: p.totals.Untaxed, TaxAmount: p.totals.Tax, TotalAmount: p.totals.Total, Note: in.Note,
			Version: in.Version, UpdatedBy: &a.UserID,
		}); err != nil {
			if database.IsNoRows(err) {
				return apperr.ErrVersionConflict
			}
			return err
		}
		if err := saveOrderLines(ctx, q, id, p.lines); err != nil {
			return err
		}
		if dto, err = loadOrder(ctx, q, a, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "sales_order", EntityID: &id,
			Summary: "修改" + orderTypes[cur.DocType].label + " " + cur.DocNo, Before: before, After: dto,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 狀態動作 ----

func orderActionPermission(action docstate.Action, status docstate.Status) (string, bool) {
	switch action {
	case docstate.Submit:
		return permission.SalesOrderWrite, true
	case docstate.Approve, docstate.Reject, docstate.Unapprove, docstate.Close, docstate.Reopen:
		return permission.SalesOrderApprove, true
	case docstate.Void:
		if status == docstate.Draft {
			return permission.SalesOrderWrite, true
		}
		return permission.SalesOrderApprove, true
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
		cur, err := q.LockSalesOrder(ctx, db.LockSalesOrderParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		doc, err := loadOrder(ctx, q, a, id) // 含資料範圍檢查
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
		next, err := trade.OrderTransition(docstate.Status(cur.Status), action)
		if err != nil {
			return err
		}
		if partial, msg, err := approval.Intercept(ctx, q, a, approval.SalesOrder, id, action, cur.TotalAmount.Mul(cur.ExchangeRate)); err != nil {
			return err
		} else if partial {
			if dto, err = loadOrder(ctx, q, a, id); err != nil {
				return err
			}
			return audit.Record(ctx, q, audit.Entry{
				Action: "approve_step", EntityType: "sales_order", EntityID: &id, Summary: msg + " 訂單 " + cur.DocNo,
			})
		}
		switch action {
		case docstate.Submit:
			if len(doc.Lines) == 0 {
				return fieldErr("lines", "請輸入明細")
			}
		case docstate.Approve:
			if cur.DocType == TypeOrder {
				if err := checkCredit(ctx, q, a.CompanyID, cur); err != nil {
					return err
				}
			}
		case docstate.Unapprove, docstate.Void:
			if err := checkNoDownstream(ctx, q, cur); err != nil {
				return err
			}
		}
		if _, err := q.SetSalesOrderStatus(ctx, db.SetSalesOrderStatusParams{
			ID: id, CompanyID: a.CompanyID, Status: string(next), Action: string(action), ActorID: a.UserID,
			Version: in.Version,
		}); err != nil {
			return err
		}
		if dto, err = loadOrder(ctx, q, a, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: string(action), EntityType: "sales_order", EntityID: &id,
			Summary: actionLabels[action] + orderTypes[cur.DocType].label + " " + cur.DocNo,
			Before:  map[string]string{"status": cur.Status}, After: map[string]string{"status": string(next)},
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// checkNoDownstream 已有下游單據(訂單 → 出貨單、報價單 → 訂單)時不可取消核准或作廢,只能結案。
func checkNoDownstream(ctx context.Context, q *db.Queries, cur db.SalesOrder) error {
	var no, msg string
	var err error
	if cur.DocType == TypeOrder {
		no, err = q.SalesOrderDeliveryNo(ctx, cur.ID)
		msg = "訂單已有出貨單 %s,請先作廢出貨單;若不再出貨請改用結案"
	} else {
		no, err = q.QuotationOrderNo(ctx, &cur.ID)
		msg = "報價單已轉訂單 %s,請先作廢訂單"
	}
	if err == nil {
		return apperr.Conflict("SAL-002", fmt.Sprintf(msg, no))
	}
	if database.IsNoRows(err) {
		return nil
	}
	return err
}

// checkCredit 核准訂單時檢查信用額度(D39):未沖應收 + 其他已核准訂單未出貨金額 + 本單 ≤ 額度;額度 0 表示不限。
// 先鎖定客戶列,同一客戶的訂單核准依序進行,避免兩張單同時通過檢查。
func checkCredit(ctx context.Context, q *db.Queries, companyID int64, cur db.SalesOrder) error {
	if err := q.LockCustomer(ctx, db.LockCustomerParams{ID: cur.CustomerID, CompanyID: companyID}); err != nil {
		return err
	}
	cust, err := q.CustomerForDoc(ctx, db.CustomerForDocParams{ID: cur.CustomerID, CompanyID: companyID})
	if err != nil {
		return err
	}
	if !cust.CreditLimit.IsPositive() {
		return nil
	}
	exp, err := q.CustomerExposure(ctx, db.CustomerExposureParams{CustomerID: cur.CustomerID, ExcludeOrderID: cur.ID})
	if err != nil {
		return err
	}
	used := money.Amount(exp.ArOpen.Add(exp.OpenOrders))
	this := money.Amount(cur.TotalAmount.Mul(cur.ExchangeRate))
	if used.Add(this).GreaterThan(cust.CreditLimit) {
		return apperr.New(http.StatusUnprocessableEntity, "SAL-003", fmt.Sprintf(
			"超過信用額度:額度 %s,已使用 %s(未收應收 %s、未出貨訂單 %s),本單 %s",
			cust.CreditLimit.StringFixed(0), used.String(), money.Amount(exp.ArOpen).String(),
			money.Amount(exp.OpenOrders).String(), this.String(),
		)).WithDetails(map[string]string{
			"credit_limit": cust.CreditLimit.String(), "used": used.String(), "this": this.String(),
		})
	}
	return nil
}

// ---- 可用量 ----

type availabilityDTO struct {
	ItemID    int64           `json:"item_id"`
	OnHand    decimal.Decimal `json:"on_hand"`
	Reserved  decimal.Decimal `json:"reserved"`
	Available decimal.Decimal `json:"available"`
}

// availability GET /sales/availability?warehouse_id=&item_ids=1,2&exclude_order_id=
// 回傳商品類料品在指定倉庫的現有量、保留量(其他已核准訂單未出貨)與可用量,皆為基本單位。
func (m *Module) availability(c *gin.Context) {
	whID, err := httpx.QueryInt64(c, "warehouse_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	if whID == nil {
		response.Error(c, fieldErr("warehouse_id", "請指定倉庫"))
		return
	}
	exclude, err := httpx.QueryInt64(c, "exclude_order_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var ids []int64
	for _, s := range strings.Split(c.Query("item_ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil || id <= 0 {
			response.Error(c, fieldErr("item_ids", "料品 id 格式錯誤"))
			return
		}
		ids = append(ids, id)
	}
	if len(ids) > trade.MaxLines {
		response.Error(c, fieldErr("item_ids", "料品過多"))
		return
	}
	var ex int64
	if exclude != nil {
		ex = *exclude
	}
	rows, err := m.store.ItemAvailability(c.Request.Context(), db.ItemAvailabilityParams{
		CompanyID: actor(c).CompanyID, WarehouseID: *whID, ItemIds: ids, ExcludeOrderID: ex,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]availabilityDTO, len(rows))
	for i, r := range rows {
		out[i] = availabilityDTO{ItemID: r.ItemID, OnHand: r.OnHand, Reserved: r.Reserved, Available: r.OnHand.Sub(r.Reserved)}
	}
	response.OK(c, out)
}

// ---- 未出貨清單 ----

type unshippedDTO struct {
	SoLineID     int64           `json:"so_line_id"`
	OrderID      int64           `json:"order_id"`
	DocNo        string          `json:"doc_no"`
	DocDate      string          `json:"doc_date"`
	DeliveryDate *string         `json:"delivery_date"`
	CustomerID   int64           `json:"customer_id"`
	CustomerCode string          `json:"customer_code"`
	CustomerName string          `json:"customer_name"`
	CustomerPoNo string          `json:"customer_po_no"`
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
	DeliveredQty decimal.Decimal `json:"delivered_qty"`
	RemainingQty decimal.Decimal `json:"remaining_qty"`
}

// unshippedLines 已核准訂單中尚未出齊的明細(依資料範圍);due_before 可查逾期。
func (m *Module) unshippedLines(c *gin.Context) {
	ctx := c.Request.Context()
	customerID, err := httpx.QueryInt64(c, "customer_id")
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
	a := actor(c)
	deptID, userID := a.ScopeFilter()
	currency, keyword := httpx.QueryString(c, "currency"), httpx.QueryString(c, "keyword")
	rows, err := m.store.UnshippedSalesLines(ctx, db.UnshippedSalesLinesParams{
		CompanyID: a.CompanyID, CustomerID: customerID, Currency: currency, Keyword: keyword, DueBefore: dueBefore,
		ScopeUserID: userID, ScopeDeptID: deptID, Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountUnshippedSalesLines(ctx, db.CountUnshippedSalesLinesParams{
		CompanyID: a.CompanyID, CustomerID: customerID, Currency: currency, Keyword: keyword, DueBefore: dueBefore,
		ScopeUserID: userID, ScopeDeptID: deptID,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]unshippedDTO, len(rows))
	for i, r := range rows {
		out[i] = unshippedDTO{
			SoLineID: r.SoLineID, OrderID: r.OrderID, DocNo: r.DocNo, DocDate: r.DocDate.Format(time.DateOnly),
			DeliveryDate: dateString(r.DeliveryDate), CustomerID: r.CustomerID, CustomerCode: r.CustomerCode,
			CustomerName: r.CustomerName, CustomerPoNo: r.CustomerPoNo, Currency: r.Currency,
			WarehouseID: r.WarehouseID, LineNo: r.LineNo, ItemID: r.ItemID, ItemCode: r.ItemCode,
			ItemName: r.ItemName, ItemSpec: r.ItemSpec, UnitID: r.UnitID, UnitName: r.UnitName, Qty: r.Qty,
			UnitPrice: r.UnitPrice, DeliveredQty: r.DeliveredQty, RemainingQty: r.RemainingQty,
		}
	}
	response.List(c, out, pg.Meta(total))
}
