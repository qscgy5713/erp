package app

import (
	"context"
	"net/http"
	"testing"
)

type binRow struct {
	ID       int64  `json:"id"`
	Code     string `json:"code"`
	IsActive bool   `json:"is_active"`
	StockQty string `json:"stock_qty"`
	Version  int32  `json:"version"`
}

type binStockRow struct {
	BinCode  string `json:"bin_code"`
	ItemCode string `json:"item_code"`
	Qty      string `json:"qty"`
}

func (e *env) setUseBins(wh int64, on bool) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), "UPDATE warehouses SET use_bins = $1 WHERE id = $2", on, wh); err != nil {
		e.t.Fatal(err)
	}
}

func (c *client) addBin(wh int64, code string) apiResp {
	return c.do(http.MethodPost, "/masterdata/bins", map[string]any{"warehouse_id": wh, "code": code, "name": "儲位 " + code, "is_active": true})
}

// binQty 某儲位內某料品的現有量
func (c *client) binQty(wh int64, bin, item string) string {
	c.e.t.Helper()
	res := c.do(http.MethodGet, "/inventory/bin-stock?include_zero=true&warehouse_id="+itoa(wh)+"&size=100", nil)
	expect(c.e.t, res, http.StatusOK, "")
	for _, r := range decode[[]binStockRow](c.e.t, res.Data) {
		if r.BinCode == bin && r.ItemCode == item {
			return r.Qty
		}
	}
	return "0"
}

func binAdj(wh, item, unit int64, qty, bin string) map[string]any {
	l := map[string]any{"item_id": item, "unit_id": unit, "qty": qty}
	if bin != "" {
		l["bin_code"] = bin
	}
	return map[string]any{"doc_type": "adjustment", "doc_date": today, "warehouse_id": wh, "lines": []map[string]any{l}}
}

func (c *client) postBin(wh, item, unit int64, qty, bin string) apiResp {
	c.e.t.Helper()
	d, res := c.createDoc(binAdj(wh, item, unit, qty, bin))
	if res.status != http.StatusCreated {
		return res
	}
	return c.postAll(&d)
}

