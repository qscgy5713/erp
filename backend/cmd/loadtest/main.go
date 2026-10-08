// Command loadtest 對 API 打混合負載(讀取為主,含庫存調整、傳票、訂單三種寫入流程),
// 輸出各情境的延遲分佈與錯誤。搭配 backend/loadtest/seed.sql 在獨立資料庫量測,不要對正式環境執行。
//
//	go run ./cmd/loadtest -url http://erp-perf-api:8080 -user perfadmin -pass '...' -c 20 -d 60s
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type result struct {
	name   string
	dur    time.Duration
	status int
	code   string
}

type env struct {
	base, token  string
	client       *http.Client
	unitID       int64
	taxID        int64
	accountIDs   []int64
	items, custs int
}

func main() {
	base := flag.String("url", "http://localhost:18081", "API 位址(不含 /api/v1)")
	user := flag.String("user", "perfadmin", "帳號")
	pass := flag.String("pass", os.Getenv("LOADTEST_PASSWORD"), "密碼(或環境變數 LOADTEST_PASSWORD)")
	conc := flag.Int("c", 20, "併發數")
	dur := flag.Duration("d", 30*time.Second, "測試時間")
	writes := flag.Float64("writes", 0.15, "寫入流程占比 0–1")
	items := flag.Int("items", 3000, "料品數(造資料的規模)")
	custs := flag.Int("customers", 800, "客戶數")
	flag.Parse()

	e := &env{base: strings.TrimRight(*base, "/") + "/api/v1", items: *items, custs: *custs,
		client: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: *conc + 4}}}
	if err := e.login(*user, *pass); err != nil {
		fmt.Fprintln(os.Stderr, "登入失敗:", err)
		os.Exit(1)
	}
	if err := e.prepare(); err != nil {
		fmt.Fprintln(os.Stderr, "準備失敗:", err)
		os.Exit(1)
	}

	reads := e.readScenarios()
	flows := e.writeScenarios()
	fmt.Printf("併發 %d、%s、寫入占比 %.0f%%、讀取情境 %d 種、寫入流程 %d 種\n", *conc, *dur, *writes*100, len(reads), len(flows))

	var (
		mu      sync.Mutex
		results []result
		stop    atomic.Bool
		wg      sync.WaitGroup
	)
	record := func(rs ...result) {
		mu.Lock()
		results = append(results, rs...)
		mu.Unlock()
	}
	start := time.Now()
	time.AfterFunc(*dur, func() { stop.Store(true) })
	for range *conc {
		wg.Go(func() {
			rng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
			for !stop.Load() {
				if rng.Float64() < *writes {
					record(flows[rng.IntN(len(flows))](rng)...)
				} else {
					record(reads[rng.IntN(len(reads))](rng))
				}
			}
		})
	}
	wg.Wait()
	report(results, time.Since(start))
}

// ---- HTTP ----

func (e *env) do(name, method, path string, body any) result {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.base+path, rdr)
	req.Header.Set("Content-Type", "application/json")
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	t := time.Now()
	res, err := e.client.Do(req)
	if err != nil {
		return result{name: name, dur: time.Since(t), code: "NET:" + err.Error()}
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	r := result{name: name, dur: time.Since(t), status: res.StatusCode}
	if res.StatusCode >= 300 {
		var b struct {
			Error struct{ Code string } `json:"error"`
		}
		_ = json.Unmarshal(raw, &b)
		r.code = b.Error.Code
	}
	return r
}

func (e *env) doJSON(name, method, path string, body any, out any) result {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.base+path, rdr)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.token)
	t := time.Now()
	res, err := e.client.Do(req)
	if err != nil {
		return result{name: name, dur: time.Since(t), code: "NET:" + err.Error()}
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	r := result{name: name, dur: time.Since(t), status: res.StatusCode}
	var env struct {
		Data  json.RawMessage `json:"data"`
		Error struct{ Code string }
	}
	_ = json.Unmarshal(raw, &env)
	if res.StatusCode >= 300 {
		r.code = env.Error.Code
	} else if out != nil {
		_ = json.Unmarshal(env.Data, out)
	}
	return r
}

