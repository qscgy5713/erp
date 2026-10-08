package masterdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/shared/taxid"
	"erp/internal/system/audit"
	"erp/internal/system/permission"
)

var (
	errCustomerCodeDup = apperr.Conflict("CUST-001", "客戶代碼已存在")
	errSupplierCodeDup = apperr.Conflict("SUPP-001", "供應商代碼已存在")
	errSalesOutOfScope = apperr.Forbidden("CUST-002", "負責業務超出你的資料範圍")
	errCreditForbidden = apperr.Forbidden("CUST-003", "沒有設定信用額度的權限")
)

const maxContacts = 20

type Contact struct {
	Name  string `json:"name" binding:"required,max=50"`
	Title string `json:"title" binding:"max=50"`
	Phone string `json:"phone" binding:"max=50"`
	Email string `json:"email" binding:"max=255"`
}

type Address struct {
	Label     string `json:"label" binding:"max=20"`
	Zip       string `json:"zip" binding:"max=6"`
	Address   string `json:"address" binding:"required,max=255"`
	IsDefault bool   `json:"is_default"`
}

// partnerInput 客戶與供應商共用的欄位。
type partnerInput struct {
	Code          string          `json:"code" binding:"required,max=20"`
	Name          string          `json:"name" binding:"required,max=200"`
	ShortName     string          `json:"short_name" binding:"max=50"`
	TaxID         *string         `json:"tax_id"`
	Phone         string          `json:"phone" binding:"max=50"`
	Email         string          `json:"email" binding:"max=255"`
	Contacts      []Contact       `json:"contacts" binding:"dive"`
	Addresses     []Address       `json:"addresses" binding:"dive"`
	Currency      string          `json:"currency" binding:"required,len=3"`
	TaxTypeID     *int64          `json:"tax_type_id"`
	PaymentTermID *int64          `json:"payment_term_id"`
	Note          string          `json:"note" binding:"max=2000"`
	IsActive      bool            `json:"is_active"`
	Version       int32           `json:"version"`
	creditLimit   decimal.Decimal // 僅客戶使用,由外層填入以共用驗證
}

func (in *partnerInput) normalize() map[string]string {
	in.Code = normCode(in.Code)
	in.Name = strings.TrimSpace(in.Name)
	in.ShortName = strings.TrimSpace(in.ShortName)
	in.TaxID = trimPtr(in.TaxID)
	in.Phone = strings.TrimSpace(in.Phone)
	in.Email = strings.TrimSpace(in.Email)
	in.Currency = strings.ToUpper(in.Currency)
	in.Note = strings.TrimSpace(in.Note)
	if in.Contacts == nil {
		in.Contacts = []Contact{}
	}
	if in.Addresses == nil {
		in.Addresses = []Address{}
	}

	fields := map[string]string{}
	if in.TaxID != nil && !taxid.Valid(*in.TaxID) {
		fields["tax_id"] = "統一編號格式或檢查碼錯誤"
	}
	if in.Email != "" && !validEmail(in.Email) {
		fields["email"] = "Email 格式錯誤"
	}
	if len(in.Contacts) > maxContacts {
		fields["contacts"] = fmt.Sprintf("最多 %d 位聯絡人", maxContacts)
	}
	for i := range in.Contacts {
		ct := &in.Contacts[i]
		ct.Name, ct.Title = strings.TrimSpace(ct.Name), strings.TrimSpace(ct.Title)
		ct.Phone, ct.Email = strings.TrimSpace(ct.Phone), strings.TrimSpace(ct.Email)
		if ct.Email != "" && !validEmail(ct.Email) {
			fields[fmt.Sprintf("contacts.%d.email", i)] = "Email 格式錯誤"
		}
	}
	if len(in.Addresses) > maxContacts {
		fields["addresses"] = fmt.Sprintf("最多 %d 個地址", maxContacts)
	}
	defaults := 0
	for i := range in.Addresses {
		ad := &in.Addresses[i]
		ad.Label, ad.Zip, ad.Address = strings.TrimSpace(ad.Label), strings.TrimSpace(ad.Zip), strings.TrimSpace(ad.Address)
		if ad.IsDefault {
			defaults++
		}
	}
	if defaults > 1 {
		fields["addresses"] = "只能有一個預設地址"
	}
	if in.creditLimit.IsNegative() || in.creditLimit.Exponent() < -4 {
		fields["credit_limit"] = "須 ≥ 0,最多 4 位小數"
	}
	return fields
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s
}

