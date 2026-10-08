// Package production 為 BOM(物料清單)與工單(D64)。
// 工單完工(過帳)時在同一交易內依領料明細扣材料庫存、成品入庫;成品成本在月結時依「材料月加權平均成本 + 加工費」計算。
package production

import (
	"github.com/gin-gonic/gin"

	"erp/internal/auth"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/system/permission"
)

type Module struct{ store *database.Store }

func New(store *database.Store) *Module { return &Module{store: store} }

func actor(c *gin.Context) *authctx.Actor { return authctx.ActorFrom(c.Request.Context()) }

func fieldErr(field, msg string) *apperr.Error {
	return apperr.Validation(map[string]string{field: msg})
}

// Register 掛上 /production;r 須已套用 auth.Authenticate。
func (m *Module) Register(r *gin.RouterGroup) {
	g := r.Group("/production")
	bomRead := auth.Require(permission.BomRead, permission.BomWrite, permission.WorkOrderRead, permission.WorkOrderWrite)
	g.GET("/boms", bomRead, m.listBoms)
	g.GET("/boms/:id", bomRead, m.getBom)
	g.POST("/boms", auth.Require(permission.BomWrite), m.createBom)
	g.PUT("/boms/:id", auth.Require(permission.BomWrite), m.updateBom)
	g.DELETE("/boms/:id", auth.Require(permission.BomWrite), m.deleteBom)

	orderRead := auth.Require(permission.WorkOrderRead, permission.WorkOrderWrite, permission.WorkOrderApprove, permission.WorkOrderPost)
	g.GET("/work-orders", orderRead, m.listOrders)
	g.GET("/work-orders/explode", auth.Require(permission.WorkOrderWrite, permission.WorkOrderRead), m.explode)
	g.GET("/work-orders/:id", orderRead, m.getOrder)
	g.POST("/work-orders", auth.Require(permission.WorkOrderWrite), m.createOrder)
	g.PUT("/work-orders/:id", auth.Require(permission.WorkOrderWrite), m.updateOrder)
	// 各動作的權限在 handler 內依動作判斷
	g.POST("/work-orders/:id/actions/:action", orderRead, m.orderAction)
}