func (e *env) login(user, pass string) error {
	var out struct {
		AccessToken string `json:"access_token"`
	}
	e.token = "x"
	r := e.doJSON("login", http.MethodPost, "/auth/login", map[string]string{"username": user, "password": pass}, &out)
	if r.status != http.StatusOK || out.AccessToken == "" {
		return fmt.Errorf("HTTP %d %s", r.status, r.code)
	}
	e.token = out.AccessToken
	return nil
}

func (e *env) prepare() error {
	var units []struct {
		ID   int64  `json:"id"`
		Code string `json:"code"`
	}
	e.doJSON("", http.MethodGet, "/masterdata/units", nil, &units)
	for _, u := range units {
		if u.Code == "PCS" {
			e.unitID = u.ID
		}
	}
	var taxes []struct {
		ID   int64  `json:"id"`
		Code string `json:"code"`
	}
	e.doJSON("", http.MethodGet, "/masterdata/tax-types", nil, &taxes)
	for _, t := range taxes {
		if t.Code == "TX5" {
			e.taxID = t.ID
		}
	}
	var accts []struct {
		ID   int64  `json:"id"`
		Code string `json:"code"`
	}
	e.doJSON("", http.MethodGet, "/gl/account-options", nil, &accts)
	for _, a := range accts {
		if a.Code == "6101" || a.Code == "1101" {
			e.accountIDs = append(e.accountIDs, a.ID)
		}
	}
	if e.unitID == 0 || e.taxID == 0 || len(e.accountIDs) < 2 {
		return fmt.Errorf("缺少單位 / 稅別 / 科目 (unit=%d tax=%d accounts=%d)", e.unitID, e.taxID, len(e.accountIDs))
	}
	return nil
}

// ---- 情境 ----

type scenario func(rng *rand.Rand) result

func (e *env) get(name string, path func(rng *rand.Rand) string) scenario {
	return func(rng *rand.Rand) result { return e.do(name, http.MethodGet, path(rng), nil) }
}

