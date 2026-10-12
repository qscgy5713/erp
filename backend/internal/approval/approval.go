// Package approval 多層簽核(D61)。
//
// 規則依單據類型、送審者部門(D96)與本位幣含稅金額決定:第 1 層永遠是「具該單據核准權限的人」(與原本相同),
// 規則可再追加第 2 層以後要指定的角色。送審時依規則把流程快照到 document_approvals,
// 之後修改規則不影響進行中的單據;沒有套用任何規則的單據沒有快照列,維持單層核准。
package approval

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/notify"
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
	WorkOrder     = "work_order"     // 金額為加工費(完工成本在月結才確定)
	StockDocument = "stock_document" // 調整 / 調撥 / 盤點;沒有金額,一律以 0 比對規則
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
	StockDocument: {"庫存單據(調整 / 調撥 / 盤點)", permission.InventoryApprove, permission.InventoryRead},
}

// DocTypeOrder 固定的顯示順序。
var DocTypeOrder = []string{PurchaseOrder, GoodsReceipt, SalesOrder, Delivery, Collection, Payment, WorkOrder, StockDocument}

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
		if err := snapshot(ctx, q, a.CompanyID, a.DepartmentID, docType, docID, amount); err != nil {
			return false, "", err
		}
		return false, "", notifyApprovers(ctx, q, a, docType, nil)
	case docstate.Reject, docstate.Unapprove, docstate.Void:
		return false, "", q.DeleteDocumentApprovals(ctx, db.DeleteDocumentApprovalsParams{CompanyID: a.CompanyID, DocType: docType, DocID: docID})
	case docstate.Approve:
		return advance(ctx, q, a, docType, docID)
	}
	return false, "", nil
}

// 簽法:或簽(任一格核准即過)/ 會簽(每一格都要核准)
const (
	ModeAny = "any"
	ModeAll = "all"
)

// slot 簽核格:一層可有多格(每個角色一格),第 1 層固定一格。
type slot struct {
	Step, No   int32
	Mode       string
	RoleID     *int64
	RoleName   string
	ApproverID *int64
	ActingFor  *int64
}

func (s slot) done() bool { return s.ApproverID != nil }

// layer 一個簽核層級。
type layer struct {
	No    int32
	Mode  string
	Slots []slot
}

// groupLayers 把依 (step, slot) 排序的簽核格依層級分組。
func groupLayers(slots []slot) []layer {
	var out []layer
	for _, s := range slots {
		if n := len(out); n == 0 || out[n-1].No != s.Step {
			out = append(out, layer{No: s.Step, Mode: s.Mode})
		}
		out[len(out)-1].Slots = append(out[len(out)-1].Slots, s)
	}
	return out
}

// doneCount 該層已核准的格數。
func (l layer) doneCount() int {
	n := 0
	for _, s := range l.Slots {
		if s.done() {
			n++
		}
	}
	return n
}

// done 或簽:任一格已核准;會簽:全部格都已核准。
func (l layer) done() bool {
	if l.Mode == ModeAll {
		return l.doneCount() == len(l.Slots)
	}
	return l.doneCount() > 0
}

// currentLayer 第一個尚未通過的層級;全部通過回傳 -1。
func currentLayer(layers []layer) int {
	for i, l := range layers {
		if !l.done() {
			return i
		}
	}
	return -1
}

// approvedByMe 這張單據是否已有格由我(或我代理的委託人)核准過;一人一票,不論層級與格。
func approvedByMe(a *authctx.Actor, layers []layer) bool {
	for _, l := range layers {
		for _, s := range l.Slots {
			if s.ApproverID != nil && (a.IsSelfOrDelegator(*s.ApproverID) || (s.ActingFor != nil && a.IsSelfOrDelegator(*s.ActingFor))) {
				return true
			}
		}
	}
	return false
}

// roleNames 未核准格的角色名稱,以「、」串接。
func openRoleNames(l layer) string {
	var names []string
	for _, s := range l.Slots {
		if !s.done() {
			names = append(names, s.RoleName)
		}
	}
	return strings.Join(names, "、")
}

