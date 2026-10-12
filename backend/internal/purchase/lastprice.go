package purchase

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/shared/response"
	"erp/internal/trade"
)

type lastPriceDTO struct {
	ItemID    int64           `json:"item_id"`
	UnitID    int64           `json:"unit_id"`
	UnitPrice decimal.Decimal `json:"unit_price"`
	DocDate   string          `json:"doc_date"`
	DocNo     string          `json:"doc_no"`
}

// lastPrices GET /purchase/last-prices?supplier_id=&currency=&item_ids=1,2
// 開採購單 / 進貨單時帶入「同供應商、同幣別」的最近進價(未稅、原單位)。
func (m *Module) lastPrices(c *gin.Context) {
	supplierID, err := strconv.ParseInt(c.Query("supplier_id"), 10, 64)
	if err != nil || supplierID <= 0 {
		response.Error(c, fieldErr("supplier_id", "請指定供應商"))
		return
	}
	currency := strings.ToUpper(strings.TrimSpace(c.Query("currency")))
	if currency == "" {
		response.Error(c, fieldErr("currency", "請指定幣別"))
		return
	}
	var ids []int64
	for _, s := range strings.Split(c.Query("item_ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil || id <= 0 {
			response.Error(c, fieldErr("item_ids", "料品 id 格式錯誤"))
			return
		}
		ids = append(ids, id)
	}
	if len(ids) > trade.MaxLines {
		response.Error(c, fieldErr("item_ids", "料品過多"))
		return
	}
	rows, err := m.store.LastPurchasePrices(c.Request.Context(), db.LastPurchasePricesParams{
		CompanyID: actor(c).CompanyID, SupplierID: supplierID, Currency: currency, ItemIds: ids,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]lastPriceDTO, len(rows))
	for i, r := range rows {
		out[i] = lastPriceDTO{ItemID: r.ItemID, UnitID: r.UnitID, UnitPrice: r.UnitPrice, DocDate: r.DocDate.Format(time.DateOnly), DocNo: r.DocNo}
	}
	response.OK(c, out)
}