func TestBinsMasterAndWarehouseRules(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	wh, plain := e.seedWarehouse("A", false), e.seedWarehouse("B", false)
	item := e.seedItem("P1", "goods")
	pcs := e.unitID("PCS")

	// 沒啟用儲位的倉庫不能建儲位;代號格式、重複
	expect(t, c.addBin(wh, "A-01"), http.StatusConflict, "BIN-002")
	e.setUseBins(wh, true)
	expect(t, c.addBin(wh, "bad code"), http.StatusUnprocessableEntity, "SYS-422")
	res := c.addBin(wh, "a-01") // 小寫自動轉大寫
	expect(t, res, http.StatusCreated, "")
	bin1 := decode[binRow](t, res.Data)
	if bin1.Code != "A-01" {
		t.Fatalf("code = %s", bin1.Code)
	}
	expect(t, c.addBin(wh, "A-01"), http.StatusConflict, "BIN-001")
	expect(t, c.addBin(wh, "A-02"), http.StatusCreated, "")
	expect(t, c.addBin(99999, "X"), http.StatusUnprocessableEntity, "SYS-422")

	// 啟用儲位的倉庫:入庫要儲位、儲位須存在;沒啟用儲位的倉庫不能指定儲位
	for name, body := range map[string]map[string]any{
		"入庫沒有儲位": binAdj(wh, item, pcs, "10", ""),
		"儲位不存在":  binAdj(wh, item, pcs, "10", "NOPE"),
		"倉庫沒啟用":  binAdj(plain, item, pcs, "10", "A-01"),
	} {
		if _, r := c.createDoc(body); r.status != http.StatusUnprocessableEntity || r.code() != "SYS-422" {
			t.Errorf("%s → %d %s", name, r.status, r.code())
		}
	}
	expect(t, c.postBin(wh, item, pcs, "10", "a-01"), http.StatusOK, "")
	if c.binQty(wh, "A-01", "P1") != "10" || e.balance(item, wh) != "10" {
		t.Fatalf("入庫後 A-01=%s total=%s", c.binQty(wh, "A-01", "P1"), e.balance(item, wh))
	}

	// 有庫存不能啟用 / 停用儲位;有庫存的儲位不能停用;有異動的儲位不能刪除
	cur := decode[[]struct {
		ID      int64  `json:"id"`
		Code    string `json:"code"`
		Version int32  `json:"version"`
	}](t, c.do(http.MethodGet, "/masterdata/warehouses", nil).Data)
	var wv struct {
		id      int64
		version int32
	}
	for _, w := range cur {
		if w.Code == "A" {
			wv.id, wv.version = w.ID, w.Version
		}
	}
	expect(t, c.do(http.MethodPut, "/masterdata/warehouses/"+itoa(wv.id), map[string]any{
		"code": "A", "name": "A", "use_bins": false, "is_active": true, "version": wv.version}), http.StatusConflict, "WH-002")
	expect(t, c.do(http.MethodPut, "/masterdata/bins/"+itoa(bin1.ID), map[string]any{"code": bin1.Code, "name": "x", "is_active": false, "version": bin1.Version}), http.StatusConflict, "BIN-003")
	expect(t, c.do(http.MethodDelete, "/masterdata/bins/"+itoa(bin1.ID), nil), http.StatusConflict, "BIN-004")
	bins := decode[[]binRow](t, c.do(http.MethodGet, "/masterdata/bins?warehouse_id="+itoa(wh), nil).Data)
	if len(bins) != 2 || bins[0].StockQty != "10" {
		t.Fatalf("儲位清單 = %+v", bins)
	}
	// 空儲位可以停用(停用後入庫被擋)與刪除
	empty := bins[1]
	expect(t, c.do(http.MethodPut, "/masterdata/bins/"+itoa(empty.ID), map[string]any{"code": empty.Code, "name": "x", "is_active": false, "version": empty.Version}), http.StatusOK, "")
	if _, r := c.createDoc(binAdj(wh, item, pcs, "1", "A-02")); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("停用的儲位不能入庫: %d", r.status)
	}
	expect(t, c.do(http.MethodDelete, "/masterdata/bins/"+itoa(empty.ID), nil), http.StatusNoContent, "")
	// 沒有庫存的倉庫可切換
	e.setUseBins(plain, false)
	expect(t, c.do(http.MethodGet, "/inventory/bin-stock", nil), http.StatusOK, "")
}

