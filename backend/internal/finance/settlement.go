package finance

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/auth"
	"erp/internal/db"
	"erp/internal/gl"
	"erp/internal/masterdata"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/docstate"
	"erp/internal/shared/money"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/system/docno"
	"erp/internal/system/permission"
	"erp/internal/trade"
)

// 收款單(side=receipt,對客戶沖應收)與付款單(side=payment,對供應商沖應付)共用同一套邏輯(D42),
// 差異集中在 sideCfg。

const (
	SideReceipt = "receipt"
	SidePayment = "payment"
)

type sideCfg struct {
	side      string
	label     string
	numbering string
	read      []string // 任一即可檢視
	write     string
	approve   string
	post      string
}

var sides = map[string]sideCfg{
	SideReceipt: {
		side: SideReceipt, label: "收款單", numbering: "receipt",
		read:  []string{permission.CollectionRead, permission.CollectionWrite, permission.CollectionApprove, permission.CollectionPost},
		write: permission.CollectionWrite, approve: permission.CollectionApprove, post: permission.CollectionPost,
	},
	SidePayment: {
		side: SidePayment, label: "付款單", numbering: "payment",
		read:  []string{permission.PaymentRead, permission.PaymentWrite, permission.PaymentApprove, permission.PaymentPost},
		write: permission.PaymentWrite, approve: permission.PaymentApprove, post: permission.PaymentPost,
	},
}

var (
	errSettlementEditNotAllowed = apperr.New(http.StatusConflict, "FIN-003", "只有草稿可以修改")
	errSettlementCannotPost     = "FIN-004"
)

func fieldErr(field, msg string) *apperr.Error {
	return apperr.Validation(map[string]string{field: msg})
}

var methodLabels = map[string]bool{"cash": true, "transfer": true, "check": true, "other": true}

type settlementLineDTO struct {
	ID           int64           `json:"id"`
	LineNo       int32           `json:"line_no"`
	TargetID     int64           `json:"target_id"` // 應收 / 應付 id
	SourceType   string          `json:"source_type"`
	SourceNo     string          `json:"source_no"`
	SourceDate   string          `json:"source_date"`
	DueDate      string          `json:"due_date"`
	SourceAmount decimal.Decimal `json:"source_amount"`
	SourcePaid   decimal.Decimal `json:"source_paid"`
	Balance      decimal.Decimal `json:"balance"` // 目前未沖餘額(已過帳的本單沖帳不計入前為過帳前餘額)
	Amount       decimal.Decimal `json:"amount"`
}

