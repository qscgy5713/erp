package approval

import (
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/auth"
	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/system/permission"
)

type Module struct{ store *database.Store }

func New(store *database.Store) *Module { return &Module{store: store} }

// Register 掛上 /approval;r 須已套用 auth.Authenticate。
func (m *Module) Register(r *gin.RouterGroup) {
	g := r.Group("/approval")
	g.GET("/doc-types", auth.Require(permission.ApprovalRead), m.docTypes)
	g.GET("/rules", auth.Require(permission.ApprovalRead), m.listRules)
	g.POST("/rules", auth.Require(permission.ApprovalWrite), m.createRule)
	g.PUT("/rules/:id", auth.Require(permission.ApprovalWrite), m.updateRule)
	g.DELETE("/rules/:id", auth.Require(permission.ApprovalWrite), m.deleteRule)
	g.GET("/delegations", m.listDelegations)   // 登入即可:自己管理自己的代理人
	g.POST("/delegations", m.createDelegation) // 替他人設定需 system.approval.write
	g.DELETE("/delegations/:id", m.deleteDelegation)
	g.GET("/inbox", m.inbox)       // 登入即可,內容依核准權限過濾
	g.GET("/progress", m.progress) // 權限依單據類型在內部檢查
}

func actor(c *gin.Context) *authctx.Actor { return authctx.ActorFrom(c.Request.Context()) }

func fieldErr(field, msg string) *apperr.Error {
	return apperr.Validation(map[string]string{field: msg})
}

func (m *Module) docTypes(c *gin.Context) {
	type item struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	}
	out := make([]item, 0, len(DocTypeOrder))
	for _, k := range DocTypeOrder {
		out = append(out, item{k, docTypes[k].Label})
	}
	response.OK(c, out)
}

type stepRoleDTO struct {
	RoleID   int64  `json:"role_id"`
	RoleName string `json:"role_name"`
}

// stepDTO 規則的一個層級(第 2 層起):一或多個角色與簽法(any 或簽 / all 會簽)。
type stepDTO struct {
	Step  int           `json:"step"`
	Mode  string        `json:"mode"`
	Roles []stepRoleDTO `json:"roles"`
}

type ruleDTO struct {
	ID        int64           `json:"id"`
	DocType   string          `json:"doc_type"`
	DocLabel  string          `json:"doc_label"`
	MinAmount decimal.Decimal `json:"min_amount"`
	Steps     []stepDTO       `json:"steps"` // 第 2 層以後的角色(第 1 層固定為核准權限)
	Version   int32           `json:"version"`
}

func loadRules(c *gin.Context, q *db.Queries, companyID int64) ([]ruleDTO, error) {
	ctx := c.Request.Context()
	rules, err := q.ListApprovalRules(ctx, companyID)
	if err != nil {
		return nil, err
	}
	steps, err := q.ListApprovalRuleSteps(ctx, companyID)
	if err != nil {
		return nil, err
	}
	out := make([]ruleDTO, len(rules))
	idx := map[int64]int{}
	for i, r := range rules {
		out[i] = ruleDTO{ID: r.ID, DocType: r.DocType, DocLabel: Label(r.DocType), MinAmount: r.MinAmount, Steps: []stepDTO{}, Version: r.Version}
		idx[r.ID] = i
	}
	for _, s := range steps {
		rule := &out[idx[s.RuleID]]
		if n := len(rule.Steps); n == 0 || rule.Steps[n-1].Step != int(s.StepNo) {
			rule.Steps = append(rule.Steps, stepDTO{Step: int(s.StepNo), Mode: s.Mode, Roles: []stepRoleDTO{}})
		}
		last := &rule.Steps[len(rule.Steps)-1]
		last.Roles = append(last.Roles, stepRoleDTO{RoleID: s.RoleID, RoleName: s.RoleName})
	}
	return out, nil
}

func (m *Module) listRules(c *gin.Context) {
	rules, err := loadRules(c, m.store.Queries, actor(c).CompanyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, rules)
}

// stepInput 第 2 層起的一個層級:一或多個角色;mode 為 any(或簽,預設)或 all(會簽,至少 2 個角色)。
type stepInput struct {
	RoleIDs []int64 `json:"role_ids"`
	Mode    string  `json:"mode"`
}

type ruleInput struct {
	DocType   string          `json:"doc_type" binding:"required"`
	MinAmount decimal.Decimal `json:"min_amount"`
	// Steps 各層設定;未提供時以 RoleIDs 代表「每層一個角色」(舊格式)
	Steps   []stepInput `json:"steps"`
	RoleIDs []int64     `json:"role_ids"`
	Version int32       `json:"version"`
}

// layers 把輸入正規化為各層設定。
func (in *ruleInput) layers() []stepInput {
	if len(in.Steps) > 0 {
		return in.Steps
	}
	out := make([]stepInput, len(in.RoleIDs))
	for i, id := range in.RoleIDs {
		out[i] = stepInput{RoleIDs: []int64{id}}
	}
	return out
}