func (e *env) readScenarios() []scenario {
	month := func(rng *rand.Rand) (string, string) {
		m := 1 + rng.IntN(9)
		return fmt.Sprintf("2026-%02d-01", m), fmt.Sprintf("2026-%02d-28", m)
	}
	return []scenario{
		e.get("dashboard", func(*rand.Rand) string { return "/dashboard" }),
		e.get("sales orders 列表", func(rng *rand.Rand) string { return fmt.Sprintf("/sales/orders?page=%d&size=20", 1+rng.IntN(50)) }),
		e.get("sales orders 篩選狀態", func(*rand.Rand) string { return "/sales/orders?status=approved&size=20" }),
		e.get("sales orders 單號搜尋", func(rng *rand.Rand) string { return fmt.Sprintf("/sales/orders?keyword=PSO%07d", 1+rng.IntN(30000)) }),
		e.get("deliveries 列表", func(rng *rand.Rand) string { return fmt.Sprintf("/sales/deliveries?page=%d&size=20", 1+rng.IntN(50)) }),
		e.get("deliveries 未開發票", func(*rand.Rand) string { return "/sales/deliveries?no_invoice=true&size=20" }),
		e.get("未出貨清單", func(*rand.Rand) string { return "/sales/unshipped-lines?size=50" }),
		e.get("purchase orders 列表", func(rng *rand.Rand) string { return fmt.Sprintf("/purchase/orders?page=%d&size=20", 1+rng.IntN(30)) }),
		e.get("未交貨清單", func(*rand.Rand) string { return "/purchase/outstanding-lines?size=50" }),
		e.get("現有量", func(rng *rand.Rand) string { return fmt.Sprintf("/inventory/balances?page=%d&size=50", 1+rng.IntN(50)) }),
		e.get("現有量 低於安全庫存", func(*rand.Rand) string { return "/inventory/balances?below_safety=true&size=50" }),
		e.get("收發存", func(rng *rand.Rand) string {
			f, t := month(rng)
			return "/inventory/movement-summary?from=" + f + "&to=" + t + "&size=50"
		}),
		e.get("料品異動明細", func(rng *rand.Rand) string {
			return fmt.Sprintf("/inventory/items/%d/ledger?from=2026-01-01&to=2026-09-30", 1+rng.IntN(e.items))
		}),
		e.get("應收帳款", func(*rand.Rand) string { return "/finance/receivables?open_only=true&size=50" }),
		e.get("應收帳齡", func(*rand.Rand) string { return "/finance/aging?side=receivable&as_of=2026-10-01" }),
		e.get("應付帳齡", func(*rand.Rand) string { return "/finance/aging?side=payable&as_of=2026-10-01" }),
		e.get("應收對帳單", func(rng *rand.Rand) string {
			return fmt.Sprintf("/finance/statements?side=receivable&partner_id=%d&currency=TWD&from=2026-01-01&to=2026-09-30", 1+rng.IntN(e.custs))
		}),
		e.get("試算表(全年)", func(*rand.Rand) string { return "/gl/reports/trial-balance?from=2026-01-01&to=2026-09-30" }),
		e.get("總分類帳", func(rng *rand.Rand) string {
			return fmt.Sprintf("/gl/reports/ledger?account_id=%d&from=2026-03-01&to=2026-03-31", e.accountIDs[rng.IntN(len(e.accountIDs))])
		}),
		e.get("日記帳", func(rng *rand.Rand) string {
			f, t := month(rng)
			return fmt.Sprintf("/gl/reports/journal?from=%s&to=%s&page=%d&size=50", f, t, 1+rng.IntN(5))
		}),
		e.get("傳票列表", func(rng *rand.Rand) string { return fmt.Sprintf("/gl/vouchers?page=%d&size=20", 1+rng.IntN(100)) }),
		e.get("傳票依科目篩選", func(rng *rand.Rand) string {
			return fmt.Sprintf("/gl/vouchers?account_id=%d&size=20", e.accountIDs[rng.IntN(len(e.accountIDs))])
		}),
		e.get("料品搜尋", func(rng *rand.Rand) string {
			return fmt.Sprintf("/masterdata/items?keyword=ITEM%04d", rng.IntN(e.items))
		}),
		e.get("開單選料", func(rng *rand.Rand) string {
			return fmt.Sprintf("/masterdata/item-options?keyword=%d", rng.IntN(e.items))
		}),
		e.get("客戶搜尋", func(rng *rand.Rand) string { return fmt.Sprintf("/masterdata/customers?keyword=%d", rng.IntN(e.custs)) }),
		e.get("對帳檢查", func(*rand.Rand) string { return "/costing/reconcile" }),
	}
}