type settlementDTO struct {
	ID              int64               `json:"id"`
	Side            string              `json:"side"`
	DocNo           string              `json:"doc_no"`
	DocDate         string              `json:"doc_date"`
	PartnerID       int64               `json:"partner_id"`
	PartnerCode     string              `json:"partner_code"`
	PartnerName     string              `json:"partner_name"`
	SalesUserName   *string             `json:"sales_user_name"`
	Currency        string              `json:"currency"`
	Method          string              `json:"method"`
	Reference       string              `json:"reference"`
	Amount          decimal.Decimal     `json:"amount"`
	Status          string              `json:"status"`
	Note            string              `json:"note"`
	CreatedByName   *string             `json:"created_by_name"`
	SubmittedByName *string             `json:"submitted_by_name"`
	SubmittedAt     *time.Time          `json:"submitted_at"`
	ApprovedByName  *string             `json:"approved_by_name"`
	ApprovedAt      *time.Time          `json:"approved_at"`
	PostedByName    *string             `json:"posted_by_name"`
	PostedAt        *time.Time          `json:"posted_at"`
	Lines           []settlementLineDTO `json:"lines"`
	Version         int32               `json:"version"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

// loadSettlement 讀取並檢查邊別與資料範圍(收款單依客戶負責業務快照);範圍外視為不存在。
func loadSettlement(ctx context.Context, q *db.Queries, a *authctx.Actor, cfg sideCfg, id int64) (settlementDTO, error) {
	s, err := q.GetSettlement(ctx, db.GetSettlementParams{ID: id, CompanyID: a.CompanyID})
	if database.IsNoRows(err) || (err == nil && s.Side != cfg.side) {
		return settlementDTO{}, apperr.ErrNotFound
	}
	if err != nil {
		return settlementDTO{}, err
	}
	if cfg.side == SideReceipt && !masterdata.CustomerVisible(a, s.SalesUserID, s.SalesDepartmentID) {
		return settlementDTO{}, apperr.ErrNotFound
	}
	rows, err := q.ListSettlementLines(ctx, id)
	if err != nil {
		return settlementDTO{}, err
	}
	lines := make([]settlementLineDTO, len(rows))
	for i, r := range rows {
		target := int64(0)
		if r.ReceivableID != nil {
			target = *r.ReceivableID
		} else if r.PayableID != nil {
			target = *r.PayableID
		}
		lines[i] = settlementLineDTO{
			ID: r.ID, LineNo: r.LineNo, TargetID: target, SourceType: r.SourceType, SourceNo: r.SourceNo,
			SourceDate: r.SourceDate.Format(time.DateOnly), DueDate: r.DueDate.Format(time.DateOnly),
			SourceAmount: r.SourceAmount, SourcePaid: r.SourcePaid, Balance: r.SourceAmount.Sub(r.SourcePaid), Amount: r.Amount,
		}
	}
	partner := int64(0)
	if s.CustomerID != nil {
		partner = *s.CustomerID
	} else if s.SupplierID != nil {
		partner = *s.SupplierID
	}
	return settlementDTO{
		ID: s.ID, Side: s.Side, DocNo: s.DocNo, DocDate: s.DocDate.Format(time.DateOnly), PartnerID: partner,
		PartnerCode: s.PartnerCode, PartnerName: s.PartnerName, SalesUserName: s.SalesUserName, Currency: s.Currency,
		Method: s.Method, Reference: s.Reference, Amount: s.Amount, Status: s.Status, Note: s.Note,
		CreatedByName: s.CreatedByName, SubmittedByName: s.SubmittedByName, SubmittedAt: s.SubmittedAt,
		ApprovedByName: s.ApprovedByName, ApprovedAt: s.ApprovedAt, PostedByName: s.PostedByName, PostedAt: s.PostedAt,
		Lines: lines, Version: s.Version, UpdatedAt: s.UpdatedAt,
	}, nil
}

// ---- 列表與單筆 ----

type settlementListDTO struct {
	ID            int64           `json:"id"`
	DocNo         string          `json:"doc_no"`
	DocDate       string          `json:"doc_date"`
	PartnerCode   string          `json:"partner_code"`
	PartnerName   string          `json:"partner_name"`
	SalesUserName *string         `json:"sales_user_name"`
	Currency      string          `json:"currency"`
	Method        string          `json:"method"`
	Reference     string          `json:"reference"`
	Amount        decimal.Decimal `json:"amount"`
	Status        string          `json:"status"`
	Note          string          `json:"note"`
	CreatedByName *string         `json:"created_by_name"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func (m *Module) listSettlements(cfg sideCfg) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		partnerID, err := httpx.QueryInt64(c, "partner_id")
		if err != nil {
			response.Error(c, err)
			return
		}
		from, err := optionalDate(c, "from")
		if err != nil {
			response.Error(c, err)
			return
		}
		to, err := optionalDate(c, "to")
		if err != nil {
			response.Error(c, err)
			return
		}
		pg := page.Parse(c.Query("page"), c.Query("size"))
		a := authctx.ActorFrom(ctx)
		var deptID, userID *int64
		if cfg.side == SideReceipt {
			deptID, userID = a.ScopeFilter()
		}
		status, keyword := httpx.QueryString(c, "status"), httpx.QueryString(c, "keyword")
		rows, err := m.store.ListSettlements(ctx, db.ListSettlementsParams{
			CompanyID: a.CompanyID, Side: cfg.side, Status: status, PartnerID: partnerID, Keyword: keyword,
			FromDate: from, ToDate: to, ScopeUserID: userID, ScopeDeptID: deptID, Lim: pg.Limit(), Off: pg.Offset(),
		})
		if err != nil {
			response.Error(c, err)
			return
		}
		total, err := m.store.CountSettlements(ctx, db.CountSettlementsParams{
			CompanyID: a.CompanyID, Side: cfg.side, Status: status, PartnerID: partnerID, Keyword: keyword,
			FromDate: from, ToDate: to, ScopeUserID: userID, ScopeDeptID: deptID,
		})
		if err != nil {
			response.Error(c, err)
			return
		}
		out := make([]settlementListDTO, len(rows))
		for i, r := range rows {
			out[i] = settlementListDTO{
				ID: r.ID, DocNo: r.DocNo, DocDate: r.DocDate.Format(time.DateOnly), PartnerCode: r.PartnerCode,
				PartnerName: r.PartnerName, SalesUserName: r.SalesUserName, Currency: r.Currency, Method: r.Method,
				Reference: r.Reference, Amount: r.Amount, Status: r.Status, Note: r.Note,
				CreatedByName: r.CreatedByName, UpdatedAt: r.UpdatedAt,
			}
		}
		response.List(c, out, pg.Meta(total))
	}
}

