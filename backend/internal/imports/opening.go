package imports

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/finance"
	"erp/internal/gl"
	"erp/internal/inventory"
	"erp/internal/masterdata"
	"erp/internal/shared/authctx"
	"erp/internal/shared/money"
)

// 期初資料的共同原則:
//   - 期初日期由使用者指定(通常是上線前一天或上期期末),所有期初金額都是該日的餘額;
//   - 期初庫存以 source_type=opening_stock 寫入流水帳(月結成本時視同進貨,以單位成本計價);
//   - 期初應收 / 應付以 source_type=opening 建立未沖帳款,之後可正常收付款沖帳;
//   - 期初科目餘額產生一張已過帳傳票(source_type=opening_balance);
//     三者須自行確保一致,「對帳檢查」會核對存貨、應收、應付子帳與總帳。

func stockSource(b db.ImportBatch, date time.Time) inventory.Source {
	return inventory.Source{Type: "opening_stock", ID: b.ID, No: fmt.Sprintf("OPEN-STK-%d", b.ID), DocDate: date}
}

var openingStockImporter = &importer{
	key: "opening_stock", label: "期初庫存", needsDate: true, undoable: true,
	desc: "匯入期初庫存數量與成本(只能匯入尚無庫存異動的料品,請在上線前一次完成)。整批可撤銷。",
	columns: []column{
		{"倉庫代號", true, "已建立且啟用的倉庫代號"},
		{"料號", true, "商品類料品(服務類不管庫存)"},
		{"數量", true, "基本單位數量,須大於 0,最多 4 位小數"},
		{"單位成本", true, "每基本單位、未稅、本位幣,最多 6 位小數;月結成本以此計算期初金額"},
		{"備註", false, ""},
		{"批號", false, "批號管理的料品必填,其他料品留空;同一倉庫、同一料品有多個批號就分多列"},
		{"效期", false, "批號與效期管理的料品必填(YYYY-MM-DD)"},
	},
	sample: [][]string{{"MAIN", "PEN-01", "500", "8.5", "期初盤點", "", ""}, {"MAIN", "MILK-01", "120", "32", "", "L20260901", "2027-03-01"}},
	exec: func(r *run) error {
		whs, err := r.q.ListWarehouses(r.ctx, r.a.CompanyID)
		if err != nil {
			return err
		}
		wh := map[string]int64{}
		for _, w := range whs {
			if w.IsActive {
				wh[strings.ToUpper(w.Code)] = w.ID
			}
		}
		its, err := r.q.AllItemCodes(r.ctx, r.a.CompanyID)
		if err != nil {
			return err
		}
		type itemInfo struct {
			id         int64
			isGoods    bool
			lotControl string
		}
		items := map[string]itemInfo{}
		for _, it := range its {
			items[strings.ToUpper(it.Code)] = itemInfo{it.ID, it.ItemType == "goods", it.LotControl}
		}
		seen := map[string]bool{}
		hasTx := map[int64]bool{}
		var moves []inventory.Movement
		total := decimal.Zero
		for _, row := range r.rows {
			ok := true
			w, found := wh[strings.ToUpper(row.get(0))]
			if !found {
				r.fail(row.n, "倉庫代號", "「%s」不存在或已停用", row.get(0))
				ok = false
			}
			it, found := items[strings.ToUpper(row.get(1))]
			switch {
			case !found:
				r.fail(row.n, "料號", "「%s」不存在", row.get(1))
				ok = false
			case !it.isGoods:
				r.fail(row.n, "料號", "「%s」是服務類料品,不能有庫存", row.get(1))
				ok = false
			default:
				used, known := hasTx[it.id]
				if !known {
					if used, err = r.q.ItemHasTransactions(r.ctx, it.id); err != nil {
						return err
					}
					hasTx[it.id] = used
				}
				if used {
					r.fail(row.n, "料號", "「%s」已有庫存異動,期初庫存只能匯入尚無異動的料品", row.get(1))
					ok = false
				}
			}
			qty, c1 := nonNegative(r, row, 2, "數量", money.QuantityPlaces)
			if c1 && !qty.IsPositive() {
				r.fail(row.n, "數量", "須大於 0")
				c1 = false
			}
			if row.get(2) == "" {
				c1 = false
			}
			cost, c2 := nonNegative(r, row, 3, "單位成本", money.UnitPricePlaces)
			if row.get(3) == "" {
				r.fail(row.n, "單位成本", "必填(沒有成本資料請填 0)")
				c2 = false
			}
			// 批號與效期(D63)
			lotNo, lotErr := "", false
			var expiry *time.Time
			if found && it.isGoods {
				raw := strings.TrimSpace(row.get(5))
				switch {
				case it.lotControl == "none" && raw != "":
					r.fail(row.n, "批號", "「%s」沒有啟用批號管理,批號請留空", row.get(1))
					lotErr = true
				case it.lotControl != "none" && raw == "":
					r.fail(row.n, "批號", "「%s」是批號管理的料品,批號必填", row.get(1))
					lotErr = true
				case raw != "":
					no, err := inventory.NormalizeLotNo(raw)
					if err != nil {
						r.fail(row.n, "批號", "批號不可含空白,最多 40 個字元")
						lotErr = true
					}
					lotNo = no
				}
				if it.lotControl == "lot_expiry" && !lotErr {
					if t, err := parseDate(strings.TrimSpace(row.get(6))); err != nil {
						r.fail(row.n, "效期", "「%s」是效期管理的料品,效期必填(YYYY-MM-DD)", row.get(1))
						lotErr = true
					} else {
						expiry = &t
					}
				}
			}
			if ok && c1 && c2 && !lotErr {
				key := fmt.Sprintf("%d/%d/%s", w, it.id, lotNo)
				if seen[key] {
					r.fail(row.n, "料號", "同一倉庫的「%s」(批號 %s)在檔內重複", row.get(1), lotNo)
					continue
				}
				seen[key] = true
				c := cost
				moves = append(moves, inventory.Movement{ItemID: it.id, WarehouseID: w, Qty: qty, UnitCost: &c, LotNo: lotNo, Expiry: expiry})
				total = total.Add(qty.Mul(cost))
			}
		}
		if len(r.errs) > 0 {
			return nil
		}
		opt := inventory.Options{CompanyID: r.a.CompanyID, ActorID: &r.a.UserID}
		if err := inventory.Post(r.ctx, r.q, opt, stockSource(r.batch, r.date), moves); err != nil {
			return err
		}
		r.summary = fmt.Sprintf("%s 期初庫存 %d 筆,金額 %s", r.date.Format(time.DateOnly), len(moves), money.Amount(total).String())
		return nil
	},
	undo: func(ctx context.Context, q *db.Queries, a *authctx.Actor, b db.ImportBatch) error {
		opt := inventory.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}
		return inventory.Reverse(ctx, q, opt, stockSource(b, time.Time{}))
	},
}

