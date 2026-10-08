package gl

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/money"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/system/docno"
	"erp/internal/system/permission"
	"erp/internal/trade"
)

const maxVoucherLines = 200

type voucherLineDTO struct {
	LineNo         int32           `json:"line_no"`
	AccountID      int64           `json:"account_id"`
	AccountCode    string          `json:"account_code"`
	AccountName    string          `json:"account_name"`
	Debit          decimal.Decimal `json:"debit"`
	Credit         decimal.Decimal `json:"credit"`
	Description    string          `json:"description"`
	CustomerID     *int64          `json:"customer_id"`
	CustomerName   *string         `json:"customer_name"`
	SupplierID     *int64          `json:"supplier_id"`
	SupplierName   *string         `json:"supplier_name"`
	DepartmentID   *int64          `json:"department_id"`
	DepartmentName *string         `json:"department_name"`
}

type voucherDTO struct {
	ID           int64            `json:"id"`
	DocNo        string           `json:"doc_no"`
	VoucherDate  string           `json:"voucher_date"`
	SourceType   string           `json:"source_type"`
	SourceID     *int64           `json:"source_id"`
	SourceNo     string           `json:"source_no"`
	Description  string           `json:"description"`
	Status       string           `json:"status"`
	ReversalOf   *int64           `json:"reversal_of"`
	ReversalOfNo *string          `json:"reversal_of_no"`
	ReversedByNo *string          `json:"reversed_by_no"`
	TotalAmount  decimal.Decimal  `json:"total_amount"`
	CreatedBy    *string          `json:"created_by_name"`
	PostedBy     *string          `json:"posted_by_name"`
	PostedAt     *time.Time       `json:"posted_at"`
	Lines        []voucherLineDTO `json:"lines"`
	Version      int32            `json:"version"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

func loadVoucher(ctx context.Context, q *db.Queries, companyID, id int64) (voucherDTO, error) {
	v, err := q.GetVoucher(ctx, db.GetVoucherParams{ID: id, CompanyID: companyID})
	if database.IsNoRows(err) {
		return voucherDTO{}, apperr.ErrNotFound
	}
	if err != nil {
		return voucherDTO{}, err
	}
	rows, err := q.ListVoucherLines(ctx, id)
	if err != nil {
		return voucherDTO{}, err
	}
	lines := make([]voucherLineDTO, len(rows))
	for i, l := range rows {
		lines[i] = voucherLineDTO{
			LineNo: l.LineNo, AccountID: l.AccountID, AccountCode: l.AccountCode, AccountName: l.AccountName,
			Debit: l.Debit, Credit: l.Credit, Description: l.Description, CustomerID: l.CustomerID,
			CustomerName: l.CustomerName, SupplierID: l.SupplierID, SupplierName: l.SupplierName,
			DepartmentID: l.DepartmentID, DepartmentName: l.DepartmentName,
		}
	}
	return voucherDTO{
		ID: v.ID, DocNo: v.DocNo, VoucherDate: v.VoucherDate.Format(time.DateOnly), SourceType: v.SourceType,
		SourceID: v.SourceID, SourceNo: v.SourceNo, Description: v.Description, Status: v.Status,
		ReversalOf: v.ReversalOf, ReversalOfNo: v.ReversalOfNo, ReversedByNo: v.ReversedByNo,
		TotalAmount: v.TotalAmount, CreatedBy: v.CreatedByName, PostedBy: v.PostedByName, PostedAt: v.PostedAt,
		Lines: lines, Version: v.Version, UpdatedAt: v.UpdatedAt,
	}, nil
}

type voucherListDTO struct {
	ID          int64           `json:"id"`
	DocNo       string          `json:"doc_no"`
	VoucherDate string          `json:"voucher_date"`
	SourceType  string          `json:"source_type"`
	SourceID    *int64          `json:"source_id"`
	SourceNo    string          `json:"source_no"`
	Description string          `json:"description"`
	Status      string          `json:"status"`
	ReversalOf  *int64          `json:"reversal_of"`
	Reversed    bool            `json:"reversed"`
	TotalAmount decimal.Decimal `json:"total_amount"`
	CreatedBy   *string         `json:"created_by_name"`
	Version     int32           `json:"version"`
}

func (m *Module) listVouchers(c *gin.Context) {
	ctx := c.Request.Context()
	accountID, err := httpx.QueryInt64(c, "account_id")
	if err != nil {
		response.Error(c, err)
		return
	}
	from, err := trade.OptionalDate(c, "from")
	if err != nil {
		response.Error(c, err)
		return
	}
	to, err := trade.OptionalDate(c, "to")
	if err != nil {
		response.Error(c, err)
		return
	}
	pg := page.Parse(c.Query("page"), c.Query("size"))
	companyID := actor(c).CompanyID
	status, source, keyword := httpx.QueryString(c, "status"), httpx.QueryString(c, "source"), httpx.QueryString(c, "keyword")
	rows, err := m.store.ListVouchers(ctx, db.ListVouchersParams{
		CompanyID: companyID, Status: status, Source: source, Keyword: keyword, FromDate: from, ToDate: to,
		AccountID: accountID, Lim: pg.Limit(), Off: pg.Offset(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountVouchers(ctx, db.CountVouchersParams{
		CompanyID: companyID, Status: status, Source: source, Keyword: keyword, FromDate: from, ToDate: to, AccountID: accountID,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]voucherListDTO, len(rows))
	for i, r := range rows {
		out[i] = voucherListDTO{
			ID: r.ID, DocNo: r.DocNo, VoucherDate: r.VoucherDate.Format(time.DateOnly), SourceType: r.SourceType,
			SourceID: r.SourceID, SourceNo: r.SourceNo, Description: r.Description, Status: r.Status,
			ReversalOf: r.ReversalOf, Reversed: r.Reversed, TotalAmount: r.TotalAmount, CreatedBy: r.CreatedByName,
			Version: r.Version,
		}
	}
	response.List(c, out, pg.Meta(total))
}

func (m *Module) getVoucher(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	dto, err := loadVoucher(c.Request.Context(), m.store.Queries, actor(c).CompanyID, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 手動傳票 ----

type voucherLineInput struct {
	AccountID    int64           `json:"account_id" binding:"required"`
	Debit        decimal.Decimal `json:"debit"`
	Credit       decimal.Decimal `json:"credit"`
	Description  string          `json:"description" binding:"max=255"`
	CustomerID   *int64          `json:"customer_id"`
	SupplierID   *int64          `json:"supplier_id"`
	DepartmentID *int64          `json:"department_id"`
}

type voucherInput struct {
	VoucherDate string             `json:"voucher_date" binding:"required"`
	Description string             `json:"description" binding:"max=255"`
	Lines       []voucherLineInput `json:"lines" binding:"dive"`
	Version     int32              `json:"version"`
}

// checkVoucher 驗證日期(期間須開放)與分錄:至少 2 行、每行恰有一方有金額、金額為整數元、
// 科目須啟用且為明細科目、輔助核算對象存在。草稿允許借貸不平,過帳時才要求平衡。
func checkVoucher(ctx context.Context, q *db.Queries, companyID int64, in *voucherInput) (time.Time, decimal.Decimal, error) {
	var total decimal.Decimal
	date, err := trade.ParseDate("voucher_date", in.VoucherDate)
	if err != nil {
		return date, total, err
	}
	in.Description = strings.TrimSpace(in.Description)
	if err := CheckPeriodOpen(ctx, q, companyID, date); err != nil {
		return date, total, err
	}
	if len(in.Lines) < 2 {
		return date, total, fieldErr("lines", "傳票至少要有兩行分錄")
	}
	if len(in.Lines) > maxVoucherLines {
		return date, total, fieldErr("lines", fmt.Sprintf("分錄最多 %d 行", maxVoucherLines))
	}
	ids := make([]int64, len(in.Lines))
	for i, l := range in.Lines {
		ids[i] = l.AccountID
	}
	rows, err := q.AccountsForPosting(ctx, db.AccountsForPostingParams{CompanyID: companyID, Ids: ids})
	if err != nil {
		return date, total, err
	}
	accts := map[int64]db.AccountsForPostingRow{}
	for _, r := range rows {
		accts[r.ID] = r
	}
	fields := map[string]string{}
	for i := range in.Lines {
		l := &in.Lines[i]
		key := fmt.Sprintf("lines.%d", i)
		l.Description = strings.TrimSpace(l.Description)
		a, ok := accts[l.AccountID]
		switch {
		case !ok:
			fields[key] = "科目不存在"
		case !a.IsActive:
			fields[key] = a.Code + " 已停用"
		case !a.IsPostable:
			fields[key] = a.Code + " 是彙總科目,不可記帳"
		case l.Debit.IsNegative() || l.Credit.IsNegative():
			fields[key] = "金額不可為負"
		case l.Debit.IsPositive() == l.Credit.IsPositive():
			fields[key] = "每行須且僅須填借方或貸方其中一個金額"
		case !l.Debit.Round(money.AmountPlaces).Equal(l.Debit) || !l.Credit.Round(money.AmountPlaces).Equal(l.Credit):
			fields[key] = "金額須為整數元"
		default:
			ok, err := q.CustomerSupplierDeptExist(ctx, db.CustomerSupplierDeptExistParams{
				CompanyID: companyID, CustomerID: l.CustomerID, SupplierID: l.SupplierID, DepartmentID: l.DepartmentID,
			})
			if err != nil {
				return date, total, err
			}
			if !ok.CustomerOk || !ok.SupplierOk || !ok.DepartmentOk {
				fields[key] = "輔助核算的客戶 / 供應商 / 部門不存在"
			}
		}
		total = total.Add(l.Debit)
	}
	if len(fields) > 0 {
		return date, total, apperr.Validation(fields)
	}
	return date, total, nil
}

func saveVoucherLines(ctx context.Context, q *db.Queries, id int64, lines []voucherLineInput) error {
	if err := q.DeleteVoucherLines(ctx, id); err != nil {
		return err
	}
	for i, l := range lines {
		if err := q.AddVoucherLine(ctx, db.AddVoucherLineParams{
			VoucherID: id, LineNo: int32(i + 1), AccountID: l.AccountID, Debit: l.Debit, Credit: l.Credit,
			Description: l.Description, CustomerID: l.CustomerID, SupplierID: l.SupplierID, DepartmentID: l.DepartmentID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) createVoucher(c *gin.Context) {
	var in voucherInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto voucherDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		date, total, err := checkVoucher(ctx, q, a.CompanyID, &in)
		if err != nil {
			return err
		}
		no, err := docno.Next(ctx, q, a.CompanyID, "journal_voucher", date)
		if err != nil {
			return err
		}
		v, err := q.CreateVoucher(ctx, db.CreateVoucherParams{
			CompanyID: a.CompanyID, DocNo: no, VoucherDate: date, SourceType: "manual", Description: in.Description,
			Status: "draft", TotalAmount: total, CreatedBy: &a.UserID,
		})
		if err != nil {
			return err
		}
		if err := saveVoucherLines(ctx, q, v.ID, in.Lines); err != nil {
			return err
		}
		if dto, err = loadVoucher(ctx, q, a.CompanyID, v.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Create, EntityType: "voucher", EntityID: &v.ID, Summary: "新增傳票 " + no, After: dto,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto)
}

func (m *Module) updateVoucher(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in voucherInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	var dto voucherDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockVoucher(ctx, db.LockVoucherParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Version != in.Version {
			return apperr.ErrVersionConflict
		}
		if cur.Status != "draft" || cur.SourceType != "manual" {
			return errVoucherEdit
		}
		before, err := loadVoucher(ctx, q, a.CompanyID, id)
		if err != nil {
			return err
		}
		date, total, err := checkVoucher(ctx, q, a.CompanyID, &in)
		if err != nil {
			return err
		}
		if _, err := q.UpdateVoucherHeader(ctx, db.UpdateVoucherHeaderParams{
			ID: id, CompanyID: a.CompanyID, VoucherDate: date, Description: in.Description, TotalAmount: total,
			Version: in.Version, UpdatedBy: &a.UserID,
		}); err != nil {
			if database.IsNoRows(err) {
				return apperr.ErrVersionConflict
			}
			return err
		}
		if err := saveVoucherLines(ctx, q, id, in.Lines); err != nil {
			return err
		}
		if dto, err = loadVoucher(ctx, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: audit.Update, EntityType: "voucher", EntityID: &id, Summary: "修改傳票 " + cur.DocNo, Before: before, After: dto,
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// ---- 動作:post 過帳、void 作廢草稿、reverse 沖銷 ----

type voucherActionInput struct {
	Version int32   `json:"version" binding:"required"`
	Date    *string `json:"date"` // 沖銷傳票日期,預設沿用原日期
}

func (m *Module) voucherAction(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in voucherActionInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	action := c.Param("action")
	var perm string
	switch action {
	case "post", "reverse":
		perm = permission.VoucherPost
	case "void":
		perm = permission.VoucherWrite
	default:
		response.Error(c, apperr.ErrNotFound)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	if !a.Can(perm) {
		response.Error(c, apperr.ErrForbidden)
		return
	}
	var dto voucherDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockVoucher(ctx, db.LockVoucherParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Version != in.Version {
			return apperr.ErrVersionConflict
		}
		summary := map[string]string{"post": "過帳", "void": "作廢", "reverse": "沖銷"}[action] + "傳票 " + cur.DocNo
		before := map[string]string{"status": cur.Status}
		switch action {
		case "post", "void":
			if cur.Status != "draft" {
				return apperr.New(http.StatusConflict, "DOC-001", "目前傳票狀態不允許此操作")
			}
			next := map[string]string{"post": "posted", "void": "voided"}[action]
			if action == "post" {
				if err := m.checkPostable(ctx, q, a, cur); err != nil {
					return err
				}
			}
			if _, err := q.SetVoucherStatus(ctx, db.SetVoucherStatusParams{
				ID: id, CompanyID: a.CompanyID, Status: next, ActorID: a.UserID, Version: in.Version,
			}); err != nil {
				return err
			}
			if dto, err = loadVoucher(ctx, q, a.CompanyID, id); err != nil {
				return err
			}
			return audit.Record(ctx, q, audit.Entry{Action: action, EntityType: "voucher", EntityID: &id, Summary: summary,
				Before: before, After: map[string]string{"status": next}})
		default: // reverse
			switch {
			case cur.Status != "posted":
				return apperr.New(http.StatusConflict, "DOC-001", "只有已過帳的傳票可以沖銷")
			case cur.SourceType != "manual":
				return errAutoVoucher
			case cur.ReversalOf != nil:
				return apperr.New(http.StatusConflict, "GL-008", "沖銷傳票不可再沖銷")
			}
			full, err := q.GetVoucher(ctx, db.GetVoucherParams{ID: id, CompanyID: a.CompanyID})
			if err != nil {
				return err
			}
			if full.ReversedByNo != nil {
				return errAlreadyReverse
			}
			date := cur.VoucherDate
			if in.Date != nil && *in.Date != "" {
				if date, err = trade.ParseDate("date", *in.Date); err != nil {
					return err
				}
			}
			if err := reverseVoucher(ctx, q, Options{CompanyID: a.CompanyID, ActorID: &a.UserID}, cur, date); err != nil {
				return err
			}
			if dto, err = loadVoucher(ctx, q, a.CompanyID, id); err != nil {
				return err
			}
			return audit.Record(ctx, q, audit.Entry{Action: action, EntityType: "voucher", EntityID: &id, Summary: summary})
		}
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

// checkPostable 過帳前:期間須開放、借貸須平衡、分錄須仍然合法(科目可能在存檔後被停用)。
func (m *Module) checkPostable(ctx context.Context, q *db.Queries, a *authctx.Actor, cur db.Voucher) error {
	if err := CheckPeriodOpen(ctx, q, a.CompanyID, cur.VoucherDate); err != nil {
		return err
	}
	lines, err := q.ListVoucherLines(ctx, cur.ID)
	if err != nil {
		return err
	}
	in := voucherInput{VoucherDate: cur.VoucherDate.Format(time.DateOnly), Description: cur.Description}
	debit, credit := decimal.Zero, decimal.Zero
	for _, l := range lines {
		in.Lines = append(in.Lines, voucherLineInput{
			AccountID: l.AccountID, Debit: l.Debit, Credit: l.Credit, Description: l.Description,
			CustomerID: l.CustomerID, SupplierID: l.SupplierID, DepartmentID: l.DepartmentID,
		})
		debit, credit = debit.Add(l.Debit), credit.Add(l.Credit)
	}
	if _, _, err := checkVoucher(ctx, q, a.CompanyID, &in); err != nil {
		return err
	}
	if !debit.Equal(credit) {
		return errUnbalanced.WithDetails(map[string]string{"debit": debit.String(), "credit": credit.String()})
	}
	return nil
}