func (m *Module) getSettlement(cfg sideCfg) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := httpx.ParamID(c, "id")
		if err != nil {
			response.Error(c, err)
			return
		}
		dto, err := loadSettlement(c.Request.Context(), m.store.Queries, authctx.ActorFrom(c.Request.Context()), cfg, id)
		if err != nil {
			response.Error(c, err)
			return
		}
		response.OK(c, dto)
	}
}

// ---- 建立與修改(草稿) ----

type lineInput struct {
	TargetID int64           `json:"target_id" binding:"required"`
	Amount   decimal.Decimal `json:"amount"`
}

type settlementInput struct {
	DocDate   string      `json:"doc_date" binding:"required"`
	PartnerID int64       `json:"partner_id" binding:"required"`
	Currency  string      `json:"currency" binding:"required,len=3"`
	Method    string      `json:"method" binding:"required"`
	Reference string      `json:"reference" binding:"max=50"`
	Note      string      `json:"note" binding:"max=2000"`
	Lines     []lineInput `json:"lines" binding:"dive"`
	Version   int32       `json:"version"`
}

// target 沖帳目標(應收或應付)的共同欄位。
type target struct {
	id         int64
	partnerID  int64
	currency   string
	amount     decimal.Decimal
	baseAmount decimal.Decimal
	paid       decimal.Decimal
	sourceNo   string
}

func (t target) balance() decimal.Decimal { return t.amount.Sub(t.paid) }

// lockTargets 依 id 順序鎖定要沖的應收 / 應付(所有交易上鎖順序一致,避免死結)。
func lockTargets(ctx context.Context, q *db.Queries, cfg sideCfg, companyID int64, ids []int64) (map[int64]target, error) {
	slices.Sort(ids)
	out := map[int64]target{}
	if cfg.side == SideReceipt {
		rows, err := q.LockReceivables(ctx, db.LockReceivablesParams{CompanyID: companyID, Ids: ids})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.ID] = target{r.ID, r.CustomerID, r.Currency, r.Amount, r.BaseAmount, r.PaidAmount, r.SourceNo}
		}
		return out, nil
	}
	rows, err := q.LockPayables(ctx, db.LockPayablesParams{CompanyID: companyID, Ids: ids})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = target{r.ID, r.SupplierID, r.Currency, r.Amount, r.BaseAmount, r.PaidAmount, r.SourceNo}
	}
	return out, nil
}

