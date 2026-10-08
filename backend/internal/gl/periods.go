package gl

import (
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
)

var periodPattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

type periodDTO struct {
	Period       string     `json:"period"`
	Status       string     `json:"status"`
	ClosedAt     *time.Time `json:"closed_at"`
	ClosedByName *string    `json:"closed_by_name"`
}

// listPeriods 近 12 個月加上曾經關帳過的期間;沒有紀錄的期間視為開放。
func (m *Module) listPeriods(c *gin.Context) {
	rows, err := m.store.ListPeriods(c.Request.Context(), actor(c).CompanyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	stored := map[string]db.ListPeriodsRow{}
	for _, r := range rows {
		stored[r.Period] = r
	}
	now := time.Now().In(time.FixedZone("TST", 8*3600))
	cur := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	seen := map[string]bool{}
	out := []periodDTO{}
	for i := 0; i < 12; i++ {
		p := cur.AddDate(0, -i, 0).Format("2006-01")
		seen[p] = true
		d := periodDTO{Period: p, Status: "open"}
		if r, ok := stored[p]; ok {
			d.Status, d.ClosedAt, d.ClosedByName = r.Status, r.ClosedAt, r.ClosedByName
		}
		out = append(out, d)
	}
	for _, r := range rows {
		if !seen[r.Period] {
			out = append(out, periodDTO{Period: r.Period, Status: r.Status, ClosedAt: r.ClosedAt, ClosedByName: r.ClosedByName})
		}
	}
	response.OK(c, out)
}

// changePeriod POST /gl/periods/:period/close|reopen
// 關帳要求該期沒有草稿傳票,且不可關未來的月份;關帳 / 重開以排他鎖序列化,
// 與各單據過帳時的共享鎖(CheckPeriodOpen)互斥。
func (m *Module) changePeriod(c *gin.Context) {
	period, action := c.Param("period"), c.Param("action")
	if !periodPattern.MatchString(period) || (action != "close" && action != "reopen") {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		if err := q.LockCompanyForPeriodChange(ctx, a.CompanyID); err != nil {
			return err
		}
		cur, err := q.GetPeriod(ctx, db.GetPeriodParams{CompanyID: a.CompanyID, Period: period})
		status := "open"
		if err == nil {
			status = cur.Status
		} else if !database.IsNoRows(err) {
			return err
		}
		switch {
		case action == "close" && status == "closed":
			return apperr.Conflict("GL-020", "此期間已關帳")
		case action == "reopen" && status != "closed":
			return apperr.Conflict("GL-021", "此期間尚未關帳")
		}
		next := "open"
		if action == "close" {
			next = "closed"
			now := time.Now().In(time.FixedZone("TST", 8*3600))
			if period > now.Format("2006-01") {
				return apperr.New(http.StatusUnprocessableEntity, "GL-022", "不可關閉未來的期間")
			}
			n, err := q.CountDraftVouchersInPeriod(ctx, db.CountDraftVouchersInPeriodParams{CompanyID: a.CompanyID, Period: period})
			if err != nil {
				return err
			}
			if n > 0 {
				return apperr.New(http.StatusConflict, "GL-023", "此期間還有草稿傳票,請先過帳或作廢後再關帳")
			}
		}
		if err := q.SetPeriodStatus(ctx, db.SetPeriodStatusParams{CompanyID: a.CompanyID, Period: period, Status: next, ActorID: &a.UserID}); err != nil {
			return err
		}
		label := map[string]string{"close": "關帳", "reopen": "重開"}[action]
		return audit.Record(ctx, q, audit.Entry{
			Action: action, EntityType: "accounting_period", Summary: label + "會計期間 " + period,
			Before: map[string]string{"status": status}, After: map[string]string{"status": next},
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, gin.H{"period": period, "status": map[string]string{"close": "closed", "reopen": "open"}[action]})
}