func (in *partnerInput) jsonFields() (contacts, addresses []byte, err error) {
	if contacts, err = json.Marshal(in.Contacts); err != nil {
		return nil, nil, err
	}
	addresses, err = json.Marshal(in.Addresses)
	return contacts, addresses, err
}

func checkPartnerRefs(ctx context.Context, q *db.Queries, companyID int64, in *partnerInput, salesUserID *int64, fields map[string]string) error {
	r, err := q.CheckPartnerRefs(ctx, db.CheckPartnerRefsParams{
		CompanyID: companyID, Currency: in.Currency, TaxTypeID: in.TaxTypeID,
		PaymentTermID: in.PaymentTermID, SalesUserID: salesUserID,
	})
	if err != nil {
		return err
	}
	if !r.CurrencyOk {
		fields["currency"] = "幣別不存在或已停用"
	}
	if !r.TaxTypeOk {
		fields["tax_type_id"] = "稅別不存在"
	}
	if !r.PaymentTermOk {
		fields["payment_term_id"] = "付款條件不存在"
	}
	if !r.SalesUserOk {
		fields["sales_user_id"] = "負責業務不存在"
	}
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}
	return nil
}

func decodeList[T any](raw []byte) []T {
	var out []T
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return []T{}
	}
	return out
}

// ---- 客戶 ----