// ---- 期初應收 / 應付 ----

type ledgerKind struct {
	key, label, partner, partnerCode string
	isAR                             bool
}

func openingLedgerImporter(k ledgerKind) *importer {
	paid := "未收金額"
	if !k.isAR {
		paid = "未付金額"
	}
	return &importer{
		key: k.key, label: k.label, needsDate: true, undoable: true,
		desc: "匯入期初" + k.partner + "的未沖帳款(依原始單據逐筆,每筆為截至期初日期尚未收付的金額);匯入後可正常" + map[bool]string{true: "收款", false: "付款"}[k.isAR] + "沖帳。整批可撤銷。",
		columns: []column{
			{k.partner + "代號", true, "已建立且啟用的" + k.partner + "代號"},
			{"單據號碼", true, "原始單據(發票 / 單據)號碼,最多 30 字;同一" + k.partner + "不可重複"},
			{"單據日期", true, "YYYY-MM-DD,不可晚於期初日期"},
			{"到期日", false, "YYYY-MM-DD;空白為單據日期"},
			{"幣別", false, "空白為 TWD"},
			{"匯率", false, "外幣必填以外可空白:空白時取單據日期當日或之前最近一筆匯率"},
			{paid + "(含稅,原幣)", true, "須大於 0;最多依幣別小數位"},
		},
		sample: [][]string{{k.partnerCode, "INV-2026-0901", "2026-09-01", "2026-10-31", "TWD", "", "52500"}},
		exec: func(r *run) error {
			l, err := loadLookups(r)
			if err != nil {
				return err
			}
			partners := map[string]int64{}
			var keysSeen = map[string]bool{}
			if k.isAR {
				ps, err := r.q.ActiveCustomerCodes(r.ctx, r.a.CompanyID)
				if err != nil {
					return err
				}
				for _, p := range ps {
					partners[strings.ToUpper(p.Code)] = p.ID
				}
				ks, err := r.q.OpeningReceivableKeys(r.ctx, r.a.CompanyID)
				if err != nil {
					return err
				}
				for _, x := range ks {
					keysSeen[fmt.Sprintf("%d|%s", x.PartnerID, x.SourceNo)] = true
				}
			} else {
				ps, err := r.q.ActiveSupplierCodes(r.ctx, r.a.CompanyID)
				if err != nil {
					return err
				}
				for _, p := range ps {
					partners[strings.ToUpper(p.Code)] = p.ID
				}
				ks, err := r.q.OpeningPayableKeys(r.ctx, r.a.CompanyID)
				if err != nil {
					return err
				}
				for _, x := range ks {
					keysSeen[fmt.Sprintf("%d|%s", x.PartnerID, x.SourceNo)] = true
				}
			}
			type line struct {
				partner      int64
				no           string
				docDate, due time.Time
				currency     string
				rate         decimal.Decimal
				amount, base decimal.Decimal
			}
			var todo []line
			baseTotal := decimal.Zero
			for _, row := range r.rows {
				ok := true
				pid, found := partners[strings.ToUpper(row.get(0))]
				if !found {
					r.fail(row.n, k.partner+"代號", "「%s」不存在或已停用", row.get(0))
					ok = false
				}
				no := row.get(1)
				switch {
				case no == "":
					r.fail(row.n, "單據號碼", "必填")
					ok = false
				case len([]rune(no)) > 30:
					r.fail(row.n, "單據號碼", "最多 30 字")
					ok = false
				case found && keysSeen[fmt.Sprintf("%d|%s", pid, no)]:
					r.fail(row.n, "單據號碼", "「%s」已存在或在檔內重複", no)
					ok = false
				}
				docDate, err := parseDate(row.get(2))
				switch {
				case err != nil:
					r.fail(row.n, "單據日期", "「%s」格式不正確,請用 YYYY-MM-DD", row.get(2))
					ok = false
				case docDate.After(r.date):
					r.fail(row.n, "單據日期", "不可晚於期初日期 %s", r.date.Format(time.DateOnly))
					ok = false
				}
				due := docDate
				if v := row.get(3); v != "" {
					if due, err = parseDate(v); err != nil {
						r.fail(row.n, "到期日", "「%s」格式不正確,請用 YYYY-MM-DD", v)
						ok = false
					}
				}
				cur := strings.ToUpper(row.get(4))
				if cur == "" {
					cur = "TWD"
				}
				dec, curOK := l.currencies[cur]
				if !curOK {
					r.fail(row.n, "幣別", "「%s」不存在或已停用", cur)
					ok = false
				}
				rate := decimal.NewFromInt(1)
				if curOK && cur != masterdata.BaseCurrency {
					if v := row.get(5); v != "" {
						rt, err := parseDecimal(v)
						if err != nil || !rt.IsPositive() || !rt.Round(money.RatePlaces).Equal(rt) {
							r.fail(row.n, "匯率", "須為大於 0 的數字,最多 %d 位小數", money.RatePlaces)
							ok = false
						} else {
							rate = rt
						}
					} else if ok {
						if rate, err = masterdata.RateOn(r.ctx, r.q, r.a.CompanyID, cur, docDate); err != nil {
							r.fail(row.n, "匯率", "查無 %s 在 %s 以前的匯率,請填入匯率", cur, docDate.Format(time.DateOnly))
							ok = false
						}
					}
				}
				amt, c1 := nonNegative(r, row, 6, "未沖金額", int32(dec))
				if row.get(6) == "" {
					r.fail(row.n, "未沖金額", "必填")
					c1 = false
				} else if c1 && !amt.IsPositive() {
					r.fail(row.n, "未沖金額", "須大於 0")
					c1 = false
				}
				if ok && c1 {
					keysSeen[fmt.Sprintf("%d|%s", pid, no)] = true
					base := money.Amount(amt.Mul(rate))
					todo = append(todo, line{pid, no, docDate, due, cur, rate, amt, base})
					baseTotal = baseTotal.Add(base)
				}
			}
			if len(r.errs) > 0 {
				return nil
			}
			for _, x := range todo {
				seq, err := r.q.NextOpeningSeq(r.ctx)
				if err != nil {
					return err
				}
				b := &r.batch.ID
				if k.isAR {
					err = r.q.InsertOpeningReceivable(r.ctx, db.InsertOpeningReceivableParams{
						CompanyID: r.a.CompanyID, CustomerID: x.partner, SourceID: seq, SourceNo: x.no, DocDate: x.docDate,
						DueDate: x.due, Currency: x.currency, ExchangeRate: x.rate, Amount: x.amount, BaseAmount: x.base,
						CreatedBy: &r.a.UserID, ImportBatchID: b,
					})
				} else {
					err = r.q.InsertOpeningPayable(r.ctx, db.InsertOpeningPayableParams{
						CompanyID: r.a.CompanyID, SupplierID: x.partner, SourceID: seq, SourceNo: x.no, DocDate: x.docDate,
						DueDate: x.due, Currency: x.currency, ExchangeRate: x.rate, Amount: x.amount, BaseAmount: x.base,
						CreatedBy: &r.a.UserID, ImportBatchID: b,
					})
				}
				if err != nil {
					return err
				}
			}
			r.summary = fmt.Sprintf("%s 期初%s %d 筆,本位幣金額 %s", r.date.Format(time.DateOnly), k.partner+"帳款", len(todo), baseTotal.String())
			return nil
		},
		undo: func(ctx context.Context, q *db.Queries, a *authctx.Actor, b db.ImportBatch) error {
			if k.isAR {
				ids, err := q.ReceivableSourceIDsByBatch(ctx, &b.ID)
				if err != nil {
					return err
				}
				for _, id := range ids {
					if err := finance.RemoveReceivable(ctx, q, "opening", id); err != nil {
						return err
					}
				}
				return nil
			}
			ids, err := q.PayableSourceIDsByBatch(ctx, &b.ID)
			if err != nil {
				return err
			}
			for _, id := range ids {
				if err := finance.RemovePayable(ctx, q, "opening", id); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

var (
	openingARImporter = openingLedgerImporter(ledgerKind{key: "opening_ar", label: "期初應收帳款", partner: "客戶", partnerCode: "C001", isAR: true})
	openingAPImporter = openingLedgerImporter(ledgerKind{key: "opening_ap", label: "期初應付帳款", partner: "供應商", partnerCode: "S001"})
)

// ---- 期初科目餘額 ----

var openingBalanceImporter = &importer{
	key: "opening_balance", label: "期初科目餘額", needsDate: true, undoable: true,
	desc: "匯入期初日期當天各會計科目的借方或貸方餘額,產生一張已過帳傳票;借貸合計須相等。存貨、應收、應付科目的金額應與期初庫存、期初應收 / 應付一致。整批可撤銷。",
	columns: []column{
		{"科目代號", true, "已建立、啟用的明細科目代號;同一科目只能出現一次"},
		{"借方金額", false, "整數元;與貸方擇一填寫"},
		{"貸方金額", false, "整數元;與借方擇一填寫"},
		{"說明", false, "最多 255 字"},
	},
	sample: [][]string{{"1101", "100000", "", "庫存現金"}, {"3101", "", "100000", "股本"}},
	exec: func(r *run) error {
		if has, err := r.q.HasOpeningBalanceVoucher(r.ctx, r.a.CompanyID); err != nil {
			return err
		} else if has {
			r.fail(0, "", "已匯入過期初科目餘額;如需重新匯入,請先到「匯入紀錄」撤銷前一批")
			return nil
		}
		as, err := r.q.AllAccountsForImport(r.ctx, r.a.CompanyID)
		if err != nil {
			return err
		}
		accts := map[string]db.AllAccountsForImportRow{}
		for _, a := range as {
			accts[a.Code] = a
		}
		seen := map[string]bool{}
		var entries []gl.RawEntry
		debit, credit := decimal.Zero, decimal.Zero
		for _, row := range r.rows {
			ok := true
			code := row.get(0)
			a, found := accts[code]
			switch {
			case !found:
				r.fail(row.n, "科目代號", "「%s」不存在", code)
				ok = false
			case !a.IsActive:
				r.fail(row.n, "科目代號", "%s 已停用", code)
				ok = false
			case !a.IsPostable:
				r.fail(row.n, "科目代號", "%s 是彙總科目,請填明細科目", code)
				ok = false
			case seen[code]:
				r.fail(row.n, "科目代號", "%s 在檔內重複", code)
				ok = false
			}
			d, c1 := nonNegative(r, row, 1, "借方金額", money.AmountPlaces)
			c, c2 := nonNegative(r, row, 2, "貸方金額", money.AmountPlaces)
			if c1 && c2 && d.IsPositive() == c.IsPositive() {
				r.fail(row.n, "借方金額", "借方與貸方須擇一填寫(且大於 0)")
				c1 = false
			}
			ok = checkLen(r, row, "說明", row.get(3), 255) && ok
			if ok && c1 && c2 {
				seen[code] = true
				entries = append(entries, gl.RawEntry{AccountID: a.ID, Debit: d, Credit: c, Description: row.get(3)})
				debit, credit = debit.Add(d), credit.Add(c)
			}
		}
		if len(r.errs) > 0 {
			return nil
		}
		if !debit.Equal(credit) {
			r.fail(0, "", "借貸合計不相等:借方 %s、貸方 %s,差額 %s", debit.String(), credit.String(), debit.Sub(credit).String())
			return nil
		}
		src := gl.Source{Type: "opening_balance", ID: r.batch.ID, No: fmt.Sprintf("OPEN-BAL-%d", r.batch.ID), Date: r.date,
			Desc: "期初科目餘額 " + r.date.Format(time.DateOnly)}
		if err := gl.PostAccounts(r.ctx, r.q, gl.Options{CompanyID: r.a.CompanyID, ActorID: &r.a.UserID}, src, entries); err != nil {
			return err
		}
		r.summary = fmt.Sprintf("%s 期初科目餘額 %d 個科目,借貸各 %s", r.date.Format(time.DateOnly), len(entries), debit.String())
		return nil
	},
	undo: func(ctx context.Context, q *db.Queries, a *authctx.Actor, b db.ImportBatch) error {
		return gl.ReverseSource(ctx, q, gl.Options{CompanyID: a.CompanyID, ActorID: &a.UserID}, "opening_balance", b.ID)
	},
}
