// Package approval 多層簽核(D61)。
//
// 規則依單據類型與本位幣含稅金額決定:第 1 層永遠是「具該單據核准權限的人」(與原本相同),
// 規則可再追加第 2 層以後要指定的角色。送審時依規則把流程快照到 document_approvals,
// 之後修改規則不影響進行中的單據;沒有套用任何規則的單據沒有快照列,維持單層核准。
package approval

import (
	"context"
	"fmt"
	"net/http"

	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/docstate"
	"erp/internal/system/permission"
)

// 單據類型。同一張表上的子類型(進貨 / 進貨退出、報價 / 訂單、出貨 / 銷貨退回)共用一組規則。
const (
	PurchaseOrder = "purchase_order"
	GoodsReceipt  = "goods_receipt"
	SalesOrder    = "sales_order"
	Delivery      = "delivery"
	Collection    = "collection"
	Payment       = "payment"
	WorkOrder     = "work_order" // 金額為加工費(完工成本在月結才確定)
)

type docInfo struct {
	Label   string
	Approve string // 第 1 層需要的核准權限
	Read    string // 檢視進度需要的權限
}

var docTypes = map[string]docInfo{
	PurchaseOrder: {"採購單", permission.PurchaseOrderApprove, permission.PurchaseOrderRead},
	GoodsReceipt:  {"進貨單 / 進貨退出", permission.ReceiptApprove, permission.ReceiptRead},
	SalesOrder:    {"報價單 / 訂單", permission.SalesOrderApprove, permission.SalesOrderRead},
	Delivery:      {"出貨單 / 銷貨退回", permission.DeliveryApprove, permission.DeliveryRead},
	Collection:    {"收款單", permission.CollectionApprove, permission.CollectionRead},
	Payment:       {"付款單", permission.PaymentApprove, permission.PaymentRead},
	WorkOrder:     {"工單(以加工費計)", permission.WorkOrderApprove, permission.WorkOrderRead},
}

// DocTypeOrder 固定的顯示順序。
var DocTypeOrder = []string{PurchaseOrder, GoodsReceipt, SalesOrder, Delivery, Collection, Payment, WorkOrder}

func Label(docType string) string { return docTypes[docType].Label }

type authCtx = context.Context

var (
	errNotThisRole = func(step int, role string) *apperr.Error {
		return apperr.New(http.StatusForbidden, "APR-001", fmt.Sprintf("此單據的第 %d 層核准須由「%s」角色執行", step, role))
	}
	errSamePerson = apperr.New(http.StatusForbidden, "APR-002", "同一個人不可核准同一張單據的多個層級,請由其他人進行下一層核准")
)

// Intercept 在單據動作通過狀態與權限檢查之後、實際改變狀態之前呼叫;amount 為本位幣含稅金額。
//
//   - 送審:依規則快照出簽核流程(沒有適用規則則不建立,維持單層核准);
//   - 核准:完成目前這一層。還有後續層級時回傳 partial=true,呼叫端不可改變單據狀態,只需回傳最新資料;
//   - 退回 / 取消核准 / 作廢:清除流程。
//
// 其他動作不處理。必須在鎖定單據列之後、同一個交易中呼叫。
func Intercept(ctx authCtx, q *db.Queries, a *authctx.Actor, docType string, docID int64, action docstate.Action,
	amount decimal.Decimal) (partial bool, msg string, err error) {
	switch action {
	case docstate.Submit:
		return false, "", snapshot(ctx, q, a.CompanyID, docType, docID, amount)
	case docstate.Reject, docstate.Unapprove, docstate.Void:
		return false, "", q.DeleteDocumentApprovals(ctx, db.DeleteDocumentApprovalsParams{CompanyID: a.CompanyID, DocType: docType, DocID: docID})
	case docstate.Approve:
		return advance(ctx, q, a, docType, docID)
	}
	return false, "", nil
}

func snapshot(ctx authCtx, q *db.Queries, companyID int64, docType string, docID int64, amount decimal.Decimal) error {
	// 重新送審時重建,避免殘留舊流程
	if err := q.DeleteDocumentApprovals(ctx, db.DeleteDocumentApprovalsParams{CompanyID: companyID, DocType: docType, DocID: docID}); err != nil {
		return err
	}
	ruleID, err := q.MatchApprovalRule(ctx, db.MatchApprovalRuleParams{CompanyID: companyID, DocType: docType, Amount: amount})
	if database.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	steps, err := q.ListApprovalRuleSteps(ctx, companyID)
	if err != nil {
		return err
	}
	if err := q.InsertDocumentApproval(ctx, db.InsertDocumentApprovalParams{
		CompanyID: companyID, DocType: docType, DocID: docID, StepNo: 1, RoleName: "核准權限",
	}); err != nil {
		return err
	}
	for _, s := range steps {
		if s.RuleID != ruleID {
			continue
		}
		role := s.RoleID
		if err := q.InsertDocumentApproval(ctx, db.InsertDocumentApprovalParams{
			CompanyID: companyID, DocType: docType, DocID: docID, StepNo: s.StepNo, RoleID: &role, RoleName: s.RoleName,
		}); err != nil {
			return err
		}
	}
	return nil
}

func advance(ctx authCtx, q *db.Queries, a *authctx.Actor, docType string, docID int64) (bool, string, error) {
	rows, err := q.ListDocumentApprovals(ctx, db.ListDocumentApprovalsParams{CompanyID: a.CompanyID, DocType: docType, DocID: docID})
	if err != nil {
		return false, "", err
	}
	if len(rows) == 0 {
		return false, "", nil // 單層核准
	}
	cur := -1
	for i, r := range rows {
		if r.ApproverID == nil {
			cur = i
			break
		}
		if *r.ApproverID == a.UserID {
			return false, "", errSamePerson
		}
	}
	if cur < 0 {
		return false, "", nil // 流程已走完(不應發生),交由呼叫端完成核准
	}
	step := rows[cur]
	if step.RoleID != nil && !a.IsSuperadmin {
		ok, err := q.UserHasRole(ctx, db.UserHasRoleParams{UserID: a.UserID, RoleID: *step.RoleID})
		if err != nil {
			return false, "", err
		}
		if !ok {
			return false, "", errNotThisRole(int(step.StepNo), step.RoleName)
		}
	}
	// 角色已被刪除(role_id 為 NULL 且不是第 1 層):只有超級管理員能核准
	if step.RoleID == nil && step.StepNo > 1 && !a.IsSuperadmin {
		return false, "", errNotThisRole(int(step.StepNo), step.RoleName+"(已刪除)")
	}
	if n, err := q.CompleteDocumentApproval(ctx, db.CompleteDocumentApprovalParams{
		ApproverID: &a.UserID, DocType: docType, DocID: docID, StepNo: step.StepNo,
	}); err != nil {
		return false, "", err
	} else if n != 1 {
		return false, "", apperr.ErrVersionConflict
	}
	if cur == len(rows)-1 {
		return false, "", nil // 最後一層 → 呼叫端完成核准
	}
	next := rows[cur+1]
	return true, fmt.Sprintf("第 %d/%d 層核准,等待第 %d 層(%s)", step.StepNo, len(rows), next.StepNo, next.RoleName), nil
}
