package system

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/db"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
)

type auditLogDTO struct {
	ID         int64           `json:"id"`
	UserID     *int64          `json:"user_id"`
	Username   *string         `json:"username"`
	UserName   *string         `json:"user_name"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   *int64          `json:"entity_id"`
	Summary    string          `json:"summary"`
	Before     json.RawMessage `json:"before"`
	After      json.RawMessage `json:"after"`
	IP         string          `json:"ip"`
	UserAgent  string          `json:"user_agent"`
	RequestID  string          `json:"request_id"`
	CreatedAt  time.Time       `json:"created_at"`
}

// parseDate 解析 YYYY-MM-DD(台灣時間);endOfDay 時回傳隔天 00:00,作為不含的上限。
func parseDate(c *gin.Context, name string, endOfDay bool) (*time.Time, error) {
	s := c.Query(name)
	if s == "" {
		return nil, nil
	}
	t, err := time.ParseInLocation(time.DateOnly, s, taipei)
	if err != nil {
		return nil, apperr.Validation(map[string]string{name: "日期格式須為 YYYY-MM-DD"})
	}
	if endOfDay {
		t = t.AddDate(0, 0, 1)
	}
	return &t, nil
}

func (m *Module) listAuditLogs(c *gin.Context) {
	ctx := c.Request.Context()
	entityID, err := httpx.QueryInt64(c, "entity_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	userID, err := httpx.QueryInt64(c, "user_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	from, err := parseDate(c, "from", false)
	if err != nil {
		response.Error(c, err)
		return
	}
	to, err := parseDate(c, "to", true)
	if err != nil {
		response.Error(c, err)
		return
	}
	p := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	entityType := httpx.QueryString(c, "entity_type")
	action := httpx.QueryString(c, "action")

	rows, err := m.store.ListAuditLogs(ctx, db.ListAuditLogsParams{
		CompanyID: companyID, EntityType: entityType, EntityID: entityID, UserID: userID,
		Action: action, FromTime: from, ToTime: to, Lim: p.Limit(), Off: p.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountAuditLogs(ctx, db.CountAuditLogsParams{
		CompanyID: companyID, EntityType: entityType, EntityID: entityID, UserID: userID,
		Action: action, FromTime: from, ToTime: to,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]auditLogDTO, len(rows))
	for i, r := range rows {
		out[i] = auditLogDTO{
			ID: r.ID, UserID: r.UserID, Username: r.Username, UserName: r.UserName, Action: r.Action,
			EntityType: r.EntityType, EntityID: r.EntityID, Summary: r.Summary,
			Before: r.BeforeData, After: r.AfterData, IP: r.Ip, UserAgent: r.UserAgent,
			RequestID: r.RequestID, CreatedAt: r.CreatedAt,
		}
	}
	response.List(c, out, p.Meta(total))
}
