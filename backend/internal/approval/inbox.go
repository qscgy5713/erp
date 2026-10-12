package approval

import (
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/shared/response"
)

type inboxDTO struct {
	DocType     string          `json:"doc_type"`
	DocLabel    string          `json:"doc_label"`
	DocID       int64           `json:"doc_id"`
	DocNo       string          `json:"doc_no"`
	DocDate     string          `json:"doc_date"`
	PartnerName string          `json:"partner_name"`
	Currency    string          `json:"currency"`
	Amount      decimal.Decimal `json:"amount"`
	SubmittedAt *time.Time      `json:"submitted_at"`
	Step        int             `json:"step"`        // 目前待核准的層級(單層核准為 1)
	TotalSteps  int             `json:"total_steps"` // 總層數(單層核准為 1)
	StepRole    string          `json:"step_role"`
}

// inbox GET /approval/inbox 待我核准:待核准的單據中,「現在輪到我」核准的那些。
// 登入即可呼叫,內容依權限過濾:只含我有核准權限的單據類型;業務類單據套用資料範圍;
// 多層簽核時,目前層級須由我的角色負責,且我沒有核准過同一張單據的前一層。
func (m *Module) inbox(c *gin.Context) {
	ctx := c.Request.Context()
	a := actor(c)
	var types []string
	for _, t := range DocTypeOrder {
		if a.Can(docTypes[t].Approve) {
			types = append(types, t)
		}
	}
	out := []inboxDTO{}
	if len(types) == 0 {
		response.OK(c, out)
		return
	}
	deptID, userID := a.ScopeFilter()
	docs, err := m.store.PendingApprovalDocs(ctx, db.PendingApprovalDocsParams{
		CompanyID: a.CompanyID, Types: types, ScopeUserID: userID, ScopeDeptID: deptID,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	if len(docs) == 0 {
		response.OK(c, out)
		return
	}
	dt, ids := make([]string, len(docs)), make([]int64, len(docs))
	for i, d := range docs {
		dt[i], ids[i] = d.DocType, d.DocID
	}
	steps, err := m.store.DocumentApprovalsForDocs(ctx, db.DocumentApprovalsForDocsParams{CompanyID: a.CompanyID, Types: dt, Ids: ids})
	if err != nil {
		response.Error(c, err)
		return
	}
	byDoc := map[string][]db.DocumentApprovalsForDocsRow{}
	for _, s := range steps {
		k := s.DocType + "/" + strconv.FormatInt(s.DocID, 10)
		byDoc[k] = append(byDoc[k], s)
	}
	roles, err := m.store.UserRoleIDs(ctx, a.UserID)
	if err != nil {
		response.Error(c, err)
		return
	}
	for _, d := range docs {
		row := inboxDTO{
			DocType: d.DocType, DocLabel: docTypes[d.DocType].Label, DocID: d.DocID, DocNo: d.DocNo,
			DocDate: d.DocDate.Format(time.DateOnly), PartnerName: d.PartnerName, Currency: d.Currency,
			Amount: d.Amount, SubmittedAt: d.SubmittedAt, Step: 1, TotalSteps: 1,
		}
		if rs := byDoc[d.DocType+"/"+strconv.FormatInt(d.DocID, 10)]; len(rs) > 0 {
			cur := -1
			mine := true
			for i, r := range rs {
				if r.ApproverID == nil {
					cur = i
					break
				}
				if *r.ApproverID == a.UserID {
					mine = false // 同一人不可核准多層
				}
			}
			if cur < 0 || !mine {
				continue
			}
			s := rs[cur]
			if s.RoleID != nil && !a.IsSuperadmin && !slices.Contains(roles, *s.RoleID) {
				continue
			}
			if s.RoleID == nil && s.StepNo > 1 && !a.IsSuperadmin {
				continue // 角色已刪除,只有超級管理員能核准
			}
			row.Step, row.TotalSteps, row.StepRole = int(s.StepNo), len(rs), s.RoleName
		}
		out = append(out, row)
	}
	response.OK(c, out)
}
