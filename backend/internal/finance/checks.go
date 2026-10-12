package finance

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/auth"
	"erp/internal/db"
	"erp/internal/gl"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/trade"
)

// 票據管理(D83):方式為 check 的收付款單過帳時登錄一張票據(應收 / 應付票據);
// 到期兌現時記「借 銀行存款 貸 應收票據」(付款則「借 應付票據 貸 銀行存款」)。
// 退票:直接反過帳該收付款單(未兌現時允許),帳款即回到未沖狀態。

type checkDTO struct {
	ID           int64           `json:"id"`
	Side         string          `json:"side"`
	SettlementID int64           `json:"settlement_id"`
	SettlementNo string          `json:"settlement_no"`
	CheckNo      string          `json:"check_no"`
	PartnerCode  string          `json:"partner_code"`
	PartnerName  string          `json:"partner_name"`
	Amount       decimal.Decimal `json:"amount"`
	DueDate      string          `json:"due_date"`
	Status       string          `json:"status"`
	ClearedDate  *string         `json:"cleared_date"`
	Overdue      bool            `json:"overdue"` // 未兌現且已過到期日
}

// listChecks GET /finance/{collections,payments}/checks?status=&due_to=
func (m *Module) listChecks(cfg sideCfg) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := db.ListChecksParams{CompanyID: authctx.ActorFrom(c.Request.Context()).CompanyID, Side: cfg.side}
		if s := c.Query("status"); s != "" {
			if s != "pending" && s != "cleared" {
				response.Error(c, fieldErr("status", "狀態不正確"))
				return
			}
			p.Status = &s
		}
		due, err := trade.OptionalDate(c, "due_to")
		if err != nil {
			response.Error(c, err)
			return
		}
		p.DueTo = due
		rows, err := m.store.ListChecks(c.Request.Context(), p)
		if err != nil {
			response.Error(c, err)
			return
		}
		today := time.Now().Format(time.DateOnly)
		out := make([]checkDTO, len(rows))
		for i, r := range rows {
			d := r.DueDate.Format(time.DateOnly)
			out[i] = checkDTO{ID: r.ID, Side: r.Side, SettlementID: r.SettlementID, SettlementNo: r.SettlementNo,
				CheckNo: r.CheckNo, PartnerCode: r.PartnerCode, PartnerName: r.PartnerName,
				Amount: r.Amount, DueDate: d, Status: r.Status, ClearedDate: trade.DateString(r.ClearedDate),
				Overdue: r.Status == "pending" && d < today}
		}
		response.OK(c, out)
	}
}

type clearInput struct {
	ClearDate string `json:"clear_date"` // 預設今天
}

// checkAction POST /finance/{...}/checks/:id/:action  clear 兌現 / unclear 取消兌現
func (m *Module) checkAction(cfg sideCfg) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := httpx.ParamID(c, "id")
		if err != nil {
			response.Error(c, err)
			return
		}
		action := c.Param("action")
		if action != "clear" && action != "unclear" {
			response.Error(c, apperr.ErrNotFound)
			return
		}
		var in clearInput
		if err := httpx.BindJSON(c, &in); err != nil {
			response.Error(c, err)
			return
		}
		date := time.Now()
		if in.ClearDate != "" {
			if date, err = trade.ParseDate("clear_date", in.ClearDate); err != nil {
				response.Error(c, err)
				return
			}
		}
		ctx := c.Request.Context()
		a := authctx.ActorFrom(ctx)
		err = m.store.InTx(ctx, func(q *db.Queries) error {
			k, err := q.LockCheck(ctx, db.LockCheckParams{ID: id, CompanyID: a.CompanyID, Side: cfg.side})
			if database.IsNoRows(err) {
				return apperr.ErrNotFound
			}
			if err != nil {
				return err
			}
			opt := gl.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
			if action == "clear" {
				if k.Status != "pending" {
					return apperr.New(http.StatusConflict, "FIN-007", "這張票據已兌現")
				}
				if err := q.SetCheckCleared(ctx, db.SetCheckClearedParams{ID: id, Status: "cleared", ClearedDate: &date, ClearedBy: &a.UserID}); err != nil {
					return err
				}
				if err := gl.PostSource(ctx, q, opt, gl.Source{
					Type: "check_clear", ID: id, No: k.CheckNo, Date: date, Desc: fmt.Sprintf("%s票據兌現 %s", cfg.label, k.CheckNo),
				}, clearEntries(cfg, k)); err != nil {
					return err
				}
			} else {
				if k.Status != "cleared" {
					return apperr.New(http.StatusConflict, "FIN-007", "這張票據尚未兌現")
				}
				if err := q.SetCheckCleared(ctx, db.SetCheckClearedParams{ID: id, Status: "pending"}); err != nil {
					return err
				}
				if err := gl.ReverseSource(ctx, q, opt, "check_clear", id); err != nil {
					return err
				}
			}
			return audit.Record(ctx, q, audit.Entry{
				Action: action, EntityType: "check", EntityID: &id, Summary: fmt.Sprintf("%s票據 %s %s", cfg.label, k.CheckNo, map[string]string{"clear": "兌現", "unclear": "取消兌現"}[action]),
			})
		})
		if err != nil {
			response.Error(c, err)
			return
		}
		response.OK(c, gin.H{"id": id, "status": map[string]string{"clear": "cleared", "unclear": "pending"}[action]})
	}
}

// clearEntries 收款票據兌現:借 銀行存款 貸 應收票據;付款票據兌現:借 應付票據 貸 銀行存款。
func clearEntries(cfg sideCfg, k db.Check) []gl.Entry {
	if cfg.side == SideReceipt {
		return []gl.Entry{
			{Key: "settle.bank", Debit: k.Amount},
			{Key: "settle.notes_receivable", Credit: k.Amount, CustomerID: k.CustomerID},
		}
	}
	return []gl.Entry{
		{Key: "settle.notes_payable", Debit: k.Amount, SupplierID: k.SupplierID},
		{Key: "settle.bank", Credit: k.Amount},
	}
}

func (m *Module) registerChecks(g *gin.RouterGroup, path string, cfg sideCfg) {
	// 票據清單不套用客戶資料範圍,僅開放給有過帳(出納)權限者
	g.GET(path+"/checks", auth.Require(cfg.post), m.listChecks(cfg))
	g.POST(path+"/checks/:id/:action", auth.Require(cfg.post), m.checkAction(cfg))
}
