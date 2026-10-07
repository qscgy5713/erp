package app

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"erp/internal/db"
)

// 預載資料的 id(000003 migration 依序插入)
func (e *env) unitID(code string) int64 {
	e.t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(), "SELECT id FROM units WHERE code = $1", code).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

type idVer struct {
	ID      int64 `json:"id"`
	Version int32 `json:"version"`
}

func TestItemsUnitsAndValidation(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	pcs, box, dozen := e.unitID("PCS"), e.unitID("BOX"), e.unitID("DOZ")

	item := map[string]any{
		"code": "a-001", "name": "原子筆", "item_type": "goods", "base_unit_id": pcs, "barcode": "4710000000001",
		"safety_stock": "10", "list_price": "12.5",
		"units": []map[string]any{{"unit_id": box, "factor": "144"}, {"unit_id": dozen, "factor": 12}},
	}
	res := c.do(http.MethodPost, "/masterdata/items", item)
	expect(t, res, http.StatusCreated, "")
	created := decode[struct {
		idVer
		Code      string `json:"code"`
		ListPrice string `json:"list_price"`
		Units     []struct {
			UnitID int64  `json:"unit_id"`
			Factor string `json:"factor"`
		} `json:"units"`
	}](t, res.Data)
	if created.Code != "A-001" || created.ListPrice != "12.5" || len(created.Units) != 2 {
		t.Fatalf("created = %+v", created)
	}

	expect(t, c.do(http.MethodPost, "/masterdata/items", item), http.StatusConflict, "ITEM-001")
	item["code"] = "A-002"
	expect(t, c.do(http.MethodPost, "/masterdata/items", item), http.StatusConflict, "ITEM-002")

	item["barcode"] = nil
	item["units"] = []map[string]any{{"unit_id": pcs, "factor": "1"}}
	res = c.do(http.MethodPost, "/masterdata/items", item)
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	if res.Error.Details["units.0"] != "不可與基本單位相同" {
		t.Fatalf("details = %v", res.Error.Details)
	}
	item["units"] = []map[string]any{{"unit_id": box, "factor": "0"}}
	expect(t, c.do(http.MethodPost, "/masterdata/items", item), http.StatusUnprocessableEntity, "SYS-422")
	item["units"] = []map[string]any{}
	item["category_id"] = 99999
	res = c.do(http.MethodPost, "/masterdata/items", item)
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	if res.Error.Details["category_id"] == "" {
		t.Fatalf("應指出分類不存在: %v", res.Error.Details)
	}
	item["category_id"] = nil
	item["list_price"] = "-1"
	expect(t, c.do(http.MethodPost, "/masterdata/items", item), http.StatusUnprocessableEntity, "SYS-422")

	// 修改:移除換算單位;用舊版本再改一次應衝突
	upd := map[string]any{
		"code": "A-001", "name": "原子筆(藍)", "item_type": "goods", "base_unit_id": pcs,
		"safety_stock": "0", "list_price": "13", "units": []any{}, "is_active": true, "version": created.Version,
	}
	res = c.do(http.MethodPut, "/masterdata/items/"+itoa(created.ID), upd)
	expect(t, res, http.StatusOK, "")
	expect(t, c.do(http.MethodPut, "/masterdata/items/"+itoa(created.ID), upd), http.StatusConflict, "SYS-409")
	got := decode[struct {
		Name  string            `json:"name"`
		Units []json.RawMessage `json:"units"`
	}](t, c.do(http.MethodGet, "/masterdata/items/"+itoa(created.ID), nil).Data)
	if got.Name != "原子筆(藍)" || len(got.Units) != 0 {
		t.Fatalf("got = %+v", got)
	}
}

func TestCategoryTreeAndItemFilter(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)

	root := decode[idVer](t, c.do(http.MethodPost, "/masterdata/item-categories", map[string]any{"code": "office", "name": "文具"}).Data)
	child := decode[idVer](t, c.do(http.MethodPost, "/masterdata/item-categories", map[string]any{
		"code": "pen", "name": "筆", "parent_id": root.ID,
	}).Data)
	expect(t, c.do(http.MethodPut, "/masterdata/item-categories/"+itoa(root.ID), map[string]any{
		"code": "OFFICE", "name": "文具", "parent_id": child.ID, "is_active": true, "version": root.Version,
	}), http.StatusUnprocessableEntity, "SYS-422")

	pcs := e.unitID("PCS")
	for code, cat := range map[string]*int64{"P1": &child.ID, "P2": &root.ID, "P3": nil} {
		expect(t, c.do(http.MethodPost, "/masterdata/items", map[string]any{
			"code": code, "name": code, "item_type": "goods", "base_unit_id": pcs, "category_id": cat,
			"safety_stock": "0", "list_price": "0",
		}), http.StatusCreated, "")
	}
	// 篩選上層分類應包含下層分類的料品
	meta := decode[struct {
		Total int64 `json:"total"`
	}](t, c.do(http.MethodGet, "/masterdata/items?category_id="+itoa(root.ID), nil).Meta)
	if meta.Total != 2 {
		t.Fatalf("上層分類應含 2 筆,got %d", meta.Total)
	}
}

