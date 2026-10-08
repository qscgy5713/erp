package masterdata

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/system/audit"
)

var (
	errUnitCodeDup         = apperr.Conflict("UNIT-001", "單位代碼已存在")
	errCategoryCodeDup     = apperr.Conflict("CAT-001", "分類代碼已存在")
	errCategoryParent      = fieldErr("parent_id", "上層分類不存在")
	errCategoryCycle       = fieldErr("parent_id", "上層分類不可為自己或自己的下層")
	errWarehouseCodeDup    = apperr.Conflict("WH-001", "倉庫代碼已存在")
	errWarehouseBinsSwitch = apperr.Conflict("WH-002", "倉庫還有庫存,不能啟用或停用儲位;請先把庫存調整為 0")
)

// ---- 單位 ----

type unitDTO struct {
	ID       int64  `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
	Version  int32  `json:"version"`
}

func toUnitDTO(u db.Unit) unitDTO {
	return unitDTO{ID: u.ID, Code: u.Code, Name: u.Name, IsActive: u.IsActive, Version: u.Version}
}

func (m *Module) listUnits(c *gin.Context) {
	rows, err := m.store.ListUnits(c.Request.Context(), actor(c).CompanyID)
	out := make([]unitDTO, len(rows))
	for i, r := range rows {
		out[i] = toUnitDTO(r)
	}
	reply(c, http.StatusOK, out, err)
}

type unitInput struct {
	Code     string `json:"code" binding:"required,max=10"`
	Name     string `json:"name" binding:"required,max=20"`
	IsActive bool   `json:"is_active"`
	Version  int32  `json:"version"`
}

func (m *Module) createUnit(c *gin.Context) {
	in, ok := bind[unitInput](c)
	if !ok {
		return
	}
	in.Code, in.Name = normCode(in.Code), strings.TrimSpace(in.Name)
	ctx := c.Request.Context()
	a := actor(c)
	var out db.Unit
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		var err error
		out, err = q.CreateUnit(ctx, db.CreateUnitParams{CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, CreatedBy: &a.UserID})
		if err != nil {
			return uniqueOr(err, "units_company_code_key", errUnitCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "unit", EntityID: &out.ID,
			Summary: "新增單位 " + out.Code + " " + out.Name, After: toUnitDTO(out),
		})
	})
	reply(c, http.StatusCreated, toUnitDTO(out), err)
}

func (m *Module) updateUnit(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	in, ok := bind[unitInput](c)
	if !ok {
		return
	}
	in.Code, in.Name = normCode(in.Code), strings.TrimSpace(in.Name)
	ctx := c.Request.Context()
	a := actor(c)
	var out db.Unit
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetUnit(ctx, db.GetUnitParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return notFoundOr(err)
		}
		out, err = q.UpdateUnit(ctx, db.UpdateUnitParams{
			ID: id, CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, IsActive: in.IsActive,
			Version: in.Version, UpdatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(versionConflictOr(err), "units_company_code_key", errUnitCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "unit", EntityID: &id,
			Summary: "修改單位 " + out.Code + " " + out.Name, Before: toUnitDTO(before), After: toUnitDTO(out),
		})
	})
	reply(c, http.StatusOK, toUnitDTO(out), err)
}

// ---- 料品分類 ----

type categoryDTO struct {
	ID        int64  `json:"id"`
	ParentID  *int64 `json:"parent_id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	SortOrder int32  `json:"sort_order"`
	IsActive  bool   `json:"is_active"`
	ItemCount int64  `json:"item_count"`
	Version   int32  `json:"version"`
}

func toCategoryDTO(c db.ItemCategory) categoryDTO {
	return categoryDTO{ID: c.ID, ParentID: c.ParentID, Code: c.Code, Name: c.Name, SortOrder: c.SortOrder,
		IsActive: c.IsActive, Version: c.Version}
}

func (m *Module) listCategories(c *gin.Context) {
	rows, err := m.store.ListItemCategories(c.Request.Context(), actor(c).CompanyID)
	out := make([]categoryDTO, len(rows))
	for i, r := range rows {
		out[i] = categoryDTO{ID: r.ID, ParentID: r.ParentID, Code: r.Code, Name: r.Name, SortOrder: r.SortOrder,
			IsActive: r.IsActive, ItemCount: r.ItemCount, Version: r.Version}
	}
	reply(c, http.StatusOK, out, err)
}

type categoryInput struct {
	ParentID  *int64 `json:"parent_id"`
	Code      string `json:"code" binding:"required,max=20"`
	Name      string `json:"name" binding:"required,max=100"`
	SortOrder int32  `json:"sort_order"`
	IsActive  bool   `json:"is_active"`
	Version   int32  `json:"version"`
}

func checkCategoryParent(ctx context.Context, q *db.Queries, companyID int64, parentID *int64, selfID int64) error {
	if parentID == nil {
		return nil
	}
	if _, err := q.GetItemCategory(ctx, db.GetItemCategoryParams{ID: *parentID, CompanyID: companyID}); err != nil {
		if database.IsNoRows(err) {
			return errCategoryParent
		}
		return err
	}
	if selfID == 0 {
		return nil
	}
	cyclic, err := q.IsItemCategoryDescendant(ctx, db.IsItemCategoryDescendantParams{AncestorID: selfID, CandidateID: *parentID})
	if err != nil {
		return err
	}
	if cyclic {
		return errCategoryCycle
	}
	return nil
}

