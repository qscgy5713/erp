package sales

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"regexp"
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
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/system/docno"
	"erp/internal/system/permission"
	"erp/internal/trade"
)

const (
	TypeDelivery = "delivery" // 出貨
	TypeReturn   = "return"   // 銷貨退回
)

// 單據類型 → 單號規則、流水帳 / 應收來源類型、中文名稱
var deliveryTypes = map[string]struct{ numbering, source, label string }{
	TypeDelivery: {"delivery", finance.SourceDelivery, "出貨單"},
	TypeReturn:   {"sales_return", finance.SourceSalesReturn, "銷貨退回單"},
}

var (
	errDeliveryEditNotAllowed = apperr.New(http.StatusConflict, "SAL-004", "只有草稿可以修改")
	errDeliveryTypeFixed      = fieldErr("doc_type", "建立後不可變更單據類型")
	invoiceNoPattern          = regexp.MustCompile(`^[A-Z]{2}[0-9]{8}$`)
)

type deliveryLineDTO struct {
	ID               int64           `json:"id"`
	LineNo           int32           `json:"line_no"`
	ItemID           int64           `json:"item_id"`
	ItemCode         string          `json:"item_code"`
	ItemName         string          `json:"item_name"`
	ItemSpec         string          `json:"item_spec"`
	ItemType         string          `json:"item_type"`
	UnitID           int64           `json:"unit_id"`
	UnitName         string          `json:"unit_name"`
	BaseUnitName     string          `json:"base_unit_name"`
	Qty              decimal.Decimal `json:"qty"`
	Factor           decimal.Decimal `json:"factor"`
	BaseQty          decimal.Decimal `json:"base_qty"`
	UnitPrice        decimal.Decimal `json:"unit_price"`
	Amount           decimal.Decimal `json:"amount"`
	BaseAmount       decimal.Decimal `json:"base_amount"`
	SoLineID         *int64          `json:"so_line_id"`
	SoNo             *string         `json:"so_no"`
	DeliveryLineID   *int64          `json:"delivery_line_id"`
	SourceDeliveryNo *string         `json:"source_delivery_no"`
	Note             string          `json:"note"`
}

