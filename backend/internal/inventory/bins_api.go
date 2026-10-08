package inventory

import (
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/httpx"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
)

type binStockDTO struct {
	BinID         int64           `json:"bin_id"`
	BinCode       string          `json:"bin_code"`
	BinName       string          `json:"bin_name"`
	WarehouseID   int64           `json:"warehouse_id"`
	WarehouseCode string          `json:"warehouse_code"`
	WarehouseName string          `json:"warehouse_name"`
	ItemID        int64           `json:"item_id"`
	ItemCode      string          `json:"item_code"`
	ItemName      string          `json:"item_name"`
	UnitName      string          `json:"unit_name"`
	Qty           decimal.Decimal `json:"qty"`
}

// listBinStock GET /inventory/bin-stock 儲位庫存:各儲位放了哪些料品、多少;可依倉庫、儲位、關鍵字(料品或儲位代號)篩選。
func (m *Module) listBinStock(c *gin.Context) {
	ctx := c.Request.Context()
	whID, err := httpx.QueryInt64(c, "warehouse_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	binID, err := httpx.QueryInt64(c, "bin_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	includeZero, err := queryFlag(c, "include_zero")
	if err != nil {
		response.Error(c, err)
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	keyword := httpx.QueryString(c, "keyword")
	rows, err := m.store.ListBinStock(ctx, db.ListBinStockParams{CompanyID: companyID, WarehouseID: whID, BinID: binID,
		Keyword: keyword, IncludeZero: includeZero, Lim: pg.Limit(), Off: pg.Offset()})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountBinStock(ctx, db.CountBinStockParams{CompanyID: companyID, WarehouseID: whID, BinID: binID,
		Keyword: keyword, IncludeZero: includeZero})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]binStockDTO, len(rows))
	for i, r := range rows {
		out[i] = binStockDTO{BinID: r.BinID, BinCode: r.BinCode, BinName: r.BinName, WarehouseID: r.WarehouseID, WarehouseCode: r.WarehouseCode,
			WarehouseName: r.WarehouseName, ItemID: r.ItemID, ItemCode: r.ItemCode, ItemName: r.ItemName, UnitName: r.UnitName, Qty: r.Qty}
	}
	response.List(c, out, pg.Meta(total))
}
