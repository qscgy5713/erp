package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// xlsx 依範本標題建立 Excel:header 為標題列,rows 為資料列。
func xlsx(t *testing.T, headers []string, rows ...[]string) []byte {
	t.Helper()
	f := excelize.NewFile()
	_ = f.SetSheetName("Sheet1", "匯入資料")
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue("匯入資料", cell, h)
	}
	for r, row := range rows {
		for i, v := range row {
			cell, _ := excelize.CoordinatesToCellName(i+1, r+2)
			_ = f.SetCellValue("匯入資料", cell, v)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type importResult struct {
	OK         bool   `json:"ok"`
	DryRun     bool   `json:"dry_run"`
	RowCount   int    `json:"row_count"`
	Summary    string `json:"summary"`
	ErrorCount int    `json:"error_count"`
	Errors     []struct {
		Row     int    `json:"row"`
		Column  string `json:"column"`
		Message string `json:"message"`
	} `json:"errors"`
	BatchID *int64 `json:"batch_id"`
}

// upload 以 multipart 上傳檔案;回傳原始回應與解析後的結果。
func (c *client) upload(typ, query string, file []byte) (apiResp, importResult) {
	c.e.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormFile("file", "test.xlsx")
	_, _ = part.Write(file)
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/imports/types/"+typ+"?"+query, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.token)
	rec := httptest.NewRecorder()
	c.e.r.ServeHTTP(rec, req)
	res := apiResp{status: rec.Code}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			c.e.t.Fatalf("回應不是 JSON: %s", rec.Body.String())
		}
	}
	var out importResult
	if res.status == http.StatusOK && res.Data != nil {
		out = decode[importResult](c.e.t, res.Data)
	}
	return res, out
}

func (r importResult) hasError(col, contains string) bool {
	for _, e := range r.Errors {
		if e.Column == col && strings.Contains(e.Message, contains) {
			return true
		}
	}
	return false
}

func (c *client) importHeaders(typ string) []string {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/imports/types", nil)
	expect(c.e.t, res, http.StatusOK, "")
	for _, ty := range decode[[]struct {
		Key     string   `json:"key"`
		Columns []string `json:"columns"`
	}](c.e.t, res.Data) {
		if ty.Key == typ {
			return ty.Columns
		}
	}
	c.e.t.Fatalf("沒有匯入類型 %s", typ)
	return nil
}

