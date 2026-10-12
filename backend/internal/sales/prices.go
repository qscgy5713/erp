package sales

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/masterdata"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
)

// 售價表(D79):單價為未稅、基本單位。
// 查價順序:客戶專屬價 > 一般價;同層取數量門檻最高者;查無則回料品建議售價(list_price)。

type priceDTO struct {
	ID           int64           `json:"id"`
	CustomerID   *int64          `json:"customer_id"`
	CustomerName *string         `json:"customer_name"`
	ItemID       int64           `json:"item_id"`
	ItemCode     string          `json:"item_code"`
	ItemName     string          `json:"item_name"`
	Currency     string          `json:"currency"`
	MinQty       decimal.Decimal `json:"min_qty"`
	UnitPrice    decimal.Decimal `json:"unit_price"`
	ValidFrom    *string         `json:"valid_from"`
	ValidTo      *string         `json:"valid_to"`
	Note         string          `json:"note"`
}

func (m *Module) listPrices(c *gin.Context) {
	p := db.ListSalesPricesParams{CompanyID: actor(c).CompanyID}
	if s := c.Query("item_id"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil || id <= 0 {
			response.Error(c, fieldErr("item_id", "格式錯誤"))
			return
		}
		p.ItemID = &id
	}
	if s := c.Query("customer_id"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil || id <= 0 {
			response.Error(c, fieldErr("customer_id", "格式錯誤"))
			return
		}
		p.CustomerID = &id
	}
	rows, err := m.store.ListSalesPrices(c.Request.Context(), p)
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]priceDTO, len(rows))
	for i, r := range rows {
		out[i] = priceDTO{ID: r.ID, CustomerID: r.CustomerID, CustomerName: r.CustomerName, ItemID: r.ItemID,
			ItemCode: r.ItemCode, ItemName: r.ItemName, Currency: r.Currency, MinQty: r.MinQty,
			UnitPrice: r.UnitPrice, ValidFrom: dateString(r.ValidFrom), ValidTo: dateString(r.ValidTo), Note: r.Note}
	}
	response.OK(c, out)
}

type priceInput struct {
	CustomerID *int64          `json:"customer_id"`
	ItemID     int64           `json:"item_id" binding:"required"`
	Currency   string          `json:"currency" binding:"required,len=3"`
	MinQty     decimal.Decimal `json:"min_qty"`
	UnitPrice  decimal.Decimal `json:"unit_price"`
	ValidFrom  *string         `json:"valid_from"`
	ValidTo    *string         `json:"valid_to"`
	Note       string          `json:"note"`
}