// checkLines 驗證沖帳明細並回傳合計:目標存在、屬於同一對象與幣別、不重複;
// 金額非 0、與該筆未沖餘額同號(負數的退回 / 退出可與正數互抵)、不超過餘額、小數位不超過幣別。
// 錯誤依明細索引回報(lines.N);過帳時以同一函式在鎖定後重新檢查。
func checkLines(ctx context.Context, q *db.Queries, cfg sideCfg, companyID, partnerID int64, currency string, decimals int32, lines []lineInput) (decimal.Decimal, decimal.Decimal, map[int]string, error) {
	total, base := decimal.Zero, decimal.Zero
	errs := map[int]string{}
	if len(lines) == 0 {
		return total, base, errs, fieldErr("lines", "請選擇要沖帳的明細")
	}
	if len(lines) > trade.MaxLines {
		return total, base, errs, fieldErr("lines", fmt.Sprintf("明細最多 %d 筆", trade.MaxLines))
	}
	ids := make([]int64, len(lines))
	for i, l := range lines {
		ids[i] = l.TargetID
	}
	targets, err := lockTargets(ctx, q, cfg, companyID, ids)
	if err != nil {
		return total, base, errs, err
	}
	seen := map[int64]bool{}
	for i, l := range lines {
		t, ok := targets[l.TargetID]
		bal := t.balance()
		switch {
		case !ok:
			errs[i] = "找不到要沖帳的帳款"
		case seen[l.TargetID]:
			errs[i] = t.sourceNo + " 重複"
		case t.partnerID != partnerID:
			errs[i] = t.sourceNo + " 不屬於此對象"
		case t.currency != currency:
			errs[i] = fmt.Sprintf("%s 的幣別為 %s", t.sourceNo, t.currency)
		case l.Amount.IsZero():
			errs[i] = "沖帳金額不可為 0"
		case !l.Amount.Round(decimals).Equal(l.Amount): // 不用 Exponent:資料庫讀回的 3150.0000 指數為 -4
			errs[i] = fmt.Sprintf("金額最多 %d 位小數", decimals)
		case bal.IsZero() || bal.Sign() != l.Amount.Sign():
			errs[i] = fmt.Sprintf("%s 未沖餘額為 %s,沖帳金額須同號", t.sourceNo, bal.String())
		case l.Amount.Abs().GreaterThan(bal.Abs()):
			errs[i] = fmt.Sprintf("超過 %s 的未沖餘額 %s", t.sourceNo, bal.String())
		default:
			total = total.Add(l.Amount)
			// 本位幣沖帳金額 = 原幣沖帳 × 該筆帳款的本位幣 / 原幣比例(沖帳不計匯兌損益,D45)
			if !t.amount.IsZero() {
				base = base.Add(money.Amount(l.Amount.Mul(t.baseAmount).Div(t.amount)))
			}
		}
		seen[l.TargetID] = true
	}
	return total, base, errs, nil
}

func lineErrors(errs map[int]string) *apperr.Error {
	fields := map[string]string{}
	for i, msg := range errs {
		fields[fmt.Sprintf("lines.%d", i)] = msg
	}
	return apperr.Validation(fields)
}

type preparedSettlement struct {
	date        time.Time
	customerID  *int64
	supplierID  *int64
	salesUserID *int64
	total       decimal.Decimal
}

func prepareSettlement(ctx context.Context, q *db.Queries, a *authctx.Actor, cfg sideCfg, in *settlementInput) (preparedSettlement, error) {
	var p preparedSettlement
	var err error
	in.Currency = strings.ToUpper(in.Currency)
	in.Reference = strings.TrimSpace(in.Reference)
	in.Note = strings.TrimSpace(in.Note)
	if p.date, err = trade.ParseDate("doc_date", in.DocDate); err != nil {
		return p, err
	}
	if !methodLabels[in.Method] {
		return p, fieldErr("method", "付款方式不正確")
	}
	cur, err := q.GetCurrency(ctx, in.Currency)
	if database.IsNoRows(err) || (err == nil && !cur.IsActive) {
		return p, fieldErr("currency", "幣別不存在或已停用")
	} else if err != nil {
		return p, err
	}
	if cfg.side == SideReceipt {
		cust, err := q.CustomerForDoc(ctx, db.CustomerForDocParams{ID: in.PartnerID, CompanyID: a.CompanyID})
		if database.IsNoRows(err) || (err == nil && (!cust.IsActive || !masterdata.CustomerVisible(a, cust.SalesUserID, cust.SalesDepartmentID))) {
			return p, fieldErr("partner_id", "客戶不存在、已停用或不在你的資料範圍內")
		} else if err != nil {
			return p, err
		}
		p.customerID, p.salesUserID = &in.PartnerID, cust.SalesUserID
	} else {
		sup, err := q.SupplierForDoc(ctx, db.SupplierForDocParams{ID: in.PartnerID, CompanyID: a.CompanyID})
		if database.IsNoRows(err) || (err == nil && !sup.IsActive) {
			return p, fieldErr("partner_id", "供應商不存在或已停用")
		} else if err != nil {
			return p, err
		}
		p.supplierID = &in.PartnerID
	}
	total, _, errs, err := checkLines(ctx, q, cfg, a.CompanyID, in.PartnerID, in.Currency, int32(cur.Decimals), in.Lines)
	if err != nil {
		return p, err
	}
	if len(errs) > 0 {
		return p, lineErrors(errs)
	}
	if total.IsNegative() {
		return p, fieldErr("lines", "沖帳合計不可為負數")
	}
	p.total = total
	return p, nil
}