type customerDTO struct {
	ID            int64           `json:"id"`
	Code          string          `json:"code"`
	Name          string          `json:"name"`
	ShortName     string          `json:"short_name"`
	TaxID         *string         `json:"tax_id"`
	InvoiceTitle  string          `json:"invoice_title"`
	Phone         string          `json:"phone"`
	Email         string          `json:"email"`
	Contacts      []Contact       `json:"contacts"`
	Addresses     []Address       `json:"addresses"`
	Currency      string          `json:"currency"`
	TaxTypeID     *int64          `json:"tax_type_id"`
	PaymentTermID *int64          `json:"payment_term_id"`
	CreditLimit   decimal.Decimal `json:"credit_limit"`
	SalesUserID   *int64          `json:"sales_user_id"`
	SalesUserName *string         `json:"sales_user_name,omitempty"`
	Note          string          `json:"note"`
	IsActive      bool            `json:"is_active"`
	Version       int32           `json:"version"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func toCustomerDTO(c db.Customer) customerDTO {
	return customerDTO{
		ID: c.ID, Code: c.Code, Name: c.Name, ShortName: c.ShortName, TaxID: c.TaxID, InvoiceTitle: c.InvoiceTitle,
		Phone: c.Phone, Email: c.Email, Contacts: decodeList[Contact](c.Contacts),
		Addresses: decodeList[Address](c.Addresses), Currency: c.Currency, TaxTypeID: c.TaxTypeID,
		PaymentTermID: c.PaymentTermID, CreditLimit: c.CreditLimit, SalesUserID: c.SalesUserID, Note: c.Note,
		IsActive: c.IsActive, Version: c.Version, UpdatedAt: c.UpdatedAt,
	}
}

// customerVisible 依資料範圍判斷能否存取此客戶(以負責業務及其部門判斷)。
func customerVisible(a *authctx.Actor, salesUserID, salesDeptID *int64) bool {
	deptID, userID := a.ScopeFilter()
	switch {
	case deptID == nil && userID == nil:
		return true
	case userID != nil:
		return salesUserID != nil && *salesUserID == *userID
	default:
		return salesDeptID != nil && *salesDeptID == *deptID
	}
}

// checkSalesScope 指定的負責業務必須在自己的資料範圍內,否則等於把客戶移出(或塞進)別人的範圍。
func checkSalesScope(ctx context.Context, q *db.Queries, a *authctx.Actor, salesUserID *int64) error {
	deptID, userID := a.ScopeFilter()
	if deptID == nil && userID == nil {
		return nil
	}
	if salesUserID == nil {
		return errSalesOutOfScope
	}
	if userID != nil {
		if *salesUserID != *userID {
			return errSalesOutOfScope
		}
		return nil
	}
	salesDept, err := q.GetUserDepartment(ctx, db.GetUserDepartmentParams{ID: *salesUserID, CompanyID: a.CompanyID})
	if database.IsNoRows(err) {
		return fieldErr("sales_user_id", "負責業務不存在")
	}
	if err != nil {
		return err
	}
	if salesDept == nil || *salesDept != *deptID {
		return errSalesOutOfScope
	}
	return nil
}

// loadCustomer 讀取客戶並檢查資料範圍;範圍外視為不存在(不透露資料存在與否)。
func loadCustomer(ctx context.Context, q *db.Queries, a *authctx.Actor, id int64) (db.GetCustomerRow, error) {
	row, err := q.GetCustomer(ctx, db.GetCustomerParams{ID: id, CompanyID: a.CompanyID})
	if err != nil {
		return row, notFoundOr(err)
	}
	if !customerVisible(a, row.SalesUserID, row.SalesDepartmentID) {
		return row, apperr.ErrNotFound
	}
	return row, nil
}

func customerFromRow(r db.GetCustomerRow) db.Customer {
	return db.Customer{
		ID: r.ID, CompanyID: r.CompanyID, Code: r.Code, Name: r.Name, ShortName: r.ShortName, TaxID: r.TaxID,
		InvoiceTitle: r.InvoiceTitle, Phone: r.Phone, Email: r.Email, Contacts: r.Contacts, Addresses: r.Addresses,
		Currency: r.Currency, TaxTypeID: r.TaxTypeID, PaymentTermID: r.PaymentTermID, CreditLimit: r.CreditLimit,
		SalesUserID: r.SalesUserID, Note: r.Note, IsActive: r.IsActive, Version: r.Version, UpdatedAt: r.UpdatedAt,
	}
}

func (m *Module) listCustomers(c *gin.Context) {
	ctx := c.Request.Context()
	isActive, err := httpx.QueryBool(c, "is_active")
	if err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	deptID, userID := a.ScopeFilter()
	pg := page.Parse(c.Query("page"), c.Query("size"))
	keyword := httpx.QueryString(c, "keyword")

	rows, err := m.store.ListCustomers(ctx, db.ListCustomersParams{
		CompanyID: a.CompanyID, Keyword: keyword, IsActive: isActive, ScopeUserID: userID, ScopeDeptID: deptID,
		Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountCustomers(ctx, db.CountCustomersParams{
		CompanyID: a.CompanyID, Keyword: keyword, IsActive: isActive, ScopeUserID: userID, ScopeDeptID: deptID,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]customerDTO, len(rows))
	for i, r := range rows {
		dto := toCustomerDTO(db.Customer{
			ID: r.ID, Code: r.Code, Name: r.Name, ShortName: r.ShortName, TaxID: r.TaxID, InvoiceTitle: r.InvoiceTitle,
			Phone: r.Phone, Email: r.Email, Contacts: r.Contacts, Addresses: r.Addresses, Currency: r.Currency,
			TaxTypeID: r.TaxTypeID, PaymentTermID: r.PaymentTermID, CreditLimit: r.CreditLimit,
			SalesUserID: r.SalesUserID, Note: r.Note, IsActive: r.IsActive, Version: r.Version, UpdatedAt: r.UpdatedAt,
		})
		dto.SalesUserName = r.SalesUserName
		out[i] = dto
	}
	response.List(c, out, pg.Meta(total))
}

func (m *Module) getCustomer(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	row, err := loadCustomer(c.Request.Context(), m.store.Queries, actor(c), id)
	dto := toCustomerDTO(customerFromRow(row))
	dto.SalesUserName = row.SalesUserName
	reply(c, http.StatusOK, dto, err)
}

type customerInput struct {
	partnerInput
	InvoiceTitle string          `json:"invoice_title" binding:"max=200"`
	CreditLimit  decimal.Decimal `json:"credit_limit"`
	SalesUserID  *int64          `json:"sales_user_id"`
}

func (in *customerInput) prepare() map[string]string {
	in.InvoiceTitle = strings.TrimSpace(in.InvoiceTitle)
	in.creditLimit = in.CreditLimit
	return in.normalize()
}

func (m *Module) createCustomer(c *gin.Context) {
	in, ok := bind[customerInput](c)
	if !ok {
		return
	}
	fields := in.prepare()
	contacts, addresses, err := in.jsonFields()
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var out db.Customer
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		if err := checkPartnerRefs(ctx, q, a.CompanyID, &in.partnerInput, in.SalesUserID, fields); err != nil {
			return err
		}
		if err := checkSalesScope(ctx, q, a, in.SalesUserID); err != nil {
			return err
		}
		if !in.CreditLimit.IsZero() && !a.Can(permission.CustomerCredit) {
			return errCreditForbidden
		}
		var err error
		out, err = q.CreateCustomer(ctx, db.CreateCustomerParams{
			CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, ShortName: in.ShortName, TaxID: in.TaxID,
			InvoiceTitle: in.InvoiceTitle, Phone: in.Phone, Email: in.Email, Contacts: contacts, Addresses: addresses,
			Currency: in.Currency, TaxTypeID: in.TaxTypeID, PaymentTermID: in.PaymentTermID,
			CreditLimit: in.CreditLimit, SalesUserID: in.SalesUserID, Note: in.Note, CreatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(err, "customers_company_code_key", errCustomerCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "customer", EntityID: &out.ID,
			Summary: "新增客戶 " + out.Code + " " + out.Name, After: toCustomerDTO(out),
		})
	})
	reply(c, http.StatusCreated, toCustomerDTO(out), err)
}

func (m *Module) updateCustomer(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	in, ok := bind[customerInput](c)
	if !ok {
		return
	}
	fields := in.prepare()
	contacts, addresses, err := in.jsonFields()
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var out db.Customer
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := loadCustomer(ctx, q, a, id)
		if err != nil {
			return err
		}
		if err := checkPartnerRefs(ctx, q, a.CompanyID, &in.partnerInput, in.SalesUserID, fields); err != nil {
			return err
		}
		if err := checkSalesScope(ctx, q, a, in.SalesUserID); err != nil {
			return err
		}
		if !in.CreditLimit.Equal(before.CreditLimit) && !a.Can(permission.CustomerCredit) {
			return errCreditForbidden
		}
		out, err = q.UpdateCustomer(ctx, db.UpdateCustomerParams{
			ID: id, CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, ShortName: in.ShortName, TaxID: in.TaxID,
			InvoiceTitle: in.InvoiceTitle, Phone: in.Phone, Email: in.Email, Contacts: contacts, Addresses: addresses,
			Currency: in.Currency, TaxTypeID: in.TaxTypeID, PaymentTermID: in.PaymentTermID,
			CreditLimit: in.CreditLimit, SalesUserID: in.SalesUserID, Note: in.Note, IsActive: in.IsActive,
			Version: in.Version, UpdatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(versionConflictOr(err), "customers_company_code_key", errCustomerCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "customer", EntityID: &id,
			Summary: "修改客戶 " + out.Code + " " + out.Name,
			Before:  toCustomerDTO(customerFromRow(before)), After: toCustomerDTO(out),
		})
	})
	reply(c, http.StatusOK, toCustomerDTO(out), err)
}

// ---- 供應商 ----

type supplierDTO struct {
	ID            int64     `json:"id"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	ShortName     string    `json:"short_name"`
	TaxID         *string   `json:"tax_id"`
	Phone         string    `json:"phone"`
	Email         string    `json:"email"`
	Contacts      []Contact `json:"contacts"`
	Addresses     []Address `json:"addresses"`
	Currency      string    `json:"currency"`
	TaxTypeID     *int64    `json:"tax_type_id"`
	PaymentTermID *int64    `json:"payment_term_id"`
	BankName      string    `json:"bank_name"`
	BankAccount   string    `json:"bank_account"`
	Note          string    `json:"note"`
	IsActive      bool      `json:"is_active"`
	Version       int32     `json:"version"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func toSupplierDTO(s db.Supplier) supplierDTO {
	return supplierDTO{
		ID: s.ID, Code: s.Code, Name: s.Name, ShortName: s.ShortName, TaxID: s.TaxID, Phone: s.Phone, Email: s.Email,
		Contacts: decodeList[Contact](s.Contacts), Addresses: decodeList[Address](s.Addresses), Currency: s.Currency,
		TaxTypeID: s.TaxTypeID, PaymentTermID: s.PaymentTermID, BankName: s.BankName, BankAccount: s.BankAccount,
		Note: s.Note, IsActive: s.IsActive, Version: s.Version, UpdatedAt: s.UpdatedAt,
	}
}

func (m *Module) listSuppliers(c *gin.Context) {
	ctx := c.Request.Context()
	isActive, err := httpx.QueryBool(c, "is_active")
	if err != nil {
		response.Error(c, err)
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	keyword := httpx.QueryString(c, "keyword")
	rows, err := m.store.ListSuppliers(ctx, db.ListSuppliersParams{
		CompanyID: companyID, Keyword: keyword, IsActive: isActive, Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountSuppliers(ctx, db.CountSuppliersParams{CompanyID: companyID, Keyword: keyword, IsActive: isActive})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]supplierDTO, len(rows))
	for i, r := range rows {
		out[i] = toSupplierDTO(r)
	}
	response.List(c, out, pg.Meta(total))
}

func (m *Module) getSupplier(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	s, err := m.store.GetSupplier(c.Request.Context(), db.GetSupplierParams{ID: id, CompanyID: actor(c).CompanyID})
	reply(c, http.StatusOK, toSupplierDTO(s), notFoundOr(err))
}

type supplierInput struct {
	partnerInput
	BankName    string `json:"bank_name" binding:"max=100"`
	BankAccount string `json:"bank_account" binding:"max=50"`
}

func (in *supplierInput) prepare() map[string]string {
	in.BankName, in.BankAccount = strings.TrimSpace(in.BankName), strings.TrimSpace(in.BankAccount)
	return in.normalize()
}

func (m *Module) createSupplier(c *gin.Context) {
	in, ok := bind[supplierInput](c)
	if !ok {
		return
	}
	fields := in.prepare()
	contacts, addresses, err := in.jsonFields()
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var out db.Supplier
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		if err := checkPartnerRefs(ctx, q, a.CompanyID, &in.partnerInput, nil, fields); err != nil {
			return err
		}
		var err error
		out, err = q.CreateSupplier(ctx, db.CreateSupplierParams{
			CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, ShortName: in.ShortName, TaxID: in.TaxID,
			Phone: in.Phone, Email: in.Email, Contacts: contacts, Addresses: addresses, Currency: in.Currency,
			TaxTypeID: in.TaxTypeID, PaymentTermID: in.PaymentTermID, BankName: in.BankName,
			BankAccount: in.BankAccount, Note: in.Note, CreatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(err, "suppliers_company_code_key", errSupplierCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "supplier", EntityID: &out.ID,
			Summary: "新增供應商 " + out.Code + " " + out.Name, After: toSupplierDTO(out),
		})
	})
	reply(c, http.StatusCreated, toSupplierDTO(out), err)
}

func (m *Module) updateSupplier(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	in, ok := bind[supplierInput](c)
	if !ok {
		return
	}
	fields := in.prepare()
	contacts, addresses, err := in.jsonFields()
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var out db.Supplier
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetSupplier(ctx, db.GetSupplierParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return notFoundOr(err)
		}
		if err := checkPartnerRefs(ctx, q, a.CompanyID, &in.partnerInput, nil, fields); err != nil {
			return err
		}
		out, err = q.UpdateSupplier(ctx, db.UpdateSupplierParams{
			ID: id, CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, ShortName: in.ShortName, TaxID: in.TaxID,
			Phone: in.Phone, Email: in.Email, Contacts: contacts, Addresses: addresses, Currency: in.Currency,
			TaxTypeID: in.TaxTypeID, PaymentTermID: in.PaymentTermID, BankName: in.BankName,
			BankAccount: in.BankAccount, Note: in.Note, IsActive: in.IsActive, Version: in.Version,
			UpdatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(versionConflictOr(err), "suppliers_company_code_key", errSupplierCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "supplier", EntityID: &id,
			Summary: "修改供應商 " + out.Code + " " + out.Name, Before: toSupplierDTO(before), After: toSupplierDTO(out),
		})
	})
	reply(c, http.StatusOK, toSupplierDTO(out), err)
}

type supplierOptionDTO struct {
	ID            int64  `json:"id"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	ShortName     string `json:"short_name"`
	Currency      string `json:"currency"`
	TaxTypeID     *int64 `json:"tax_type_id"`
	PaymentTermID *int64 `json:"payment_term_id"`
}

// supplierOptions 開單選供應商(最多 20 筆),附預設幣別、稅別、付款條件。
func (m *Module) supplierOptions(c *gin.Context) {
	rows, err := m.store.SupplierOptions(c.Request.Context(), db.SupplierOptionsParams{
		CompanyID: actor(c).CompanyID, Keyword: httpx.QueryString(c, "keyword"),
	})
	out := make([]supplierOptionDTO, len(rows))
	for i, r := range rows {
		out[i] = supplierOptionDTO{
			ID: r.ID, Code: r.Code, Name: r.Name, ShortName: r.ShortName, Currency: r.Currency,
			TaxTypeID: r.TaxTypeID, PaymentTermID: r.PaymentTermID,
		}
	}
	reply(c, http.StatusOK, out, err)
}
