package production

import (
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

type mrpParamDTO struct {
	ItemID        int64           `json:"item_id"`
	ItemCode      string          `json:"item_code"`
	ItemName      string          `json:"item_name"`
	UnitName      string          `json:"unit_name"`
	LeadTimeDays  int32           `json:"lead_time_days"`
	MinOrderQty   decimal.Decimal `json:"min_order_qty"`
	OrderMultiple decimal.Decimal `json:"order_multiple"`
	HasBom        bool            `json:"has_bom"`
}

// listMrpParams GET /production/mrp-params?keyword= 商品類料品的 MRP 計畫參數(沒設定的為 0)。
func (m *Module) listMrpParams(c *gin.Context) {
	pg := page.Parse(c.Query("page"), c.Query("size"))
	keyword := httpx.QueryString(c, "keyword")
	ctx := c.Request.Context()
	companyID := actor(c).CompanyID
	rows, err := m.store.ListMrpParams(ctx, db.ListMrpParamsParams{CompanyID: companyID, Keyword: keyword, Lim: pg.Limit(), Off: pg.Offset()})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountMrpParams(ctx, db.CountMrpParamsParams{CompanyID: companyID, Keyword: keyword})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]mrpParamDTO, len(rows))
	for i, r := range rows {
		out[i] = mrpParamDTO{ItemID: r.ItemID, ItemCode: r.ItemCode, ItemName: r.ItemName, UnitName: r.UnitName,
			LeadTimeDays: r.LeadTimeDays, MinOrderQty: r.MinOrderQty, OrderMultiple: r.OrderMultiple, HasBom: r.HasBom}
	}
	response.List(c, out, pg.Meta(total))
}

type mrpParamInput struct {
	LeadTimeDays  int32           `json:"lead_time_days"`
	MinOrderQty   decimal.Decimal `json:"min_order_qty"`
	OrderMultiple decimal.Decimal `json:"order_multiple"`
}

// setMrpParams PUT /production/mrp-params/:item_id 全部為 0 時等同清除設定。
func (m *Module) setMrpParams(c *gin.Context) {
	itemID, err := httpx.ParamID(c, "item_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in mrpParamInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	fields := map[string]string{}
	if in.LeadTimeDays < 0 || in.LeadTimeDays > 365 {
		fields["lead_time_days"] = "須為 0 到 365 天"
	}
	if in.MinOrderQty.IsNegative() || in.MinOrderQty.Exponent() < -money.QuantityPlaces {
		fields["min_order_qty"] = "須 ≥ 0,最多 4 位小數"
	}
	if in.OrderMultiple.IsNegative() || in.OrderMultiple.Exponent() < -money.QuantityPlaces {
		fields["order_multiple"] = "須 ≥ 0,最多 4 位小數"
	}
	if len(fields) > 0 {
		response.Error(c, apperr.Validation(fields))
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		ok, err := q.MrpParamsItemOK(ctx, db.MrpParamsItemOKParams{ID: itemID, CompanyID: a.CompanyID})
		if err != nil {
			return err
		}
		if !ok {
			return apperr.ErrNotFound
		}
		if in.LeadTimeDays == 0 && in.MinOrderQty.IsZero() && in.OrderMultiple.IsZero() {
			if err := q.DeleteMrpParams(ctx, db.DeleteMrpParamsParams{ItemID: itemID, CompanyID: a.CompanyID}); err != nil {
				return err
			}
		} else if err := q.UpsertMrpParams(ctx, db.UpsertMrpParamsParams{ItemID: itemID, CompanyID: a.CompanyID,
			LeadTimeDays: in.LeadTimeDays, MinOrderQty: in.MinOrderQty, OrderMultiple: in.OrderMultiple, ActorID: &a.UserID}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Update, EntityType: "mrp_params", EntityID: &itemID, Summary: "設定 MRP 計畫參數", After: in})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}