// fillable 目前使用者能否填這一格,以及代理核准時的委託人(0 = 本人 / 不是代理)。
// holds 回傳使用者(含代理中的委託人)是否具有某角色:0 本人、>0 委託人 id、<0 沒有。
func fillable(a *authctx.Actor, approvePerm string, s slot, holds func(roleID int64) (int64, error)) (int64, bool, error) {
	switch {
	case s.RoleID == nil && s.Step == 1:
		return a.DelegatedPerms[approvePerm], true, nil // 第 1 層:具核准權限即可(呼叫端已檢查權限)
	case s.RoleID == nil:
		return 0, a.IsSuperadmin, nil // 角色已被刪除:只有超級管理員能核准
	case a.IsSuperadmin:
		return 0, true, nil
	}
	who, err := holds(*s.RoleID)
	return max(who, 0), who >= 0, err
}

func toSlots[T any](rows []T, conv func(T) slot) []slot {
	out := make([]slot, len(rows))
	for i, r := range rows {
		out[i] = conv(r)
	}
	return out
}

func snapshot(ctx authCtx, q *db.Queries, companyID int64, deptID *int64, docType string, docID int64, amount decimal.Decimal) error {
	// 重新送審時重建,避免殘留舊流程
	if err := q.DeleteDocumentApprovals(ctx, db.DeleteDocumentApprovalsParams{CompanyID: companyID, DocType: docType, DocID: docID}); err != nil {
		return err
	}
	ruleID, err := q.MatchApprovalRule(ctx, db.MatchApprovalRuleParams{CompanyID: companyID, DocType: docType, Amount: amount, DepartmentID: deptID})
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
		CompanyID: companyID, DocType: docType, DocID: docID, StepNo: 1, SlotNo: 1, Mode: ModeAny, RoleName: "核准權限",
	}); err != nil {
		return err
	}
	slotNo := map[int32]int32{}
	for _, s := range steps {
		if s.RuleID != ruleID {
			continue
		}
		slotNo[s.StepNo]++
		role := s.RoleID
		if err := q.InsertDocumentApproval(ctx, db.InsertDocumentApprovalParams{
			CompanyID: companyID, DocType: docType, DocID: docID, StepNo: s.StepNo, SlotNo: slotNo[s.StepNo], Mode: s.Mode,
			RoleID: &role, RoleName: s.RoleName,
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
	layers := groupLayers(toSlots(rows, func(r db.ListDocumentApprovalsRow) slot {
		return slot{Step: r.StepNo, No: r.SlotNo, Mode: r.Mode, RoleID: r.RoleID, RoleName: r.RoleName, ApproverID: r.ApproverID, ActingFor: r.ActingFor}
	}))
	if approvedByMe(a, layers) {
		return false, "", errSamePerson
	}
	ci := currentLayer(layers)
	if ci < 0 {
		return false, "", nil // 流程已走完(不應發生),交由呼叫端完成核准
	}
	cur := layers[ci]
	holds := func(roleID int64) (int64, error) { return roleHolder(ctx, q, a, roleID) }
	type cand struct {
		s         slot
		actingFor int64
		holders   int64
	}
	var cands []cand
	for _, s := range cur.Slots {
		if s.done() {
			continue
		}
		who, ok, err := fillable(a, docTypes[docType].Approve, s, holds)
		if err != nil {
			return false, "", err
		}
		if ok {
			cands = append(cands, cand{s: s, actingFor: who})
		}
	}
	if len(cands) == 0 {
		return false, "", errNotThisRole(int(cur.No), openRoleNames(cur))
	}
	// 會簽時我可能同時符合多格:優先填「其他人較少能填」的格,避免佔掉別人唯一能簽的格
	if len(cands) > 1 {
		for i := range cands {
			if cands[i].s.RoleID != nil {
				if cands[i].holders, err = q.RoleActiveHolderCount(ctx, db.RoleActiveHolderCountParams{RoleID: *cands[i].s.RoleID, ExceptUserID: a.UserID}); err != nil {
					return false, "", err
				}
			}
		}
		slices.SortStableFunc(cands, func(x, y cand) int { return int(x.holders - y.holders) })
	}
	pick := cands[0]
	var af *int64
	if pick.actingFor > 0 {
		af = &pick.actingFor
	}
	if n, err := q.CompleteDocumentApproval(ctx, db.CompleteDocumentApprovalParams{
		ApproverID: &a.UserID, ActingFor: af, DocType: docType, DocID: docID, StepNo: pick.s.Step, SlotNo: pick.s.No,
	}); err != nil {
		return false, "", err
	} else if n != 1 {
		return false, "", apperr.ErrVersionConflict
	}
	// 套用這次核准後,這一層是否通過
	for i := range cur.Slots {
		if cur.Slots[i].No == pick.s.No {
			cur.Slots[i].ApproverID = &a.UserID
		}
	}
	if !cur.done() {
		return true, fmt.Sprintf("第 %d 層會簽:已 %d/%d,尚缺 %s", cur.No, cur.doneCount(), len(cur.Slots), openRoleNames(cur)), nil
	}
	if ci == len(layers)-1 {
		return false, "", nil // 最後一層通過 → 呼叫端完成核准
	}
	next := layers[ci+1]
	var roleIDs []int64
	for _, s := range next.Slots {
		if s.RoleID != nil {
			roleIDs = append(roleIDs, *s.RoleID)
		}
	}
	if err := notifyApprovers(ctx, q, a, docType, roleIDs); err != nil {
		return false, "", err
	}
	mode := "或簽"
	if next.Mode == ModeAll {
		mode = "會簽"
	}
	return true, fmt.Sprintf("第 %d/%d 層核准,等待第 %d 層(%s:%s)", cur.No, len(layers), next.No, mode, strings.Join(slotNames(next), "、")), nil
}

func slotNames(l layer) []string {
	names := make([]string, len(l.Slots))
	for i, s := range l.Slots {
		names[i] = s.RoleName
	}
	return names
}

// roleHolder 目前使用者能否負責某角色的簽核層:本人具該角色回傳 0;只有代理中的委託人具該角色回傳委託人 id;
// 都沒有回傳 -1。
func roleHolder(ctx authCtx, q *db.Queries, a *authctx.Actor, roleID int64) (int64, error) {
	ok, err := q.UserHasRole(ctx, db.UserHasRoleParams{UserID: a.UserID, RoleID: roleID})
	if err != nil || ok {
		return 0, err
	}
	for _, d := range a.Delegators {
		if ok, err := q.UserHasRole(ctx, db.UserHasRoleParams{UserID: d, RoleID: roleID}); err != nil {
			return 0, err
		} else if ok {
			return d, nil
		}
	}
	return -1, nil
}

// notifyApprovers 單據送審或進到下一層時,通知「輪到核准」的人(Email,D88)。
// roleIDs 為空代表第 1 層(具該單據核准權限的人),否則為該層所有角色的成員(去重);都連同有效的代理人。
// 不通知本人;信件不含單據內容,只提示到「待我核准」查看。
func notifyApprovers(ctx authCtx, q *db.Queries, a *authctx.Actor, docType string, roleIDs []int64) error {
	if !notify.Enabled() {
		return nil
	}
	var ids []int64
	if len(roleIDs) > 0 {
		for _, rid := range roleIDs {
			got, err := q.ApproverIDsByRole(ctx, db.ApproverIDsByRoleParams{CompanyID: a.CompanyID, RoleID: rid})
			if err != nil {
				return err
			}
			ids = append(ids, got...)
		}
	} else {
		var err error
		ids, err = q.ApproverIDsByPermission(ctx, db.ApproverIDsByPermissionParams{CompanyID: a.CompanyID, Permission: docTypes[docType].Approve})
		if err != nil {
			return err
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	ids = slices.DeleteFunc(ids, func(id int64) bool { return id == a.UserID })
	label := docTypes[docType].Label
	return notify.Enqueue(ctx, q, a.CompanyID, ids, "approval_pending",
		"有「"+label+"」待你核准",
		"有一張「"+label+"」送審,輪到你(或你代理的人)核准。\n\n請到系統「待我核准」查看:"+notify.Link("/approval/inbox")+"\n\n(此為系統自動通知)", "")
}
