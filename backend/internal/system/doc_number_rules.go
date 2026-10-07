package system

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/system/docno"
)

type docNumberRuleDTO struct {
	DocType    string    `json:"doc_type"`
	Name       string    `json:"name"`
	Prefix     string    `json:"prefix"`
	DateFormat string    `json:"date_format"`
	SeqLength  int32     `json:"seq_length"`
	Example    string    `json:"example"`
	Version    int32     `json:"version"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func toDocNumberRuleDTO(r db.DocNumberRule) docNumberRuleDTO {
	return docNumberRuleDTO{
		DocType: r.DocType, Name: r.Name, Prefix: r.Prefix, DateFormat: r.DateFormat,
		SeqLength: r.SeqLength, Example: docno.Preview(r, time.Now().In(taipei)),
		Version: r.Version, UpdatedAt: r.UpdatedAt,
	}
}

func (m *Module) listDocNumberRules(c *gin.Context) {
	rows, err := m.store.ListDocNumberRules(c.Request.Context(), actor(c).CompanyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]docNumberRuleDTO, len(rows))
	for i, r := range rows {
		out[i] = toDocNumberRuleDTO(r)
	}
	response.OK(c, out)
}

type docNumberRuleInput struct {
	Name       string `json:"name" binding:"required,max=50"`
	Prefix     string `json:"prefix" binding:"required,max=10,alphanum"`
	DateFormat string `json:"date_format" binding:"required,oneof=YYYYMMDD YYYYMM YYYY NONE"`
	SeqLength  int32  `json:"seq_length" binding:"required,min=3,max=10"`
	Version    int32  `json:"version" binding:"required"`
}

// updateDocNumberRule 修改單號規則。已發出的單號不受影響;
// 若改了日期格式,新期間的流水號會從 1 開始,舊計數保留。
func (m *Module) updateDocNumberRule(c *gin.Context) {
	docType := c.Param("docType")
	var in docNumberRuleInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Prefix = strings.ToUpper(in.Prefix)
	ctx := c.Request.Context()
	a := actor(c)

	var after db.DocNumberRule
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetDocNumberRule(ctx, db.GetDocNumberRuleParams{CompanyID: a.CompanyID, DocType: docType})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		after, err = q.UpdateDocNumberRule(ctx, db.UpdateDocNumberRuleParams{
			CompanyID: a.CompanyID, DocType: docType, Name: in.Name, Prefix: in.Prefix,
			DateFormat: in.DateFormat, SeqLength: in.SeqLength, Version: in.Version, UpdatedBy: &a.UserID,
		})
		if database.IsNoRows(err) {
			return apperr.ErrVersionConflict
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "doc_number_rule", Summary: "修改單號規則 " + after.Name,
			Before: toDocNumberRuleDTO(before), After: toDocNumberRuleDTO(after),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toDocNumberRuleDTO(after))
}