func (m *Module) createPrice(c *gin.Context) {
	var in priceInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	if in.MinQty.IsNegative() {
		response.Error(c, fieldErr("min_qty", "數量門檻不可為負"))
		return
	}
	if in.UnitPrice.IsNegative() {
		response.Error(c, fieldErr("unit_price", "單價不可為負"))
		return
	}
	var from, to *time.Time
	for _, d := range []struct {
		name string
		src  *string
		dst  **time.Time
	}{{"valid_from", in.ValidFrom, &from}, {"valid_to", in.ValidTo, &to}} {
		if d.src == nil || *d.src == "" {
			continue
		}
		t, err := time.Parse(time.DateOnly, *d.src)
		if err != nil {
			response.Error(c, fieldErr(d.name, "日期格式須為 YYYY-MM-DD"))
			return
		}
		*d.dst = &t
	}
	if from != nil && to != nil && to.Before(*from) {
		response.Error(c, fieldErr("valid_to", "迄日不可早於起日"))
		return
	}
	a := actor(c)
	var id int64
	err := m.store.InTx(c.Request.Context(), func(q *db.Queries) error {
		ctx := c.Request.Context()
		if _, err := q.ItemListPrice(ctx, db.ItemListPriceParams{ID: in.ItemID, CompanyID: a.CompanyID}); err != nil {
			if database.IsNoRows(err) {
				return fieldErr("item_id", "料品不存在")
			}
			return err
		}
		if in.CustomerID != nil {
			if _, err := q.CustomerForDoc(ctx, db.CustomerForDocParams{ID: *in.CustomerID, CompanyID: a.CompanyID}); err != nil {
				if database.IsNoRows(err) {
					return fieldErr("customer_id", "客戶不存在")
				}
				return err
			}
		}
		var err error
		id, err = q.CreateSalesPrice(ctx, db.CreateSalesPriceParams{
			CompanyID: a.CompanyID, CustomerID: in.CustomerID, ItemID: in.ItemID,
			Currency: strings.ToUpper(in.Currency), MinQty: in.MinQty, UnitPrice: in.UnitPrice,
			ValidFrom: from, ValidTo: to, Note: strings.TrimSpace(in.Note), CreatedBy: &a.UserID,
		})
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: "create", EntityType: "sales_price", EntityID: &id,
			Summary: fmt.Sprintf("新增售價:料品 #%d %s 單價 %s(≥%s)", in.ItemID, strings.ToUpper(in.Currency), in.UnitPrice, in.MinQty),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, map[string]int64{"id": id})
}

func (m *Module) deletePrice(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	err = m.store.InTx(c.Request.Context(), func(q *db.Queries) error {
		ctx := c.Request.Context()
		n, err := q.DeleteSalesPrice(ctx, db.DeleteSalesPriceParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.ErrNotFound
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: "delete", EntityType: "sales_price", EntityID: &id, Summary: fmt.Sprintf("刪除售價 #%d", id),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type priceLookupDTO struct {
	UnitPrice decimal.Decimal `json:"unit_price"` // 基本單位、未稅
	Source    string          `json:"source"`     // customer | general | list | none
}

// lookupPrice GET /sales/price?customer_id=&item_id=&currency=&qty=&date= (qty 為基本單位數量)
func (m *Module) lookupPrice(c *gin.Context) {
	customerID, err1 := strconv.ParseInt(c.Query("customer_id"), 10, 64)
	itemID, err2 := strconv.ParseInt(c.Query("item_id"), 10, 64)
	if err1 != nil || customerID <= 0 {
		response.Error(c, fieldErr("customer_id", "請指定客戶"))
		return
	}
	if err2 != nil || itemID <= 0 {
		response.Error(c, fieldErr("item_id", "請指定料品"))
		return
	}
	currency := strings.ToUpper(strings.TrimSpace(c.Query("currency")))
	if len(currency) != 3 {
		response.Error(c, fieldErr("currency", "請指定幣別"))
		return
	}
	qty := decimal.NewFromInt(1)
	if s := c.Query("qty"); s != "" {
		q, err := decimal.NewFromString(s)
		if err != nil || q.IsNegative() {
			response.Error(c, fieldErr("qty", "數量格式錯誤"))
			return
		}
		qty = q
	}
	on := time.Now()
	if s := c.Query("date"); s != "" {
		t, err := time.Parse(time.DateOnly, s)
		if err != nil {
			response.Error(c, fieldErr("date", "日期格式須為 YYYY-MM-DD"))
			return
		}
		on = t
	}
	ctx := c.Request.Context()
	cid := actor(c).CompanyID
	row, err := m.store.LookupSalesPrice(ctx, db.LookupSalesPriceParams{
		CompanyID: cid, ItemID: itemID, Currency: currency, CustomerID: &customerID, Qty: qty, OnDate: &on,
	})
	if err == nil {
		src := "general"
		if row.CustomerID != nil {
			src = "customer"
		}
		response.OK(c, priceLookupDTO{UnitPrice: row.UnitPrice, Source: src})
		return
	}
	if !database.IsNoRows(err) {
		response.Error(c, err)
		return
	}
	lp, err := m.store.ItemListPrice(ctx, db.ItemListPriceParams{ID: itemID, CompanyID: cid})
	if err != nil {
		if database.IsNoRows(err) {
			response.Error(c, apperr.ErrNotFound)
			return
		}
		response.Error(c, err)
		return
	}
	if lp.IsPositive() && currency == masterdata.BaseCurrency { // 建議售價為本位幣
		response.OK(c, priceLookupDTO{UnitPrice: lp, Source: "list"})
		return
	}
	response.OK(c, priceLookupDTO{UnitPrice: decimal.Zero, Source: "none"})
}
