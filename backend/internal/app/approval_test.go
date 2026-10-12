package app

import (
	"net/http"
	"testing"

	"erp/internal/system/permission"
)

type apprProgress struct {
	Required   int  `json:"required"`
	Current    int  `json:"current"`
	CanApprove bool `json:"can_approve"`
	Steps      []struct {
		Step         int    `json:"step"`
		RoleName     string `json:"role_name"`
		ApproverName string `json:"approver_name"`
		ActingFor    string `json:"acting_for"`
		Done         bool   `json:"done"`
	} `json:"steps"`
}

func (c *client) progress(docType string, id int64) apprProgress {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/approval/progress?doc_type="+docType+"&doc_id="+itoa(id), nil)
	expect(c.e.t, res, http.StatusOK, "")
	return decode[apprProgress](c.e.t, res.Data)
}

type inboxRow struct {
	DocType    string `json:"doc_type"`
	DocID      int64  `json:"doc_id"`
	Step       int    `json:"step"`
	TotalSteps int    `json:"total_steps"`
}

// inboxHas 回傳該使用者的「待我核准」中是否有指定單據,以及其目前層級。
func (c *client) inboxHas(docType string, id int64) (bool, inboxRow) {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/approval/inbox", nil)
	expect(c.e.t, res, http.StatusOK, "")
	for _, r := range decode[[]inboxRow](c.e.t, res.Data) {
		if r.DocType == docType && r.DocID == id {
			return true, r
		}
	}
	return false, inboxRow{}
}

