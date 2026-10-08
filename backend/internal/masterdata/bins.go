package masterdata

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/system/audit"
)

var (
	errBinCodeDup  = apperr.Conflict("BIN-001", "這個倉庫已有相同代號的儲位")
	errBinNoUse    = apperr.Conflict("BIN-002", "這個倉庫沒有啟用儲位,請先到倉庫設定啟用")
	errBinHasStock = apperr.Conflict("BIN-003", "儲位還有庫存,不能停用")
	errBinUsed     = apperr.Conflict("BIN-004", "儲位已有庫存異動紀錄,不能刪除;不再使用請改為停用")
	binCodeRe      = regexp.MustCompile(`^[A-Z0-9._-]{1,20}$`)
)

type binDTO struct {
	ID          int64           `json:"id"`
	WarehouseID int64           `json:"warehouse_id"`
	Code        string          `json:"code"`
	Name        string          `json:"name"`
	IsActive    bool            `json:"is_active"`
	StockQty    decimal.Decimal `json:"stock_qty"` // 儲位內所有料品的數量合計(只用來判斷是否空了)
	Version     int32           `json:"version"`
}

func toBinDTO(b db.Bin, stock decimal.Decimal) binDTO {
	return binDTO{ID: b.ID, WarehouseID: b.WarehouseID, Code: b.Code, Name: b.Name, IsActive: b.IsActive, StockQty: stock, Version: b.Version}
}

// listBins GET /masterdata/bins?warehouse_id= 儲位清單。
func (m *Module) listBins(c *gin.Context) {
	wh, err := httpx.QueryInt64(c, "warehouse_id")
	if err != nil {
		reply(c, http.StatusOK, nil, err)
		return
	}
	rows, err := m.store.ListBins(c.Request.Context(), db.ListBinsParams{CompanyID: actor(c).CompanyID, WarehouseID: wh})
	out := make([]binDTO, len(rows))
	for i, r := range rows {
		out[i] = toBinDTO(db.Bin{ID: r.ID, WarehouseID: r.WarehouseID, Code: r.Code, Name: r.Name, IsActive: r.IsActive, Version: r.Version}, r.StockQty)
	}
	reply(c, http.StatusOK, out, err)
}

type binInput struct {
	WarehouseID int64  `json:"warehouse_id"`
	Code        string `json:"code" binding:"required,max=20"`
	Name        string `json:"name" binding:"max=100"`
	IsActive    bool   `json:"is_active"`
	Version     int32  `json:"version"`
}

func (m *Module) createBin(c *gin.Context) {
	in, ok := bind[binInput](c)
	if !ok {
		return
	}
	in.Code, in.Name = normCode(in.Code), strings.TrimSpace(in.Name)
	if !binCodeRe.MatchString(in.Code) {
		reply(c, http.StatusCreated, nil, apperr.Validation(map[string]string{"code": "儲位代號限英數字與 . _ -,最多 20 碼"}))
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var out db.Bin
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		w, err := q.GetWarehouse(ctx, db.GetWarehouseParams{ID: in.WarehouseID, CompanyID: a.CompanyID})
		if err != nil {
			return apperr.Validation(map[string]string{"warehouse_id": "倉庫不存在"})
		}
		if !w.UseBins {
			return errBinNoUse
		}
		out, err = q.CreateBin(ctx, db.CreateBinParams{CompanyID: a.CompanyID, WarehouseID: w.ID, Code: in.Code, Name: in.Name, ActorID: &a.UserID})
		if err != nil {
			return uniqueOr(err, "bins_warehouse_code_key", errBinCodeDup)
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Create, EntityType: "bin", EntityID: &out.ID,
			Summary: "新增儲位 " + w.Code + "/" + out.Code, After: toBinDTO(out, decimal.Zero)})
	})
	reply(c, http.StatusCreated, toBinDTO(out, decimal.Zero), err)
}

func (m *Module) updateBin(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	in, ok := bind[binInput](c)
	if !ok {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	ctx := c.Request.Context()
	a := actor(c)
	var out db.Bin
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetBin(ctx, db.GetBinParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return notFoundOr(err)
		}
		if before.IsActive && !in.IsActive {
			if has, err := q.BinHasStock(ctx, id); err != nil {
				return err
			} else if has {
				return errBinHasStock
			}
		}
		out, err = q.UpdateBin(ctx, db.UpdateBinParams{ID: id, CompanyID: a.CompanyID, Name: in.Name, IsActive: in.IsActive, ActorID: &a.UserID, Version: in.Version})
		if err != nil {
			return versionConflictOr(err)
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Update, EntityType: "bin", EntityID: &id,
			Summary: "修改儲位 " + out.Code, Before: toBinDTO(before, decimal.Zero), After: toBinDTO(out, decimal.Zero)})
	})
	reply(c, http.StatusOK, toBinDTO(out, decimal.Zero), err)
}

// deleteBin 沒有任何庫存異動紀錄的儲位才能刪除(否則歷史紀錄會指向不存在的儲位),其餘請停用。
func (m *Module) deleteBin(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetBin(ctx, db.GetBinParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return notFoundOr(err)
		}
		if used, err := q.BinHasTransactions(ctx, &id); err != nil {
			return err
		} else if used {
			return errBinUsed
		}
		if _, err := q.DeleteBin(ctx, db.DeleteBinParams{ID: id, CompanyID: a.CompanyID}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Delete, EntityType: "bin", EntityID: &id,
			Summary: "刪除儲位 " + before.Code, Before: toBinDTO(before, decimal.Zero)})
	})
	if err != nil {
		reply(c, http.StatusOK, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}