func TestExchangeRatesLookup(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)

	for _, r := range []map[string]any{
		{"currency": "usd", "rate_date": "2026-10-01", "rate": "32.1"},
		{"currency": "USD", "rate_date": "2026-10-05", "rate": "32.45"},
	} {
		expect(t, c.do(http.MethodPost, "/masterdata/exchange-rates", r), http.StatusCreated, "")
	}
	expect(t, c.do(http.MethodPost, "/masterdata/exchange-rates", map[string]any{
		"currency": "USD", "rate_date": "2026-10-05", "rate": "1",
	}), http.StatusConflict, "FX-001")
	expect(t, c.do(http.MethodPost, "/masterdata/exchange-rates", map[string]any{
		"currency": "TWD", "rate_date": "2026-10-05", "rate": "1",
	}), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, c.do(http.MethodPost, "/masterdata/exchange-rates", map[string]any{
		"currency": "USD", "rate_date": "2026-10-06", "rate": "0",
	}), http.StatusUnprocessableEntity, "SYS-422")

	lookup := func(q string) apiResp { return c.do(http.MethodGet, "/masterdata/exchange-rates/lookup?"+q, nil) }
	rate := func(res apiResp) string {
		return decode[struct {
			Rate string `json:"rate"`
		}](t, res.Data).Rate
	}
	if r := rate(lookup("currency=USD&date=2026-10-07")); r != "32.45" { // 取之前最近一筆
		t.Fatalf("10/07 應取 10/05 匯率,got %s", r)
	}
	if r := rate(lookup("currency=USD&date=2026-10-03")); r != "32.1" {
		t.Fatalf("10/03 應取 10/01 匯率,got %s", r)
	}
	if r := rate(lookup("currency=TWD&date=2026-10-03")); r != "1" {
		t.Fatalf("本位幣應為 1,got %s", r)
	}
	expect(t, lookup("currency=USD&date=2026-09-30"), http.StatusUnprocessableEntity, "FX-002")
}

func TestTaxTypeRules(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	expect(t, c.do(http.MethodPost, "/masterdata/tax-types", map[string]any{
		"code": "z1", "name": "零稅率但有稅率", "kind": "zero", "rate": "0.05",
	}), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, c.do(http.MethodPost, "/masterdata/tax-types", map[string]any{
		"code": "big", "name": "稅率 100%", "kind": "taxable", "rate": "1",
	}), http.StatusUnprocessableEntity, "SYS-422")
	expect(t, c.do(http.MethodPost, "/masterdata/tax-types", map[string]any{
		"code": "TX5", "name": "重複", "kind": "taxable", "rate": "0.05",
	}), http.StatusConflict, "TAX-001")
}