func saveSettlementLines(ctx context.Context, q *db.Queries, cfg sideCfg, id int64, lines []lineInput) error {
	if err := q.DeleteSettlementLines(ctx, id); err != nil {
		return err
	}
	for i, l := range lines {
		t := l.TargetID
		p := db.AddSettlementLineParams{SettlementID: id, LineNo: int32(i + 1), Amount: l.Amount}
		if cfg.side == SideReceipt {
			p.ReceivableID = &t
		} else {
			p.PayableID = &t
		}
		if err := q.AddSettlementLine(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) createSettlement(cfg sideCfg) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in settlementInput
		if err := httpx.BindJSON(c, &in); err != nil {
			response.Error(c, err)
			return
		}
		ctx := c.Request.Context()
		a := authctx.ActorFrom(ctx)
		var dto settlementDTO
		err := m.store.InTx(ctx, func(q *db.Queries) error {
			p, err := prepareSettlement(ctx, q, a, cfg, &in)
			if err != nil {
				return err
			}
			no, err := docno.Next(ctx, q, a.CompanyID, cfg.numbering, p.date)
			if err != nil {
				return err
			}
			s, err := q.CreateSettlement(ctx, db.CreateSettlementParams{
				CompanyID: a.CompanyID, Side: cfg.side, DocNo: no, DocDate: p.date, CustomerID: p.customerID,
				SupplierID: p.supplierID, SalesUserID: p.salesUserID, Currency: in.Currency, Method: in.Method,
				Reference: in.Reference, Amount: p.total, Note: in.Note, CreatedBy: &a.UserID,
			})
			if err != nil {
				return err
			}
			if err := saveSettlementLines(ctx, q, cfg, s.ID, in.Lines); err != nil {
				return err
			}
			if dto, err = loadSettlement(ctx, q, a, cfg, s.ID); err != nil {
				return err
			}
			return audit.Record(ctx, q, audit.Entry{
				Action: audit.Create, EntityType: "settlement", EntityID: &s.ID, Summary: "新增" + cfg.label + " " + no, After: dto,
			})
		})
		if err != nil {
			response.Error(c, err)
			return
		}
		response.Created(c, dto)
	}
}

func (m *Module) updateSettlement(cfg sideCfg) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := httpx.ParamID(c, "id")
		if err != nil {
			response.Error(c, err)
			return
		}
		var in settlementInput
		if err := httpx.BindJSON(c, &in); err != nil {
			response.Error(c, err)
			return
		}
		ctx := c.Request.Context()
		a := authctx.ActorFrom(ctx)
		var dto settlementDTO
		err = m.store.InTx(ctx, func(q *db.Queries) error {
			cur, err := q.LockSettlement(ctx, db.LockSettlementParams{ID: id, CompanyID: a.CompanyID})
			if database.IsNoRows(err) || (err == nil && cur.Side != cfg.side) {
				return apperr.ErrNotFound
			}
			if err != nil {
				return err
			}
			before, err := loadSettlement(ctx, q, a, cfg, id) // 含資料範圍檢查
			if err != nil {
				return err
			}
			if cur.Version != in.Version {
				return apperr.ErrVersionConflict
			}
			if !docstate.Editable(docstate.Status(cur.Status)) {
				return errSettlementEditNotAllowed
			}
			p, err := prepareSettlement(ctx, q, a, cfg, &in)
			if err != nil {
				return err
			}
			if _, err := q.UpdateSettlementHeader(ctx, db.UpdateSettlementHeaderParams{
				ID: id, CompanyID: a.CompanyID, DocDate: p.date, CustomerID: p.customerID, SupplierID: p.supplierID,
				SalesUserID: p.salesUserID, Currency: in.Currency, Method: in.Method, Reference: in.Reference,
				Amount: p.total, Note: in.Note, Version: in.Version, UpdatedBy: &a.UserID,
			}); err != nil {
				if database.IsNoRows(err) {
					return apperr.ErrVersionConflict
				}
				return err
			}
			if err := saveSettlementLines(ctx, q, cfg, id, in.Lines); err != nil {
				return err
			}
			if dto, err = loadSettlement(ctx, q, a, cfg, id); err != nil {
				return err
			}
			return audit.Record(ctx, q, audit.Entry{
				Action: audit.Update, EntityType: "settlement", EntityID: &id,
				Summary: "修改" + cfg.label + " " + cur.DocNo, Before: before, After: dto,
			})
		})
		if err != nil {
			response.Error(c, err)
			return
		}
		response.OK(c, dto)
	}
}

