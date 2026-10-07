package system

import (
	"context"
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
	errDeptCodeTaken   = apperr.Conflict("DEPT-001", "部門代碼已存在")
	errDeptBadParent   = apperr.Validation(map[string]string{"parent_id": "上層部門不存在"})
	errDeptCycleParent = apperr.Validation(map[string]string{"parent_id": "上層部門不可為自己或自己的下層部門"})
)

type departmentDTO struct {
	ID        int64     `json:"id"`
	ParentID  *int64    `json:"parent_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	SortOrder int32     `json:"sort_order"`
	IsActive  bool      `json:"is_active"`
	UserCount int64     `json:"user_count"`
	Version   int32     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toDepartmentDTO(d db.Department) departmentDTO {
	return departmentDTO{
		ID: d.ID, ParentID: d.ParentID, Code: d.Code, Name: d.Name, SortOrder: d.SortOrder,
		IsActive: d.IsActive, Version: d.Version, UpdatedAt: d.UpdatedAt,
	}
}

func (m *Module) listDepartments(c *gin.Context) {
	rows, err := m.store.ListDepartments(c.Request.Context(), actor(c).CompanyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]departmentDTO, len(rows))
	for i, r := range rows {
		out[i] = departmentDTO{
			ID: r.ID, ParentID: r.ParentID, Code: r.Code, Name: r.Name, SortOrder: r.SortOrder,
			IsActive: r.IsActive, UserCount: r.UserCount, Version: r.Version, UpdatedAt: r.UpdatedAt,
		}
	}
	response.OK(c, out)
}

type departmentInput struct {
	ParentID  *int64 `json:"parent_id"`
	Code      string `json:"code" binding:"required,max=20"`
	Name      string `json:"name" binding:"required,max=100"`
	SortOrder int32  `json:"sort_order"`
}

func (in *departmentInput) normalize() {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.TrimSpace(in.Name)
}

func (m *Module) createDepartment(c *gin.Context) {
	var in departmentInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	in.normalize()
	ctx := c.Request.Context()
	a := actor(c)

	var dept db.Department
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		if err := checkParent(ctx, q, a.CompanyID, in.ParentID, 0); err != nil {
			return err
		}
		var err error
		dept, err = q.CreateDepartment(ctx, db.CreateDepartmentParams{
			CompanyID: a.CompanyID, ParentID: in.ParentID, Code: in.Code, Name: in.Name,
			SortOrder: in.SortOrder, CreatedBy: &a.UserID,
		})
		if database.IsUniqueViolation(err, "departments_company_code_key") {
			return errDeptCodeTaken
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "department", EntityID: &dept.ID,
			Summary: "新增部門 " + dept.Code + " " + dept.Name, After: toDepartmentDTO(dept),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, toDepartmentDTO(dept))
}

type updateDepartmentInput struct {
	departmentInput
	IsActive bool  `json:"is_active"`
	Version  int32 `json:"version" binding:"required"`
}

func (m *Module) updateDepartment(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in updateDepartmentInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	in.normalize()
	ctx := c.Request.Context()
	a := actor(c)

	var after db.Department
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := q.GetDepartment(ctx, db.GetDepartmentParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := checkParent(ctx, q, a.CompanyID, in.ParentID, id); err != nil {
			return err
		}
		after, err = q.UpdateDepartment(ctx, db.UpdateDepartmentParams{
			ID: id, CompanyID: a.CompanyID, ParentID: in.ParentID, Code: in.Code, Name: in.Name,
			SortOrder: in.SortOrder, IsActive: in.IsActive, Version: in.Version, UpdatedBy: &a.UserID,
		})
		if database.IsNoRows(err) {
			return apperr.ErrVersionConflict
		}
		if database.IsUniqueViolation(err, "departments_company_code_key") {
			return errDeptCodeTaken
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "department", EntityID: &id,
			Summary: "修改部門 " + after.Code + " " + after.Name,
			Before:  toDepartmentDTO(before), After: toDepartmentDTO(after),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toDepartmentDTO(after))
}

// checkParent 驗證上層部門存在於同公司,且(修改時)不是自己或自己的下層。
func checkParent(ctx context.Context, q *db.Queries, companyID int64, parentID *int64, selfID int64) error {
	if parentID == nil {
		return nil
	}
	if _, err := q.GetDepartment(ctx, db.GetDepartmentParams{ID: *parentID, CompanyID: companyID}); err != nil {
		if database.IsNoRows(err) {
			return errDeptBadParent
		}
		return err
	}
	if selfID == 0 {
		return nil
	}
	cyclic, err := q.IsDepartmentDescendant(ctx, db.IsDepartmentDescendantParams{AncestorID: selfID, CandidateID: *parentID})
	if err != nil {
		return err
	}
	if cyclic {
		return errDeptCycleParent
	}
	return nil
}
