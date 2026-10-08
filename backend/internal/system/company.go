package system

import (
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/response"
	"erp/internal/shared/taxid"
	"erp/internal/system/audit"
)

type companyDTO struct {
	Name     string `json:"name"`
	TaxID    string `json:"tax_id"`     // 統一編號(8 碼)
	TaxRegNo string `json:"tax_reg_no"` // 稅籍編號(9 碼,營業稅媒體申報檔用)
	// Require2FA 要求所有使用者啟用雙因素驗證:未啟用者登入後只能先設定
	Require2FA bool  `json:"require_2fa"`
	Version    int32 `json:"version"`
}

func toCompanyDTO(c db.Company) companyDTO {
	d := companyDTO{Name: c.Name, TaxRegNo: c.TaxRegNo, Require2FA: c.Require2fa, Version: c.Version}
	if c.TaxID != nil {
		d.TaxID = strings.TrimSpace(*c.TaxID)
	}
	return d
}

func (m *Module) getCompany(c *gin.Context) {
	co, err := m.store.GetCompany(c.Request.Context(), actor(c).CompanyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toCompanyDTO(co))
}

type companyInput struct {
	Name       string `json:"name" binding:"required,max=100"`
	TaxID      string `json:"tax_id"`
	TaxRegNo   string `json:"tax_reg_no"`
	Require2FA bool   `json:"require_2fa"`
	Version    int32  `json:"version" binding:"required"`
}

var taxRegNoRe = regexp.MustCompile(`^[A-Z0-9]{9}$`)

func (m *Module) updateCompany(c *gin.Context) {
	var in companyInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	in.Name, in.TaxID = strings.TrimSpace(in.Name), strings.TrimSpace(in.TaxID)
	in.TaxRegNo = strings.ToUpper(strings.TrimSpace(in.TaxRegNo))
	errs := map[string]string{}
	if in.TaxID != "" && !taxid.Valid(in.TaxID) {
		errs["tax_id"] = "統一編號不正確(8 碼,含檢查碼)"
	}
	if in.TaxRegNo != "" && !taxRegNoRe.MatchString(in.TaxRegNo) {
		errs["tax_reg_no"] = "稅籍編號須為 9 碼英數字"
	}
	if len(errs) > 0 {
		response.Error(c, apperr.Validation(errs))
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	var out companyDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetCompany(ctx, a.CompanyID)
		if err != nil {
			return err
		}
		var taxID *string
		if in.TaxID != "" {
			taxID = &in.TaxID
		}
		co, err := q.UpdateCompany(ctx, db.UpdateCompanyParams{
			ID: a.CompanyID, Name: in.Name, TaxID: taxID, TaxRegNo: in.TaxRegNo, Version: in.Version,
		})
		if database.IsNoRows(err) {
			return apperr.ErrVersionConflict
		}
		if err != nil {
			return err
		}
		if in.Require2FA && !before.Require2fa {
			// 避免把自己鎖在系統管理之外:開啟前,操作的人自己必須已啟用雙因素驗證
			me, err := q.GetUserByID(ctx, a.UserID)
			if err != nil {
				return err
			}
			if !me.TotpEnabled {
				return apperr.Conflict("SYS-022", "請先為自己啟用雙因素驗證(個人設定 → 帳號安全),再開啟公司政策")
			}
		}
		if in.Require2FA != before.Require2fa {
			if err := q.SetCompanyRequire2FA(ctx, db.SetCompanyRequire2FAParams{ID: a.CompanyID, Require: in.Require2FA}); err != nil {
				return err
			}
			co.Require2fa = in.Require2FA
		}
		out = toCompanyDTO(co)
		id := co.ID
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "company", EntityID: &id, Summary: "修改公司資料",
			Before: toCompanyDTO(before), After: out,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, out)
}