// ---- 狀態動作 ----

type actionInput struct {
	Version int32 `json:"version" binding:"required"`
}

func actionPermission(cfg sideCfg, action docstate.Action, status docstate.Status) (string, bool) {
	switch action {
	case docstate.Submit:
		return cfg.write, true
	case docstate.Approve, docstate.Reject, docstate.Unapprove:
		return cfg.approve, true
	case docstate.Post, docstate.Unpost:
		return cfg.post, true
	case docstate.Void:
		if status == docstate.Draft {
			return cfg.write, true
		}
		return cfg.approve, true
	default:
		return "", false
	}
}

func (m *Module) settlementAction(cfg sideCfg) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := httpx.ParamID(c, "id")
		if err != nil {
			response.Error(c, err)
			return
		}
		var in actionInput
		if err := httpx.BindJSON(c, &in); err != nil {
			response.Error(c, err)
			return
		}
		action := docstate.Action(c.Param("action"))
		ctx := c.Request.Context()
		a := authctx.ActorFrom(ctx)
		var dto settlementDTO
		err = m.store.InTx(ctx, func(q *db.Queries) error {
			cur, err := q.LockSettlement(ctx, db.LockSettlementParams{ID: id, CompanyID: a.CompanyID})
			if database.IsNoRows(err) || (err == nil && cur.Side != cfg.side) {
				return apperr.ErrNotFound
			}
			if err != nil {
				return err
			}
			doc, err := loadSettlement(ctx, q, a, cfg, id) // 含資料範圍檢查
			if err != nil {
				return err
			}
			perm, ok := actionPermission(cfg, action, docstate.Status(cur.Status))
			if !ok {
				return apperr.ErrNotFound
			}
			if !a.Can(perm) {
				return apperr.ErrForbidden
			}
			if cur.Version != in.Version {
				return apperr.ErrVersionConflict
			}
			next, err := trade.PostingTransition(docstate.Status(cur.Status), action)
			if err != nil {
				return err
			}
			if err := applySettlementAction(ctx, q, a, cfg, cur, doc, action); err != nil {
				return err
			}
			if _, err := q.SetSettlementStatus(ctx, db.SetSettlementStatusParams{
				ID: id, CompanyID: a.CompanyID, Status: string(next), Action: string(action), ActorID: a.UserID,
				Version: in.Version,
			}); err != nil {
				return err
			}
			if dto, err = loadSettlement(ctx, q, a, cfg, id); err != nil {
				return err
			}
			return audit.Record(ctx, q, audit.Entry{
				Action: string(action), EntityType: "settlement", EntityID: &id,
				Summary: trade.ActionLabels[action] + cfg.label + " " + cur.DocNo,
				Before:  map[string]string{"status": cur.Status}, After: map[string]string{"status": string(next)},
			})
		})
		if err != nil {
			response.Error(c, err)
			return
		}
		response.OK(c, dto)
	}
}

