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
	g.GET("/mrp", orderRead, m.mrp)
	// MRP 計畫參數(D98):前置時間、最小訂購量、批量倍數
	g.GET("/mrp-params", bomRead, m.listMrpParams)
	g.PUT("/mrp-params/:item_id", auth.Require(permission.BomWrite), m.setMrpParams)
	g.GET("/work-orders", orderRead, m.listOrders)
	g.GET("/work-orders/explode", auth.Require(permission.WorkOrderWrite, permission.WorkOrderRead), m.explode)
	g.GET("/work-orders/:id", orderRead, m.getOrder)
	g.POST("/work-orders", auth.Require(permission.WorkOrderWrite), m.createOrder)
	g.PUT("/work-orders/:id", auth.Require(permission.WorkOrderWrite), m.updateOrder)
	// 各動作的權限在 handler 內依動作判斷
	g.POST("/work-orders/:id/actions/:action", orderRead, m.orderAction)
	// 分批完工(D94):回報 / 作廢 / 結案都需要工單過帳權限
	g.GET("/work-orders/:id/completion-preview", orderRead, m.completionPreview)
	g.POST("/work-orders/:id/completions", auth.Require(permission.WorkOrderPost), m.createCompletion)
	g.POST("/work-orders/:id/completions/:cid/void", auth.Require(permission.WorkOrderPost), m.voidCompletion)
	g.POST("/work-orders/:id/close", auth.Require(permission.WorkOrderPost), m.closeOrder)

	// 委外加工(D100)
	scRead := auth.Require(permission.SubcontractRead, permission.SubcontractWrite, permission.SubcontractPost)
	scWrite := auth.Require(permission.SubcontractWrite)
	scPost := auth.Require(permission.SubcontractPost)
	g.GET("/subcontracts", scRead, m.listSubcontracts)
	g.GET("/subcontracts/:id", scRead, m.getSubcontract)
	g.GET("/subcontracts/:id/receive-preview", scRead, m.subcontractReceivePreview)
	g.POST("/subcontracts", scWrite, m.createSubcontract)
	g.PUT("/subcontracts/:id", scWrite, m.updateSubcontract)
	g.POST("/subcontracts/:id/close", scWrite, m.closeSubcontract)
	g.POST("/subcontracts/:id/void", scWrite, m.voidSubcontract)
	g.POST("/subcontracts/:id/issue", scPost, m.postScMaterial(kindIssue))
	g.POST("/subcontracts/:id/return", scPost, m.postScMaterial(kindReturn))
	g.POST("/subcontracts/:id/receive", scPost, m.receiveSubcontract)
	g.POST("/subcontracts/:id/txns/:tid/void", scPost, m.voidScTxn)
}
