package gl

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
)

// 專案(D80):傳票明細的輔助核算維度,用於專案別損益。

type projectDTO struct {
	ID       int64  `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
}

func (m *Module) listProjects(c *gin.Context) {
	rows, err := m.store.ListProjects(c.Request.Context(), actor(c).CompanyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]projectDTO, len(rows))
	for i, r := range rows {
		out[i] = projectDTO{r.ID, r.Code, r.Name, r.IsActive}
	}
	response.OK(c, out)
}

type projectInput struct {
	Code     string `json:"code" binding:"required,max=20"`
	Name     string `json:"name" binding:"required,max=100"`
	IsActive *bool  `json:"is_active"`
	Version  int32  `json:"version"` // 不做樂觀鎖,接受但忽略
}

func (m *Module) createProject(c *gin.Context) {
	var in projectInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	var out db.CreateProjectRow
	err := m.store.InTx(c.Request.Context(), func(q *db.Queries) error {
		ctx := c.Request.Context()
		var err error
		out, err = q.CreateProject(ctx, db.CreateProjectParams{CompanyID: a.CompanyID, Code: code, Name: strings.TrimSpace(in.Name), CreatedBy: &a.UserID})
		if database.IsUniqueViolation(err, "projects_company_id_code_key") {
			return apperr.Conflict("GL-060", "專案代碼已存在")
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: "create", EntityType: "project", EntityID: &out.ID, Summary: "新增專案 " + code})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, projectDTO{out.ID, out.Code, out.Name, out.IsActive})
}

func (m *Module) updateProject(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in projectInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	var out db.UpdateProjectRow
	err = m.store.InTx(c.Request.Context(), func(q *db.Queries) error {
		ctx := c.Request.Context()
		var err error
		out, err = q.UpdateProject(ctx, db.UpdateProjectParams{ID: id, CompanyID: actor(c).CompanyID, Name: strings.TrimSpace(in.Name), IsActive: in.IsActive})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: "update", EntityType: "project", EntityID: &id, Summary: fmt.Sprintf("修改專案 %s", out.Code)})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, projectDTO{out.ID, out.Code, out.Name, out.IsActive})
}

// projectPnl GET /gl/reports/project-pnl?from=&to= 專案別損益(同部門別損益,維度換成專案)。
func (m *Module) projectPnl(c *gin.Context) {
	from, to, err := dateRange(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	rows, err := m.store.ProjectPnl(c.Request.Context(), db.ProjectPnlParams{CompanyID: actor(c).CompanyID, FromDate: from, ToDate: to})
	if err != nil {
		response.Error(c, err)
		return
	}
	entries := make([]pnlEntry, len(rows))
	for i, r := range rows {
		entries[i] = pnlEntry{ID: r.ProjectID, Code: deref(r.ProjectCode), Name: deref(r.ProjectName), AcctType: r.AcctType, Amount: r.Amount}
	}
	out, total := buildPnl(entries, "未指定專案")
	response.OK(c, gin.H{"from": from.Format(time.DateOnly), "to": to.Format(time.DateOnly), "rows": out, "total": total})
}