func TestImportMasters(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	e.seedUser("sales1", pw, false, false)

	// 範本可下載,標題與類型定義一致
	req := httptest.NewRequest(http.MethodGet, "/api/v1/imports/types/items/template", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	rec := httptest.NewRecorder()
	e.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "spreadsheetml") {
		t.Fatalf("template: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	f, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := f.GetRows("匯入資料")
	if len(rows) != 1 || rows[0][0] != "料號*" {
		t.Fatalf("範本資料頁應只有標題列: %+v", rows)
	}

	h := c.importHeaders("items")
	good := []string{"I-001", "原子筆", "藍", "", "商品", "PCS", "4710000000011", "TX5", "100", "15", ""}
	bad := []string{"I-001", "", "", "NOPE", "雜項", "XXX", "", "TX9", "-1", "abc", ""} // 重複、缺品名、各種錯誤
	svc := []string{"S-001", "運費", "", "", "服務", "PCS", "", "", "", "", ""}

	// 預檢:列出所有錯誤,不寫入
	res, out := c.upload("items", "dry_run=true", xlsx(t, h, good, bad, svc))
	expect(t, res, http.StatusOK, "")
	if out.OK || !out.DryRun || out.ErrorCount < 5 || !out.hasError("品名", "必填") || !out.hasError("分類代號", "不存在") ||
		!out.hasError("基本單位代號", "不存在") || !out.hasError("料號", "重複") {
		t.Fatalf("預檢結果 = %+v", out)
	}
	if n := e.count("items"); n != 0 {
		t.Fatalf("預檢不應寫入,items = %d", n)
	}
	// 有錯誤時正式匯入也整批不寫入
	_, out = c.upload("items", "", xlsx(t, h, good, bad))
	if out.OK || e.count("items") != 0 {
		t.Fatalf("有錯誤應整批不寫入: %+v", out)
	}
	// 標題被改過、非 Excel
	res, _ = c.upload("items", "", xlsx(t, append([]string{"料號*", "品名改過*"}, h[2:]...), good))
	expect(t, res, http.StatusUnprocessableEntity, "IMP-003")
	res, _ = c.upload("items", "", []byte("not an excel"))
	expect(t, res, http.StatusUnprocessableEntity, "IMP-001")

	// 全部正確:寫入並留下批次紀錄與稽核
	res, out = c.upload("items", "", xlsx(t, h, good, svc))
	expect(t, res, http.StatusOK, "")
	if !out.OK || out.BatchID == nil || !strings.Contains(out.Summary, "2") || e.count("items") != 2 {
		t.Fatalf("匯入結果 = %+v items=%d", out, e.count("items"))
	}
	// 同一份檔案再匯入:料號已存在
	_, out = c.upload("items", "", xlsx(t, h, good))
	if out.OK || !out.hasError("料號", "已存在") {
		t.Fatalf("重複匯入應被擋: %+v", out)
	}

	// 客戶:統編檢查碼、負責業務帳號、信用額度
	ch := c.importHeaders("customers")
	cust := func(code, taxID, sales, limit string) []string {
		return []string{code, code + " 公司", "", taxID, "", "", "TWD", "TX5", "M30", "王小明", "台北市", "", "", limit, sales}
	}
	_, out = c.upload("customers", "", xlsx(t, ch, cust("C-1", "12345678", "ghost", "-5")))
	if out.OK || !out.hasError("統一編號", "有效") || !out.hasError("負責業務帳號", "不存在") || !out.hasError("信用額度", "負數") {
		t.Fatalf("客戶預檢 = %+v", out)
	}
	_, out = c.upload("customers", "", xlsx(t, ch, cust("C-1", "04595257", "sales1", "500000"), cust("C-2", "", "", "")))
	if !out.OK || e.count("customers") != 2 {
		t.Fatalf("客戶匯入 = %+v", out)
	}
	var sales *int64
	var contacts string
	if err := e.pool.QueryRow(context.Background(), "SELECT sales_user_id, contacts::text FROM customers WHERE code = 'C-1'").Scan(&sales, &contacts); err != nil || sales == nil || !strings.Contains(contacts, "王小明") {
		t.Fatalf("customer = %v %s err=%v", sales, contacts, err)
	}

	// 供應商
	sh := c.importHeaders("suppliers")
	_, out = c.upload("suppliers", "", xlsx(t, sh, []string{"V-1", "文具行", "", "", "", "", "USD", "TX5", "M30", "", "", "", "第一銀行", "123"}))
	if !out.OK || e.count("suppliers") != 1 {
		t.Fatalf("供應商匯入 = %+v", out)
	}
	// 批次紀錄
	bat := decode[[]struct {
		ImportType string `json:"import_type"`
		Undoable   bool   `json:"undoable"`
	}](t, c.do(http.MethodGet, "/imports/batches", nil).Data)
	if len(bat) != 3 || bat[0].Undoable {
		t.Fatalf("batches = %+v", bat)
	}
}

func (e *env) count(table string) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestImportOpeningBalancesAndUndo(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	wh, item := e.seedWarehouse("MAIN", false), e.seedItem("P1", "goods")
	cust, sup := e.seedCustomer("C1", "0", nil), e.seedSupplier("S1", "TWD")
	_, _, _ = wh, cust, sup

	// ---- 期初庫存 ----
	sh := c.importHeaders("opening_stock")
	res, _ := c.upload("opening_stock", "", xlsx(t, sh, []string{"MAIN", "P1", "10", "100", ""}))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422") // 缺期初日期
	q := "date=2026-09-30"
	_, out := c.upload("opening_stock", q, xlsx(t, sh,
		[]string{"NOPE", "P1", "10", "100", ""}, []string{"MAIN", "P1", "0", "100", ""}, []string{"MAIN", "P1", "5", "", ""}, []string{"MAIN", "GHOST", "1", "1", ""}))
	if out.OK || !out.hasError("倉庫代號", "不存在") || !out.hasError("數量", "大於 0") || !out.hasError("單位成本", "必填") || !out.hasError("料號", "不存在") {
		t.Fatalf("期初庫存預檢 = %+v", out)
	}
	_, out = c.upload("opening_stock", q, xlsx(t, sh, []string{"MAIN", "P1", "24", "100", ""}))
	if !out.OK || out.BatchID == nil || e.balance(item, wh) != "24" {
		t.Fatalf("期初庫存 = %+v balance=%s", out, e.balance(item, wh))
	}
	stockBatch := *out.BatchID
	// 已有庫存異動的料品不可再匯入期初
	_, out = c.upload("opening_stock", q, xlsx(t, sh, []string{"MAIN", "P1", "1", "1", ""}))
	if out.OK || !out.hasError("料號", "已有庫存異動") {
		t.Fatalf("應擋已有異動的料品: %+v", out)
	}

	// ---- 期初應收 / 應付 ----
	ah := c.importHeaders("opening_ar")
	_, out = c.upload("opening_ar", q, xlsx(t, ah,
		[]string{"GHOST", "INV-1", "2026-09-01", "", "TWD", "", "1000"},
		[]string{"C1", "INV-2", "2026-10-15", "", "TWD", "", "1000"},
		[]string{"C1", "INV-3", "2026-09-01", "", "USD", "", "100"}, // 查無匯率
		[]string{"C1", "INV-4", "不是日期", "", "TWD", "", "0"}))
	if out.OK || !out.hasError("客戶代號", "不存在") || !out.hasError("單據日期", "晚於期初日期") || !out.hasError("匯率", "查無") ||
		!out.hasError("單據日期", "格式") || !out.hasError("未沖金額", "大於 0") {
		t.Fatalf("期初應收預檢 = %+v", out)
	}
	_, out = c.upload("opening_ar", q, xlsx(t, ah,
		[]string{"C1", "INV-1", "2026-09-01", "2026-10-31", "TWD", "", "52500"},
		[]string{"C1", "INV-2", "2026/9/15", "", "TWD", "", "10500"}))
	if !out.OK || out.BatchID == nil {
		t.Fatalf("期初應收 = %+v", out)
	}
	arBatch := *out.BatchID
	_, out = c.upload("opening_ar", q, xlsx(t, ah, []string{"C1", "INV-1", "2026-09-01", "", "TWD", "", "1"}))
	if out.OK || !out.hasError("單據號碼", "已存在") {
		t.Fatalf("期初應收重複 = %+v", out)
	}
	ph := c.importHeaders("opening_ap")
	_, out = c.upload("opening_ap", q, xlsx(t, ph, []string{"S1", "BILL-1", "2026-09-05", "2026-10-05", "", "", "31500"}))
	if !out.OK {
		t.Fatalf("期初應付 = %+v", out)
	}
	apBatch := *out.BatchID

	// 期初應收可正常收款沖帳
	ids := c.arIDs()
	rc, r := c.createSettle("/finance/collections", settleBody(cust, sl(ids[0], "2500")))
	expect(t, r, http.StatusCreated, "")
	c.approveSettle("/finance/collections", &rc)
	expect(t, c.actSettle("/finance/collections", &rc, "post"), http.StatusOK, "")
	// 已沖帳的期初應收不可撤銷
	expect(t, c.do(http.MethodPost, "/imports/batches/"+itoa(arBatch)+"/undo", nil), http.StatusConflict, "FIN-002")
	expect(t, c.actSettle("/finance/collections", &rc, "unpost"), http.StatusOK, "")
	expect(t, c.actSettle("/finance/collections", &rc, "void"), http.StatusOK, "")

	// ---- 期初科目餘額 ----
	bh := c.importHeaders("opening_balance")
	acct := func(code string) int64 { return e.idByCode("accounts", code) }
	_ = acct
	_, out = c.upload("opening_balance", q, xlsx(t, bh,
		[]string{"1141", "2400", "", ""}, []string{"1131", "63000", "", ""}, []string{"2101", "", "31500", ""}, []string{"3101", "", "30000", ""}))
	if out.OK || !out.hasError("", "借貸合計不相等") {
		t.Fatalf("不平衡應被擋: %+v", out)
	}
	_, out = c.upload("opening_balance", q, xlsx(t, bh,
		[]string{"1141", "2400", "", ""}, []string{"1131", "", "", ""}, []string{"1100", "5", "", ""}, []string{"1141", "1", "", ""}, []string{"3101", "", "2400", ""}))
	if out.OK || !out.hasError("借方金額", "擇一") || !out.hasError("科目代號", "不存在") || !out.hasError("科目代號", "重複") {
		t.Fatalf("科目餘額預檢 = %+v", out)
	}
	_, out = c.upload("opening_balance", q, xlsx(t, bh,
		[]string{"1141", "2400", "", "存貨"}, []string{"1131", "63000", "", "應收"}, []string{"2101", "", "31500", "應付"}, []string{"3101", "", "33900", "股本"}))
	if !out.OK || out.BatchID == nil {
		t.Fatalf("期初科目餘額 = %+v", out)
	}
	balBatch := *out.BatchID
	// 不可重複匯入
	_, out = c.upload("opening_balance", q, xlsx(t, bh, []string{"1101", "1", "", ""}, []string{"3101", "", "1", ""}))
	if out.OK || !out.hasError("", "已匯入過") {
		t.Fatalf("重複匯入科目餘額應被擋: %+v", out)
	}

	// 試算表(期初日之後的期間)期初平衡;應收 / 應付子帳與總帳對得起來
	tb := decode[struct {
		Rows []struct {
			Code    string `json:"code"`
			Opening string `json:"opening"`
		} `json:"rows"`
		Balanced bool `json:"balanced"`
	}](t, c.do(http.MethodGet, "/gl/reports/trial-balance?from=2026-10-01&to=2026-10-31", nil).Data)
	open := map[string]string{}
	for _, r := range tb.Rows {
		open[r.Code] = r.Opening
	}
	if !tb.Balanced || open["1131"] != "63000" || open["2101"] != "-31500" || open["3101"] != "-33900" {
		t.Fatalf("試算表 = %+v", tb)
	}
	rec := decode[struct {
		Checks []struct {
			Key    string `json:"key"`
			Status string `json:"status"`
		} `json:"checks"`
	}](t, c.do(http.MethodGet, "/costing/reconcile", nil).Data)
	for _, ch := range rec.Checks {
		if (ch.Key == "receivable" || ch.Key == "payable" || ch.Key == "stock") && ch.Status != "ok" {
			t.Fatalf("對帳 %s = %s", ch.Key, ch.Status)
		}
	}

	// 月結:期初庫存視同進貨(24 個 @100),9 月月結後存貨金額 2400
	res = c.do(http.MethodPost, "/costing/closings/2026-09/run", nil)
	expect(t, res, http.StatusOK, "")
	if d := decode[costingDTO](t, res.Data); d.InventoryValue != "2400" || d.CogsAmount != "0" || d.AdjustAmount != "0" {
		t.Fatalf("月結 = %+v", d)
	}
	// 月結後期初庫存不可撤銷(鎖定),取消月結後可以
	expect(t, c.do(http.MethodPost, "/imports/batches/"+itoa(stockBatch)+"/undo", nil), http.StatusConflict, "INV-008")
	expect(t, c.do(http.MethodPost, "/costing/closings/2026-09/cancel", nil), http.StatusOK, "")

	// ---- 撤銷 ----
	expect(t, c.do(http.MethodPost, "/imports/batches/"+itoa(stockBatch)+"/undo", nil), http.StatusOK, "")
	if b := e.balance(item, wh); b != "0" {
		t.Fatalf("撤銷後現有量 = %s", b)
	}
	expect(t, c.do(http.MethodPost, "/imports/batches/"+itoa(stockBatch)+"/undo", nil), http.StatusConflict, "IMP-007")
	expect(t, c.do(http.MethodPost, "/imports/batches/"+itoa(arBatch)+"/undo", nil), http.StatusOK, "")
	expect(t, c.do(http.MethodPost, "/imports/batches/"+itoa(apBatch)+"/undo", nil), http.StatusOK, "")
	if ar, _ := c.receivables(); len(ar) != 0 {
		t.Fatalf("撤銷後應收 = %+v", ar)
	}
	expect(t, c.do(http.MethodPost, "/imports/batches/"+itoa(balBatch)+"/undo", nil), http.StatusOK, "")
	wantTrial(t, c.trialRange("2026-09-01", "2026-09-30"), "1141", "2400", "2400") // 沖銷後淨額為 0
	// 撤銷後可重新匯入科目餘額
	_, out = c.upload("opening_balance", q, xlsx(t, bh, []string{"1101", "10", "", ""}, []string{"3101", "", "10", ""}))
	if !out.OK {
		t.Fatalf("撤銷後應可重新匯入: %+v", out)
	}
	// 主檔批次不支援撤銷
	_, out = c.upload("items", "", xlsx(t, c.importHeaders("items"), []string{"X1", "測試", "", "", "", "PCS", "", "", "", "", ""}))
	expect(t, c.do(http.MethodPost, "/imports/batches/"+itoa(*out.BatchID)+"/undo", nil), http.StatusConflict, "IMP-006")
}

func TestImportPermissions(t *testing.T) {
	e := newEnv(t)
	e.seedUser("nobody", pw, false, false)
	role := e.seedRole("IMPORTER", "all", "system.import.run")
	e.seedUser("importer", pw, false, false, role.ID)
	nb, im := e.loggedIn("nobody", pw), e.loggedIn("importer", pw)
	expect(t, nb.do(http.MethodGet, "/imports/types", nil), http.StatusForbidden, "SYS-403")
	expect(t, nb.do(http.MethodGet, "/imports/batches", nil), http.StatusForbidden, "SYS-403")
	expect(t, im.do(http.MethodGet, "/imports/types", nil), http.StatusOK, "")
	expect(t, im.do(http.MethodGet, "/imports/types/nope/template", nil), http.StatusNotFound, "SYS-404")
	res, _ := nb.upload("items", "", xlsx(t, im.importHeaders("items")))
	expect(t, res, http.StatusForbidden, "SYS-403")
	res, _ = im.upload("items", "", xlsx(t, im.importHeaders("items"))) // 只有標題列
	expect(t, res, http.StatusUnprocessableEntity, "IMP-002")
}

// 壓縮炸彈:5 MB 以內的 xlsx 展開後可能是數 GB;須在讀取前就拒絕,不能吃光記憶體。
func TestImportRejectsZipBomb(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	good := xlsx(t, c.importHeaders("items"), []string{"X1", "測試", "", "", "", "PCS", "", "", "", "", ""})

	// 把正常檔案的工作表 XML 換成「開頭正常、後面接 300 MB 空白」的版本(壓縮後很小)
	zr, err := zip.NewReader(bytes.NewReader(good), int64(len(good)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		w, _ := zw.Create(f.Name)
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") {
			data = append(bytes.TrimSuffix(data, []byte("</worksheet>")), bytes.Repeat([]byte(" "), 300<<20)...)
			data = append(data, []byte("</worksheet>")...)
		}
		_, _ = w.Write(data)
	}
	_ = zw.Close()
	if out.Len() > 5<<20 {
		t.Fatalf("測試檔壓縮後 %d 位元組,應小於 5 MB", out.Len())
	}
	start := time.Now()
	res, _ := c.upload("items", "dry_run=true", out.Bytes())
	expect(t, res, http.StatusUnprocessableEntity, "IMP-001")
	if time.Since(start) > 10*time.Second {
		t.Fatalf("拒絕太慢: %v", time.Since(start))
	}
}