func (m *Module) createCategory(c *gin.Context) {
	in, ok := bind[categoryInput](c)
	if !ok {
		return
	}
	in.Code, in.Name = normCode(in.Code), strings.TrimSpace(in.Name)
	ctx := c.Request.Context()
	a := actor(c)
	var out db.ItemCategory
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		if err := checkCategoryParent(ctx, q, a.CompanyID, in.ParentID, 0); err != nil {
			return err
		}
		var err error
		out, err = q.CreateItemCategory(ctx, db.CreateItemCategoryParams{
			CompanyID: a.CompanyID, ParentID: in.ParentID, Code: in.Code, Name: in.Name, SortOrder: in.SortOrder,
			CreatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(err, "item_categories_company_code_key", errCategoryCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "item_category", EntityID: &out.ID,
			Summary: "新增料品分類 " + out.Code + " " + out.Name, After: toCategoryDTO(out),
		})
	})
	reply(c, http.StatusCreated, toCategoryDTO(out), err)
}

func (m *Module) updateCategory(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	in, ok := bind[categoryInput](c)
	if !ok {
		return
	}
	in.Code, in.Name = normCode(in.Code), strings.TrimSpace(in.Name)
	ctx := c.Request.Context()
	a := actor(c)
	var out db.ItemCategory
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetItemCategory(ctx, db.GetItemCategoryParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return notFoundOr(err)
		}
		if err := checkCategoryParent(ctx, q, a.CompanyID, in.ParentID, id); err != nil {
			return err
		}
		out, err = q.UpdateItemCategory(ctx, db.UpdateItemCategoryParams{
			ID: id, CompanyID: a.CompanyID, ParentID: in.ParentID, Code: in.Code, Name: in.Name,
			SortOrder: in.SortOrder, IsActive: in.IsActive, Version: in.Version, UpdatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(versionConflictOr(err), "item_categories_company_code_key", errCategoryCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "item_category", EntityID: &id,
			Summary: "修改料品分類 " + out.Code + " " + out.Name, Before: toCategoryDTO(before), After: toCategoryDTO(out),
		})
	})
	reply(c, http.StatusOK, toCategoryDTO(out), err)
}

// ---- 倉庫 ----

type warehouseDTO struct {
	ID            int64  `json:"id"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	Address       string `json:"address"`
	AllowNegative bool   `json:"allow_negative"`
	UseBins       bool   `json:"use_bins"`
	IsActive      bool   `json:"is_active"`
	Version       int32  `json:"version"`
}

func toWarehouseDTO(w db.Warehouse) warehouseDTO {
	return warehouseDTO{ID: w.ID, Code: w.Code, Name: w.Name, Address: w.Address, AllowNegative: w.AllowNegative,
		UseBins: w.UseBins, IsActive: w.IsActive, Version: w.Version}
}

func (m *Module) listWarehouses(c *gin.Context) {
	rows, err := m.store.ListWarehouses(c.Request.Context(), actor(c).CompanyID)
	out := make([]warehouseDTO, len(rows))
	for i, r := range rows {
		out[i] = toWarehouseDTO(r)
	}
	reply(c, http.StatusOK, out, err)
}

type warehouseInput struct {
	Code          string `json:"code" binding:"required,max=20"`
	Name          string `json:"name" binding:"required,max=100"`
	Address       string `json:"address" binding:"max=255"`
	AllowNegative bool   `json:"allow_negative"`
	UseBins       bool   `json:"use_bins"`
	IsActive      bool   `json:"is_active"`
	Version       int32  `json:"version"`
}

func (in *warehouseInput) normalize() {
	in.Code, in.Name, in.Address = normCode(in.Code), strings.TrimSpace(in.Name), strings.TrimSpace(in.Address)
}

func (m *Module) createWarehouse(c *gin.Context) {
	in, ok := bind[warehouseInput](c)
	if !ok {
		return
	}
	in.normalize()
	ctx := c.Request.Context()
	a := actor(c)
	var out db.Warehouse
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		var err error
		out, err = q.CreateWarehouse(ctx, db.CreateWarehouseParams{
			CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, Address: in.Address,
			AllowNegative: in.AllowNegative, UseBins: in.UseBins, CreatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(err, "warehouses_company_code_key", errWarehouseCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "warehouse", EntityID: &out.ID,
			Summary: "新增倉庫 " + out.Code + " " + out.Name, After: toWarehouseDTO(out),
		})
	})
	reply(c, http.StatusCreated, toWarehouseDTO(out), err)
}

func (m *Module) updateWarehouse(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	in, ok := bind[warehouseInput](c)
	if !ok {
		return
	}
	in.normalize()
	ctx := c.Request.Context()
	a := actor(c)
	var out db.Warehouse
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetWarehouse(ctx, db.GetWarehouseParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return notFoundOr(err)
		}
		if in.UseBins != before.UseBins {
			// 儲位現有量要與倉庫現有量一致,所以只能在倉庫沒有庫存時切換(啟用 / 停用都一樣)
			if has, err := q.WarehouseHasStock(ctx, id); err != nil {
				return err
			} else if has {
				return errWarehouseBinsSwitch
			}
		}
		out, err = q.UpdateWarehouse(ctx, db.UpdateWarehouseParams{
			ID: id, CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, Address: in.Address,
			AllowNegative: in.AllowNegative, UseBins: in.UseBins, IsActive: in.IsActive, Version: in.Version, UpdatedBy: &a.UserID,
		})
		if err != nil {
			return uniqueOr(versionConflictOr(err), "warehouses_company_code_key", errWarehouseCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "warehouse", EntityID: &id,
			Summary: "修改倉庫 " + out.Code + " " + out.Name, Before: toWarehouseDTO(before), After: toWarehouseDTO(out),
		})
	})
	reply(c, http.StatusOK, toWarehouseDTO(out), err)
}