// validate 檢查規則內容;角色須存在、啟用,並具備該單據的核准權限(否則這一層永遠沒有人能核准)。
func validate(c *gin.Context, q *db.Queries, companyID int64, in *ruleInput) error {
	ctx := c.Request.Context()
	info, ok := docTypes[in.DocType]
	if !ok {
		return fieldErr("doc_type", "單據類型不正確")
	}
	if in.MinAmount.IsNegative() || in.MinAmount.Exponent() < -2 {
		return fieldErr("min_amount", "金額須為 0 以上,最多 2 位小數")
	}
	layers := in.layers()
	if n := len(layers); n < 1 || n > 4 {
		return fieldErr("role_ids", "請指定第 2 層起的角色,最多 4 層")
	}
	var all []int64
	seen := map[int64]bool{}
	for i := range layers {
		l := &layers[i]
		if l.Mode == "" {
			l.Mode = ModeAny
		}
		if l.Mode != ModeAny && l.Mode != ModeAll {
			return fieldErr("role_ids", "簽法須為或簽或會簽")
		}
		if n := len(l.RoleIDs); n < 1 || n > 5 {
			return fieldErr("role_ids", "每一層須指定 1 到 5 個角色")
		} else if l.Mode == ModeAll && n < 2 {
			return fieldErr("role_ids", "會簽至少需要 2 個角色")
		}
		for _, id := range l.RoleIDs {
			if seen[id] {
				return fieldErr("role_ids", "每個角色只能出現在一個層級")
			}
			seen[id] = true
			all = append(all, id)
		}
	}
	in.Steps = layers
	roles, err := q.CompanyRolesByIDs(ctx, db.CompanyRolesByIDsParams{CompanyID: companyID, Ids: all})
	if err != nil {
		return err
	}
	if len(roles) != len(all) {
		return fieldErr("role_ids", "角色不存在或已停用")
	}
	for _, r := range roles {
		perms, err := q.ListRolePermissions(ctx, r.ID)
		if err != nil {
			return err
		}
		if !slices.Contains(perms, info.Approve) {
			return fieldErr("role_ids", "角色「"+r.Name+"」沒有「"+info.Label+"」的核准權限,無法擔任簽核層級")
		}
	}
	return nil
}

func saveSteps(c *gin.Context, q *db.Queries, ruleID int64, layers []stepInput) error {
	ctx := c.Request.Context()
	if err := q.DeleteApprovalRuleSteps(ctx, ruleID); err != nil {
		return err
	}
	for i, l := range layers {
		for _, id := range l.RoleIDs {
			if err := q.InsertApprovalRuleStep(ctx, db.InsertApprovalRuleStepParams{RuleID: ruleID, StepNo: int32(i + 2), Mode: l.Mode, RoleID: id}); err != nil {
				return err
			}
		}
	}
	return nil
}

var errDuplicateRule = apperr.Conflict("APR-010", "此單據類型已有相同起始金額的規則")

func (m *Module) createRule(c *gin.Context) {
	var in ruleInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	var out ruleDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		if err := validate(c, q, a.CompanyID, &in); err != nil {
			return err
		}
		r, err := q.CreateApprovalRule(ctx, db.CreateApprovalRuleParams{
			CompanyID: a.CompanyID, DocType: in.DocType, MinAmount: in.MinAmount, ActorID: &a.UserID,
		})
		if database.IsUniqueViolation(err, "approval_rules_key") {
			return errDuplicateRule
		}
		if err != nil {
			return err
		}
		if err := saveSteps(c, q, r.ID, in.Steps); err != nil {
			return err
		}
		rules, err := loadRules(c, q, a.CompanyID)
		if err != nil {
			return err
		}
		for _, x := range rules {
			if x.ID == r.ID {
				out = x
			}
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Create, EntityType: "approval_rule", EntityID: &r.ID,
			Summary: "新增簽核規則 " + Label(in.DocType) + " ≥ " + in.MinAmount.String(), After: out})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, out)
}

func (m *Module) updateRule(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in ruleInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	var out ruleDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		if _, err := q.GetApprovalRule(ctx, db.GetApprovalRuleParams{ID: id, CompanyID: a.CompanyID}); database.IsNoRows(err) {
			return apperr.ErrNotFound
		} else if err != nil {
			return err
		}
		if err := validate(c, q, a.CompanyID, &in); err != nil {
			return err
		}
		before, _ := loadRules(c, q, a.CompanyID)
		_, err := q.UpdateApprovalRule(ctx, db.UpdateApprovalRuleParams{
			ID: id, CompanyID: a.CompanyID, DocType: in.DocType, MinAmount: in.MinAmount, ActorID: &a.UserID, Version: in.Version,
		})
		if database.IsNoRows(err) {
			return apperr.ErrVersionConflict
		}
		if database.IsUniqueViolation(err, "approval_rules_key") {
			return errDuplicateRule
		}
		if err != nil {
			return err
		}
		if err := saveSteps(c, q, id, in.Steps); err != nil {
			return err
		}
		rules, err := loadRules(c, q, a.CompanyID)
		if err != nil {
			return err
		}
		var prev ruleDTO
		for _, x := range rules {
			if x.ID == id {
				out = x
			}
		}
		for _, x := range before {
			if x.ID == id {
				prev = x
			}
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Update, EntityType: "approval_rule", EntityID: &id,
			Summary: "修改簽核規則 " + Label(in.DocType), Before: prev, After: out})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, out)
}

