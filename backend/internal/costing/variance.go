package costing

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/shared/money"
)

// 成本差異拆解(D97):把「實際成本 − 標準成本」拆成三塊,三者相加恆等於總差異——
//
//	材料用量差異 = Σ (實際用量 − 標準用量) × 標準單價
//	材料價格差異 = Σ (實際金額 − 實際用量 × 標準單價)
//	加工費差異   = 總差異 − 用量差異 − 價格差異(= 實際加工費 − 標準加工費,含四捨五入尾差)
//
// 標準用量 = 期間結束日適用的 BOM 用量 × (1 + 損耗率) × 完工數量 ÷ 產出量;替代料不在 BOM 內,標準用量為 0,
// 所以會表現為用量差異。材料標準單價取材料自己在期間結束日有效的標準成本,沒設就用實際單價(價格差異為 0)。
// 標準加工費 = 成品標準成本 × 完工數量 − 標準材料成本。

type materialVariance struct {
	MaterialID    int64            `json:"material_id"`
	MaterialCode  string           `json:"material_code"`
	MaterialName  string           `json:"material_name"`
	UnitName      string           `json:"unit_name"`
	StandardQty   decimal.Decimal  `json:"standard_qty"`
	ActualQty     decimal.Decimal  `json:"actual_qty"`
	StandardPrice *decimal.Decimal `json:"standard_price"` // 材料沒設標準成本為 null(以實際單價計)
	ActualPrice   *decimal.Decimal `json:"actual_price"`   // 沒有實際領用為 null
	QtyVariance   decimal.Decimal  `json:"qty_variance"`
	PriceVariance decimal.Decimal  `json:"price_variance"`
}

type breakdown struct {
	Materials          []materialVariance `json:"materials"`
	QtyVariance        decimal.Decimal    `json:"qty_variance"`
	PriceVariance      decimal.Decimal    `json:"price_variance"`
	ProcessingVariance decimal.Decimal    `json:"processing_variance"`
	StandardProcessing decimal.Decimal    `json:"standard_processing"` // 標準加工費總額
	ActualProcessing   decimal.Decimal    `json:"actual_processing"`   // 實際加工費總額(實際成本 − 實際材料成本)
}

// itemInput 單一成品算差異拆解需要的資料。
type itemInput struct {
	ItemID      int64
	ProducedQty decimal.Decimal
	ActualAmt   decimal.Decimal // 實際完工成本(已四捨五入到元角分)
	StandardAmt decimal.Decimal
	Variance    decimal.Decimal
}

// breakdowns 一次算出多個成品的差異拆解。
func breakdowns(ctx context.Context, q *db.Queries, companyID int64, start, end time.Time, in []itemInput) (map[int64]*breakdown, error) {
	ids := make([]int64, len(in))
	for i, x := range in {
		ids[i] = x.ItemID
	}
	boms, err := q.VarianceBomLines(ctx, db.VarianceBomLinesParams{CompanyID: companyID, ItemIds: ids, EndDate: end})
	if err != nil {
		return nil, err
	}
	acts, err := q.VarianceActualMaterials(ctx, db.VarianceActualMaterialsParams{CompanyID: companyID, ItemIds: ids, StartDate: start, EndDate: end})
	if err != nil {
		return nil, err
	}
	matIDs := map[int64]bool{}
	for _, b := range boms {
		matIDs[b.MaterialID] = true
	}
	for _, a := range acts {
		matIDs[a.MaterialID] = true
	}
	mids := make([]int64, 0, len(matIDs))
	for id := range matIDs {
		mids = append(mids, id)
	}
	scs, err := q.StandardCostsOf(ctx, db.StandardCostsOfParams{CompanyID: companyID, ItemIds: mids, AsOf: end})
	if err != nil {
		return nil, err
	}
	stdPrice := map[int64]decimal.Decimal{}
	for _, s := range scs {
		stdPrice[s.ItemID] = s.StandardCost
	}

	hundred := decimal.NewFromInt(100)
	out := map[int64]*breakdown{}
	for _, x := range in {
		type acc struct {
			m       materialVariance
			actAmt  decimal.Decimal
			hasUsed bool
		}
		lines := map[int64]*acc{}
		get := func(id int64) *acc {
			if lines[id] == nil {
				lines[id] = &acc{}
			}
			return lines[id]
		}
		for _, b := range boms {
			if b.OutputItemID != x.ItemID {
				continue
			}
			l := get(b.MaterialID)
			l.m.MaterialID, l.m.MaterialCode, l.m.MaterialName, l.m.UnitName = b.MaterialID, b.MaterialCode, b.MaterialName, b.UnitName
			factor := decimal.NewFromInt(1).Add(b.ScrapPct.Div(hundred))
			l.m.StandardQty = l.m.StandardQty.Add(b.Qty.Mul(factor).Mul(x.ProducedQty).Div(b.YieldQty))
		}
		for _, a := range acts {
			if a.OutputItemID != x.ItemID {
				continue
			}
			l := get(a.MaterialID)
			l.m.MaterialID, l.m.MaterialCode, l.m.MaterialName, l.m.UnitName = a.MaterialID, a.MaterialCode, a.MaterialName, a.UnitName
			l.m.ActualQty, l.actAmt, l.hasUsed = a.Qty, a.Amount, a.Qty.IsPositive()
		}
		bd := &breakdown{Materials: []materialVariance{}}
		var qtyVar, priceVar, stdMat, actMat decimal.Decimal
		order := make([]int64, 0, len(lines))
		for id := range lines {
			order = append(order, id)
		}
		slices.SortFunc(order, func(a, b int64) int { return strings.Compare(lines[a].m.MaterialCode, lines[b].m.MaterialCode) })
		for _, id := range order {
			l := lines[id]
			m := l.m
			var sp decimal.Decimal
			if p, ok := stdPrice[id]; ok {
				sp = p
				m.StandardPrice = &p
			} else if l.hasUsed {
				sp = l.actAmt.Div(m.ActualQty) // 沒設標準:以實際單價計,價格差異為 0
			}
			if l.hasUsed {
				ap := l.actAmt.Div(m.ActualQty).Round(6)
				m.ActualPrice = &ap
			}
			lq := m.ActualQty.Sub(m.StandardQty).Mul(sp)
			lp := l.actAmt.Sub(m.ActualQty.Mul(sp))
			m.QtyVariance, m.PriceVariance = money.Amount(lq), money.Amount(lp)
			qtyVar, priceVar = qtyVar.Add(lq), priceVar.Add(lp)
			stdMat, actMat = stdMat.Add(m.StandardQty.Mul(sp)), actMat.Add(l.actAmt)
			bd.Materials = append(bd.Materials, m)
		}
		bd.QtyVariance, bd.PriceVariance = money.Amount(qtyVar), money.Amount(priceVar)
		bd.ProcessingVariance = x.Variance.Sub(bd.QtyVariance).Sub(bd.PriceVariance)
		bd.StandardProcessing = money.Amount(x.StandardAmt.Sub(stdMat))
		bd.ActualProcessing = money.Amount(x.ActualAmt.Sub(actMat))
		out[x.ItemID] = bd
	}
	return out, nil
}
