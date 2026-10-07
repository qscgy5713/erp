// Package masterdata 提供基本資料 API:料品、分類、單位、倉庫、客戶、供應商、
// 幣別匯率、稅別、付款條件。
package masterdata

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/auth"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/response"
	p "erp/internal/system/permission"
)

type Module struct {
	store *database.Store
}

func New(store *database.Store) *Module {
	return &Module{store: store}
}

// Register 掛上 /masterdata 路由;r 須已套用 auth.Authenticate。
// 下拉選單會用到的清單(單位、分類、倉庫、稅別、付款條件、幣別)只要登入即可讀取,
// 因為開單的人需要選它們,卻不一定有維護基本資料的權限。
func (m *Module) Register(r *gin.RouterGroup) {
	g := r.Group("/masterdata")

	g.GET("/currencies", m.listCurrencies)
	g.PUT("/currencies/:code", auth.Require(p.FinanceWrite), m.setCurrencyActive)
	g.GET("/exchange-rates", auth.Require(p.FinanceRead), m.listExchangeRates)
	g.GET("/exchange-rates/lookup", m.lookupExchangeRate)
	g.POST("/exchange-rates", auth.Require(p.FinanceWrite), m.createExchangeRate)
	g.PUT("/exchange-rates/:id", auth.Require(p.FinanceWrite), m.updateExchangeRate)
	g.DELETE("/exchange-rates/:id", auth.Require(p.FinanceWrite), m.deleteExchangeRate)
	g.GET("/tax-types", m.listTaxTypes)
	g.POST("/tax-types", auth.Require(p.FinanceWrite), m.createTaxType)
	g.PUT("/tax-types/:id", auth.Require(p.FinanceWrite), m.updateTaxType)
	g.GET("/payment-terms", m.listPaymentTerms)
	g.POST("/payment-terms", auth.Require(p.FinanceWrite), m.createPaymentTerm)
	g.PUT("/payment-terms/:id", auth.Require(p.FinanceWrite), m.updatePaymentTerm)

	g.GET("/units", m.listUnits)
	g.POST("/units", auth.Require(p.ItemWrite), m.createUnit)
	g.PUT("/units/:id", auth.Require(p.ItemWrite), m.updateUnit)
	g.GET("/item-categories", m.listCategories)
	g.POST("/item-categories", auth.Require(p.ItemWrite), m.createCategory)
	g.PUT("/item-categories/:id", auth.Require(p.ItemWrite), m.updateCategory)
	g.GET("/warehouses", m.listWarehouses)
	g.POST("/warehouses", auth.Require(p.WarehouseWrite), m.createWarehouse)
	g.PUT("/warehouses/:id", auth.Require(p.WarehouseWrite), m.updateWarehouse)

	g.GET("/items", auth.Require(p.ItemRead), m.listItems)
	g.GET("/items/:id", auth.Require(p.ItemRead), m.getItem)
	g.POST("/items", auth.Require(p.ItemWrite), m.createItem)
	g.PUT("/items/:id", auth.Require(p.ItemWrite), m.updateItem)

	g.GET("/customers", auth.Require(p.CustomerRead), m.listCustomers)
	g.GET("/customers/:id", auth.Require(p.CustomerRead), m.getCustomer)
	g.POST("/customers", auth.Require(p.CustomerWrite), m.createCustomer)
	g.PUT("/customers/:id", auth.Require(p.CustomerWrite), m.updateCustomer)

	g.GET("/suppliers", auth.Require(p.SupplierRead), m.listSuppliers)
	g.GET("/suppliers/:id", auth.Require(p.SupplierRead), m.getSupplier)
	g.POST("/suppliers", auth.Require(p.SupplierWrite), m.createSupplier)
	g.PUT("/suppliers/:id", auth.Require(p.SupplierWrite), m.updateSupplier)
}

// ---- handler 共用小工具 ----

func actor(c *gin.Context) *authctx.Actor { return authctx.ActorFrom(c.Request.Context()) }

// bind 解析 JSON body;失敗時已回應錯誤,呼叫端直接 return。
func bind[T any](c *gin.Context) (T, bool) {
	var in T
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return in, false
	}
	return in, true
}

func pathID(c *gin.Context) (int64, bool) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return 0, false
	}
	return id, true
}

// reply 依 err 回應錯誤,否則以 status 回應 data。
func reply(c *gin.Context, status int, data any, err error) {
	switch {
	case err != nil:
		response.Error(c, err)
	case status == http.StatusCreated:
		response.Created(c, data)
	default:
		response.OK(c, data)
	}
}

// notFoundOr 查無資料時轉為 404。
func notFoundOr(err error) error {
	if database.IsNoRows(err) {
		return apperr.ErrNotFound
	}
	return err
}

// versionConflictOr 樂觀鎖更新沒有影響任何列(呼叫前已確認資料存在)時轉為 409。
func versionConflictOr(err error) error {
	if database.IsNoRows(err) {
		return apperr.ErrVersionConflict
	}
	return err
}

func uniqueOr(err error, constraint string, dup *apperr.Error) error {
	if database.IsUniqueViolation(err, constraint) {
		return dup
	}
	return err
}

func normCode(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

const dateLayout = time.DateOnly

func parseDate(field, s string) (time.Time, error) {
	t, err := time.ParseInLocation(dateLayout, s, time.UTC)
	if err != nil {
		return time.Time{}, apperr.Validation(map[string]string{field: "日期格式須為 YYYY-MM-DD"})
	}
	return t, nil
}

func fieldErr(field, msg string) error {
	return apperr.Validation(map[string]string{field: msg})
}
