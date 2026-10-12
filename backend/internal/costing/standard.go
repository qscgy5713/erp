package costing

import (
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

// 標準成本與差異(D91):標準成本是管理用的目標單位成本(基本單位),不入帳;
// 差異報表比較「月結後的實際完工成本」與「完工數量 × 標準成本」,正數 = 實際高於標準(不利差異)。

type standardCostDTO struct {
	ItemID       int64            `json:"item_id"`
	ItemCode     string           `json:"item_code"`
	ItemName     string           `json:"item_name"`
	UnitName     string           `json:"unit_name"`
	StandardCost *decimal.Decimal `json:"standard_cost"`
	UpdatedAt    *time.Time       `json:"updated_at"`
	HasBom       bool             `json:"has_bom"`
}

type standardVersionDTO struct {
	EffectiveFrom string          `json:"effective_from"` // 2000-01-01 = 一直有效
	StandardCost  decimal.Decimal `json:"standard_cost"`
	UpdatedAt     time.Time       `json:"updated_at"`
	UpdatedByName string          `json:"updated_by_name"`
}

// listStandardVersions GET /costing/standard-costs/:item_id/versions
func (m *Module) listStandardVersions(c *gin.Context) {
	itemID, err := httpx.ParamID(c, "item_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	rows, err := m.store.ListStandardCostVersions(c.Request.Context(), db.ListStandardCostVersionsParams{ItemID: itemID, CompanyID: actor(c).CompanyID})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]standardVersionDTO, len(rows))
	for i, r := range rows {
		out[i] = standardVersionDTO{EffectiveFrom: r.EffectiveFrom.Format(time.DateOnly), StandardCost: r.StandardCost, UpdatedAt: r.UpdatedAt, UpdatedByName: r.UpdatedByName}
	}
	response.OK(c, out)
}

// listStandardCosts GET /costing/standard-costs?keyword=&only_bom=true
func (m *Module) listStandardCosts(c *gin.Context) {
	pg := page.Parse(c.Query("page"), c.Query("size"))
	keyword := httpx.QueryString(c, "keyword")
	onlyBom := c.Query("only_bom") == "true"
	ctx := c.Request.Context()
	companyID := actor(c).CompanyID
	rows, err := m.store.ListStandardCosts(ctx, db.ListStandardCostsParams{CompanyID: companyID, Keyword: keyword, OnlyBom: onlyBom, Lim: pg.Limit(), Off: pg.Offset()})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountStandardCosts(ctx, db.CountStandardCostsParams{CompanyID: companyID, Keyword: keyword, OnlyBom: onlyBom})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]standardCostDTO, len(rows))
	for i, r := range rows {
		out[i] = standardCostDTO{ItemID: r.ItemID, ItemCode: r.ItemCode, ItemName: r.ItemName, UnitName: r.UnitName, HasBom: r.HasBom}
		out[i].StandardCost, out[i].UpdatedAt = r.StandardCost, r.UpdatedAt
	}
	response.List(c, out, pg.Meta(total))
}

type standardCostInput struct {
	// null 或空白 = 清除標準成本:未指定生效日清除全部版本,指定生效日只清除該版本
	StandardCost *decimal.Decimal `json:"standard_cost"`
	// EffectiveFrom 生效日(YYYY-MM-DD);空白 = 一直有效(基準版本,2000-01-01)。差異報表依各筆完工日取當時有效的版本
	EffectiveFrom string `json:"effective_from"`
}

const baseEffective = "2000-01-01"