// 寫入流程回傳多個結果(每個請求一筆),最後一筆 name 為「流程:完整」代表整個流程的總時間。
func (e *env) writeScenarios() []func(rng *rand.Rand) []result {
	today := time.Now().Format(time.DateOnly)
	flow := func(name string, steps func(rng *rand.Rand, run func(step, method, path string, body any, out any) bool)) func(*rand.Rand) []result {
		return func(rng *rand.Rand) []result {
			var rs []result
			t := time.Now()
			ok := true
			run := func(step, method, path string, body any, out any) bool {
				if !ok {
					return false
				}
				r := e.doJSON(name+" "+step, method, path, body, out)
				rs = append(rs, r)
				ok = r.status >= 200 && r.status < 300
				return ok
			}
			steps(rng, run)
			rs = append(rs, result{name: name + ":完整流程", dur: time.Since(t), status: map[bool]int{true: 200, false: 500}[ok]})
			return rs
		}
	}
	type doc struct {
		ID      int64 `json:"id"`
		Version int32 `json:"version"`
	}
	act := func(run func(string, string, string, any, any) bool, base string, d *doc, action string) bool {
		return run(action, http.MethodPost, fmt.Sprintf("%s/%d/actions/%s", base, d.ID, action), map[string]any{"version": d.Version}, d)
	}
	return []func(*rand.Rand) []result{
		flow("庫存調整", func(rng *rand.Rand, run func(string, string, string, any, any) bool) {
			var d doc
			if !run("建立", http.MethodPost, "/inventory/documents", map[string]any{
				"doc_type": "adjustment", "doc_date": today, "warehouse_id": 1 + rng.IntN(2),
				"lines": []map[string]any{{"item_id": 1 + rng.IntN(e.items), "unit_id": e.unitID, "qty": "1"}},
			}, &d) {
				return
			}
			for _, a := range []string{"submit", "approve", "post"} {
				if !act(run, "/inventory/documents", &d, a) {
					return
				}
			}
		}),
		flow("手動傳票", func(rng *rand.Rand, run func(string, string, string, any, any) bool) {
			var d doc
			amt := fmt.Sprint(100 + rng.IntN(9000))
			if !run("建立", http.MethodPost, "/gl/vouchers", map[string]any{
				"voucher_date": today, "description": "壓測",
				"lines": []map[string]any{
					{"account_id": e.accountIDs[0], "debit": amt, "credit": "0"},
					{"account_id": e.accountIDs[1], "debit": "0", "credit": amt},
				},
			}, &d) {
				return
			}
			act(run, "/gl/vouchers", &d, "post")
		}),
		flow("銷售訂單", func(rng *rand.Rand, run func(string, string, string, any, any) bool) {
			var d doc
			lines := make([]map[string]any, 3)
			for i := range lines {
				lines[i] = map[string]any{"item_id": 1 + rng.IntN(e.items), "unit_id": e.unitID, "qty": "5", "unit_price": "100"}
			}
			if !run("建立", http.MethodPost, "/sales/orders", map[string]any{
				"doc_type": "order", "doc_date": today, "customer_id": 1 + rng.IntN(e.custs), "warehouse_id": 1,
				"currency": "TWD", "tax_type_id": e.taxID, "lines": lines,
			}, &d) {
				return
			}
			for _, a := range []string{"submit", "approve"} {
				if !act(run, "/sales/orders", &d, a) {
					return
				}
			}
		}),
	}
}

// ---- 報表 ----

func pct(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted)-1) * p)
	return sorted[i]
}

func report(rs []result, elapsed time.Duration) {
	by := map[string][]result{}
	for _, r := range rs {
		by[r.name] = append(by[r.name], r)
	}
	type row struct {
		name                    string
		n, errs                 int
		avg, p50, p95, p99, max time.Duration
		codes                   map[string]int
	}
	var rows []row
	totalReq, totalErr := 0, 0
	for name, list := range by {
		ds := make([]time.Duration, 0, len(list))
		var sum time.Duration
		codes := map[string]int{}
		errs := 0
		for _, r := range list {
			ds = append(ds, r.dur)
			sum += r.dur
			if r.status < 200 || r.status >= 300 {
				errs++
				codes[fmt.Sprintf("%d%s", r.status, strings.TrimPrefix(" "+r.code, " "))]++
			}
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		rows = append(rows, row{name, len(list), errs, sum / time.Duration(len(list)), pct(ds, .5), pct(ds, .95), pct(ds, .99), ds[len(ds)-1], codes})
		if !strings.HasSuffix(name, ":完整流程") {
			totalReq += len(list)
			totalErr += errs
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].p95 > rows[j].p95 })
	ms := func(d time.Duration) string { return fmt.Sprintf("%.0f", float64(d.Microseconds())/1000) }
	fmt.Printf("\n%-28s %7s %5s %7s %7s %7s %7s %7s\n", "情境(依 p95 排序,毫秒)", "次數", "錯誤", "avg", "p50", "p95", "p99", "max")
	for _, r := range rows {
		fmt.Printf("%-28s %7d %5d %7s %7s %7s %7s %7s", r.name, r.n, r.errs, ms(r.avg), ms(r.p50), ms(r.p95), ms(r.p99), ms(r.max))
		if len(r.codes) > 0 {
			fmt.Printf("  %v", r.codes)
		}
		fmt.Println()
	}
	fmt.Printf("\n總請求 %d(%.1f 請求/秒),錯誤 %d(%.2f%%),耗時 %s\n", totalReq, float64(totalReq)/elapsed.Seconds(), totalErr, 100*float64(totalErr)/float64(max(totalReq, 1)), elapsed.Round(time.Millisecond))
}