func TestBinAllocationTransferAndReverse(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	c := e.loggedIn("root", pw)
	wh, other := e.seedWarehouse("A", false), e.seedWarehouse("B", false)
	e.setUseBins(wh, true)
	item := e.seedItem("P1", "goods")
	pcs := e.unitID("PCS")
	for _, b := range []string{"A-01", "A-02", "A-03"} {
		expect(t, c.addBin(wh, b), http.StatusCreated, "")
	}
	expect(t, c.postBin(wh, item, pcs, "10", "A-01"), http.StatusOK, "")
	expect(t, c.postBin(wh, item, pcs, "30", "A-02"), http.StatusOK, "")
	expect(t, c.postBin(wh, item, pcs, "5", "A-03"), http.StatusOK, "")

	// 出庫不指定儲位:庫存多的先出 A-02 30 → 再 A-01 10 → 再 A-03 5(共 45 → 這裡出 38:A-02 30 + A-01 8)
	d, res := c.createDoc(binAdj(wh, item, pcs, "-38", ""))
	expect(t, res, http.StatusCreated, "")
	expect(t, c.postAll(&d), http.StatusOK, "")
	if c.binQty(wh, "A-02", "P1") != "0" || c.binQty(wh, "A-01", "P1") != "2" || c.binQty(wh, "A-03", "P1") != "5" || e.balance(item, wh) != "7" {
		t.Fatalf("分配後 A-01=%s A-02=%s A-03=%s total=%s", c.binQty(wh, "A-01", "P1"), c.binQty(wh, "A-02", "P1"), c.binQty(wh, "A-03", "P1"), e.balance(item, wh))
	}
	// 反過帳還原到原本的儲位
	expect(t, c.act(&d, "unpost"), http.StatusOK, "")
	if c.binQty(wh, "A-02", "P1") != "30" || c.binQty(wh, "A-01", "P1") != "10" || e.balance(item, wh) != "45" {
		t.Fatalf("反過帳後 A-01=%s A-02=%s", c.binQty(wh, "A-01", "P1"), c.binQty(wh, "A-02", "P1"))
	}

	// 指定儲位出庫:超過該儲位的庫存即使整倉夠也不行(儲位庫存不可為負)
	expect(t, c.postBin(wh, item, pcs, "-11", "A-01"), http.StatusUnprocessableEntity, "INV-018")
	expect(t, c.postBin(wh, item, pcs, "-4", "A-03"), http.StatusOK, "")
	// 整倉不夠:出 50 > 總量 41
	if r := c.postBin(wh, item, pcs, "-50", ""); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("超出總庫存應被擋: %d %s", r.status, r.code())
	}

	// 同倉調撥:A-02 → A-03 搬 12 個;來源與目的相同被擋;未指定目的儲位被擋
	tr := func(qty, from, to string) (stockDoc, apiResp) {
		l := map[string]any{"item_id": item, "unit_id": pcs, "qty": qty}
		if from != "" {
			l["bin_code"] = from
		}
		if to != "" {
			l["to_bin_code"] = to
		}
		return c.createDoc(map[string]any{"doc_type": "transfer", "doc_date": today, "warehouse_id": wh, "to_warehouse_id": wh, "lines": []map[string]any{l}})
	}
	if _, r := tr("12", "A-02", "A-02"); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("同儲位調撥應被擋: %d", r.status)
	}
	if _, r := tr("12", "A-02", ""); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("缺目的儲位應被擋: %d", r.status)
	}
	mv, res := tr("12", "A-02", "A-03")
	expect(t, res, http.StatusCreated, "")
	expect(t, c.postAll(&mv), http.StatusOK, "")
	if c.binQty(wh, "A-02", "P1") != "18" || c.binQty(wh, "A-03", "P1") != "13" || e.balance(item, wh) != "41" {
		t.Fatalf("儲位間調撥後 A-02=%s A-03=%s total=%s", c.binQty(wh, "A-02", "P1"), c.binQty(wh, "A-03", "P1"), e.balance(item, wh))
	}
	expect(t, c.act(&mv, "unpost"), http.StatusOK, "")
	if c.binQty(wh, "A-02", "P1") != "30" || c.binQty(wh, "A-03", "P1") != "1" {
		t.Fatalf("調撥反過帳後 A-02=%s A-03=%s", c.binQty(wh, "A-02", "P1"), c.binQty(wh, "A-03", "P1"))
	}

	// 跨倉調撥:啟用儲位的倉庫 → 沒有啟用的倉庫(目的儲位須留空);反向則須指定目的儲位
	cross := func(from, to int64, bin, toBin string) (stockDoc, apiResp) {
		l := map[string]any{"item_id": item, "unit_id": pcs, "qty": "5"}
		if bin != "" {
			l["bin_code"] = bin
		}
		if toBin != "" {
			l["to_bin_code"] = toBin
		}
		return c.createDoc(map[string]any{"doc_type": "transfer", "doc_date": today, "warehouse_id": from, "to_warehouse_id": to, "lines": []map[string]any{l}})
	}
	if _, r := cross(wh, other, "A-02", "A-01"); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("目的倉沒啟用儲位卻指定儲位應被擋: %d", r.status)
	}
	out, res := cross(wh, other, "A-02", "")
	expect(t, res, http.StatusCreated, "")
	expect(t, c.postAll(&out), http.StatusOK, "")
	if c.binQty(wh, "A-02", "P1") != "25" || e.balance(item, other) != "5" || e.balance(item, wh) != "36" {
		t.Fatalf("跨倉調出後 A-02=%s other=%s", c.binQty(wh, "A-02", "P1"), e.balance(item, other))
	}
	if _, r := cross(other, wh, "", ""); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("調入啟用儲位的倉庫須指定儲位: %d", r.status)
	}
	back, res := cross(other, wh, "", "A-03")
	expect(t, res, http.StatusCreated, "")
	expect(t, c.postAll(&back), http.StatusOK, "")
	if c.binQty(wh, "A-03", "P1") != "6" || e.balance(item, other) != "0" {
		t.Fatalf("調回後 A-03=%s", c.binQty(wh, "A-03", "P1"))
	}

	// 盤點:啟用儲位的倉庫不支援
	if _, r := c.createDoc(map[string]any{"doc_type": "count", "doc_date": today, "warehouse_id": wh}); r.status != http.StatusUnprocessableEntity || r.code() != "INV-021" {
		t.Fatalf("盤點應被擋: %d %s", r.status, r.code())
	}
	assertReconcileOK(t, c, "bins")
	assertReconcileOK(t, c, "stock")
}

