package app

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

type lotRow struct {
	LotID        int64  `json:"lot_id"`
	LotNo        string `json:"lot_no"`
	ExpiryDate   string `json:"expiry_date"`
	ExpiryStatus string `json:"expiry_status"`
	Qty          string `json:"qty"`
	WarehouseID  int64  `json:"warehouse_id"`
}

// lotQty 某批號在某倉庫的現有量(沒有紀錄為 "0")
func (c *client) lotQty(item, wh int64, lotNo string) string {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/inventory/lots?include_zero=true&item_id="+itoa(item)+"&warehouse_id="+itoa(wh)+"&size=100", nil)
	expect(c.e.t, res, http.StatusOK, "")
	for _, r := range decode[[]lotRow](c.e.t, res.Data) {
		if r.LotNo == lotNo {
			return r.Qty
		}
	}
	return "0"
}

func (e *env) setLotControl(item int64, mode string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), "UPDATE items SET lot_control = $1 WHERE id = $2", mode, item); err != nil {
		e.t.Fatal(err)
	}
}

func lotAdj(wh, item, unit int64, qty, lotNo, expiry string) map[string]any {
	l := map[string]any{"item_id": item, "unit_id": unit, "qty": qty}
	if lotNo != "" {
		l["lot_no"] = lotNo
	}
	if expiry != "" {
		l["expiry_date"] = expiry
	}
	return map[string]any{"doc_type": "adjustment", "doc_date": today, "warehouse_id": wh, "lines": []map[string]any{l}}
}

func (c *client) postLot(wh, item, unit int64, qty, lotNo, expiry string) apiResp {
	c.e.t.Helper()
	d, res := c.createDoc(lotAdj(wh, item, unit, qty, lotNo, expiry))
	if res.status != http.StatusCreated {
		return res
	}
	return c.postAll(&d)
}

