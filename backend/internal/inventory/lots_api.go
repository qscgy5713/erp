package inventory

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/masterdata"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
)

var tst = time.FixedZone("TST", 8*3600)

// today 今天(台北時間)的日期,效期以此判斷。
func today() time.Time {
	n := time.Now().In(tst)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// ExpiryStatus 效期狀態:expired 已過期 / expiring 即將到期(days 天內)/ ok / none 沒有效期
func ExpiryStatus(expiry *time.Time, now time.Time, days int) (status string, daysLeft *int) {
	if expiry == nil {
		return "none", nil
	}
	left := int(expiry.Sub(now).Hours() / 24)
	switch {
	case expiry.Before(now):
		return "expired", &left
	case left <= days:
		return "expiring", &left
	}
	return "ok", &left
}

type lotBalanceDTO struct {
	LotID         int64           `json:"lot_id"`
	LotNo         string          `json:"lot_no"`
	ExpiryDate    *string         `json:"expiry_date"`
	ExpiryStatus  string          `json:"expiry_status"`
	DaysLeft      *int            `json:"days_left"`
	ItemID        int64           `json:"item_id"`
	ItemCode      string          `json:"item_code"`
	ItemName      string          `json:"item_name"`
	ItemSpec      string          `json:"item_spec"`
	UnitName      string          `json:"unit_name"`
	WarehouseID   int64           `json:"warehouse_id"`
	WarehouseCode string          `json:"warehouse_code"`
	WarehouseName string          `json:"warehouse_name"`
	Qty           decimal.Decimal `json:"qty"`
}

const defaultExpiringDays = 30

// listLots GET /inventory/lots 批號庫存:可依料品、倉庫、關鍵字(批號 / 料號 / 品名)與效期狀態篩選。
// expiry=expired 已過期、expiring 即將到期(days 天內,預設 30)。
func (m *Module) listLots(c *gin.Context) {
	ctx := c.Request.Context()
	itemID, err := httpx.QueryInt64(c, "item_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	whID, err := httpx.QueryInt64(c, "warehouse_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	includeZero, err := queryFlag(c, "include_zero")
	if err != nil {
		response.Error(c, err)
		return
	}
	mode := c.Query("expiry")
	if mode != "" && mode != "expired" && mode != "expiring" {
		response.Error(c, fieldErr("expiry", "效期篩選須為 expired 或 expiring"))
		return
	}
	days := defaultExpiringDays
	if v, err := httpx.QueryInt64(c, "days"); err != nil {
		response.Error(c, err)
		return
	} else if v != nil && *v >= 0 && *v <= 3650 {
		days = int(*v)
	}
	now := today()
	pg := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	keyword := httpx.QueryString(c, "keyword")
	rows, err := m.store.ListLotBalances(ctx, db.ListLotBalancesParams{
		CompanyID: companyID, ItemID: itemID, WarehouseID: whID, Keyword: keyword, IncludeZero: includeZero,
		ExpiryMode: mode, Today: now, Until: now.AddDate(0, 0, days), Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountLotBalances(ctx, db.CountLotBalancesParams{
		CompanyID: companyID, ItemID: itemID, WarehouseID: whID, Keyword: keyword, IncludeZero: includeZero,
		ExpiryMode: mode, Today: now, Until: now.AddDate(0, 0, days),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]lotBalanceDTO, len(rows))
	for i, r := range rows {
		st, left := ExpiryStatus(r.ExpiryDate, now, days)
		out[i] = lotBalanceDTO{
			LotID: r.LotID, LotNo: r.LotNo, ExpiryDate: dateStr(r.ExpiryDate), ExpiryStatus: st, DaysLeft: left,
			ItemID: r.ItemID, ItemCode: r.ItemCode, ItemName: r.ItemName, ItemSpec: r.ItemSpec, UnitName: r.UnitName,
			WarehouseID: r.WarehouseID, WarehouseCode: r.WarehouseCode, WarehouseName: r.WarehouseName, Qty: r.Qty,
		}
	}
	response.List(c, out, pg.Meta(total))
}

type lotMoveDTO struct {
	Date          string          `json:"date"`
	Qty           decimal.Decimal `json:"qty"` // 正入負出
	SourceType    string          `json:"source_type"`
	SourceID      int64           `json:"source_id"`
	SourceNo      string          `json:"source_no"`
	WarehouseName string          `json:"warehouse_name"`
	Partner       string          `json:"partner"` // 進貨的供應商 / 出貨的客戶(客戶依資料範圍,範圍外不顯示)
	IsReversal    bool            `json:"is_reversal"`
	Balance       decimal.Decimal `json:"balance"` // 該批號的累計結存(所有倉庫)
}

type lotLedgerDTO struct {
	LotID      int64        `json:"lot_id"`
	LotNo      string       `json:"lot_no"`
	ExpiryDate *string      `json:"expiry_date"`
	ItemID     int64        `json:"item_id"`
	ItemCode   string       `json:"item_code"`
	ItemName   string       `json:"item_name"`
	Moves      []lotMoveDTO `json:"moves"`
	Bins       []lotBinDTO  `json:"bins"` // 啟用儲位的倉庫:這個批號目前放在哪些儲位
}

type lotBinDTO struct {
	WarehouseID int64           `json:"warehouse_id"`
	BinCode     string          `json:"bin_code"`
	BinName     string          `json:"bin_name"`
	Qty         decimal.Decimal `json:"qty"`
}

// lotLedger GET /inventory/lots/:id/ledger 批號追溯:這個批號從哪裡進、出到哪裡。
func (m *Module) lotLedger(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	lot, err := m.store.GetLotDetail(ctx, db.GetLotDetailParams{ID: id, CompanyID: a.CompanyID})
	if err != nil {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	rows, err := m.store.LotLedger(ctx, db.LotLedgerParams{LotID: &id, CompanyID: a.CompanyID})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := lotLedgerDTO{LotID: lot.ID, LotNo: lot.LotNo, ExpiryDate: dateStr(lot.ExpiryDate), ItemID: lot.ItemID,
		ItemCode: lot.ItemCode, ItemName: lot.ItemName, Moves: make([]lotMoveDTO, 0, len(rows)), Bins: []lotBinDTO{}}
	bins, err := m.store.ListLotBinStock(ctx, db.ListLotBinStockParams{CompanyID: a.CompanyID, LotID: id})
	if err != nil {
		response.Error(c, err)
		return
	}
	for _, b := range bins {
		out.Bins = append(out.Bins, lotBinDTO{WarehouseID: b.WarehouseID, BinCode: b.BinCode, BinName: b.BinName, Qty: b.Qty})
	}
	bal := decimal.Zero
	for _, r := range rows {
		bal = bal.Add(r.Qty)
		partner := r.SupplierName
		if r.CustomerName != "" && customerVisible(a, r.SalesUserID, r.SalesDepartmentID) {
			partner = r.CustomerName
		}
		out.Moves = append(out.Moves, lotMoveDTO{
			Date: r.DocDate.Format(time.DateOnly), Qty: r.Qty, SourceType: r.SourceType, SourceID: r.SourceID, SourceNo: r.SourceNo,
			WarehouseName: r.WarehouseName, Partner: partner, IsReversal: r.ReversalOf != nil, Balance: bal,
		})
	}
	response.OK(c, out)
}

type lotOptionDTO struct {
	LotNo        string           `json:"lot_no"`
	ExpiryDate   *string          `json:"expiry_date"`
	ExpiryStatus string           `json:"expiry_status"`
	Qty          *decimal.Decimal `json:"qty,omitempty"` // 指定倉庫時為該倉現有量
}

// lotOptions GET /inventory/lot-options?item_id=&warehouse_id= 開單挑選批號:
// 指定倉庫時列出該倉有庫存的批號(先到期先出的順序);沒指定倉庫則列出料品所有已建立的批號。
func (m *Module) lotOptions(c *gin.Context) {
	ctx := c.Request.Context()
	itemID, err := httpx.QueryInt64(c, "item_id")
	if err != nil || itemID == nil {
		response.Error(c, fieldErr("item_id", "請指定料品"))
		return
	}
	whID, err := httpx.QueryInt64(c, "warehouse_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	companyID := actor(c).CompanyID
	now := today()
	out := []lotOptionDTO{}
	if whID != nil {
		rows, err := m.store.ListLotOptions(ctx, db.ListLotOptionsParams{CompanyID: companyID, ItemID: *itemID, WarehouseID: *whID})
		if err != nil {
			response.Error(c, err)
			return
		}
		for _, r := range rows {
			st, _ := ExpiryStatus(r.ExpiryDate, now, defaultExpiringDays)
			q := r.Qty
			out = append(out, lotOptionDTO{LotNo: r.LotNo, ExpiryDate: dateStr(r.ExpiryDate), ExpiryStatus: st, Qty: &q})
		}
	} else {
		rows, err := m.store.ListItemLots(ctx, db.ListItemLotsParams{CompanyID: companyID, ItemID: *itemID})
		if err != nil {
			response.Error(c, err)
			return
		}
		for _, r := range rows {
			st, _ := ExpiryStatus(r.ExpiryDate, now, defaultExpiringDays)
			out = append(out, lotOptionDTO{LotNo: r.LotNo, ExpiryDate: dateStr(r.ExpiryDate), ExpiryStatus: st})
		}
	}
	response.OK(c, out)
}

func customerVisible(a *authctx.Actor, salesUserID, salesDeptID *int64) bool {
	return masterdata.CustomerVisible(a, salesUserID, salesDeptID)
}
