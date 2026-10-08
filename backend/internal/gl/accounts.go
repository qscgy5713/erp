package gl

import (
	"net/http"
	"regexp"
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

var (
	codePattern     = regexp.MustCompile(`^[0-9]{3,10}$`)
	acctTypes       = map[string]string{"asset": "資產", "liability": "負債", "equity": "權益", "revenue": "收入", "cost": "成本", "expense": "費用"}
	errAccountCode  = apperr.Conflict("GL-010", "科目代號已存在")
	errAccountInUse = "GL-011"
)

type accountDTO struct {
	ID         int64     `json:"id"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	AcctType   string    `json:"acct_type"`
	ParentID   *int64    `json:"parent_id"`
	ParentCode *string   `json:"parent_code"`
	IsPostable bool      `json:"is_postable"`
	IsActive   bool      `json:"is_active"`
	Note       string    `json:"note"`
	HasEntries bool      `json:"has_entries"`
	Version    int32     `json:"version"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func toAccountDTO(a db.ListAccountsRow) accountDTO {
	return accountDTO{
		ID: a.ID, Code: a.Code, Name: a.Name, AcctType: a.AcctType, ParentID: a.ParentID, ParentCode: a.ParentCode,
		IsPostable: a.IsPostable, IsActive: a.IsActive, Note: a.Note, HasEntries: a.HasEntries, Version: a.Version,
		UpdatedAt: a.UpdatedAt,
	}
}

func (m *Module) listAccounts(c *gin.Context) {
	active, err := httpx.QueryBool(c, "is_active")
	if err != nil {
		response.Error(c, err)
		return
	}
	rows, err := m.store.ListAccounts(c.Request.Context(), db.ListAccountsParams{
		CompanyID: actor(c).CompanyID, Keyword: httpx.QueryString(c, "keyword"), IsActive: active,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]accountDTO, len(rows))
	for i, r := range rows {
		out[i] = toAccountDTO(r)
	}
	response.OK(c, out)
}

type accountOptionDTO struct {
	ID       int64  `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	AcctType string `json:"acct_type"`
}

// accountOptions 開傳票 / 查報表選科目:只列啟用中的明細科目。
func (m *Module) accountOptions(c *gin.Context) {
	active, postable := true, true
	rows, err := m.store.ListAccounts(c.Request.Context(), db.ListAccountsParams{
		CompanyID: actor(c).CompanyID, Keyword: httpx.QueryString(c, "keyword"), IsActive: &active, PostableOnly: &postable,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]accountOptionDTO, len(rows))
	for i, r := range rows {
		out[i] = accountOptionDTO{ID: r.ID, Code: r.Code, Name: r.Name, AcctType: r.AcctType}
	}
	response.OK(c, out)
}

type accountInput struct {
	Code       string `json:"code" binding:"required"`
	Name       string `json:"name" binding:"required,max=100"`
	AcctType   string `json:"acct_type" binding:"required"`
	ParentID   *int64 `json:"parent_id"`
	IsPostable bool   `json:"is_postable"`
	IsActive   bool   `json:"is_active"`
	Note       string `json:"note" binding:"max=255"`
	Version    int32  `json:"version"`
}

// checkAccount 驗證輸入;parent 須為同公司的彙總科目(不可記帳),且不可形成循環。
func (m *Module) checkAccount(c *gin.Context, q *db.Queries, in *accountInput, selfID int64) error {
	in.Code, in.Name, in.Note = strings.TrimSpace(in.Code), strings.TrimSpace(in.Name), strings.TrimSpace(in.Note)
	if in.Name == "" {
		return fieldErr("name", "請輸入名稱")
	}
	if !codePattern.MatchString(in.Code) {
		return fieldErr("code", "科目代號須為 3–10 碼數字")
	}
	if _, ok := acctTypes[in.AcctType]; !ok {
		return fieldErr("acct_type", "科目類別不正確")
	}
	if in.ParentID != nil {
		p, err := q.GetAccount(c.Request.Context(), db.GetAccountParams{ID: *in.ParentID, CompanyID: actor(c).CompanyID})
		switch {
		case database.IsNoRows(err):
			return fieldErr("parent_id", "上層科目不存在")
		case err != nil:
			return err
		case p.IsPostable:
			return fieldErr("parent_id", "上層科目須為彙總科目(不可記帳)")
		case p.AcctType != in.AcctType:
			return fieldErr("parent_id", "上層科目的類別須相同")
		}
		if selfID != 0 {
			cyc, err := q.AccountParentCycle(c.Request.Context(), db.AccountParentCycleParams{ParentID: *in.ParentID, SelfID: selfID})
			if err != nil {
				return err
			}
			if cyc {
				return fieldErr("parent_id", "上層科目不可是自己或自己的下層")
			}
		}
	}
	return nil
}

func (m *Module) createAccount(c *gin.Context) {
	var in accountInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto accountDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		if err := m.checkAccount(c, q, &in, 0); err != nil {
			return err
		}
		created, err := q.CreateAccount(ctx, db.CreateAccountParams{
			CompanyID: a.CompanyID, Code: in.Code, Name: in.Name, AcctType: in.AcctType, ParentID: in.ParentID,
			IsPostable: in.IsPostable, Note: in.Note, CreatedBy: &a.UserID,
		})
		if database.IsUniqueViolation(err, "accounts_company_code_key") {
			return errAccountCode
		}
		if err != nil {
			return err
		}
		row, err := q.GetAccount(ctx, db.GetAccountParams{ID: created.ID, CompanyID: a.CompanyID})
		if err != nil {
			return err
		}
		dto = toAccountDTO(db.ListAccountsRow(row))
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "account", EntityID: &created.ID, Summary: "新增會計科目 " + in.Code + " " + in.Name, After: dto,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto)
}

func (m *Module) updateAccount(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in accountInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto accountDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.GetAccount(ctx, db.GetAccountParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Version != in.Version {
			return apperr.ErrVersionConflict
		}
		if err := m.checkAccount(c, q, &in, id); err != nil {
			return err
		}
		// 代號不可改(傳票與報表以代號辨識);已有分錄後不可改類別或變成彙總科目
		if in.Code != cur.Code {
			return fieldErr("code", "科目代號建立後不可修改")
		}
		if cur.HasEntries && (in.AcctType != cur.AcctType || !in.IsPostable) {
			return apperr.New(http.StatusConflict, errAccountInUse, "科目已有傳票分錄,不可修改類別或改為彙總科目")
		}
		// 被拋轉規則使用的科目不可停用或改為彙總科目
		if !in.IsActive || !in.IsPostable {
			maps, err := q.ListAccountMappings(ctx, a.CompanyID)
			if err != nil {
				return err
			}
			for _, mp := range maps {
				if mp.AccountID == id {
					return apperr.New(http.StatusConflict, errAccountInUse, "此科目被拋轉規則「"+mappingLabel(mp.MapKey)+"」使用,請先改設其他科目")
				}
			}
		}
		before := toAccountDTO(db.ListAccountsRow(cur))
		if _, err := q.UpdateAccount(ctx, db.UpdateAccountParams{
			ID: id, CompanyID: a.CompanyID, Name: in.Name, AcctType: in.AcctType, ParentID: in.ParentID,
			IsPostable: in.IsPostable, IsActive: in.IsActive, Note: in.Note, Version: in.Version, UpdatedBy: &a.UserID,
		}); err != nil {
			if database.IsNoRows(err) {
				return apperr.ErrVersionConflict
			}
			return err
		}
		row, err := q.GetAccount(ctx, db.GetAccountParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return err
		}
		dto = toAccountDTO(db.ListAccountsRow(row))
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "account", EntityID: &id, Summary: "修改會計科目 " + cur.Code, Before: before, After: dto,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 拋轉規則 ----

type mappingDTO struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	AccountID   int64  `json:"account_id"`
	AccountCode string `json:"account_code"`
	AccountName string `json:"account_name"`
}

func (m *Module) listMappings(c *gin.Context) {
	rows, err := m.store.ListAccountMappings(c.Request.Context(), actor(c).CompanyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	byKey := map[string]db.ListAccountMappingsRow{}
	for _, r := range rows {
		byKey[r.MapKey] = r
	}
	out := make([]mappingDTO, 0, len(MappingKeys))
	for _, k := range MappingKeys {
		d := mappingDTO{Key: k.Key, Label: k.Label}
		if r, ok := byKey[k.Key]; ok {
			d.AccountID, d.AccountCode, d.AccountName = r.AccountID, r.AccountCode, r.AccountName
		}
		out = append(out, d)
	}
	response.OK(c, out)
}

type mappingInput struct {
	AccountID int64 `json:"account_id" binding:"required"`
}

func (m *Module) setMapping(c *gin.Context) {
	key := c.Param("key")
	if mappingLabel(key) == key {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	var in mappingInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		accts, err := q.AccountsForPosting(ctx, db.AccountsForPostingParams{CompanyID: a.CompanyID, Ids: []int64{in.AccountID}})
		if err != nil {
			return err
		}
		if len(accts) != 1 || !accts[0].IsActive || !accts[0].IsPostable {
			return fieldErr("account_id", "科目不存在、已停用或不是明細科目")
		}
		if err := q.SetAccountMapping(ctx, db.SetAccountMappingParams{
			CompanyID: a.CompanyID, MapKey: key, AccountID: in.AccountID, UpdatedBy: &a.UserID,
		}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "account_mapping", Summary: "調整拋轉規則「" + mappingLabel(key) + "」→ " + accts[0].Code + " " + accts[0].Name,
			After: map[string]any{"key": key, "account_id": in.AccountID},
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, gin.H{"key": key, "account_id": in.AccountID})
}
