// Package costing 為月結成本(D51):月加權平均成本、銷貨成本與存貨損益傳票、成本回寫流水帳,
// 以及子帳與總帳的自動對帳檢查。月結後該月庫存異動鎖定(inventory.apply 檢查),取消月結可重算。
package costing

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/auth"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/money"
	"erp/internal/system/permission"
)

type Module struct {
	store *database.Store
}

func New(store *database.Store) *Module { return &Module{store: store} }

func actor(c *gin.Context) *authctx.Actor { return authctx.ActorFrom(c.Request.Context()) }

var (
	errAlreadyClosed = apperr.New(http.StatusConflict, "CST-001", "此月份已月結成本")
	errNotClosed     = apperr.New(http.StatusConflict, "CST-004", "此月份尚未月結成本")
)

// Register 掛上 /costing 路由;r 須已套用 auth.Authenticate。
func (m *Module) Register(r *gin.RouterGroup) {
	g := r.Group("/costing")
	read := auth.Require(permission.CostRead, permission.CostClose)
	g.GET("/closings", read, m.listClosings)
	g.GET("/closings/:period/items", read, m.listItems)
	g.POST("/closings/:period/run", auth.Require(permission.CostClose), m.run)
	g.POST("/closings/:period/cancel", auth.Require(permission.CostClose), m.cancel)
	g.GET("/reconcile", read, m.reconcile)
}

// ---- 計算(純函式,不碰資料庫) ----

// opening 料品的期初(上一次月結的期末)。
type opening struct {
	qty, value, avg decimal.Decimal
}

// activity 料品當月異動彙總(基本單位)。
type activity struct {
	purchaseQty, purchaseValue decimal.Decimal // 進貨 − 進貨退出(數量與依單位成本的金額)
	salesQty                   decimal.Decimal // 出貨(負)+ 銷貨退回(正)
	adjustQty                  decimal.Decimal // 盤點 / 調整 / 其他來源
	consumeQty                 decimal.Decimal // 工單領料(負):扣庫存,依平均成本計價,但不是銷貨成本
	produceQty                 decimal.Decimal // 工單完工入庫(正):金額為材料成本 + 加工費,月結時併入 purchaseQty / purchaseValue
}

func (a activity) idle() bool {
	return a.purchaseQty.IsZero() && a.purchaseValue.IsZero() && a.salesQty.IsZero() && a.adjustQty.IsZero() &&
		a.consumeQty.IsZero() && a.produceQty.IsZero()
}

// result 單一料品的月結結果。
type result struct {
	avg                  decimal.Decimal
	cogs, adjust         decimal.Decimal // 銷貨成本、存貨損失(正為損失、負為盈餘),皆已捨入到元
	consumeV             decimal.Decimal // 工單領料的金額(正數)
	closingQty, closingV decimal.Decimal
}

// compute 月加權平均:
//
//	平均成本 = (期初金額 + 本月進貨金額) ÷ (期初數量 + 本月進貨數量)
//	出貨、銷貨退回、盤點與調整的異動都以平均成本計價;調撥在公司層級數量互抵,不影響成本。
//	期末金額 = 期末數量 × 平均成本。
//
// 分母不為正(例如期初為負庫存)時沿用上期平均成本,再不行就用本月進貨單價。
// 整月沒有異動時整筆沿用期初,避免因捨入讓金額逐月漂移。
func compute(o opening, a activity) result {
	if a.idle() {
		return result{avg: o.avg, closingQty: o.qty, closingV: o.value}
	}
	denom := o.qty.Add(a.purchaseQty)
	avg := o.avg
	switch {
	case denom.IsPositive():
		avg = o.value.Add(a.purchaseValue).Div(denom).Round(money.UnitPricePlaces)
	case avg.IsZero() && !a.purchaseQty.IsZero():
		avg = a.purchaseValue.Div(a.purchaseQty).Round(money.UnitPricePlaces)
	}
	closingQty := o.qty.Add(a.purchaseQty).Add(a.salesQty).Add(a.adjustQty).Add(a.consumeQty)
	return result{
		consumeV:   a.consumeQty.Neg().Mul(avg).Round(4),
		avg:        avg,
		cogs:       money.Amount(a.salesQty.Neg().Mul(avg)),
		adjust:     money.Amount(a.adjustQty.Neg().Mul(avg)),
		closingQty: closingQty,
		closingV:   closingQty.Mul(avg).Round(4),
	}
}