func TestMultiLevelApproval(t *testing.T) {
	p := newPurCtx(t)
	e, root := p.e, p.c
	maker := e.seedRole("maker", "all", permission.PurchaseOrderRead, permission.PurchaseOrderWrite)
	l1 := e.seedRole("l1", "all", permission.PurchaseOrderRead, permission.PurchaseOrderApprove)
	cfo := e.seedRole("cfo", "all", permission.PurchaseOrderRead, permission.PurchaseOrderApprove)
	noPerm := e.seedRole("plain", "all", permission.PurchaseOrderRead)
	e.seedUser("maker", pw, false, false, maker.ID)
	e.seedUser("appr1", pw, false, false, l1.ID)
	e.seedUser("appr2", pw, false, false, l1.ID)
	e.seedUser("boss", pw, false, false, cfo.ID, l1.ID) // 同時有第 1 層與 CFO 角色
	e.seedUser("plain", pw, false, false, noPerm.ID)
	mk, a1, a2, boss := e.loggedIn("maker", pw), e.loggedIn("appr1", pw), e.loggedIn("appr2", pw), e.loggedIn("boss", pw)

	// ---- 規則管理 ----
	rule := func(c *client, body map[string]any) apiResp { return c.do(http.MethodPost, "/approval/rules", body) }
	expect(t, rule(e.loggedIn("plain", pw), map[string]any{"doc_type": "purchase_order", "min_amount": "0", "role_ids": []int64{cfo.ID}}), http.StatusForbidden, "SYS-403")
	for _, bad := range []map[string]any{
		{"doc_type": "nope", "min_amount": "0", "role_ids": []int64{cfo.ID}},
		{"doc_type": "purchase_order", "min_amount": "-1", "role_ids": []int64{cfo.ID}},
		{"doc_type": "purchase_order", "min_amount": "0", "role_ids": []int64{}},
		{"doc_type": "purchase_order", "min_amount": "0", "role_ids": []int64{cfo.ID, cfo.ID}},
		{"doc_type": "purchase_order", "min_amount": "0", "role_ids": []int64{noPerm.ID}}, // 沒有核准權限
		{"doc_type": "purchase_order", "min_amount": "0", "role_ids": []int64{99999}},
	} {
		expect(t, rule(root, bad), http.StatusUnprocessableEntity, "SYS-422")
	}
	res := rule(root, map[string]any{"doc_type": "purchase_order", "min_amount": "10000", "role_ids": []int64{cfo.ID}})
	expect(t, res, http.StatusCreated, "")
	ruleID := decode[struct {
		ID      int64 `json:"id"`
		Version int32 `json:"version"`
	}](t, res.Data)
	expect(t, rule(root, map[string]any{"doc_type": "purchase_order", "min_amount": "10000", "role_ids": []int64{cfo.ID}}), http.StatusConflict, "APR-010")
	// 角色被規則使用時不可刪除(spare 沒有任何使用者,所以擋下來的原因只可能是簽核規則)
	spare := e.seedRole("spare", "all", permission.PurchaseOrderRead, permission.PurchaseOrderApprove)
	sr := rule(root, map[string]any{"doc_type": "purchase_order", "min_amount": "900000", "role_ids": []int64{spare.ID}})
	expect(t, sr, http.StatusCreated, "")
	spareRule := decode[struct {
		ID int64 `json:"id"`
	}](t, sr.Data)
	expect(t, root.do(http.MethodDelete, "/system/roles/"+itoa(spare.ID), nil), http.StatusConflict, "SYS-021")
	expect(t, root.do(http.MethodDelete, "/approval/rules/"+itoa(spareRule.ID), nil), http.StatusNoContent, "")
	expect(t, root.do(http.MethodDelete, "/system/roles/"+itoa(spare.ID), nil), http.StatusNoContent, "")

	newPO := func(qty string) purDoc {
		d, res := mk.createPur(orders, p.header(map[string]any{
			"lines": []map[string]any{line(p.item, p.box, qty, "1200", nil)},
		}))
		expect(t, res, http.StatusCreated, "")
		return d
	}
	act := func(c *client, d *purDoc, action string) apiResp { return c.actPur(orders, d, action) }

	// ---- 金額低於門檻:維持單層核准 ----
	small := newPO("1") // 1200 + 稅 = 1260 < 10000
	expect(t, act(mk, &small, "submit"), http.StatusOK, "")
	if pr := mk.progress("purchase_order", small.ID); pr.Required != 0 {
		t.Fatalf("低於門檻應為單層,required=%d", pr.Required)
	}
	expect(t, act(a1, &small, "approve"), http.StatusOK, "")
	if small.Status != "approved" {
		t.Fatalf("單層核准後 status=%s", small.Status)
	}

	// ---- 超過門檻:兩層 ----
	big := newPO("10") // 12600
	expect(t, act(mk, &big, "submit"), http.StatusOK, "")
	pr := mk.progress("purchase_order", big.ID)
	if pr.Required != 2 || pr.Current != 1 || pr.Steps[1].RoleName != "cfo" {
		t.Fatalf("送審後流程 = %+v", pr)
	}
	if mk.progress("purchase_order", big.ID).CanApprove {
		t.Fatal("開單者沒有核准權限,不應能核准")
	}
	if !a1.progress("purchase_order", big.ID).CanApprove {
		t.Fatal("a1 應能核准第 1 層")
	}
	// 待我核准:第 1 層由有核准權限者看到;開單者、無權限者看不到
	if ok, r := a1.inboxHas("purchase_order", big.ID); !ok || r.Step != 1 || r.TotalSteps != 2 {
		t.Fatalf("a1 的收件匣應有第 1/2 層,got %v %+v", ok, r)
	}
	if ok, _ := mk.inboxHas("purchase_order", big.ID); ok {
		t.Fatal("開單者沒有核准權限,收件匣不應有")
	}
	if ok, _ := e.loggedIn("plain", pw).inboxHas("purchase_order", big.ID); ok {
		t.Fatal("無核准權限者收件匣不應有")
	}
	expect(t, act(a1, &big, "approve"), http.StatusOK, "") // 第 1 層
	// 第 2 層輪到 CFO 角色:a1 / a2 沒有該角色,boss 才有;且 a1 已核准過第 1 層
	if ok, _ := a1.inboxHas("purchase_order", big.ID); ok {
		t.Fatal("a1 已核准第 1 層,不應再出現")
	}
	if ok, _ := a2.inboxHas("purchase_order", big.ID); ok {
		t.Fatal("a2 沒有 CFO 角色,不應出現")
	}
	if ok, r := boss.inboxHas("purchase_order", big.ID); !ok || r.Step != 2 {
		t.Fatalf("boss 應在第 2 層看到,got %v %+v", ok, r)
	}
	if big.Status != "pending" {
		t.Fatalf("第 1 層核准後仍應為待審,status=%s", big.Status)
	}
	pr = a2.progress("purchase_order", big.ID)
	if pr.Current != 2 || pr.CanApprove { // a2 沒有 CFO 角色
		t.Fatalf("第 1 層後進度 = %+v", pr)
	}
	expect(t, act(a2, &big, "approve"), http.StatusForbidden, "APR-001") // 角色不符
	expect(t, act(a1, &big, "approve"), http.StatusForbidden, "APR-002") // 同一人不可核准兩層
	if a1.progress("purchase_order", big.ID).CanApprove {
		t.Fatal("已核准第 1 層的人不應能核准第 2 層")
	}
	expect(t, act(boss, &big, "approve"), http.StatusOK, "") // CFO
	if big.Status != "approved" {
		t.Fatalf("全部核准後 status=%s", big.Status)
	}
	if pr := mk.progress("purchase_order", big.ID); pr.Current != 0 || !pr.Steps[0].Done || !pr.Steps[1].Done || pr.Steps[1].ApproverName == "" {
		t.Fatalf("核准完成後進度 = %+v", pr)
	}

	// ---- 退回清除流程;重新送審重建;已送審的快照不受規則修改影響 ----
	d := newPO("10")
	expect(t, act(mk, &d, "submit"), http.StatusOK, "")
	expect(t, act(a1, &d, "approve"), http.StatusOK, "")
	expect(t, act(a2, &d, "reject"), http.StatusOK, "")
	if pr := mk.progress("purchase_order", d.ID); pr.Required != 0 {
		t.Fatalf("退回後應清除流程: %+v", pr)
	}
	expect(t, act(mk, &d, "submit"), http.StatusOK, "")
	expect(t, act(a1, &d, "approve"), http.StatusOK, "")
	expect(t, root.do(http.MethodDelete, "/approval/rules/"+itoa(ruleID.ID), nil), http.StatusNoContent, "")
	if pr := mk.progress("purchase_order", d.ID); pr.Required != 2 || pr.Current != 2 {
		t.Fatalf("刪除規則後進行中的單據應維持原流程: %+v", pr)
	}
	expect(t, act(a2, &d, "approve"), http.StatusForbidden, "APR-001")
	expect(t, act(boss, &d, "approve"), http.StatusOK, "") // boss 沒核准過這張單,有 CFO 角色 → 可核准第 2 層
	if d.Status != "approved" {
		t.Fatalf("status=%s", d.Status)
	}

	// ---- 同一人不可核准兩層(即使同時具備兩個身分,超級管理員也一樣);超級管理員可越過角色 ----
	expect(t, rule(root, map[string]any{"doc_type": "purchase_order", "min_amount": "10000", "role_ids": []int64{cfo.ID}}), http.StatusCreated, "")
	x := newPO("10")
	expect(t, act(mk, &x, "submit"), http.StatusOK, "")
	expect(t, act(boss, &x, "approve"), http.StatusOK, "")
	expect(t, act(boss, &x, "approve"), http.StatusForbidden, "APR-002")
	y := newPO("10")
	expect(t, act(mk, &y, "submit"), http.StatusOK, "")
	expect(t, act(root, &y, "approve"), http.StatusOK, "")
	expect(t, act(root, &y, "approve"), http.StatusForbidden, "APR-002")
	expect(t, act(boss, &y, "approve"), http.StatusOK, "") // 第 2 層由 CFO 完成
	if y.Status != "approved" {
		t.Fatalf("status=%s", y.Status)
	}
	// 取消核准會清除流程,重新送審後要重新走完全部層級
	expect(t, act(a2, &y, "unapprove"), http.StatusOK, "")
	if pr := mk.progress("purchase_order", y.ID); pr.Required != 0 {
		t.Fatalf("取消核准後應清除流程: %+v", pr)
	}
	// 作廢也會清除
	expect(t, act(mk, &y, "submit"), http.StatusOK, "")
	if pr := mk.progress("purchase_order", y.ID); pr.Required != 2 {
		t.Fatalf("重新送審應重建流程: %+v", pr)
	}
	expect(t, act(a2, &y, "void"), http.StatusOK, "")
	if pr := mk.progress("purchase_order", y.ID); pr.Required != 0 {
		t.Fatalf("作廢後應清除流程: %+v", pr)
	}

	// ---- 規則可修改(版本衝突、重複金額) ----
	res = rule(root, map[string]any{"doc_type": "purchase_order", "min_amount": "50000", "role_ids": []int64{cfo.ID, l1.ID}})
	expect(t, res, http.StatusCreated, "")
	r2 := decode[struct {
		ID      int64 `json:"id"`
		Version int32 `json:"version"`
	}](t, res.Data)
	upd := func(version int32, amount string) apiResp {
		return root.do(http.MethodPut, "/approval/rules/"+itoa(r2.ID), map[string]any{
			"doc_type": "purchase_order", "min_amount": amount, "role_ids": []int64{cfo.ID}, "version": version})
	}
	expect(t, upd(r2.Version, "10000"), http.StatusConflict, "APR-010")
	expect(t, upd(r2.Version+5, "60000"), http.StatusConflict, "SYS-409")
	expect(t, upd(r2.Version, "60000"), http.StatusOK, "")
}
