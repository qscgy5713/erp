package approval

import (
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/db"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/system/permission"
)

// 簽核代理人(D87):委託人在期間內把「核准」類權限與簽核角色交給代理人。
// 代理人只多出委託人的 *.approve 權限,資料範圍仍用自己的;代理核准會記錄「代誰核准」。
// 登入即可管理自己的代理人;替他人設定 / 檢視全部需 system.approval.write。

type delegationDTO struct {
	ID            int64  `json:"id"`
	DelegatorID   int64  `json:"delegator_id"`
	DelegatorName string `json:"delegator_name"`
	DelegateID    int64  `json:"delegate_id"`
	DelegateName  string `json:"delegate_name"`
	ValidFrom     string `json:"valid_from"`
	ValidTo       string `json:"valid_to"`
	Note          string `json:"note"`
}

// listDelegations GET /approval/delegations?scope=all  預設只列與我有關的;scope=all 需 approval.write。
func (m *Module) listDelegations(c *gin.Context) {
	a := actor(c)
	var uid *int64
	if c.Query("scope") != "all" || !a.Can(permission.ApprovalWrite) {
		uid = &a.UserID
	}
	rows, err := m.store.ListDelegations(c.Request.Context(), db.ListDelegationsParams{CompanyID: a.CompanyID, UserID: uid})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]delegationDTO, len(rows))
	for i, r := range rows {
		out[i] = delegationDTO{ID: r.ID, DelegatorID: r.DelegatorID, DelegatorName: r.DelegatorName, DelegateID: r.DelegateID,
			DelegateName: r.DelegateName, ValidFrom: r.ValidFrom.Format(time.DateOnly), ValidTo: r.ValidTo.Format(time.DateOnly), Note: r.Note}
	}
	response.OK(c, out)
}

type delegationInput struct {
	DelegatorID int64  `json:"delegator_id"` // 省略 = 自己
	DelegateID  int64  `json:"delegate_id" binding:"required"`
	ValidFrom   string `json:"valid_from" binding:"required"`
	ValidTo     string `json:"valid_to" binding:"required"`
	Note        string `json:"note" binding:"max=200"`
}

func (m *Module) createDelegation(c *gin.Context) {
	var in delegationInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	if in.DelegatorID == 0 {
		in.DelegatorID = a.UserID
	}
	if in.DelegatorID != a.UserID && !a.Can(permission.ApprovalWrite) {
		response.Error(c, apperr.ErrForbidden)
		return
	}
	from, err1 := time.Parse(time.DateOnly, in.ValidFrom)
	to, err2 := time.Parse(time.DateOnly, in.ValidTo)
	switch {
	case err1 != nil:
		response.Error(c, fieldErr("valid_from", "日期格式須為 YYYY-MM-DD"))
		return
	case err2 != nil:
		response.Error(c, fieldErr("valid_to", "日期格式須為 YYYY-MM-DD"))
		return
	case to.Before(from):
		response.Error(c, fieldErr("valid_to", "結束日不可早於起始日"))
		return
	case in.DelegateID == in.DelegatorID:
		response.Error(c, fieldErr("delegate_id", "代理人不可是本人"))
		return
	}
	ctx := c.Request.Context()
	var out delegationDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		for field, id := range map[string]int64{"delegate_id": in.DelegateID, "delegator_id": in.DelegatorID} {
			ok, err := q.ActiveUserInCompany(ctx, db.ActiveUserInCompanyParams{ID: id, CompanyID: a.CompanyID})
			if err != nil {
				return err
			}
			if !ok {
				return fieldErr(field, "找不到此使用者或已停用")
			}
		}
		r, err := q.CreateDelegation(ctx, db.CreateDelegationParams{CompanyID: a.CompanyID, DelegatorID: in.DelegatorID,
			DelegateID: in.DelegateID, ValidFrom: from, ValidTo: to, Note: in.Note, ActorID: &a.UserID})
		if err != nil {
			return err
		}
		out = delegationDTO{ID: r.ID, DelegatorID: r.DelegatorID, DelegateID: r.DelegateID, ValidFrom: in.ValidFrom, ValidTo: in.ValidTo, Note: r.Note}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Create, EntityType: "approval_delegation", EntityID: &r.ID,
			Summary: "設定簽核代理人 " + in.ValidFrom + " ~ " + in.ValidTo, After: out})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, out)
}

// deleteDelegation 委託人、代理人本人或 approval.write 可終止代理。
func (m *Module) deleteDelegation(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		d, err := q.GetDelegation(ctx, db.GetDelegationParams{ID: id, CompanyID: a.CompanyID})
		if err != nil {
			return apperr.ErrNotFound
		}
		if d.DelegatorID != a.UserID && d.DelegateID != a.UserID && !a.Can(permission.ApprovalWrite) {
			return apperr.ErrNotFound // 與自己無關的代理不透露存在
		}
		if _, err := q.DeleteDelegation(ctx, db.DeleteDelegationParams{ID: id, CompanyID: a.CompanyID}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Delete, EntityType: "approval_delegation", EntityID: &id,
			Summary: "終止簽核代理人", Before: d})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}