// deleteRule 刪除規則。已送審、已快照流程的單據不受影響。
func (m *Module) deleteRule(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		r, err := q.GetApprovalRule(ctx, db.GetApprovalRuleParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := q.DeleteApprovalRule(ctx, db.DeleteApprovalRuleParams{ID: id, CompanyID: a.CompanyID}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Delete, EntityType: "approval_rule", EntityID: &id,
			Summary: "刪除簽核規則 " + Label(r.DocType) + " ≥ " + r.MinAmount.String()})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

// ---- 進度 ----

type progressStep struct {
	Step         int        `json:"step"`
	Slot         int        `json:"slot"`
	Mode         string     `json:"mode"`   // any 或簽 / all 會簽
	Waived       bool       `json:"waived"` // 或簽層已由其他格通過,這一格免簽
	RoleName     string     `json:"role_name"`
	ApproverName string     `json:"approver_name"`
	ActingFor    string     `json:"acting_for"` // 代理核准時的委託人姓名
	ApprovedAt   *time.Time `json:"approved_at"`
	Done         bool       `json:"done"`
}

type progressDTO struct {
	Required   int            `json:"required"` // 0 = 單層核准(沒有適用規則)
	Steps      []progressStep `json:"steps"`
	Current    int            `json:"current"`     // 目前等待的層級,0 = 已全部核准
	CanApprove bool           `json:"can_approve"` // 目前使用者能否核准目前這一層(只看層級,不看單據狀態)
}

// progress GET /approval/progress?doc_type=&doc_id= 單據的簽核流程與目前進度。
func (m *Module) progress(c *gin.Context) {
	docType := c.Query("doc_type")
	info, ok := docTypes[docType]
	docID, err := strconv.ParseInt(c.Query("doc_id"), 10, 64)
	if !ok || err != nil || docID <= 0 {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	a := actor(c)
	if !a.Can(info.Read) {
		response.Error(c, apperr.ErrForbidden)
		return
	}
	ctx := c.Request.Context()
	// 單據須存在於本公司且在資料範圍內;範圍外視為不存在(與各單據頁一致,不透露存在與否)
	deptID, userID := a.ScopeFilter()
	if ok, err := m.store.ApprovalDocVisible(ctx, db.ApprovalDocVisibleParams{
		DocType: docType, DocID: docID, CompanyID: a.CompanyID, ScopeUserID: userID, ScopeDeptID: deptID,
	}); err != nil {
		response.Error(c, err)
		return
	} else if !ok {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	rows, err := m.store.ListDocumentApprovals(ctx, db.ListDocumentApprovalsParams{CompanyID: a.CompanyID, DocType: docType, DocID: docID})
	if err != nil {
		response.Error(c, err)
		return
	}
	layers := groupLayers(toSlots(rows, func(r db.ListDocumentApprovalsRow) slot {
		return slot{Step: r.StepNo, No: r.SlotNo, Mode: r.Mode, RoleID: r.RoleID, RoleName: r.RoleName, ApproverID: r.ApproverID, ActingFor: r.ActingFor}
	}))
	out := progressDTO{Steps: []progressStep{}, Required: len(layers)}
	for _, r := range rows {
		waived := false
		for _, l := range layers {
			if l.No == r.StepNo {
				waived = l.Mode == ModeAny && l.done() && r.ApproverID == nil
			}
		}
		out.Steps = append(out.Steps, progressStep{Step: int(r.StepNo), Slot: int(r.SlotNo), Mode: r.Mode, Waived: waived,
			RoleName: r.RoleName, ApproverName: r.ApproverName, ActingFor: r.ActingForName, ApprovedAt: r.ApprovedAt, Done: r.ApproverID != nil})
	}
	if ci := currentLayer(layers); ci >= 0 {
		cur := layers[ci]
		out.Current = int(cur.No)
		can := a.Can(info.Approve) && !approvedByMe(a, layers)
		if can {
			holds := func(roleID int64) (int64, error) { return roleHolder(ctx, m.store.Queries, a, roleID) }
			can = false
			for _, s := range cur.Slots {
				if s.done() {
					continue
				}
				_, ok, err := fillable(a, info.Approve, s, holds)
				if err != nil {
					response.Error(c, err)
					return
				}
				if ok {
					can = true
					break
				}
			}
		}
		out.CanApprove = can
	}
	response.OK(c, out)
}
