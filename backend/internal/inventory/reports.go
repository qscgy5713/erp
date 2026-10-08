package inventory

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/httpx"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
)

type balanceDTO struct {
	ItemID        int64           `json:"item_id"`
	ItemCode      string          `json:"item_code"`
	ItemName      string          `json:"item_name"`
	ItemSpec      string          `json:"item_spec"`
	UnitName      string          `json:"unit_name"`
	WarehouseID   int64           `json:"warehouse_id"`
	WarehouseCode string          `json:"warehouse_code"`
	WarehouseName string          `json:"warehouse_name"`
	Qty           decimal.Decimal `json:"qty"`
	ItemTotal     decimal.Decimal `json:"item_total"` // 料品在所有倉庫的合計
	SafetyStock   decimal.Decimal `json:"safety_stock"`
	BelowSafety   bool            `json:"below_safety"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func queryFlag(c *gin.Context, name string) (bool, error) {
	v, err := httpx.QueryBool(c, name)
	return v != nil && *v, err
}

// listBalances 現有量:可依倉庫、關鍵字、分類(含下層)、非零、低於安全庫存篩選。
func (m *Module) listBalances(c *gin.Context) {
	ctx := c.Request.Context()
	whID, err := httpx.QueryInt64(c, "warehouse_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	catID, err := httpx.QueryInt64(c, "category_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	nonzero, err := queryFlag(c, "nonzero")
	if err != nil {
		response.Error(c, err)
		return
	}
	below, err := queryFlag(c, "below_safety")
	if err != nil {
		response.Error(c, err)
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	keyword := httpx.QueryString(c, "keyword")
	rows, err := m.store.ListBalances(ctx, db.ListBalancesParams{
		CompanyID: companyID, WarehouseID: whID, Keyword: keyword, CategoryID: catID, Nonzero: nonzero,
		BelowSafety: below, Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountBalances(ctx, db.CountBalancesParams{
		CompanyID: companyID, WarehouseID: whID, Keyword: keyword, CategoryID: catID, Nonzero: nonzero, BelowSafety: below,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]balanceDTO, len(rows))
	for i, r := range rows {
		out[i] = balanceDTO{
			ItemID: r.ItemID, ItemCode: r.ItemCode, ItemName: r.ItemName, ItemSpec: r.ItemSpec, UnitName: r.UnitName,
			WarehouseID: r.WarehouseID, WarehouseCode: r.WarehouseCode, WarehouseName: r.WarehouseName,
			Qty: r.Qty, ItemTotal: r.ItemTotal, SafetyStock: r.SafetyStock,
			BelowSafety: r.SafetyStock.IsPositive() && r.ItemTotal.LessThan(r.SafetyStock), UpdatedAt: r.UpdatedAt,
		}
	}
	response.List(c, out, pg.Meta(total))
}

// dateRange 解析 from/to(必填,YYYY-MM-DD)。
func dateRange(c *gin.Context) (time.Time, time.Time, error) {
	from, err := time.Parse(time.DateOnly, c.Query("from"))
	if err != nil {
		return from, from, fieldErr("from", "請指定開始日期(YYYY-MM-DD)")
	}
	to, err := time.Parse(time.DateOnly, c.Query("to"))
	if err != nil {
		return from, to, fieldErr("to", "請指定結束日期(YYYY-MM-DD)")
	}
	if to.Before(from) {
		return from, to, fieldErr("to", "結束日期不可早於開始日期")
	}
	return from, to, nil
}

type movementSummaryDTO struct {
	ItemID     int64           `json:"item_id"`
	ItemCode   string          `json:"item_code"`
	ItemName   string          `json:"item_name"`
	UnitName   string          `json:"unit_name"`
	OpeningQty decimal.Decimal `json:"opening_qty"`
	InQty      decimal.Decimal `json:"in_qty"`
	OutQty     decimal.Decimal `json:"out_qty"`
	ClosingQty decimal.Decimal `json:"closing_qty"`
}

// movementSummary 收發存:期初 + 收 − 發 = 期末(依單據日期)。
func (m *Module) movementSummary(c *gin.Context) {
	ctx := c.Request.Context()
	from, to, err := dateRange(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	whID, err := httpx.QueryInt64(c, "warehouse_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	keyword := httpx.QueryString(c, "keyword")
	rows, err := m.store.StockMovementSummary(ctx, db.StockMovementSummaryParams{
		CompanyID: companyID, FromDate: from, ToDate: to, WarehouseID: whID, Keyword: keyword,
		Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountStockMovementSummary(ctx, db.CountStockMovementSummaryParams{
		CompanyID: companyID, ToDate: to, WarehouseID: whID, Keyword: keyword,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]movementSummaryDTO, len(rows))
	for i, r := range rows {
		out[i] = movementSummaryDTO{
			ItemID: r.ItemID, ItemCode: r.ItemCode, ItemName: r.ItemName, UnitName: r.UnitName,
			OpeningQty: r.OpeningQty, InQty: r.InQty, OutQty: r.OutQty, ClosingQty: r.ClosingQty,
		}
	}
	response.List(c, out, pg.Meta(total))
}

type ledgerEntryDTO struct {
	ID            int64           `json:"id"`
	DocDate       string          `json:"doc_date"`
	WarehouseCode string          `json:"warehouse_code"`
	WarehouseName string          `json:"warehouse_name"`
	SourceType    string          `json:"source_type"`
	SourceID      int64           `json:"source_id"`
	SourceNo      string          `json:"source_no"`
	IsReversal    bool            `json:"is_reversal"`
	Qty           decimal.Decimal `json:"qty"`
	BalanceQty    decimal.Decimal `json:"balance_qty"`
	CreatedAt     time.Time       `json:"created_at"`
}

// itemLedger 料品異動明細:期初結存 + 每筆異動與累計結存(最多 1000 筆)。
func (m *Module) itemLedger(c *gin.Context) {
	ctx := c.Request.Context()
	itemID, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	from, to, err := dateRange(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	whID, err := httpx.QueryInt64(c, "warehouse_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	companyID := actor(c).CompanyID
	opening, err := m.store.ItemLedgerOpening(ctx, db.ItemLedgerOpeningParams{
		CompanyID: companyID, ItemID: itemID, FromDate: from, WarehouseID: whID,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	rows, err := m.store.ItemLedger(ctx, db.ItemLedgerParams{
		CompanyID: companyID, ItemID: itemID, FromDate: from, ToDate: to, WarehouseID: whID,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	entries := make([]ledgerEntryDTO, len(rows))
	for i, r := range rows {
		entries[i] = ledgerEntryDTO{
			ID: r.ID, DocDate: r.DocDate.Format(time.DateOnly), WarehouseCode: r.WarehouseCode,
			WarehouseName: r.WarehouseName, SourceType: r.SourceType, SourceID: r.SourceID, SourceNo: r.SourceNo,
			IsReversal: r.ReversalOf != nil, Qty: r.Qty, BalanceQty: opening.Add(r.RunningQty), CreatedAt: r.CreatedAt,
		}
	}
	response.OK(c, gin.H{"opening_qty": opening, "entries": entries})
}