func TestLotInboundValidationAndFEFO(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	milk := e.seedItem("MILK", "goods")
	e.setLotControl(milk, "lot_expiry")
	pcs := e.unitID("PCS")

	// ---- 開單檢查:入庫要批號,新批號要效期;沒有批號管理的料品不可填批號 ----
	for name, body := range map[string]map[string]any{
		"沒有批號":    lotAdj(s.wh, milk, pcs, "10", "", "2026-12-01"),
		"新批號沒有效期": lotAdj(s.wh, milk, pcs, "10", "A1", ""),
		"效期格式錯":   lotAdj(s.wh, milk, pcs, "10", "A1", "2026/12/01"),
		"批號含空白":   lotAdj(s.wh, milk, pcs, "10", "A 1", "2026-12-01"),
		"一般料品填批號": lotAdj(s.wh, s.item, pcs, "10", "A1", ""),
	} {
		if _, res := c.createDoc(body); res.status != http.StatusUnprocessableEntity || res.code() != "SYS-422" {
			t.Errorf("%s → %d %s", name, res.status, res.code())
		}
	}

	// ---- 入庫兩個批號:B 先到期、A 後到期(批號統一為大寫) ----
	expect(t, c.postLot(s.wh, milk, pcs, "60", "lot-a", "2026-12-01"), http.StatusOK, "")
	expect(t, c.postLot(s.wh, milk, pcs, "36", "LOT-B", "2026-10-20"), http.StatusOK, "")
	if e.balance(milk, s.wh) != "96" || c.lotQty(milk, s.wh, "LOT-A") != "60" || c.lotQty(milk, s.wh, "LOT-B") != "36" {
		t.Fatalf("入庫後 total=%s A=%s B=%s", e.balance(milk, s.wh), c.lotQty(milk, s.wh, "LOT-A"), c.lotQty(milk, s.wh, "LOT-B"))
	}
	// 同批號效期不同 → 過帳被擋(INV-011);批號已存在時效期可省略
	d, res := c.createDoc(lotAdj(s.wh, milk, pcs, "1", "LOT-A", "2027-01-01"))
	expect(t, res, http.StatusCreated, "")
	expect(t, c.postAllExpect(&d), http.StatusConflict, "INV-011")
	expect(t, c.postLot(s.wh, milk, pcs, "4", "LOT-A", ""), http.StatusOK, "") // LOT-A 變 64

	// ---- 批號庫存清單:依效期排序、效期狀態、即將到期篩選 ----
	list := decode[[]lotRow](t, c.do(http.MethodGet, "/inventory/lots?item_id="+itoa(milk), nil).Data)
	if len(list) != 2 || list[0].LotNo != "LOT-B" || list[1].LotNo != "LOT-A" {
		t.Fatalf("批號清單 = %+v", list)
	}
	expect(t, c.do(http.MethodGet, "/inventory/lots?expiry=bogus", nil), http.StatusUnprocessableEntity, "SYS-422")

	// ---- 出貨先到期先出:4 箱 = 48 → LOT-B 36 + LOT-A 12 ----
	dn, res := c.createPur(deliveries, s.header(map[string]any{
		"doc_type": "delivery", "lines": []map[string]any{line(milk, s.box, "4", "1000", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")
	if c.lotQty(milk, s.wh, "LOT-B") != "0" || c.lotQty(milk, s.wh, "LOT-A") != "52" || e.balance(milk, s.wh) != "52" {
		t.Fatalf("出貨後 A=%s B=%s total=%s", c.lotQty(milk, s.wh, "LOT-A"), c.lotQty(milk, s.wh, "LOT-B"), e.balance(milk, s.wh))
	}
	dnFull := decode[struct {
		Lines []struct {
			Lots []struct {
				LotNo string `json:"lot_no"`
				Qty   string `json:"qty"`
			} `json:"lots"`
		} `json:"lines"`
	}](t, c.do(http.MethodGet, deliveries+"/"+itoa(dn.ID), nil).Data)
	if got := dnFull.Lines[0].Lots; len(got) != 2 || got[0].LotNo != "LOT-B" || got[0].Qty != "36" || got[1].LotNo != "LOT-A" || got[1].Qty != "12" {
		t.Fatalf("出貨單批號分配 = %+v", got)
	}

	// ---- 指定批號出貨;庫存不足 ----
	dn2, res := c.createPur(deliveries, s.header(map[string]any{
		"doc_type": "delivery", "lines": []map[string]any{{"item_id": milk, "unit_id": s.box, "qty": "1", "unit_price": "1000", "lot_no": "lot-a"}},
	}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(deliveries, &dn2)
	expect(t, c.actPur(deliveries, &dn2, "post"), http.StatusOK, "")
	if c.lotQty(milk, s.wh, "LOT-A") != "40" {
		t.Fatalf("指定批號出貨後 A=%s", c.lotQty(milk, s.wh, "LOT-A"))
	}
	over, _ := c.createPur(deliveries, s.header(map[string]any{
		"doc_type": "delivery", "lines": []map[string]any{line(milk, s.box, "4", "1000", nil)}, // 48 > 40
	}))
	c.approvePur(deliveries, &over)
	if res := c.actPur(deliveries, &over, "post"); res.status != http.StatusUnprocessableEntity {
		t.Fatalf("超出庫存應被擋: %d %s", res.status, res.code())
	}

	// ---- 反過帳:批號庫存還原、對帳檢查通過 ----
	expect(t, c.actPur(deliveries, &dn, "unpost"), http.StatusOK, "")
	if c.lotQty(milk, s.wh, "LOT-B") != "36" || c.lotQty(milk, s.wh, "LOT-A") != "52" {
		t.Fatalf("反過帳後 A=%s B=%s", c.lotQty(milk, s.wh, "LOT-A"), c.lotQty(milk, s.wh, "LOT-B"))
	}
	assertReconcileLotsOK(t, c)
}

// postAllExpect 同 postAll,但最後一步失敗時回傳回應而不是中斷測試。
func (c *client) postAllExpect(d *stockDoc) apiResp { return c.postAll(d) }

func assertReconcileLotsOK(t *testing.T, c *client) {
	t.Helper()
	rec := decode[struct {
		Checks []struct {
			Key    string `json:"key"`
			Status string `json:"status"`
		} `json:"checks"`
	}](t, c.do(http.MethodGet, "/costing/reconcile", nil).Data)
	for _, ch := range rec.Checks {
		if ch.Key == "lots" {
			if ch.Status != "ok" {
				t.Fatalf("批號對帳檢查 = %s", ch.Status)
			}
			return
		}
	}
	t.Fatal("對帳檢查沒有批號項目")
}

func TestLotExpiredStockRules(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	milk := e.seedItem("MILK", "goods")
	e.setLotControl(milk, "lot_expiry")
	pcs := e.unitID("PCS")
	expect(t, c.postLot(s.wh, milk, pcs, "24", "OLD", "2026-09-01"), http.StatusOK, "") // 單據日 2026-10-07 時已過期
	expect(t, c.postLot(s.wh, milk, pcs, "12", "NEW", "2027-06-01"), http.StatusOK, "")

	ship := func(qty, lotNo string) apiResp {
		l := map[string]any{"item_id": milk, "unit_id": pcs, "qty": qty, "unit_price": "10"}
		if lotNo != "" {
			l["lot_no"] = lotNo
		}
		d, res := c.createPur(deliveries, s.header(map[string]any{"doc_type": "delivery", "lines": []map[string]any{l}}))
		expect(t, res, http.StatusCreated, "")
		c.approvePur(deliveries, &d)
		return c.actPur(deliveries, &d, "post")
	}
	// 先到期先出會略過已過期批號:只有 NEW 的 12 可出,要 20 → 不足,訊息說明有過期庫存
	res := ship("20", "")
	if res.status != http.StatusUnprocessableEntity || res.code() != "INV-012" || !strings.Contains(res.Error.Message, "過期") {
		t.Fatalf("略過過期批號: %d %s %+v", res.status, res.code(), res.Error)
	}
	// 指定過期批號出貨 → INV-013
	expect(t, ship("5", "OLD"), http.StatusUnprocessableEntity, "INV-013")
	// 沒指定時正常出未過期的
	expect(t, ship("10", ""), http.StatusOK, "")
	if c.lotQty(milk, s.wh, "NEW") != "2" || c.lotQty(milk, s.wh, "OLD") != "24" {
		t.Fatalf("NEW=%s OLD=%s", c.lotQty(milk, s.wh, "NEW"), c.lotQty(milk, s.wh, "OLD"))
	}
	// 報廢(調整減少)可以出已過期的批號,不指定批號時也會先出最早到期的
	expect(t, c.postLot(s.wh, milk, pcs, "-24", "OLD", ""), http.StatusOK, "")
	if c.lotQty(milk, s.wh, "OLD") != "0" {
		t.Fatalf("報廢後 OLD=%s", c.lotQty(milk, s.wh, "OLD"))
	}
	// 批號庫存不可為負(即使倉庫允許負庫存)
	if _, err := e.pool.Exec(context.Background(), "UPDATE warehouses SET allow_negative = TRUE WHERE id = $1", s.wh); err != nil {
		t.Fatal(err)
	}
	if res := c.postLot(s.wh, milk, pcs, "-50", "NEW", ""); res.status != http.StatusUnprocessableEntity || res.code() != "INV-012" {
		t.Fatalf("批號庫存不可為負: %d %s", res.status, res.code())
	}
	assertReconcileLotsOK(t, c)
}

func TestLotTransferCountAndTrace(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	milk := e.seedItem("MILK", "goods")
	e.setLotControl(milk, "lot_expiry")
	pcs := e.unitID("PCS")
	wh2 := e.seedWarehouse("B", false)
	expect(t, c.postLot(s.wh, milk, pcs, "30", "LA", "2027-02-01"), http.StatusOK, "")
	expect(t, c.postLot(s.wh, milk, pcs, "20", "LB", "2027-01-01"), http.StatusOK, "")

	// ---- 調撥沿用批號:先到期先出,LB 20 + LA 5 → 目的倉有兩個批號 ----
	tr, res := c.createDoc(map[string]any{
		"doc_type": "transfer", "doc_date": today, "warehouse_id": s.wh, "to_warehouse_id": wh2,
		"lines": []map[string]any{{"item_id": milk, "unit_id": pcs, "qty": "25"}},
	})
	expect(t, res, http.StatusCreated, "")
	expect(t, c.postAll(&tr), http.StatusOK, "")
	if c.lotQty(milk, wh2, "LB") != "20" || c.lotQty(milk, wh2, "LA") != "5" || c.lotQty(milk, s.wh, "LA") != "25" || c.lotQty(milk, s.wh, "LB") != "0" {
		t.Fatalf("調撥後 B倉 LB=%s LA=%s,A倉 LA=%s", c.lotQty(milk, wh2, "LB"), c.lotQty(milk, wh2, "LA"), c.lotQty(milk, s.wh, "LA"))
	}
	if e.balance(milk, wh2) != "25" || e.balance(milk, s.wh) != "25" {
		t.Fatalf("調撥後總量 %s / %s", e.balance(milk, wh2), e.balance(milk, s.wh))
	}
	expect(t, c.act(&tr, "unpost"), http.StatusOK, "") // 反過帳還原到原批號
	if c.lotQty(milk, s.wh, "LB") != "20" || c.lotQty(milk, wh2, "LA") != "0" {
		t.Fatalf("調撥反過帳後 LB=%s", c.lotQty(milk, s.wh, "LB"))
	}

	// ---- 盤點:批號管理的料品每個批號一行;實盤差異依批號調整 ----
	cd, res := c.createDoc(map[string]any{"doc_type": "count", "doc_date": today, "warehouse_id": s.wh})
	expect(t, res, http.StatusCreated, "")
	full := decode[struct {
		Version int32 `json:"version"`
		Lines   []struct {
			ItemID    int64   `json:"item_id"`
			LotNo     string  `json:"lot_no"`
			SystemQty *string `json:"system_qty"`
		} `json:"lines"`
	}](t, c.do(http.MethodGet, "/inventory/documents/"+itoa(cd.ID), nil).Data)
	var lines []map[string]any
	for _, l := range full.Lines {
		if l.ItemID != milk {
			lines = append(lines, map[string]any{"item_id": l.ItemID, "unit_id": e.unitID("PCS"), "qty": *l.SystemQty})
			continue
		}
		counted := map[string]string{"LA": "28", "LB": "18"}[l.LotNo] // LA 盤盈 −2?:帳面 30 → 28 盤虧 2;LB 帳面 20 → 18 盤虧 2
		if counted == "" {
			t.Fatalf("盤點行出現未預期的批號 %q", l.LotNo)
		}
		lines = append(lines, map[string]any{"item_id": milk, "unit_id": pcs, "qty": counted, "lot_no": l.LotNo})
	}
	expect(t, c.do(http.MethodPut, "/inventory/documents/"+itoa(cd.ID), map[string]any{
		"doc_type": "count", "doc_date": today, "warehouse_id": s.wh, "lines": lines, "version": full.Version}), http.StatusOK, "")
	cd, _ = c.createDocReload(cd.ID)
	expect(t, c.postAll(&cd), http.StatusOK, "")
	if c.lotQty(milk, s.wh, "LA") != "28" || c.lotQty(milk, s.wh, "LB") != "18" || e.balance(milk, s.wh) != "46" {
		t.Fatalf("盤點後 LA=%s LB=%s total=%s", c.lotQty(milk, s.wh, "LA"), c.lotQty(milk, s.wh, "LB"), e.balance(milk, s.wh))
	}

	// ---- 出貨後的批號追溯:LA 從調整入庫 → 調撥 → 出貨給 C1 ----
	dn, res := c.createPur(deliveries, s.header(map[string]any{
		"doc_type": "delivery", "lines": []map[string]any{{"item_id": milk, "unit_id": pcs, "qty": "8", "unit_price": "10", "lot_no": "LA"}},
	}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")
	lotID := int64(0)
	for _, r := range decode[[]lotRow](t, c.do(http.MethodGet, "/inventory/lots?item_id="+itoa(milk)+"&warehouse_id="+itoa(s.wh), nil).Data) {
		if r.LotNo == "LA" {
			lotID = r.LotID
		}
	}
	trace := decode[struct {
		LotNo string `json:"lot_no"`
		Moves []struct {
			SourceType string `json:"source_type"`
			Qty        string `json:"qty"`
			Partner    string `json:"partner"`
			Balance    string `json:"balance"`
		} `json:"moves"`
	}](t, c.do(http.MethodGet, "/inventory/lots/"+itoa(lotID)+"/ledger", nil).Data)
	last := trace.Moves[len(trace.Moves)-1]
	if trace.LotNo != "LA" || last.SourceType != "delivery" || last.Qty != "-8" || last.Partner != "C1 公司" || last.Balance != "20" {
		t.Fatalf("批號追溯 = %+v", trace)
	}
	expect(t, c.do(http.MethodGet, "/inventory/lots/999999/ledger", nil), http.StatusNotFound, "SYS-404")
	assertReconcileLotsOK(t, c)

	// ---- 儀表板效期警示 ----
	dash := decode[map[string]any](t, c.do(http.MethodGet, "/dashboard", nil).Data)
	if _, ok := dash["expiry"]; !ok {
		t.Fatalf("儀表板應有效期警示卡片: %v", dash)
	}
}

func (c *client) createDocReload(id int64) (stockDoc, apiResp) {
	res := c.do(http.MethodGet, "/inventory/documents/"+itoa(id), nil)
	return decode[stockDoc](c.e.t, res.Data), res
}

func TestLotPurchaseReturnAndSalesReturn(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	milk := e.seedItem("MILK", "goods")
	e.setLotControl(milk, "lot_expiry")
	p := &purCtx{e: e, c: c, wh: s.wh, sup: e.seedSupplier("S1", "TWD"), tax: s.tax, item: milk, box: s.box}

	// 進貨:批號必填、新批號須效期
	_, res := c.createPur(receipts, p.header(map[string]any{"doc_type": "receipt", "lines": []map[string]any{line(milk, s.box, "2", "100", nil)}}))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	r1, res := c.createPur(receipts, p.header(map[string]any{"doc_type": "receipt", "lines": []map[string]any{
		{"item_id": milk, "unit_id": s.box, "qty": "5", "unit_price": "100", "lot_no": "r-1", "expiry_date": "2027-05-01"}}}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(receipts, &r1)
	expect(t, c.actPur(receipts, &r1, "post"), http.StatusOK, "")
	if c.lotQty(milk, s.wh, "R-1") != "60" {
		t.Fatalf("進貨後 R-1=%s", c.lotQty(milk, s.wh, "R-1"))
	}

	// 進貨退出沒填批號:沿用被退進貨明細的批號
	r1Full := decode[purDoc](t, c.do(http.MethodGet, receipts+"/"+itoa(r1.ID), nil).Data)
	ret, res := c.createPur(receipts, p.header(map[string]any{"doc_type": "return", "lines": []map[string]any{
		line(milk, s.box, "1", "100", map[string]any{"receipt_line_id": r1Full.Lines[0].ID})}}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(receipts, &ret)
	expect(t, c.actPur(receipts, &ret, "post"), http.StatusOK, "")
	if c.lotQty(milk, s.wh, "R-1") != "48" {
		t.Fatalf("進貨退出後 R-1=%s", c.lotQty(milk, s.wh, "R-1"))
	}

	// 出貨 3 箱(先到期先出 → R-1),銷貨退回:必須指定批號,退回後回到該批號
	dn, res := c.createPur(deliveries, s.header(map[string]any{"doc_type": "delivery", "lines": []map[string]any{line(milk, s.box, "3", "200", nil)}}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")
	dnFull := decode[purDoc](t, c.do(http.MethodGet, deliveries+"/"+itoa(dn.ID), nil).Data)
	_, res = c.createPur(deliveries, s.header(map[string]any{"doc_type": "return", "lines": []map[string]any{
		line(milk, s.box, "1", "200", map[string]any{"delivery_line_id": dnFull.Lines[0].ID})}}))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422") // 沒有批號
	sr, res := c.createPur(deliveries, s.header(map[string]any{"doc_type": "return", "lines": []map[string]any{
		line(milk, s.box, "1", "200", map[string]any{"delivery_line_id": dnFull.Lines[0].ID, "lot_no": "R-1"})}}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(deliveries, &sr)
	expect(t, c.actPur(deliveries, &sr, "post"), http.StatusOK, "")
	if c.lotQty(milk, s.wh, "R-1") != "24" { // 48 − 36 + 12
		t.Fatalf("銷貨退回後 R-1=%s", c.lotQty(milk, s.wh, "R-1"))
	}
	assertReconcileLotsOK(t, c)

	// 有異動後不可改批號管理方式
	cur := decode[idVer](t, c.do(http.MethodGet, "/masterdata/items/"+itoa(milk), nil).Data)
	expect(t, c.do(http.MethodPut, "/masterdata/items/"+itoa(milk), map[string]any{
		"code": "MILK", "name": "MILK", "item_type": "goods", "base_unit_id": e.unitID("PCS"), "lot_control": "none",
		"safety_stock": "0", "list_price": "0", "is_active": true, "version": cur.Version,
	}), http.StatusConflict, "ITEM-003")
	// 服務類料品不能做批號管理
	expect(t, c.do(http.MethodPost, "/masterdata/items", map[string]any{
		"code": "SVC1", "name": "服務", "item_type": "service", "base_unit_id": e.unitID("PCS"), "lot_control": "lot",
		"safety_stock": "0", "list_price": "0",
	}), http.StatusUnprocessableEntity, "SYS-422")
}

// 效期篩選與儀表板:效期以「真實的今天」判斷,所以日期相對於現在
func TestLotExpiryFiltersAndDashboard(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	milk := e.seedItem("MILK", "goods")
	e.setLotControl(milk, "lot_expiry")
	pcs := e.unitID("PCS")
	day := func(offset int) string {
		return time.Now().In(time.FixedZone("TST", 8*3600)).AddDate(0, 0, offset).Format(time.DateOnly)
	}
	// 單據日期用今天以前(調整單的單據日期不影響效期判斷;出庫才看單據日期)
	for _, l := range []struct{ no, exp string }{{"EXPIRED", day(-5)}, {"SOON", day(10)}, {"LATER", day(200)}} {
		expect(t, c.postLot(s.wh, milk, pcs, "10", l.no, l.exp), http.StatusOK, "")
	}
	nos := func(q string) []string {
		var out []string
		for _, r := range decode[[]lotRow](t, c.do(http.MethodGet, "/inventory/lots?item_id="+itoa(milk)+q, nil).Data) {
			out = append(out, r.LotNo)
		}
		return out
	}
	if got := strings.Join(nos(""), ","); got != "EXPIRED,SOON,LATER" {
		t.Fatalf("依效期排序 = %s", got)
	}
	if got := strings.Join(nos("&expiry=expired"), ","); got != "EXPIRED" {
		t.Fatalf("已過期 = %s", got)
	}
	if got := strings.Join(nos("&expiry=expiring&days=30"), ","); got != "SOON" {
		t.Fatalf("30 天內到期 = %s", got)
	}
	if got := strings.Join(nos("&expiry=expiring&days=5"), ","); got != "" {
		t.Fatalf("5 天內到期 = %q", got)
	}
	status := map[string]string{}
	for _, r := range decode[[]lotRow](t, c.do(http.MethodGet, "/inventory/lots?item_id="+itoa(milk), nil).Data) {
		status[r.LotNo] = r.ExpiryStatus
	}
	if status["EXPIRED"] != "expired" || status["SOON"] != "expiring" || status["LATER"] != "ok" {
		t.Fatalf("效期狀態 = %v", status)
	}
	// 儀表板:已過期 1、即將到期 1
	dash := decode[struct {
		Expiry struct {
			Expired  int64 `json:"expired"`
			Expiring int64 `json:"expiring"`
			Items    []struct {
				LotNo   string `json:"lot_no"`
				Expired bool   `json:"expired"`
			} `json:"items"`
		} `json:"expiry"`
	}](t, c.do(http.MethodGet, "/dashboard", nil).Data)
	if dash.Expiry.Expired != 1 || dash.Expiry.Expiring != 1 || len(dash.Expiry.Items) != 2 || dash.Expiry.Items[0].LotNo != "EXPIRED" || !dash.Expiry.Items[0].Expired {
		t.Fatalf("儀表板效期卡片 = %+v", dash.Expiry)
	}
	// 沒有庫存權限的人看不到效期卡片與批號清單
	e.seedUser("nobody", pw, false, false)
	nb := e.loggedIn("nobody", pw)
	expect(t, nb.do(http.MethodGet, "/inventory/lots", nil), http.StatusForbidden, "SYS-403")
	if raw := decode[map[string]any](t, nb.do(http.MethodGet, "/dashboard", nil).Data); raw["expiry"] != nil {
		t.Fatalf("沒有權限不應看到效期卡片")
	}
}

// 同時出貨(先到期先出)不可超賣任何批號:兩個批號各 5 箱共 10 箱,14 張各 1 箱的出貨單並行過帳 → 10 張成功。
func TestConcurrentLotDeliveriesNeverOversell(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	milk := e.seedItem("MILK", "goods")
	e.setLotControl(milk, "lot_expiry")
	pcs := e.unitID("PCS")
	expect(t, c.postLot(s.wh, milk, pcs, "60", "L1", "2027-03-01"), http.StatusOK, "")
	expect(t, c.postLot(s.wh, milk, pcs, "60", "L2", "2027-06-01"), http.StatusOK, "")
	docs := make([]purDoc, 14)
	for i := range docs {
		d, res := c.createPur(deliveries, s.header(map[string]any{
			"doc_type": "delivery", "lines": []map[string]any{line(milk, s.box, "1", "100", nil)},
		}))
		expect(t, res, http.StatusCreated, "")
		c.approvePur(deliveries, &d)
		docs[i] = d
	}
	rs := parallel(len(docs), func(i int) apiResp { return c.actPur(deliveries, &docs[i], "post") })
	ok, codes := tally(rs)
	if ok != 10 || codes["INV-001"] != 4 {
		t.Fatalf("成功 %d,錯誤 %v;應為 10 張成功、4 張庫存不足", ok, codes)
	}
	if c.lotQty(milk, s.wh, "L1") != "0" || c.lotQty(milk, s.wh, "L2") != "0" || e.balance(milk, s.wh) != "0" {
		t.Fatalf("結存 L1=%s L2=%s total=%s", c.lotQty(milk, s.wh, "L1"), c.lotQty(milk, s.wh, "L2"), e.balance(milk, s.wh))
	}
	assertReconcileLotsOK(t, c)
}

func TestImportOpeningStockWithLots(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	wh := e.seedWarehouse("MAIN", false)
	plain, milk, juice := e.seedItem("P1", "goods"), e.seedItem("MILK", "goods"), e.seedItem("JUICE", "goods")
	e.setLotControl(milk, "lot_expiry")
	e.setLotControl(juice, "lot")
	sh := c.importHeaders("opening_stock")
	if len(sh) != 7 || sh[5] != "批號" || sh[6] != "效期" {
		t.Fatalf("範本標題 = %v", sh)
	}
	q := "date=2026-09-30"

	// 預檢:批號管理的料品要批號與效期、一般料品不可填批號、同批號重複
	_, out := c.upload("opening_stock", q, xlsx(t, sh,
		[]string{"MAIN", "MILK", "10", "30", "", "", ""},             // 缺批號
		[]string{"MAIN", "MILK", "10", "30", "", "M1", ""},           // 缺效期
		[]string{"MAIN", "MILK", "10", "30", "", "M2", "2027-03-01"}, // 正確
		[]string{"MAIN", "MILK", "10", "30", "", "m2", "2027-03-01"}, // 與上一列同批號(大小寫不同)
		[]string{"MAIN", "P1", "10", "5", "", "X1", ""},              // 一般料品不可填批號
		[]string{"MAIN", "JUICE", "6", "20", "", "J1", ""}))          // 批號管理(無效期)正確
	if out.OK || !out.hasError("批號", "批號必填") || !out.hasError("效期", "效期必填") || !out.hasError("批號", "沒有啟用批號管理") || !out.hasError("料號", "重複") {
		t.Fatalf("預檢 = %+v", out)
	}
	_, out = c.upload("opening_stock", q, xlsx(t, sh,
		[]string{"MAIN", "MILK", "10", "30", "", "m2", "2027-03-01"},
		[]string{"MAIN", "MILK", "5", "30", "", "M3", "2027-09-01"},
		[]string{"MAIN", "JUICE", "6", "20", "", "J1", ""},
		[]string{"MAIN", "P1", "4", "5", "", "", ""}))
	if !out.OK || out.BatchID == nil {
		t.Fatalf("匯入 = %+v", out)
	}
	if e.balance(milk, wh) != "15" || e.balance(juice, wh) != "6" || e.balance(plain, wh) != "4" ||
		c.lotQty(milk, wh, "M2") != "10" || c.lotQty(milk, wh, "M3") != "5" || c.lotQty(juice, wh, "J1") != "6" {
		t.Fatalf("匯入後 milk=%s M2=%s M3=%s juice=%s", e.balance(milk, wh), c.lotQty(milk, wh, "M2"), c.lotQty(milk, wh, "M3"), c.lotQty(juice, wh, "J1"))
	}
	assertReconcileLotsOK(t, c)
	// 撤銷整批:批號庫存一併回到 0
	expect(t, c.do(http.MethodPost, "/imports/batches/"+itoa(*out.BatchID)+"/undo", nil), http.StatusOK, "")
	if e.balance(milk, wh) != "0" || c.lotQty(milk, wh, "M2") != "0" {
		t.Fatalf("撤銷後 milk=%s M2=%s", e.balance(milk, wh), c.lotQty(milk, wh, "M2"))
	}
	assertReconcileLotsOK(t, c)
}

func TestImportItemsLotControlColumn(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	h := c.importHeaders("items")
	if len(h) != 12 || h[11] != "批號管理" {
		t.Fatalf("範本標題 = %v", h)
	}
	row := func(code, typ, lot string) []string {
		return []string{code, code, "", "", typ, "PCS", "", "", "", "", "", lot}
	}
	_, out := c.upload("items", "dry_run=true", xlsx(t, h, row("A1", "商品", "亂填"), row("S1", "服務", "批號")))
	if out.OK || !out.hasError("批號管理", "須為") || !out.hasError("批號管理", "服務類") {
		t.Fatalf("預檢 = %+v", out)
	}
	_, out = c.upload("items", "", xlsx(t, h, row("P1", "商品", ""), row("P2", "商品", "批號"), row("P3", "商品", "批號與效期")))
	if !out.OK {
		t.Fatalf("匯入 = %+v", out)
	}
	got := map[string]string{}
	rows, err := e.pool.Query(context.Background(), "SELECT code, lot_control FROM items ORDER BY code")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var code, lc string
		if err := rows.Scan(&code, &lc); err != nil {
			t.Fatal(err)
		}
		got[code] = lc
	}
	if got["P1"] != "none" || got["P2"] != "lot" || got["P3"] != "lot_expiry" {
		t.Fatalf("lot_control = %v", got)
	}
}
