// Package gl 為會計總帳:科目、拋轉規則、傳票、會計期間與報表。
//
// 業務單據(進貨、出貨、收付款)過帳時,在同一個資料庫交易內呼叫 PostSource 產生已過帳傳票,
// 反過帳時呼叫 ReverseSource 產生沖銷傳票(D46)。關帳後的期間,過帳與反過帳一律被擋(CheckPeriodOpen)。
package gl

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"erp/internal/auth"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/system/permission"
)

type Module struct {
	store *database.Store
}

func New(store *database.Store) *Module { return &Module{store: store} }

func actor(c *gin.Context) *authctx.Actor { return authctx.ActorFrom(c.Request.Context()) }

func fieldErr(field, msg string) *apperr.Error {
	return apperr.Validation(map[string]string{field: msg})
}

var (
	errPeriodClosed   = "GL-001"
	errNoMapping      = apperr.New(http.StatusUnprocessableEntity, "GL-003", "尚未設定拋轉科目")
	errUnbalanced     = apperr.New(http.StatusUnprocessableEntity, "GL-004", "借貸不平衡")
	errAutoVoucher    = apperr.New(http.StatusConflict, "GL-005", "自動拋轉的傳票請由來源單據反過帳沖銷")
	errVoucherEdit    = apperr.New(http.StatusConflict, "GL-006", "只有草稿可以修改")
	errAlreadyReverse = apperr.New(http.StatusConflict, "GL-007", "此傳票已沖銷")
)

// Register 掛上 /gl 路由;r 須已套用 auth.Authenticate。
func (m *Module) Register(r *gin.RouterGroup) {
	g := r.Group("/gl")
	acctRead := auth.Require(permission.AccountRead, permission.AccountWrite, permission.VoucherWrite)
	// 科目下拉:開傳票的人需要,不一定有維護科目權限
	g.GET("/account-options", auth.Require(permission.AccountRead, permission.AccountWrite, permission.VoucherRead,
		permission.VoucherWrite, permission.VoucherPost, permission.ReportRead), m.accountOptions)
	g.GET("/accounts", acctRead, m.listAccounts)
	g.POST("/accounts", auth.Require(permission.AccountWrite), m.createAccount)
	g.PUT("/accounts/:id", auth.Require(permission.AccountWrite), m.updateAccount)
	g.GET("/mappings", auth.Require(permission.AccountRead, permission.AccountWrite), m.listMappings)
	g.PUT("/mappings/:key", auth.Require(permission.AccountWrite), m.setMapping)

	voucherRead := auth.Require(permission.VoucherRead, permission.VoucherWrite, permission.VoucherPost)
	g.GET("/vouchers", voucherRead, m.listVouchers)
	g.GET("/vouchers/:id", voucherRead, m.getVoucher)
	g.POST("/vouchers", auth.Require(permission.VoucherWrite), m.createVoucher)
	g.PUT("/vouchers/:id", auth.Require(permission.VoucherWrite), m.updateVoucher)
	g.POST("/vouchers/:id/actions/:action", voucherRead, m.voucherAction)

	g.GET("/periods", auth.Require(permission.PeriodRead, permission.PeriodClose), m.listPeriods)
	g.POST("/periods/:period/:action", auth.Require(permission.PeriodClose), m.changePeriod)

	report := auth.Require(permission.ReportRead)
	g.GET("/reports/trial-balance", report, m.trialBalance)
	g.GET("/reports/ledger", report, m.generalLedger)
	g.GET("/reports/journal", report, m.journal)
}