func assertReconcileOK(t *testing.T, c *client, key string) {
	t.Helper()
	rec := decode[struct {
		Checks []struct {
			Key    string `json:"key"`
			Status string `json:"status"`
		} `json:"checks"`
	}](t, c.do(http.MethodGet, "/costing/reconcile", nil).Data)
	for _, ch := range rec.Checks {
		if ch.Key == key {
			if ch.Status != "ok" {
				t.Fatalf("對帳檢查 %s = %s", key, ch.Status)
			}
			return
		}
	}
	t.Fatalf("對帳檢查沒有 %s", key)
}

func TestBinsWithDocumentsAndLots(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	wh := e.seedWarehouse("BIN", false)
	e.setUseBins(wh, true)
	for _, b := range []string{"R1", "R2"} {
		expect(t, c.addBin(wh, b), http.StatusCreated, "")
	}
	milk := e.seedItem("MILK", "goods")
	e.setLotControl(milk, "lot_expiry")
	pcs := e.unitID("PCS")
	p := &purCtx{e: e, c: c, wh: wh, sup: e.seedSupplier("S1", "TWD"), tax: s.tax, item: milk, box: s.box}

	// 進貨:啟用儲位的倉庫須指定儲位;批號與儲位各自記錄
	rcv := func(bin string) (purDoc, apiResp) {
		l := map[string]any{"item_id": milk, "unit_id": pcs, "qty": "20", "unit_price": "10", "lot_no": "L1", "expiry_date": "2027-05-01"}
		if bin != "" {
			l["bin_code"] = bin
		}
		return c.createPur(receipts, p.header(map[string]any{"doc_type": "receipt", "lines": []map[string]any{l}}))
	}
	_, res := rcv("")
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	r1, res := rcv("r1")
	expect(t, res, http.StatusCreated, "")
	c.approvePur(receipts, &r1)
	expect(t, c.actPur(receipts, &r1, "post"), http.StatusOK, "")
	if c.binQty(wh, "R1", "MILK") != "20" || c.lotQty(milk, wh, "L1") != "20" {
		t.Fatalf("進貨後 R1=%s L1=%s", c.binQty(wh, "R1", "MILK"), c.lotQty(milk, wh, "L1"))
	}

	// 出貨不指定儲位、不指定批號:自動分配儲位與批號
	dn, res := c.createPur(deliveries, s.header(map[string]any{
		"warehouse_id": wh, "doc_type": "delivery", "lines": []map[string]any{line(milk, pcs, "8", "30", nil)},
	}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(deliveries, &dn)
	expect(t, c.actPur(deliveries, &dn, "post"), http.StatusOK, "")
	if c.binQty(wh, "R1", "MILK") != "12" || c.lotQty(milk, wh, "L1") != "12" || e.balance(milk, wh) != "12" {
		t.Fatalf("出貨後 R1=%s L1=%s", c.binQty(wh, "R1", "MILK"), c.lotQty(milk, wh, "L1"))
	}
	// 銷貨退回須指定儲位,退回後回到該儲位
	dnFull := decode[purDoc](t, c.do(http.MethodGet, deliveries+"/"+itoa(dn.ID), nil).Data)
	_, res = c.createPur(deliveries, s.header(map[string]any{"warehouse_id": wh, "doc_type": "return", "lines": []map[string]any{
		line(milk, pcs, "3", "30", map[string]any{"delivery_line_id": dnFull.Lines[0].ID, "lot_no": "L1"})}}))
	expect(t, res, http.StatusUnprocessableEntity, "SYS-422")
	sr, res := c.createPur(deliveries, s.header(map[string]any{"warehouse_id": wh, "doc_type": "return", "lines": []map[string]any{
		line(milk, pcs, "3", "30", map[string]any{"delivery_line_id": dnFull.Lines[0].ID, "lot_no": "L1", "bin_code": "R2"})}}))
	expect(t, res, http.StatusCreated, "")
	c.approvePur(deliveries, &sr)
	expect(t, c.actPur(deliveries, &sr, "post"), http.StatusOK, "")
	if c.binQty(wh, "R2", "MILK") != "3" || c.binQty(wh, "R1", "MILK") != "12" {
		t.Fatalf("退回後 R1=%s R2=%s", c.binQty(wh, "R1", "MILK"), c.binQty(wh, "R2", "MILK"))
	}

	// 工單:領料倉啟用儲位(領料自動分配)、成品入庫須指定儲位
	fg, mat := e.seedItem("FG1", "goods"), e.seedItem("MAT", "goods")
	expect(t, c.postBin(wh, mat, pcs, "50", "R1"), http.StatusOK, "")
	body := woBody(fg, "5", wh, "0", map[string]any{"lines": []map[string]any{{"item_id": mat, "qty": "10"}}})
	expect(t, c.do(http.MethodPost, prodWO, body), http.StatusUnprocessableEntity, "SYS-422")
	body["output_bin_code"] = "r2"
	res = c.do(http.MethodPost, prodWO, body)
	expect(t, res, http.StatusCreated, "")
	wo := decode[woDoc](t, res.Data)
	expect(t, c.woPostAll(&wo), http.StatusOK, "")
	if c.binQty(wh, "R1", "MAT") != "40" || c.binQty(wh, "R2", "FG1") != "5" {
		t.Fatalf("完工後 MAT@R1=%s FG1@R2=%s", c.binQty(wh, "R1", "MAT"), c.binQty(wh, "R2", "FG1"))
	}
	expect(t, c.woAct(&wo, "unpost"), http.StatusOK, "")
	if c.binQty(wh, "R1", "MAT") != "50" || c.binQty(wh, "R2", "FG1") != "0" {
		t.Fatalf("反完工後 MAT@R1=%s FG1@R2=%s", c.binQty(wh, "R1", "MAT"), c.binQty(wh, "R2", "FG1"))
	}
	assertReconcileOK(t, c, "bins")
	assertReconcileOK(t, c, "lots")
	assertReconcileOK(t, c, "stock")
}

func TestConcurrentBinPostingsNeverOversell(t *testing.T) {
	s := newSalCtx(t)
	e, c := s.e, s.c
	wh := e.seedWarehouse("BIN", false)
	e.setUseBins(wh, true)
	for _, b := range []string{"B1", "B2"} {
		expect(t, c.addBin(wh, b), http.StatusCreated, "")
	}
	item := e.seedItem("P2", "goods")
	pcs := e.unitID("PCS")
	expect(t, c.postBin(wh, item, pcs, "5", "B1"), http.StatusOK, "")
	expect(t, c.postBin(wh, item, pcs, "5", "B2"), http.StatusOK, "")
	docs := make([]stockDoc, 14)
	for i := range docs {
		d, res := c.createDoc(binAdj(wh, item, pcs, "-1", ""))
		expect(t, res, http.StatusCreated, "")
		for _, a := range []string{"submit", "approve"} {
			expect(t, c.act(&d, a), http.StatusOK, "")
		}
		docs[i] = d
	}
	rs := parallel(len(docs), func(i int) apiResp { return c.act(&docs[i], "post") })
	ok, codes := tally(rs)
	if ok != 10 {
		t.Fatalf("成功 %d,錯誤 %v;應為 10 張成功", ok, codes)
	}
	if c.binQty(wh, "B1", "P2") != "0" || c.binQty(wh, "B2", "P2") != "0" || e.balance(item, wh) != "0" {
		t.Fatalf("結存 B1=%s B2=%s total=%s", c.binQty(wh, "B1", "P2"), c.binQty(wh, "B2", "P2"), e.balance(item, wh))
	}
	assertReconcileOK(t, c, "bins")
}