// addPaid 依沖帳明細更新應收 / 應付的已沖金額(sign 為 +1 過帳、-1 反過帳)。
func addPaid(ctx context.Context, q *db.Queries, cfg sideCfg, lines []settlementLineDTO, sign int64) error {
	for _, l := range lines {
		delta := l.Amount.Mul(decimal.NewFromInt(sign))
		var err error
		if cfg.side == SideReceipt {
			err = q.AddReceivablePaid(ctx, db.AddReceivablePaidParams{ID: l.TargetID, Delta: delta})
		} else {
			err = q.AddPayablePaid(ctx, db.AddPayablePaidParams{ID: l.TargetID, Delta: delta})
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func applySettlementAction(ctx context.Context, q *db.Queries, a *authctx.Actor, cfg sideCfg, cur db.Settlement, doc settlementDTO, action docstate.Action) error {
	switch action {
	case docstate.Submit, docstate.Approve:
		if len(doc.Lines) == 0 {
			return fieldErr("lines", "請選擇要沖帳的明細")
		}
	case docstate.Post:
		cu, err := q.GetCurrency(ctx, cur.Currency)
		if err != nil {
			return err
		}
		lines := make([]lineInput, len(doc.Lines))
		for i, l := range doc.Lines {
			lines[i] = lineInput{TargetID: l.TargetID, Amount: l.Amount}
		}
		// 鎖定帳款後重新檢查餘額(草稿期間可能已被其他收付款沖掉)
		_, base, errs, err := checkLines(ctx, q, cfg, a.CompanyID, doc.PartnerID, cur.Currency, int32(cu.Decimals), lines)
		if err != nil {
			return err
		}
		if len(errs) > 0 {
			msgs := make([]string, 0, len(errs))
			details := map[string]string{}
			for i, l := range doc.Lines {
				if msg, ok := errs[i]; ok {
					msgs = append(msgs, fmt.Sprintf("第 %d 行 %s", l.LineNo, msg))
					details[fmt.Sprintf("lines.%d", i)] = msg
				}
			}
			return apperr.New(http.StatusUnprocessableEntity, errSettlementCannotPost, "無法過帳:"+strings.Join(msgs, ";")).WithDetails(details)
		}
		if err := addPaid(ctx, q, cfg, doc.Lines, 1); err != nil {
			return err
		}
		return gl.PostSource(ctx, q, gl.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}, gl.Source{
			Type: glSourceType(cfg), ID: cur.ID, No: cur.DocNo, Date: cur.DocDate, Desc: cfg.label + " " + cur.DocNo,
		}, settleEntries(cfg, cur, base))
	case docstate.Unpost:
		ids := make([]int64, len(doc.Lines))
		for i, l := range doc.Lines {
			ids[i] = l.TargetID
		}
		if _, err := lockTargets(ctx, q, cfg, a.CompanyID, ids); err != nil {
			return err
		}
		if err := addPaid(ctx, q, cfg, doc.Lines, -1); err != nil {
			return err
		}
		return gl.ReverseSource(ctx, q, gl.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}, glSourceType(cfg), cur.ID)
	}
	return nil
}

// ---- 路由 ----

func (m *Module) registerSettlements(g *gin.RouterGroup, path string, cfg sideCfg) {
	read := auth.Require(cfg.read...)
	g.GET(path, read, m.listSettlements(cfg))
	g.GET(path+"/:id", read, m.getSettlement(cfg))
	g.POST(path, auth.Require(cfg.write), m.createSettlement(cfg))
	g.PUT(path+"/:id", auth.Require(cfg.write), m.updateSettlement(cfg))
	g.POST(path+"/:id/actions/:action", read, m.settlementAction(cfg))
}

func glSourceType(cfg sideCfg) string {
	if cfg.side == SideReceipt {
		return "collection"
	}
	return "payment"
}

// settleEntries 收款:借 現金 / 銀行存款,貸 應收帳款;付款:借 應付帳款,貸 現金 / 銀行存款。
// 金額為所沖帳款的本位幣金額(不計匯兌損益),淨額為 0 時不產生傳票。
func settleEntries(cfg sideCfg, cur db.Settlement, base decimal.Decimal) []gl.Entry {
	cash := gl.SettleKey(cur.Method)
	if cfg.side == SideReceipt {
		return []gl.Entry{
			{Key: cash, Debit: base},
			{Key: "sales.receivable", Credit: base, CustomerID: cur.CustomerID},
		}
	}
	return []gl.Entry{
		{Key: "purchase.payable", Debit: base, SupplierID: cur.SupplierID},
		{Key: cash, Credit: base},
	}
}