// setStandardCost PUT /costing/standard-costs/:item_id
func (m *Module) setStandardCost(c *gin.Context) {
	itemID, err := httpx.ParamID(c, "item_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in standardCostInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	if in.StandardCost != nil && (in.StandardCost.IsNegative() || in.StandardCost.Exponent() < -money.UnitPricePlaces) {
		response.Error(c, apperr.Validation(map[string]string{"standard_cost": "須 ≥ 0,最多 6 位小數"}))
		return
	}
	eff := baseEffective
	if in.EffectiveFrom != "" {
		eff = in.EffectiveFrom
	}
	effDate, perr := time.Parse(time.DateOnly, eff)
	if perr != nil {
		response.Error(c, apperr.Validation(map[string]string{"effective_from": "請輸入日期 YYYY-MM-DD"}))
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		ok, err := q.StandardCostItemOK(ctx, db.StandardCostItemOKParams{ID: itemID, CompanyID: a.CompanyID})
		if err != nil {
			return err
		}
		if !ok {
			return apperr.ErrNotFound
		}
		summary := "清除標準成本"
		switch {
		case in.StandardCost == nil && in.EffectiveFrom == "":
			if err := q.DeleteStandardCost(ctx, db.DeleteStandardCostParams{ItemID: itemID, CompanyID: a.CompanyID}); err != nil {
				return err
			}
		case in.StandardCost == nil:
			n, err := q.DeleteStandardCostVersion(ctx, db.DeleteStandardCostVersionParams{ItemID: itemID, CompanyID: a.CompanyID, EffectiveFrom: effDate})
			if err != nil {
				return err
			}
			if n == 0 {
				return apperr.ErrNotFound
			}
			summary = "刪除標準成本版本 " + eff
		default:
			if err := q.UpsertStandardCost(ctx, db.UpsertStandardCostParams{ItemID: itemID, CompanyID: a.CompanyID, EffectiveFrom: effDate,
				StandardCost: *in.StandardCost, ActorID: &a.UserID}); err != nil {
				return err
			}
			summary = "設定標準成本 " + in.StandardCost.String()
			if in.EffectiveFrom != "" {
				summary += "(" + eff + " 起)"
			}
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Update, EntityType: "standard_cost", EntityID: &itemID, Summary: summary, After: in})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

type varianceRow struct {
	ItemID       int64            `json:"item_id"`
	ItemCode     string           `json:"item_code"`
	ItemName     string           `json:"item_name"`
	UnitName     string           `json:"unit_name"`
	ProducedQty  decimal.Decimal  `json:"produced_qty"`
	ActualAmount decimal.Decimal  `json:"actual_amount"`
	ActualUnit   *decimal.Decimal `json:"actual_unit"`   // 實際單位成本;尚未月結為 null
	StandardUnit *decimal.Decimal `json:"standard_unit"` // 標準金額 ÷ 完工量(各筆依完工日取當時版本);沒設標準成本為 null
	StandardAmt  *decimal.Decimal `json:"standard_amount"`
	Variance     *decimal.Decimal `json:"variance"`     // 實際 − 標準;正數 = 實際高於標準
	VariancePct  *decimal.Decimal `json:"variance_pct"` // 差異 ÷ 標準 × 100
	Status       string           `json:"status"`       // ok / uncosted 尚未月結 / no_standard 未設標準成本
	// Breakdown 差異拆解(用量 / 價格 / 加工費);尚未月結或沒設標準成本為 null
	Breakdown *breakdown `json:"breakdown"`
}

// variance GET /costing/variance?period=YYYY-MM
func (m *Module) variance(c *gin.Context) {
	period := c.Query("period")
	if !periodPattern.MatchString(period) {
		response.Error(c, apperr.Validation(map[string]string{"period": "請輸入期間 YYYY-MM"}))
		return
	}
	start, end := monthRange(period)
	rows, err := m.store.StandardCostVariance(c.Request.Context(), db.StandardCostVarianceParams{CompanyID: actor(c).CompanyID, StartDate: start, EndDate: end})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]varianceRow, len(rows))
	var inputs []itemInput
	hundred := decimal.NewFromInt(100)
	for i, r := range rows {
		v := varianceRow{ItemID: r.ItemID, ItemCode: r.ItemCode, ItemName: r.ItemName, UnitName: r.UnitName,
			ProducedQty: r.ProducedQty, ActualAmount: money.Amount(r.ActualAmount), Status: "ok"}
		costed := r.UncostedQty.IsZero()
		if costed {
			u := r.ActualAmount.Div(r.ProducedQty).Round(6)
			v.ActualUnit = &u
		} else {
			v.Status = "uncosted"
		}
		// 完工量全部找得到當時有效的標準成本才比較;只有部分有版本視為未設標準
		if r.NoStdQty.IsZero() {
			sa := money.Amount(r.StandardAmount)
			su := r.StandardAmount.Div(r.ProducedQty).Round(6)
			v.StandardUnit, v.StandardAmt = &su, &sa
			if costed {
				d := money.Amount(r.ActualAmount).Sub(sa)
				v.Variance = &d
				inputs = append(inputs, itemInput{ItemID: r.ItemID, ProducedQty: r.ProducedQty, ActualAmt: money.Amount(r.ActualAmount), StandardAmt: sa, Variance: d})
				if !sa.IsZero() {
					p := d.Div(sa).Mul(hundred).Round(2)
					v.VariancePct = &p
				}
			}
		} else if costed {
			v.Status = "no_standard"
		}
		out[i] = v
	}
	if len(inputs) > 0 {
		bds, err := breakdowns(c.Request.Context(), m.store.Queries, actor(c).CompanyID, start, end, inputs)
		if err != nil {
			response.Error(c, err)
			return
		}
		for i := range out {
			out[i].Breakdown = bds[out[i].ItemID]
		}
	}
	response.OK(c, gin.H{"period": period, "rows": out})
}