type deliveryDTO struct {
	ID              int64             `json:"id"`
	DocType         string            `json:"doc_type"`
	DocNo           string            `json:"doc_no"`
	DocDate         string            `json:"doc_date"`
	CustomerID      int64             `json:"customer_id"`
	CustomerCode    string            `json:"customer_code"`
	CustomerName    string            `json:"customer_name"`
	SalesUserID     *int64            `json:"sales_user_id"`
	SalesUserName   *string           `json:"sales_user_name"`
	WarehouseID     int64             `json:"warehouse_id"`
	WarehouseName   string            `json:"warehouse_name"`
	Currency        string            `json:"currency"`
	ExchangeRate    decimal.Decimal   `json:"exchange_rate"`
	TaxTypeID       int64             `json:"tax_type_id"`
	TaxTypeName     string            `json:"tax_type_name"`
	TaxRate         decimal.Decimal   `json:"tax_rate"`
	PaymentTermID   *int64            `json:"payment_term_id"`
	PaymentTermName *string           `json:"payment_term_name"`
	InvoiceNo       string            `json:"invoice_no"`
	InvoiceDate     *string           `json:"invoice_date"`
	UntaxedAmount   decimal.Decimal   `json:"untaxed_amount"`
	TaxAmount       decimal.Decimal   `json:"tax_amount"`
	TotalAmount     decimal.Decimal   `json:"total_amount"`
	BaseUntaxed     decimal.Decimal   `json:"base_untaxed"`
	BaseTax         decimal.Decimal   `json:"base_tax"`
	BaseTotal       decimal.Decimal   `json:"base_total"`
	Status          string            `json:"status"`
	Note            string            `json:"note"`
	CreatedByName   *string           `json:"created_by_name"`
	SubmittedByName *string           `json:"submitted_by_name"`
	SubmittedAt     *time.Time        `json:"submitted_at"`
	ApprovedByName  *string           `json:"approved_by_name"`
	ApprovedAt      *time.Time        `json:"approved_at"`
	PostedByName    *string           `json:"posted_by_name"`
	PostedAt        *time.Time        `json:"posted_at"`
	Lines           []deliveryLineDTO `json:"lines"`
	Version         int32             `json:"version"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// loadDelivery 讀取出貨 / 退回單並檢查資料範圍。
func loadDelivery(ctx context.Context, q *db.Queries, a *authctx.Actor, id int64) (deliveryDTO, error) {
	d, err := q.GetDelivery(ctx, db.GetDeliveryParams{ID: id, CompanyID: a.CompanyID})
	if database.IsNoRows(err) {
		return deliveryDTO{}, apperr.ErrNotFound
	}
	if err != nil {
		return deliveryDTO{}, err
	}
	if err := visible(a, d.SalesUserID, d.SalesDepartmentID); err != nil {
		return deliveryDTO{}, err
	}
	rows, err := q.ListDeliveryLines(ctx, id)
	if err != nil {
		return deliveryDTO{}, err
	}
	lines := make([]deliveryLineDTO, len(rows))
	for i, l := range rows {
		lines[i] = deliveryLineDTO{
			ID: l.ID, LineNo: l.LineNo, ItemID: l.ItemID, ItemCode: l.ItemCode, ItemName: l.ItemName,
			ItemSpec: l.ItemSpec, ItemType: l.ItemType, UnitID: l.UnitID, UnitName: l.UnitName,
			BaseUnitName: l.BaseUnitName, Qty: l.Qty, Factor: l.Factor, BaseQty: l.BaseQty, UnitPrice: l.UnitPrice,
			Amount: l.Amount, BaseAmount: l.BaseAmount, SoLineID: l.SoLineID, SoNo: l.SoNo,
			DeliveryLineID: l.DeliveryLineID, SourceDeliveryNo: l.SourceDeliveryNo, Note: l.Note,
		}
	}
	return deliveryDTO{
		ID: d.ID, DocType: d.DocType, DocNo: d.DocNo, DocDate: d.DocDate.Format(time.DateOnly),
		CustomerID: d.CustomerID, CustomerCode: d.CustomerCode, CustomerName: d.CustomerName,
		SalesUserID: d.SalesUserID, SalesUserName: d.SalesUserName, WarehouseID: d.WarehouseID,
		WarehouseName: d.WarehouseName, Currency: d.Currency, ExchangeRate: d.ExchangeRate, TaxTypeID: d.TaxTypeID,
		TaxTypeName: d.TaxTypeName, TaxRate: d.TaxRate, PaymentTermID: d.PaymentTermID,
		PaymentTermName: d.PaymentTermName, InvoiceNo: d.InvoiceNo, InvoiceDate: dateString(d.InvoiceDate),
		UntaxedAmount: d.UntaxedAmount, TaxAmount: d.TaxAmount, TotalAmount: d.TotalAmount,
		BaseUntaxed: d.BaseUntaxed, BaseTax: d.BaseTax, BaseTotal: d.BaseTotal, Status: d.Status, Note: d.Note,
		CreatedByName: d.CreatedByName, SubmittedByName: d.SubmittedByName, SubmittedAt: d.SubmittedAt,
		ApprovedByName: d.ApprovedByName, ApprovedAt: d.ApprovedAt, PostedByName: d.PostedByName,
		PostedAt: d.PostedAt, Lines: lines, Version: d.Version, UpdatedAt: d.UpdatedAt,
	}, nil
}

// ---- 列表與單筆 ----

type deliveryListDTO struct {
	ID            int64           `json:"id"`
	DocType       string          `json:"doc_type"`
	DocNo         string          `json:"doc_no"`
	DocDate       string          `json:"doc_date"`
	CustomerCode  string          `json:"customer_code"`
	CustomerName  string          `json:"customer_name"`
	SalesUserName *string         `json:"sales_user_name"`
	WarehouseName string          `json:"warehouse_name"`
	Currency      string          `json:"currency"`
	TotalAmount   decimal.Decimal `json:"total_amount"`
	InvoiceNo     string          `json:"invoice_no"`
	Status        string          `json:"status"`
	Note          string          `json:"note"`
	CreatedByName *string         `json:"created_by_name"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func (m *Module) listDeliveries(c *gin.Context) {
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
	noInvoice, err := httpx.QueryBool(c, "no_invoice")
	if err != nil {
		response.Error(c, err)
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	a := actor(c)
	deptID, userID := a.ScopeFilter()
	docType, status, keyword := httpx.QueryString(c, "doc_type"), httpx.QueryString(c, "status"), httpx.QueryString(c, "keyword")
	ni := noInvoice != nil && *noInvoice
	rows, err := m.store.ListDeliveries(ctx, db.ListDeliveriesParams{
		CompanyID: a.CompanyID, DocType: docType, Status: status, CustomerID: customerID, Keyword: keyword,
		FromDate: from, ToDate: to, NoInvoice: ni, ScopeUserID: userID, ScopeDeptID: deptID,
		Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountDeliveries(ctx, db.CountDeliveriesParams{
		CompanyID: a.CompanyID, DocType: docType, Status: status, CustomerID: customerID, Keyword: keyword,
		FromDate: from, ToDate: to, NoInvoice: ni, ScopeUserID: userID, ScopeDeptID: deptID,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]deliveryListDTO, len(rows))
	for i, r := range rows {
		out[i] = deliveryListDTO{
			ID: r.ID, DocType: r.DocType, DocNo: r.DocNo, DocDate: r.DocDate.Format(time.DateOnly),
			CustomerCode: r.CustomerCode, CustomerName: r.CustomerName, SalesUserName: r.SalesUserName,
			WarehouseName: r.WarehouseName, Currency: r.Currency, TotalAmount: r.TotalAmount, InvoiceNo: r.InvoiceNo,
			Status: r.Status, Note: r.Note, CreatedByName: r.CreatedByName, UpdatedAt: r.UpdatedAt,
		}
	}
	response.List(c, out, pg.Meta(total))
}

func (m *Module) getDelivery(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	dto, err := loadDelivery(c.Request.Context(), m.store.Queries, actor(c), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 來源單據檢查 ----

type refLine struct {
	itemID, unitID           int64
	qty                      decimal.Decimal
	soLineID, deliveryLineID *int64
}

type refDoc struct {
	companyID  int64
	docType    string
	customerID int64
	currency   string
	excludeID  int64
}

// checkRefs 驗證明細引用的來源並檢查剩餘量,回傳各明細索引的錯誤訊息。
// 出貨引用「已核准」訂單明細,累計不可超過未出貨量;退回引用「已過帳」出貨明細,累計不可超過可退量。
// lock 為 true(過帳)時先依 id 順序鎖定來源單據再重新讀取剩餘量,避免同時過帳而超出。
func checkRefs(ctx context.Context, q *db.Queries, d refDoc, lines []refLine, lock bool) (map[int]string, error) {
	errs := map[int]string{}
	var ids []int64
	for _, l := range lines {
		if d.docType == TypeDelivery && l.soLineID != nil {
			ids = append(ids, *l.soLineID)
		}
		if d.docType == TypeReturn && l.deliveryLineID != nil {
			ids = append(ids, *l.deliveryLineID)
		}
	}
	if len(ids) == 0 {
		return errs, nil
	}

	type source struct {
		docID, customerID, itemID, unitID int64
		docNo, docType, status, currency  string
		available                         decimal.Decimal
	}
	fetch := func() (map[int64]source, error) {
		out := map[int64]source{}
		if d.docType == TypeDelivery {
			rows, err := q.SoLineRefs(ctx, db.SoLineRefsParams{CompanyID: d.companyID, Ids: ids, ExcludeDeliveryID: d.excludeID})
			if err != nil {
				return nil, err
			}
			for _, r := range rows {
				out[r.ID] = source{r.OrderID, r.CustomerID, r.ItemID, r.UnitID, r.DocNo, r.DocType, r.Status, r.Currency, r.Qty.Sub(r.DeliveredQty)}
			}
			return out, nil
		}
		rows, err := q.DeliveryLineRefs(ctx, db.DeliveryLineRefsParams{CompanyID: d.companyID, Ids: ids, ExcludeDeliveryID: d.excludeID})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.ID] = source{r.DeliveryID, r.CustomerID, r.ItemID, r.UnitID, r.DocNo, r.DocType, r.Status, r.Currency, r.Qty.Sub(r.ReturnedQty)}
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
		if d.docType == TypeDelivery {
			_, err = q.LockSalesOrders(ctx, db.LockSalesOrdersParams{CompanyID: d.companyID, Ids: sorted})
		} else {
			_, err = q.LockDeliveries(ctx, db.LockDeliveriesParams{CompanyID: d.companyID, Ids: sorted})
		}
		if err != nil {
			return nil, err
		}
		if sources, err = fetch(); err != nil {
			return nil, err
		}
	}

	srcLabel, srcType, wantStatus, what := "訂單", TypeOrder, string(docstate.Approved), "未出貨量"
	if d.docType == TypeReturn {
		srcLabel, srcType, wantStatus, what = "出貨單", TypeDelivery, string(docstate.Posted), "可退量"
	}
	used := map[int64]decimal.Decimal{}
	for i, l := range lines {
		ref := l.soLineID
		if d.docType == TypeReturn {
			ref = l.deliveryLineID
		}
		if ref == nil {
			continue
		}
		s, ok := sources[*ref]
		switch {
		case !ok || s.docType != srcType:
			errs[i] = "找不到來源" + srcLabel + "明細"
		case s.status != wantStatus:
			if d.docType == TypeDelivery {
				errs[i] = fmt.Sprintf("訂單 %s 未核准或已結案", s.docNo)
			} else {
				errs[i] = fmt.Sprintf("出貨單 %s 尚未過帳", s.docNo)
			}
		case s.customerID != d.customerID:
			errs[i] = fmt.Sprintf("%s %s 的客戶與本單不同", srcLabel, s.docNo)
		case s.currency != d.currency:
			errs[i] = fmt.Sprintf("%s %s 的幣別為 %s", srcLabel, s.docNo, s.currency)
		case s.itemID != l.itemID || s.unitID != l.unitID:
			errs[i] = fmt.Sprintf("料品或單位與%s %s 不同", srcLabel, s.docNo)
		default:
			used[*ref] = used[*ref].Add(l.qty)
			if used[*ref].GreaterThan(s.available) {
				errs[i] = fmt.Sprintf("超過%s %s 的%s %s", srcLabel, s.docNo, what, decimal.Max(s.available, decimal.Zero).String())
			}
		}
	}
	return errs, nil
}

// ---- 建立與修改(草稿) ----

type deliveryInput struct {
	headerInput
	DocType string      `json:"doc_type" binding:"required,oneof=delivery return"`
	Lines   []lineInput `json:"lines" binding:"dive"`
}

type preparedDelivery struct {
	h           trade.Header
	salesUserID *int64
	lines       []pricedLine
	totals      trade.Totals
}

func prepareDelivery(ctx context.Context, q *db.Queries, a *authctx.Actor, excludeID int64, in *deliveryInput) (preparedDelivery, error) {
	var p preparedDelivery
	h, cust, err := checkHeader(ctx, q, a, &in.headerInput)
	if err != nil {
		return p, err
	}
	p.h, p.salesUserID = h, cust.SalesUserID
	refs := make([]refLine, len(in.Lines))
	for i := range in.Lines {
		// 出貨只能引用訂單明細、退回只能引用出貨明細
		if in.DocType == TypeDelivery {
			in.Lines[i].DeliveryLineID = nil
		} else {
			in.Lines[i].SoLineID = nil
		}
		l := in.Lines[i]
		refs[i] = refLine{itemID: l.ItemID, unitID: l.UnitID, qty: l.Qty, soLineID: l.SoLineID, deliveryLineID: l.DeliveryLineID}
	}
	if p.lines, p.totals, err = priceLines(ctx, q, a.CompanyID, h, in.Lines); err != nil {
		return p, err
	}
	errs, err := checkRefs(ctx, q, refDoc{a.CompanyID, in.DocType, in.CustomerID, in.Currency, excludeID}, refs, false)
	if err != nil {
		return p, err
	}
	if len(errs) > 0 {
		fields := map[string]string{}
		for i, msg := range errs {
			fields[fmt.Sprintf("lines.%d", i)] = msg
		}
		return p, apperr.Validation(fields)
	}
	return p, nil
}

func saveDeliveryLines(ctx context.Context, q *db.Queries, deliveryID int64, lines []pricedLine) error {
	if err := q.DeleteDeliveryLines(ctx, deliveryID); err != nil {
		return err
	}
	for i, l := range lines {
		if err := q.AddDeliveryLine(ctx, db.AddDeliveryLineParams{
			DeliveryID: deliveryID, LineNo: int32(i + 1), ItemID: l.ItemID, UnitID: l.UnitID, Qty: l.Qty,
			Factor: l.Factor, BaseQty: l.BaseQty, UnitPrice: l.UnitPrice, Amount: l.Amount, BaseAmount: l.BaseAmount,
			SoLineID: l.SoLineID, DeliveryLineID: l.DeliveryLineID, Note: l.Note,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) createDelivery(c *gin.Context) {
	var in deliveryInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto deliveryDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		p, err := prepareDelivery(ctx, q, a, 0, &in)
		if err != nil {
			return err
		}
		typ := deliveryTypes[in.DocType]
		no, err := docno.Next(ctx, q, a.CompanyID, typ.numbering, p.h.Date)
		if err != nil {
			return err
		}
		d, err := q.CreateDelivery(ctx, db.CreateDeliveryParams{
			CompanyID: a.CompanyID, DocType: in.DocType, DocNo: no, DocDate: p.h.Date, CustomerID: in.CustomerID,
			SalesUserID: p.salesUserID, WarehouseID: in.WarehouseID, Currency: in.Currency, ExchangeRate: p.h.Rate,
			TaxTypeID: in.TaxTypeID, TaxRate: p.h.TaxRate, PaymentTermID: in.PaymentTermID,
			UntaxedAmount: p.totals.Untaxed, TaxAmount: p.totals.Tax, TotalAmount: p.totals.Total,
			BaseUntaxed: p.totals.BaseUntaxed, BaseTax: p.totals.BaseTax, BaseTotal: p.totals.BaseTotal,
			Note: in.Note, CreatedBy: &a.UserID,
		})
		if err != nil {
			return err
		}
		if err := saveDeliveryLines(ctx, q, d.ID, p.lines); err != nil {
			return err
		}
		if dto, err = loadDelivery(ctx, q, a, d.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "delivery", EntityID: &d.ID, Summary: "新增" + typ.label + " " + no, After: dto,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto)
}

func (m *Module) updateDelivery(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in deliveryInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto deliveryDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockDelivery(ctx, db.LockDeliveryParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		before, err := loadDelivery(ctx, q, a, id) // 含資料範圍檢查
		if err != nil {
			return err
		}
		if cur.Version != in.Version {
			return apperr.ErrVersionConflict
		}
		if !docstate.Editable(docstate.Status(cur.Status)) {
			return errDeliveryEditNotAllowed
		}
		if in.DocType != cur.DocType {
			return errDeliveryTypeFixed
		}
		p, err := prepareDelivery(ctx, q, a, id, &in)
		if err != nil {
			return err
		}
		if _, err := q.UpdateDeliveryHeader(ctx, db.UpdateDeliveryHeaderParams{
			ID: id, CompanyID: a.CompanyID, DocDate: p.h.Date, CustomerID: in.CustomerID, SalesUserID: p.salesUserID,
			WarehouseID: in.WarehouseID, Currency: in.Currency, ExchangeRate: p.h.Rate, TaxTypeID: in.TaxTypeID,
			TaxRate: p.h.TaxRate, PaymentTermID: in.PaymentTermID, UntaxedAmount: p.totals.Untaxed,
			TaxAmount: p.totals.Tax, TotalAmount: p.totals.Total, BaseUntaxed: p.totals.BaseUntaxed,
			BaseTax: p.totals.BaseTax, BaseTotal: p.totals.BaseTotal, Note: in.Note, Version: in.Version,
			UpdatedBy: &a.UserID,
		}); err != nil {
			if database.IsNoRows(err) {
				return apperr.ErrVersionConflict
			}
			return err
		}
		if err := saveDeliveryLines(ctx, q, id, p.lines); err != nil {
			return err
		}
		if dto, err = loadDelivery(ctx, q, a, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "delivery", EntityID: &id,
			Summary: "修改" + deliveryTypes[cur.DocType].label + " " + cur.DocNo, Before: before, After: dto,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 發票登錄 ----

type invoiceInput struct {
	InvoiceNo   string  `json:"invoice_no" binding:"max=10"`
	InvoiceDate *string `json:"invoice_date"`
	Version     int32   `json:"version" binding:"required"`
}

// setInvoice 登錄或清除發票號碼與日期;任何未作廢狀態皆可(過帳後才拿到發票是常態,D40)。
func (m *Module) setInvoice(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in invoiceInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	in.InvoiceNo = strings.ToUpper(strings.TrimSpace(in.InvoiceNo))
	if in.InvoiceNo != "" && !invoiceNoPattern.MatchString(in.InvoiceNo) {
		response.Error(c, fieldErr("invoice_no", "發票號碼格式為 2 碼英文 + 8 碼數字"))
		return
	}
	date, err := trade.OptionalInputDate("invoice_date", in.InvoiceDate)
	if err != nil {
		response.Error(c, err)
		return
	}
	if in.InvoiceNo == "" {
		date = nil
	} else if date == nil {
		response.Error(c, fieldErr("invoice_date", "請輸入發票日期"))
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto deliveryDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockDelivery(ctx, db.LockDeliveryParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := loadDelivery(ctx, q, a, id); err != nil {
			return err
		}
		if cur.Version != in.Version {
			return apperr.ErrVersionConflict
		}
		if cur.Status == string(docstate.Voided) {
			return apperr.Conflict("SAL-005", "已作廢的單據不可登錄發票")
		}
		if _, err := q.SetDeliveryInvoice(ctx, db.SetDeliveryInvoiceParams{
			ID: id, CompanyID: a.CompanyID, InvoiceNo: in.InvoiceNo, InvoiceDate: date, Version: in.Version,
			UpdatedBy: &a.UserID,
		}); err != nil {
			if database.IsUniqueViolation(err, "deliveries_invoice_no_key") {
				return apperr.Conflict("SAL-006", "發票號碼 "+in.InvoiceNo+" 已用於其他單據").
					WithDetails(map[string]string{"invoice_no": "發票號碼已被使用"})
			}
			return err
		}
		if dto, err = loadDelivery(ctx, q, a, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "delivery", EntityID: &id,
			Summary: "登錄發票 " + deliveryTypes[cur.DocType].label + " " + cur.DocNo,
			Before:  map[string]any{"invoice_no": cur.InvoiceNo, "invoice_date": dateString(cur.InvoiceDate)},
			After:   map[string]any{"invoice_no": in.InvoiceNo, "invoice_date": dateString(date)},
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 狀態動作 ----

func deliveryActionPermission(action docstate.Action, status docstate.Status) (string, bool) {
	switch action {
	case docstate.Submit:
		return permission.DeliveryWrite, true
	case docstate.Approve, docstate.Reject, docstate.Unapprove:
		return permission.DeliveryApprove, true
	case docstate.Post, docstate.Unpost:
		return permission.DeliveryPost, true
	case docstate.Void:
		if status == docstate.Draft {
			return permission.DeliveryWrite, true
		}
		return permission.DeliveryApprove, true
	default:
		return "", false
	}
}

func (m *Module) deliveryAction(c *gin.Context) {
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
	var dto deliveryDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockDelivery(ctx, db.LockDeliveryParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		doc, err := loadDelivery(ctx, q, a, id) // 含資料範圍檢查
		if err != nil {
			return err
		}
		perm, ok := deliveryActionPermission(action, docstate.Status(cur.Status))
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
		if err := applyDeliveryAction(ctx, q, a, cur, doc, action); err != nil {
			return err
		}
		if _, err := q.SetDeliveryStatus(ctx, db.SetDeliveryStatusParams{
			ID: id, CompanyID: a.CompanyID, Status: string(next), Action: string(action), ActorID: a.UserID,
			Version: in.Version,
		}); err != nil {
			return err
		}
		if dto, err = loadDelivery(ctx, q, a, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: string(action), EntityType: "delivery", EntityID: &id,
			Summary: actionLabels[action] + deliveryTypes[cur.DocType].label + " " + cur.DocNo,
			Before:  map[string]string{"status": cur.Status}, After: map[string]string{"status": string(next)},
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// applyDeliveryAction 動作的附帶檢查、庫存與應收處理(狀態轉換本身由呼叫端處理)。
func applyDeliveryAction(ctx context.Context, q *db.Queries, a *authctx.Actor, cur db.Delivery, doc deliveryDTO, action docstate.Action) error {
	typ := deliveryTypes[cur.DocType]
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
			refs[i] = refLine{itemID: l.ItemID, unitID: l.UnitID, qty: l.Qty, soLineID: l.SoLineID, deliveryLineID: l.DeliveryLineID}
		}
		errs, err := checkRefs(ctx, q, refDoc{a.CompanyID, cur.DocType, cur.CustomerID, cur.Currency, cur.ID}, refs, true)
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
			return apperr.New(http.StatusUnprocessableEntity, "SAL-007", "無法過帳:"+strings.Join(msgs, ";")).WithDetails(details)
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
		return finance.CreateReceivable(ctx, q, db.InsertReceivableParams{
			CompanyID: a.CompanyID, CustomerID: cur.CustomerID, SourceType: typ.source, SourceID: cur.ID,
			SourceNo: cur.DocNo, DocDate: cur.DocDate, DueDate: due, Currency: cur.Currency,
			ExchangeRate: cur.ExchangeRate, Amount: amount, BaseAmount: baseAmount, CreatedBy: &a.UserID,
		})
	case docstate.Unpost:
		if cur.DocType == TypeDelivery {
			no, err := q.DeliveryReturnNo(ctx, cur.ID)
			if err == nil {
				return apperr.Conflict("SAL-008", "出貨單已有退回單 "+no+",請先作廢退回單")
			}
			if !database.IsNoRows(err) {
				return err
			}
		}
		if err := finance.RemoveReceivable(ctx, q, typ.source, cur.ID); err != nil {
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

// movementsOf 商品類明細的庫存異動:出貨為負、退回為正。銷貨成本於月結計算(M7),此處不帶單位成本。
func movementsOf(cur db.Delivery, doc deliveryDTO) []inventory.Movement {
	var moves []inventory.Movement
	for _, l := range doc.Lines {
		if l.ItemType != "goods" {
			continue
		}
		lineID := l.ID
		qty := l.BaseQty
		if cur.DocType == TypeDelivery {
			qty = qty.Neg()
		}
		moves = append(moves, inventory.Movement{ItemID: l.ItemID, WarehouseID: cur.WarehouseID, Qty: qty, SourceLineID: &lineID})
	}
	return moves
}

// ---- 可退回明細 ----

type returnableDTO struct {
	DeliveryLineID int64           `json:"delivery_line_id"`
	DeliveryID     int64           `json:"delivery_id"`
	DocNo          string          `json:"doc_no"`
	DocDate        string          `json:"doc_date"`
	WarehouseID    int64           `json:"warehouse_id"`
	LineNo         int32           `json:"line_no"`
	ItemID         int64           `json:"item_id"`
	ItemCode       string          `json:"item_code"`
	ItemName       string          `json:"item_name"`
	ItemSpec       string          `json:"item_spec"`
	UnitID         int64           `json:"unit_id"`
	UnitName       string          `json:"unit_name"`
	Qty            decimal.Decimal `json:"qty"`
	UnitPrice      decimal.Decimal `json:"unit_price"`
	ReturnedQty    decimal.Decimal `json:"returned_qty"`
	RemainingQty   decimal.Decimal `json:"remaining_qty"`
}

// returnableLines 指定客戶(須在資料範圍內)與幣別、已過帳且尚未退完的出貨明細(最多 200 筆)。
func (m *Module) returnableLines(c *gin.Context) {
	customerID, err := httpx.QueryInt64(c, "customer_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	currency := strings.ToUpper(c.Query("currency"))
	if customerID == nil || currency == "" {
		response.Error(c, fieldErr("customer_id", "請指定客戶與幣別"))
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	cust, err := m.store.CustomerForDoc(ctx, db.CustomerForDocParams{ID: *customerID, CompanyID: a.CompanyID})
	if database.IsNoRows(err) || (err == nil && visible(a, cust.SalesUserID, cust.SalesDepartmentID) != nil) {
		response.Error(c, apperr.ErrNotFound)
		return
	} else if err != nil {
		response.Error(c, err)
		return
	}
	rows, err := m.store.ReturnableDeliveryLines(ctx, db.ReturnableDeliveryLinesParams{
		CompanyID: a.CompanyID, CustomerID: *customerID, Currency: currency, Keyword: httpx.QueryString(c, "keyword"),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]returnableDTO, len(rows))
	for i, r := range rows {
		out[i] = returnableDTO{
			DeliveryLineID: r.DeliveryLineID, DeliveryID: r.DeliveryID, DocNo: r.DocNo,
			DocDate: r.DocDate.Format(time.DateOnly), WarehouseID: r.WarehouseID, LineNo: r.LineNo, ItemID: r.ItemID,
			ItemCode: r.ItemCode, ItemName: r.ItemName, ItemSpec: r.ItemSpec, UnitID: r.UnitID, UnitName: r.UnitName,
			Qty: r.Qty, UnitPrice: r.UnitPrice, ReturnedQty: r.ReturnedQty, RemainingQty: r.RemainingQty,
		}
	}
	response.OK(c, out)
}