func TestCustomerValidationAndDataScope(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.seedUser("root", pw, true, false)
	dept, err := e.q.CreateDepartment(ctx, db.CreateDepartmentParams{CompanyID: 1, Code: "S1", Name: "業務一部"})
	if err != nil {
		t.Fatal(err)
	}
	selfRole := e.seedRole("SALES", "self", "masterdata.customer.read", "masterdata.customer.write")
	deptRole := e.seedRole("SALESMGR", "department", "masterdata.customer.read", "masterdata.customer.write")
	amy := e.seedUser("amy", pw, false, false, selfRole.ID)
	ben := e.seedUser("ben", pw, false, false, selfRole.ID)
	mgr := e.seedUser("mgr", pw, false, false, deptRole.ID)
	outsider := e.seedUser("zed", pw, false, false, selfRole.ID)
	for _, u := range []int64{amy.ID, ben.ID, mgr.ID} {
		if _, err := e.pool.Exec(ctx, "UPDATE users SET department_id = $1 WHERE id = $2", dept.ID, u); err != nil {
			t.Fatal(err)
		}
	}

	root := e.loggedIn("root", pw)
	cust := func(code string, sales *int64) map[string]any {
		return map[string]any{
			"code": code, "name": code + " 公司", "currency": "TWD", "credit_limit": "0", "sales_user_id": sales,
			"contacts":  []map[string]any{{"name": "王小明", "email": "ming@example.com"}},
			"addresses": []map[string]any{{"label": "公司", "address": "台北市", "is_default": true}},
		}
	}
	bad := cust("BAD", nil)
	bad["tax_id"] = "12345678"
	res := root.do(http.MethodPost, "/masterdata/customers", bad)
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	if res.Error.Details["tax_id"] == "" {
		t.Fatalf("應指出統編錯誤: %v", res.Error.Details)
	}
	bad["tax_id"] = "04595257"
	bad["addresses"] = []map[string]any{{"address": "a", "is_default": true}, {"address": "b", "is_default": true}}
	expect(t, root.do(http.MethodPost, "/masterdata/customers", bad), http.StatusUnprocessableEntity, "SYS-422")

	ids := map[string]int64{}
	for code, sales := range map[string]*int64{"C-AMY": &amy.ID, "C-BEN": &ben.ID, "C-ZED": &outsider.ID, "C-NONE": nil} {
		ids[code] = decode[idVer](t, root.do(http.MethodPost, "/masterdata/customers", cust(code, sales)).Data).ID
	}

	total := func(c *client) int64 {
		return decode[struct {
			Total int64 `json:"total"`
		}](t, c.do(http.MethodGet, "/masterdata/customers", nil).Meta).Total
	}
	a, m := e.loggedIn("amy", pw), e.loggedIn("mgr", pw)
	if n := total(a); n != 1 {
		t.Fatalf("本人範圍只應看到自己負責的 1 位客戶,got %d", n)
	}
	if n := total(m); n != 2 { // amy、ben 同部門;zed 無部門;未指定業務的不顯示
		t.Fatalf("部門範圍應看到 2 位,got %d", n)
	}
	if n := total(root); n != 4 {
		t.Fatalf("全部範圍應看到 4 位,got %d", n)
	}

	// 範圍外的客戶視同不存在
	expect(t, a.do(http.MethodGet, "/masterdata/customers/"+itoa(ids["C-BEN"]), nil), http.StatusNotFound, "SYS-404")
	expect(t, a.do(http.MethodGet, "/masterdata/customers/"+itoa(ids["C-AMY"]), nil), http.StatusOK, "")
	// 本人範圍不可把客戶指派給別人,也不可建立無負責業務的客戶
	expect(t, a.do(http.MethodPost, "/masterdata/customers", cust("C-X", &ben.ID)), http.StatusForbidden, "CUST-002")
	expect(t, a.do(http.MethodPost, "/masterdata/customers", cust("C-Y", nil)), http.StatusForbidden, "CUST-002")
	expect(t, a.do(http.MethodPost, "/masterdata/customers", cust("C-Z", &amy.ID)), http.StatusCreated, "")
	// 信用額度需另外的權限:沒有權限不可設定非 0 額度,也不可修改既有額度
	withCredit := cust("C-CR", &amy.ID)
	withCredit["credit_limit"] = "5000"
	expect(t, a.do(http.MethodPost, "/masterdata/customers", withCredit), http.StatusForbidden, "CUST-003")
	expect(t, root.do(http.MethodPost, "/masterdata/customers", withCredit), http.StatusCreated, "")
	amyCust := decode[struct {
		idVer
		CreditLimit string `json:"credit_limit"`
	}](t, a.do(http.MethodGet, "/masterdata/customers/"+itoa(ids["C-AMY"]), nil).Data)
	upd := cust("C-AMY", &amy.ID)
	upd["is_active"], upd["version"] = true, amyCust.Version
	upd["credit_limit"] = "999999"
	expect(t, a.do(http.MethodPut, "/masterdata/customers/"+itoa(ids["C-AMY"]), upd), http.StatusForbidden, "CUST-003")
	upd["credit_limit"] = amyCust.CreditLimit // 額度不變:可以修改其他欄位
	upd["phone"] = "02-0000"
	expect(t, a.do(http.MethodPut, "/masterdata/customers/"+itoa(ids["C-AMY"]), upd), http.StatusOK, "")
	// 部門範圍可指派給同部門,不可指派給部門外
	expect(t, m.do(http.MethodPost, "/masterdata/customers", cust("C-M1", &ben.ID)), http.StatusCreated, "")
	expect(t, m.do(http.MethodPost, "/masterdata/customers", cust("C-M2", &outsider.ID)), http.StatusForbidden, "CUST-002")
}

func TestDropdownListsNeedOnlyLogin(t *testing.T) {
	e := newEnv(t)
	e.seedUser("nobody", pw, false, false) // 沒有任何角色
	c := e.loggedIn("nobody", pw)
	for _, path := range []string{"/masterdata/units", "/masterdata/tax-types", "/masterdata/payment-terms",
		"/masterdata/currencies", "/masterdata/warehouses", "/masterdata/item-categories", "/system/user-options"} {
		expect(t, c.do(http.MethodGet, path, nil), http.StatusOK, "")
	}
	expect(t, c.do(http.MethodGet, "/masterdata/items", nil), http.StatusForbidden, "SYS-403")
	expect(t, c.do(http.MethodPost, "/masterdata/units", map[string]any{"code": "x", "name": "x"}), http.StatusForbidden, "SYS-403")
}
