package app

import (
	"net/http"
	"testing"
	"time"

	"erp/internal/system/permission"
)

// TestApprovalDelegationAndStockDocs 庫存單據走多層簽核,並以代理人代為核准(D87)。
func TestApprovalDelegationAndStockDocs(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	root := e.loggedIn("root", pw)
	wh, item := e.seedWarehouse("A", false), e.seedItem("P1", "goods")

	maker := e.seedRole("maker", "all", permission.InventoryRead, permission.InventoryWrite)
	l1 := e.seedRole("l1", "all", permission.InventoryRead, permission.InventoryApprove)
	cfo := e.seedRole("cfo", "all", permission.InventoryRead, permission.InventoryApprove)
	plain := e.seedRole("plain", "all", permission.InventoryRead)
	e.seedUser("maker", pw, false, false, maker.ID)
	apprU := e.seedUser("appr", pw, false, false, l1.ID)
	bossU := e.seedUser("boss", pw, false, false, cfo.ID, l1.ID)
	depU := e.seedUser("deputy", pw, false, false, plain.ID)
	mk, ap, boss, dep := e.loggedIn("maker", pw), e.loggedIn("appr", pw), e.loggedIn("boss", pw), e.loggedIn("deputy", pw)

	expect(t, root.do(http.MethodPost, "/approval/rules", map[string]any{
		"doc_type": "stock_document", "min_amount": "0", "role_ids": []int64{cfo.ID},
	}), http.StatusCreated, "")

	newDoc := func() stockDoc {
		d, res := mk.createDoc(adjust(wh, item, e.unitID("PCS"), "5"))
		expect(t, res, http.StatusCreated, "")
		expect(t, mk.act(&d, "submit"), http.StatusOK, "")
		return d
	}

	// ---- 庫存單據兩層簽核 ----
	d := newDoc()
	if pr := mk.progress("stock_document", d.ID); pr.Required != 2 || pr.Current != 1 {
		t.Fatalf("送審後流程 = %+v", pr)
	}
	if ok, _ := ap.inboxHas("stock_document", d.ID); !ok {
		t.Fatal("有核准權限者的收件匣應有庫存單據")
	}
	if ok, _ := dep.inboxHas("stock_document", d.ID); ok {
		t.Fatal("代理人尚未被委託,收件匣不應有")
	}
	expect(t, dep.act(&d, "approve"), http.StatusForbidden, "SYS-403")

	// ---- 代理人 ----
	day := func(off int) string { return time.Now().AddDate(0, 0, off).Format(time.DateOnly) }
	deputyID := depU.ID
	for _, bad := range []map[string]any{
		{"delegate_id": apprU.ID, "valid_from": day(-1), "valid_to": day(2)},                           // 代理自己
		{"delegate_id": deputyID, "valid_from": day(2), "valid_to": day(-1)},                           // 迄日早於起日
		{"delegate_id": deputyID, "valid_from": "x", "valid_to": day(2)},                               // 格式
		{"delegate_id": 99999, "valid_from": day(-1), "valid_to": day(2)},                              // 不存在
		{"delegate_id": deputyID, "delegator_id": bossU.ID, "valid_from": day(-1), "valid_to": day(2)}, // 替別人設定需權限
	} {
		res := ap.do(http.MethodPost, "/approval/delegations", bad)
		if res.status != http.StatusUnprocessableEntity && res.status != http.StatusForbidden {
			t.Fatalf("%v 應被拒絕,實際 %d", bad, res.status)
		}
	}
	res := ap.do(http.MethodPost, "/approval/delegations", map[string]any{"delegate_id": deputyID, "valid_from": day(-1), "valid_to": day(2)})
	expect(t, res, http.StatusCreated, "")
	del := decode[struct {
		ID int64 `json:"id"`
	}](t, res.Data)

	if ok, r := dep.inboxHas("stock_document", d.ID); !ok || r.Step != 1 {
		t.Fatalf("代理期間內代理人收件匣應有第 1 層,got %v %+v", ok, r)
	}
	expect(t, dep.act(&d, "approve"), http.StatusOK, "") // 代 appr 核准第 1 層
	if d.Status != "pending" {
		t.Fatalf("第 1 層核准後仍應待審,status=%s", d.Status)
	}
	pr := mk.progress("stock_document", d.ID)
	if pr.Current != 2 || pr.Steps[0].ApproverName == "" || pr.Steps[0].ActingFor == "" {
		t.Fatalf("應記錄代理核准,got %+v", pr.Steps)
	}
	// 委託人本人不可再核准第 2 層(同一個人);代理人沒有 cfo 角色
	expect(t, ap.act(&d, "approve"), http.StatusForbidden, "APR-002")
	expect(t, dep.act(&d, "approve"), http.StatusForbidden, "APR-002")
	expect(t, boss.act(&d, "approve"), http.StatusOK, "")
	if d.Status != "approved" {
		t.Fatalf("兩層核准後 status=%s", d.Status)
	}

	// ---- 管理:列表、刪除 ----
	list := decode[[]struct {
		ID int64 `json:"id"`
	}](t, dep.do(http.MethodGet, "/approval/delegations", nil).Data)
	if len(list) != 1 || list[0].ID != del.ID {
		t.Fatalf("代理人也應看到與自己有關的代理,got %+v", list)
	}
	if l := decode[[]struct{}](t, boss.do(http.MethodGet, "/approval/delegations", nil).Data); len(l) != 0 {
		t.Fatal("與自己無關的代理不應列出")
	}
	expect(t, boss.do(http.MethodDelete, "/approval/delegations/"+itoa(del.ID), nil), http.StatusNotFound, "SYS-404")
	expect(t, ap.do(http.MethodDelete, "/approval/delegations/"+itoa(del.ID), nil), http.StatusNoContent, "")

	// 終止後代理人不再有權限
	d2 := newDoc()
	if ok, _ := dep.inboxHas("stock_document", d2.ID); ok {
		t.Fatal("代理終止後收件匣不應有")
	}
	expect(t, dep.act(&d2, "approve"), http.StatusForbidden, "SYS-403")

	// 已過期的代理無效
	expect(t, ap.do(http.MethodPost, "/approval/delegations", map[string]any{
		"delegate_id": deputyID, "valid_from": day(-10), "valid_to": day(-5),
	}), http.StatusCreated, "")
	expect(t, dep.act(&d2, "approve"), http.StatusForbidden, "SYS-403")
}
