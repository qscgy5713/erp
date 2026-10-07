package masterdata

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/money"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
)

var (
	errItemCodeDup    = apperr.Conflict("ITEM-001", "料號已存在")
	errItemBarcodeDup = apperr.Conflict("ITEM-002", "條碼已被其他料品使用")
)

const maxItemUnits = 10

type itemUnitDTO struct {
	UnitID   int64           `json:"unit_id"`
	UnitCode string          `json:"unit_code,omitempty"`
	UnitName string          `json:"unit_name,omitempty"`
	Factor   decimal.Decimal `json:"factor"`
	Barcode  *string         `json:"barcode"`
}

type itemDTO struct {
	ID                 int64           `json:"id"`
	Code               string          `json:"code"`
	Name               string          `json:"name"`
	Spec               string          `json:"spec"`
	CategoryID         *int64          `json:"category_id"`
	CategoryName       *string         `json:"category_name,omitempty"`
	ItemType           string          `json:"item_type"`
	BaseUnitID         int64           `json:"base_unit_id"`
	BaseUnitName       string          `json:"base_unit_name,omitempty"`
	Barcode            *string         `json:"barcode"`
	TaxTypeID          *int64          `json:"tax_type_id"`
	DefaultWarehouseID *int64          `json:"default_warehouse_id"`
	SafetyStock        decimal.Decimal `json:"safety_stock"`
	ListPrice          decimal.Decimal `json:"list_price"`
	Note               string          `json:"note"`
	IsActive           bool            `json:"is_active"`
	Units              []itemUnitDTO   `json:"units"`
	Version            int32           `json:"version"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

func toItemDTO(i db.Item, units []itemUnitDTO) itemDTO {
	if units == nil {
		units = []itemUnitDTO{}
	}
	return itemDTO{
		ID: i.ID, Code: i.Code, Name: i.Name, Spec: i.Spec, CategoryID: i.CategoryID, ItemType: i.ItemType,
		BaseUnitID: i.BaseUnitID, Barcode: i.Barcode, TaxTypeID: i.TaxTypeID, DefaultWarehouseID: i.DefaultWarehouseID,
		SafetyStock: i.SafetyStock, ListPrice: i.ListPrice, Note: i.Note, IsActive: i.IsActive, Units: units,
		Version: i.Version, UpdatedAt: i.UpdatedAt,
	}
}

func (m *Module) itemUnits(ctx context.Context, q *db.Queries, ids []int64) (map[int64][]itemUnitDTO, error) {
	rows, err := q.ListItemUnits(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := map[int64][]itemUnitDTO{}
	for _, r := range rows {
		out[r.ItemID] = append(out[r.ItemID], itemUnitDTO{
			UnitID: r.UnitID, UnitCode: r.UnitCode, UnitName: r.UnitName, Factor: r.Factor, Barcode: r.Barcode,
		})
	}
	return out, nil
}

func (m *Module) listItems(c *gin.Context) {
	ctx := c.Request.Context()
	categoryID, err := httpx.QueryInt64(c, "category_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	isActive, err := httpx.QueryBool(c, "is_active")
	if err != nil {
		response.Error(c, err)
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	keyword, itemType := httpx.QueryString(c, "keyword"), httpx.QueryString(c, "item_type")

	rows, err := m.store.ListItems(ctx, db.ListItemsParams{
		CompanyID: companyID, Keyword: keyword, CategoryID: categoryID, ItemType: itemType, IsActive: isActive,
		Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountItems(ctx, db.CountItemsParams{
		CompanyID: companyID, Keyword: keyword, CategoryID: categoryID, ItemType: itemType, IsActive: isActive,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	units, err := m.itemUnits(ctx, m.store.Queries, ids)
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]itemDTO, len(rows))
	for i, r := range rows {
		dto := toItemDTO(db.Item{
			ID: r.ID, Code: r.Code, Name: r.Name, Spec: r.Spec, CategoryID: r.CategoryID, ItemType: r.ItemType,
			BaseUnitID: r.BaseUnitID, Barcode: r.Barcode, TaxTypeID: r.TaxTypeID, DefaultWarehouseID: r.DefaultWarehouseID,
			SafetyStock: r.SafetyStock, ListPrice: r.ListPrice, Note: r.Note, IsActive: r.IsActive,
			Version: r.Version, UpdatedAt: r.UpdatedAt,
		}, units[r.ID])
		dto.CategoryName, dto.BaseUnitName = r.CategoryName, r.BaseUnitName
		out[i] = dto
	}
	response.List(c, out, pg.Meta(total))
}

func (m *Module) getItem(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	item, err := m.store.GetItem(ctx, db.GetItemParams{ID: id, CompanyID: actor(c).CompanyID})
	if err != nil {
		response.Error(c, notFoundOr(err))
		return
	}
	units, err := m.itemUnits(ctx, m.store.Queries, []int64{id})
	reply(c, http.StatusOK, toItemDTO(item, units[id]), err)
}

type itemUnitInput struct {
	UnitID  int64           `json:"unit_id" binding:"required"`
	Factor  decimal.Decimal `json:"factor"`
	Barcode *string         `json:"barcode" binding:"omitempty,max=50"`
}

type itemInput struct {
	Code               string          `json:"code" binding:"required,max=40"`
	Name               string          `json:"name" binding:"required,max=200"`
	Spec               string          `json:"spec" binding:"max=255"`
	CategoryID         *int64          `json:"category_id"`
	ItemType           string          `json:"item_type" binding:"required,oneof=goods service"`
	BaseUnitID         int64           `json:"base_unit_id" binding:"required"`
	Barcode            *string         `json:"barcode" binding:"omitempty,max=50"`
	TaxTypeID          *int64          `json:"tax_type_id"`
	DefaultWarehouseID *int64          `json:"default_warehouse_id"`
	SafetyStock        decimal.Decimal `json:"safety_stock"`
	ListPrice          decimal.Decimal `json:"list_price"`
	Note               string          `json:"note" binding:"max=2000"`
	Units              []itemUnitInput `json:"units" binding:"dive"`
	IsActive           bool            `json:"is_active"`
	Version            int32           `json:"version"`
}

func (in *itemInput) normalize() error {
	in.Code = normCode(in.Code)
	in.Name = strings.TrimSpace(in.Name)
	in.Spec = strings.TrimSpace(in.Spec)
	in.Note = strings.TrimSpace(in.Note)
	in.Barcode = trimPtr(in.Barcode)

	fields := map[string]string{}
	if in.SafetyStock.IsNegative() || in.SafetyStock.Exponent() < -money.QuantityPlaces {
		fields["safety_stock"] = fmt.Sprintf("須 ≥ 0,最多 %d 位小數", money.QuantityPlaces)
	}
	if in.ListPrice.IsNegative() || in.ListPrice.Exponent() < -money.UnitPricePlaces {
		fields["list_price"] = fmt.Sprintf("須 ≥ 0,最多 %d 位小數", money.UnitPricePlaces)
	}
	if len(in.Units) > maxItemUnits {
		fields["units"] = fmt.Sprintf("最多 %d 個換算單位", maxItemUnits)
	}
	seen := map[int64]bool{}
	for i := range in.Units {
		u := &in.Units[i]
		u.Barcode = trimPtr(u.Barcode)
		key := fmt.Sprintf("units.%d", i)
		switch {
		case u.UnitID == in.BaseUnitID:
			fields[key] = "不可與基本單位相同"
		case seen[u.UnitID]:
			fields[key] = "單位重複"
		case !u.Factor.IsPositive() || u.Factor.Exponent() < -6:
			fields[key] = "換算數量須 > 0,最多 6 位小數"
		}
		seen[u.UnitID] = true
	}
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}
	return nil
}

func (in *itemInput) unitIDs() []int64 {
	ids := make([]int64, len(in.Units))
	for i, u := range in.Units {
		ids[i] = u.UnitID
	}
	return ids
}

// checkItemRefs 驗證分類、單位、稅別、倉庫皆存在且屬於同公司。
func checkItemRefs(ctx context.Context, q *db.Queries, companyID int64, in *itemInput) error {
	r, err := q.CheckItemRefs(ctx, db.CheckItemRefsParams{
		CompanyID: companyID, CategoryID: in.CategoryID, BaseUnitID: in.BaseUnitID,
		TaxTypeID: in.TaxTypeID, WarehouseID: in.DefaultWarehouseID,
	})
	if err != nil {
		return err
	}
	fields := map[string]string{}
	if !r.CategoryOk {
		fields["category_id"] = "分類不存在"
	}
	if !r.BaseUnitOk {
		fields["base_unit_id"] = "單位不存在"
	}
	if !r.TaxTypeOk {
		fields["tax_type_id"] = "稅別不存在"
	}
	if !r.WarehouseOk {
		fields["default_warehouse_id"] = "倉庫不存在"
	}
	if len(in.Units) > 0 {
		n, err := q.CountUnitsByIDs(ctx, db.CountUnitsByIDsParams{CompanyID: companyID, Ids: in.unitIDs()})
		if err != nil {
			return err
		}
		if n != int64(len(in.Units)) {
			fields["units"] = "包含不存在的單位"
		}
	}
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}
	return nil
}

// plainUnits 去掉單位代碼與名稱,讓稽核前後的結構一致,比對差異時不會誤判。
func plainUnits(units []itemUnitDTO) []itemUnitDTO {
	out := make([]itemUnitDTO, len(units))
	for i, u := range units {
		out[i] = itemUnitDTO{UnitID: u.UnitID, Factor: u.Factor, Barcode: u.Barcode}
	}
	return out
}

func saveItemUnits(ctx context.Context, q *db.Queries, itemID int64, units []itemUnitInput) ([]itemUnitDTO, error) {
	if err := q.DeleteItemUnits(ctx, itemID); err != nil {
		return nil, err
	}
	out := make([]itemUnitDTO, len(units))
	for i, u := range units {
		if err := q.AddItemUnit(ctx, db.AddItemUnitParams{ItemID: itemID, UnitID: u.UnitID, Factor: u.Factor, Barcode: u.Barcode}); err != nil {
			return nil, err
		}
		out[i] = itemUnitDTO{UnitID: u.UnitID, Factor: u.Factor, Barcode: u.Barcode}
	}
	return out, nil
}

func itemWriteErr(err error) error {
	err = uniqueOr(err, "items_company_code_key", errItemCodeDup)
	return uniqueOr(err, "items_company_barcode_key", errItemBarcodeDup)
}

func (m *Module) createItem(c *gin.Context) {
	in, ok := bind[itemInput](c)
	if !ok {
		return
	}
	if err := in.normalize(); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto itemDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		if err := checkItemRefs(ctx, q, a.CompanyID, &in); err != nil {
			return err
		}
		item, err := q.CreateItem(ctx, db.CreateItemParams{
			CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, Spec: in.Spec, CategoryID: in.CategoryID,
			ItemType: in.ItemType, BaseUnitID: in.BaseUnitID, Barcode: in.Barcode, TaxTypeID: in.TaxTypeID,
			DefaultWarehouseID: in.DefaultWarehouseID, SafetyStock: in.SafetyStock, ListPrice: in.ListPrice,
			Note: in.Note, CreatedBy: &a.UserID,
		})
		if err != nil {
			return itemWriteErr(err)
		}
		units, err := saveItemUnits(ctx, q, item.ID, in.Units)
		if err != nil {
			return err
		}
		dto = toItemDTO(item, units)
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "item", EntityID: &item.ID,
			Summary: "新增料品 " + item.Code + " " + item.Name, After: dto,
		})
	})
	reply(c, http.StatusCreated, dto, err)
}

func (m *Module) updateItem(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	in, ok := bind[itemInput](c)
	if !ok {
		return
	}
	if err := in.normalize(); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto itemDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetItem(ctx, db.GetItemParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return notFoundOr(err)
		}
		beforeUnits, err := m.itemUnits(ctx, q, []int64{id})
		if err != nil {
			return err
		}
		if err := checkItemRefs(ctx, q, a.CompanyID, &in); err != nil {
			return err
		}
		item, err := q.UpdateItem(ctx, db.UpdateItemParams{
			ID: id, CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, Spec: in.Spec, CategoryID: in.CategoryID,
			ItemType: in.ItemType, BaseUnitID: in.BaseUnitID, Barcode: in.Barcode, TaxTypeID: in.TaxTypeID,
			DefaultWarehouseID: in.DefaultWarehouseID, SafetyStock: in.SafetyStock, ListPrice: in.ListPrice,
			Note: in.Note, IsActive: in.IsActive, Version: in.Version, UpdatedBy: &a.UserID,
		})
		if err != nil {
			return itemWriteErr(versionConflictOr(err))
		}
		units, err := saveItemUnits(ctx, q, id, in.Units)
		if err != nil {
			return err
		}
		dto = toItemDTO(item, units)
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "item", EntityID: &id,
			Summary: "修改料品 " + item.Code + " " + item.Name,
			Before:  toItemDTO(before, plainUnits(beforeUnits[id])), After: dto,
		})
	})
	reply(c, http.StatusOK, dto, err)
}
