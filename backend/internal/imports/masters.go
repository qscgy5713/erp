package imports

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/shared/money"
	"erp/internal/shared/taxid"
)

// lookups 主檔對照表(代號 → id),啟用中才可被引用。
type lookups struct {
	units      map[string]int64
	taxTypes   map[string]int64
	terms      map[string]int64
	categories map[string]int64
	currencies map[string]int16
}

func loadLookups(r *run) (*lookups, error) {
	l := &lookups{units: map[string]int64{}, taxTypes: map[string]int64{}, terms: map[string]int64{},
		categories: map[string]int64{}, currencies: map[string]int16{}}
	cid := r.a.CompanyID
	us, err := r.q.ListUnits(r.ctx, cid)
	if err != nil {
		return nil, err
	}
	for _, u := range us {
		if u.IsActive {
			l.units[strings.ToUpper(u.Code)] = u.ID
		}
	}
	ts, err := r.q.ListTaxTypes(r.ctx, cid)
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		if t.IsActive {
			l.taxTypes[strings.ToUpper(t.Code)] = t.ID
		}
	}
	ps, err := r.q.ListPaymentTerms(r.ctx, cid)
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		if p.IsActive {
			l.terms[strings.ToUpper(p.Code)] = p.ID
		}
	}
	cs, err := r.q.ListItemCategories(r.ctx, cid)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		if c.IsActive {
			l.categories[strings.ToUpper(c.Code)] = c.ID
		}
	}
	cu, err := r.q.CurrencyActive(r.ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range cu {
		l.currencies[c.Code] = c.Decimals
	}
	return l, nil
}

// ref 依代號查 id;空白回傳 (nil, true);查不到記錄錯誤並回傳 (nil, false)。
func ref(r *run, row sheetRow, i int, col string, m map[string]int64) (*int64, bool) {
	v := row.get(i)
	if v == "" {
		return nil, true
	}
	id, ok := m[strings.ToUpper(v)]
	if !ok {
		r.fail(row.n, col, "「%s」不存在或已停用", v)
		return nil, false
	}
	return &id, true
}

func checkLen(r *run, row sheetRow, col, v string, maxLen int) bool {
	if utf8.RuneCountInString(v) > maxLen {
		r.fail(row.n, col, "最多 %d 字", maxLen)
		return false
	}
	return true
}

// nonNegative 空白視為 0;不是數字或為負數記錄錯誤。
func nonNegative(r *run, row sheetRow, i int, col string, places int32) (decimal.Decimal, bool) {
	v := row.get(i)
	if v == "" {
		return decimal.Zero, true
	}
	d, err := parseDecimal(v)
	switch {
	case err != nil:
		r.fail(row.n, col, "「%s」不是數字", v)
	case d.IsNegative():
		r.fail(row.n, col, "不可為負數")
	case !d.Round(places).Equal(d):
		r.fail(row.n, col, "最多 %d 位小數", places)
	default:
		return d, true
	}
	return decimal.Zero, false
}

// ---- 料品 ----

var itemsImporter = &importer{
	key: "items", label: "料品", desc: "匯入料品主檔(單一基本單位;多單位換算請於畫面維護)。已存在的料號會被視為錯誤,不會覆蓋。",
	columns: []column{
		{"料號", true, "最多 40 字,不可與現有料品或檔內其他列重複"},
		{"品名", true, "最多 200 字"},
		{"規格", false, "最多 255 字"},
		{"分類代號", false, "須為已建立且啟用的料品分類代號"},
		{"類型", false, "商品 或 服務;空白為商品。服務類不管庫存"},
		{"基本單位代號", true, "須為已建立且啟用的單位代號,例如 PCS"},
		{"條碼", false, "不可與現有料品或檔內其他列重複"},
		{"稅別代號", false, "須為已建立且啟用的稅別代號,例如 TX5"},
		{"安全庫存", false, "基本單位數量,最多 4 位小數"},
		{"建議售價", false, "未稅、每基本單位,最多 6 位小數"},
		{"備註", false, ""},
		{"批號管理", false, "不管理(空白)/ 批號 / 批號與效期;服務類不可管理批號。匯入期初庫存前先設定好"},
	},
	sample: [][]string{{"PEN-01", "原子筆", "藍 0.5mm", "", "商品", "PCS", "4710000000011", "TX5", "100", "15", "", ""}, {"MILK-01", "鮮乳 1L", "", "", "商品", "PCS", "", "TX5", "0", "60", "", "批號與效期"}},
	exec: func(r *run) error {
		l, err := loadLookups(r)
		if err != nil {
			return err
		}
		existing, err := r.q.AllItemCodes(r.ctx, r.a.CompanyID)
		if err != nil {
			return err
		}
		codes, barcodes := map[string]bool{}, map[string]bool{}
		for _, e := range existing {
			codes[strings.ToUpper(e.Code)] = true
		}
		bcs, err := r.q.AllBarcodes(r.ctx, r.a.CompanyID)
		if err != nil {
			return err
		}
		for _, b := range bcs {
			if b != nil {
				barcodes[*b] = true
			}
		}
		var todo []db.CreateItemParams
		for _, row := range r.rows {
			code, name := row.get(0), row.get(1)
			ok := true
			switch {
			case code == "":
				r.fail(row.n, "料號", "必填")
				ok = false
			case utf8.RuneCountInString(code) > 40:
				r.fail(row.n, "料號", "最多 40 字")
				ok = false
			case codes[strings.ToUpper(code)]:
				r.fail(row.n, "料號", "「%s」已存在或在檔內重複", code)
				ok = false
			}
			if name == "" {
				r.fail(row.n, "品名", "必填")
				ok = false
			} else if !checkLen(r, row, "品名", name, 200) {
				ok = false
			}
			ok = checkLen(r, row, "規格", row.get(2), 255) && ok
			cat, c1 := ref(r, row, 3, "分類代號", l.categories)
			itemType := "goods"
			switch row.get(4) {
			case "", "商品":
			case "服務":
				itemType = "service"
			default:
				r.fail(row.n, "類型", "須為「商品」或「服務」")
				ok = false
			}
			unit, c2 := ref(r, row, 5, "基本單位代號", l.units)
			if unit == nil && c2 {
				r.fail(row.n, "基本單位代號", "必填")
				c2 = false
			}
			var barcode *string
			if b := row.get(6); b != "" {
				switch {
				case utf8.RuneCountInString(b) > 50:
					r.fail(row.n, "條碼", "最多 50 字")
					ok = false
				case barcodes[b]:
					r.fail(row.n, "條碼", "「%s」已存在或在檔內重複", b)
					ok = false
				default:
					barcode = &b
				}
			}
			tax, c3 := ref(r, row, 7, "稅別代號", l.taxTypes)
			safety, c4 := nonNegative(r, row, 8, "安全庫存", money.QuantityPlaces)
			price, c5 := nonNegative(r, row, 9, "建議售價", money.UnitPricePlaces)
			lotControl := "none"
			switch row.get(11) {
			case "", "不管理":
			case "批號":
				lotControl = "lot"
			case "批號與效期":
				lotControl = "lot_expiry"
			default:
				r.fail(row.n, "批號管理", "須為「不管理」、「批號」或「批號與效期」")
				ok = false
			}
			if lotControl != "none" && itemType == "service" {
				r.fail(row.n, "批號管理", "服務類料品沒有庫存,不能做批號管理")
				ok = false
			}
			if !ok || !c1 || !c2 || !c3 || !c4 || !c5 {
				continue
			}
			codes[strings.ToUpper(code)] = true
			if barcode != nil {
				barcodes[*barcode] = true
			}
			todo = append(todo, db.CreateItemParams{
				CompanyID: r.a.CompanyID, Code: code, Name: name, Spec: row.get(2), CategoryID: cat, ItemType: itemType,
				BaseUnitID: *unit, Barcode: barcode, TaxTypeID: tax, SafetyStock: safety, ListPrice: price,
				Note: row.get(10), LotControl: lotControl, CreatedBy: &r.a.UserID,
			})
		}
		if len(r.errs) > 0 {
			return nil
		}
		for _, p := range todo {
			if _, err := r.q.CreateItem(r.ctx, p); err != nil {
				return err
			}
		}
		r.summary = "新增 " + itoa(len(todo)) + " 筆料品"
		return nil
	},
}

// ---- 客戶 / 供應商 ----

func contactsJSON(name string) []byte {
	if name == "" {
		return []byte("[]")
	}
	b, _ := json.Marshal([]map[string]string{{"name": name, "title": "", "phone": "", "email": ""}})
	return b
}

func addressesJSON(addr string) []byte {
	if addr == "" {
		return []byte("[]")
	}
	b, _ := json.Marshal([]map[string]any{{"label": "公司", "zip": "", "address": addr, "is_default": true}})
	return b
}

// partnerRow 客戶與供應商共用欄位的解析結果。
type partnerRow struct {
	code, name, short, phone, email, note, currency string
	taxID                                           *string
	taxType, term                                   *int64
	contacts, addresses                             []byte
}

// parsePartner 解析共用欄位(代號、名稱、簡稱、統編、電話、Email、幣別、稅別、付款條件、聯絡人、地址、備註)。
// 欄位位置由 pos 指定,各自的專屬欄位由呼叫端處理。
func parsePartner(r *run, row sheetRow, l *lookups, codes map[string]bool, pos [12]int, kind string) (partnerRow, bool) {
	var p partnerRow
	ok := true
	p.code, p.name = row.get(pos[0]), row.get(pos[1])
	switch {
	case p.code == "":
		r.fail(row.n, kind+"代號", "必填")
		ok = false
	case utf8.RuneCountInString(p.code) > 20:
		r.fail(row.n, kind+"代號", "最多 20 字")
		ok = false
	case codes[strings.ToUpper(p.code)]:
		r.fail(row.n, kind+"代號", "「%s」已存在或在檔內重複", p.code)
		ok = false
	}
	if p.name == "" {
		r.fail(row.n, "名稱", "必填")
		ok = false
	} else {
		ok = checkLen(r, row, "名稱", p.name, 200) && ok
	}
	p.short, p.phone, p.email, p.note = row.get(pos[2]), row.get(pos[4]), row.get(pos[5]), row.get(pos[11])
	ok = checkLen(r, row, "簡稱", p.short, 50) && checkLen(r, row, "電話", p.phone, 50) && checkLen(r, row, "Email", p.email, 255) && ok
	if t := row.get(pos[3]); t != "" {
		if !taxid.Valid(t) {
			r.fail(row.n, "統一編號", "「%s」不是有效的統一編號(8 碼,含檢查碼)", t)
			ok = false
		} else {
			p.taxID = &t
		}
	}
	p.currency = strings.ToUpper(row.get(pos[6]))
	if p.currency == "" {
		p.currency = "TWD"
	}
	if _, found := l.currencies[p.currency]; !found {
		r.fail(row.n, "幣別", "「%s」不存在或已停用", p.currency)
		ok = false
	}
	var c1, c2 bool
	p.taxType, c1 = ref(r, row, pos[7], "稅別代號", l.taxTypes)
	p.term, c2 = ref(r, row, pos[8], "付款條件代號", l.terms)
	ok = ok && c1 && c2
	contact, addr := row.get(pos[9]), row.get(pos[10])
	ok = checkLen(r, row, "聯絡人", contact, 50) && checkLen(r, row, "地址", addr, 255) && ok
	p.contacts, p.addresses = contactsJSON(contact), addressesJSON(addr)
	return p, ok
}

var customersImporter = &importer{
	key: "customers", label: "客戶", desc: "匯入客戶主檔。已存在的客戶代號會被視為錯誤,不會覆蓋。",
	columns: []column{
		{"客戶代號", true, "最多 20 字,不可重複"},
		{"名稱", true, "最多 200 字"},
		{"簡稱", false, "最多 50 字"},
		{"統一編號", false, "8 碼,會驗證檢查碼"},
		{"電話", false, ""},
		{"Email", false, ""},
		{"幣別", false, "例如 TWD、USD;空白為 TWD"},
		{"稅別代號", false, "已建立的稅別代號,例如 TX5"},
		{"付款條件代號", false, "已建立的付款條件代號,例如 M30"},
		{"聯絡人", false, "建立一位聯絡人"},
		{"地址", false, "建立一筆預設地址"},
		{"備註", false, ""},
		{"發票抬頭", false, "最多 200 字"},
		{"信用額度", false, "本位幣、含稅;0 或空白為不限"},
		{"負責業務帳號", false, "使用者帳號;決定業務的資料範圍"},
	},
	sample: [][]string{{"C001", "大同股份有限公司", "大同", "04595257", "02-2345-6789", "buy@example.com", "TWD", "TX5", "M30", "王小明", "台北市中山區 1 號", "", "大同股份有限公司", "500000", "sales1"}},
	exec: func(r *run) error {
		l, err := loadLookups(r)
		if err != nil {
			return err
		}
		existing, err := r.q.AllCustomerCodes(r.ctx, r.a.CompanyID)
		if err != nil {
			return err
		}
		codes := map[string]bool{}
		for _, e := range existing {
			codes[strings.ToUpper(e.Code)] = true
		}
		var todo []db.CreateCustomerParams
		for _, row := range r.rows {
			p, ok := parsePartner(r, row, l, codes, [12]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, "客戶")
			title := row.get(12)
			ok = checkLen(r, row, "發票抬頭", title, 200) && ok
			limit, c1 := nonNegative(r, row, 13, "信用額度", 4)
			var salesID *int64
			c2 := true
			if u := row.get(14); u != "" {
				usr, err := r.q.GetUserByUsername(r.ctx, u)
				switch {
				case err != nil || usr.CompanyID != r.a.CompanyID:
					r.fail(row.n, "負責業務帳號", "帳號「%s」不存在", u)
					c2 = false
				default:
					salesID = &usr.ID
				}
			}
			if !ok || !c1 || !c2 {
				continue
			}
			codes[strings.ToUpper(p.code)] = true
			todo = append(todo, db.CreateCustomerParams{
				CompanyID: r.a.CompanyID, Code: p.code, Name: p.name, ShortName: p.short, TaxID: p.taxID,
				InvoiceTitle: title, Phone: p.phone, Email: p.email, Contacts: p.contacts, Addresses: p.addresses,
				Currency: p.currency, TaxTypeID: p.taxType, PaymentTermID: p.term, CreditLimit: limit,
				SalesUserID: salesID, Note: p.note, CreatedBy: &r.a.UserID,
			})
		}
		if len(r.errs) > 0 {
			return nil
		}
		for _, p := range todo {
			if _, err := r.q.CreateCustomer(r.ctx, p); err != nil {
				return err
			}
		}
		r.summary = "新增 " + itoa(len(todo)) + " 位客戶"
		return nil
	},
}

var suppliersImporter = &importer{
	key: "suppliers", label: "供應商", desc: "匯入供應商主檔。已存在的供應商代號會被視為錯誤,不會覆蓋。",
	columns: []column{
		{"供應商代號", true, "最多 20 字,不可重複"},
		{"名稱", true, "最多 200 字"},
		{"簡稱", false, "最多 50 字"},
		{"統一編號", false, "8 碼,會驗證檢查碼"},
		{"電話", false, ""},
		{"Email", false, ""},
		{"幣別", false, "例如 TWD、USD;空白為 TWD"},
		{"稅別代號", false, "已建立的稅別代號,例如 TX5"},
		{"付款條件代號", false, "已建立的付款條件代號,例如 M30"},
		{"聯絡人", false, "建立一位聯絡人"},
		{"地址", false, "建立一筆預設地址"},
		{"備註", false, ""},
		{"銀行名稱", false, "匯款資訊,最多 100 字"},
		{"銀行帳號", false, "匯款資訊,最多 50 字"},
	},
	sample: [][]string{{"S001", "文具批發有限公司", "文具批發", "", "02-8765-4321", "", "TWD", "TX5", "M30", "李小華", "新北市板橋區 2 號", "", "第一銀行", "123-45-678901"}},
	exec: func(r *run) error {
		l, err := loadLookups(r)
		if err != nil {
			return err
		}
		existing, err := r.q.AllSupplierCodes(r.ctx, r.a.CompanyID)
		if err != nil {
			return err
		}
		codes := map[string]bool{}
		for _, e := range existing {
			codes[strings.ToUpper(e.Code)] = true
		}
		var todo []db.CreateSupplierParams
		for _, row := range r.rows {
			p, ok := parsePartner(r, row, l, codes, [12]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, "供應商")
			ok = checkLen(r, row, "銀行名稱", row.get(12), 100) && checkLen(r, row, "銀行帳號", row.get(13), 50) && ok
			if !ok {
				continue
			}
			codes[strings.ToUpper(p.code)] = true
			todo = append(todo, db.CreateSupplierParams{
				CompanyID: r.a.CompanyID, Code: p.code, Name: p.name, ShortName: p.short, TaxID: p.taxID, Phone: p.phone,
				Email: p.email, Contacts: p.contacts, Addresses: p.addresses, Currency: p.currency, TaxTypeID: p.taxType,
				PaymentTermID: p.term, BankName: row.get(12), BankAccount: row.get(13), Note: p.note, CreatedBy: &r.a.UserID,
			})
		}
		if len(r.errs) > 0 {
			return nil
		}
		for _, p := range todo {
			if _, err := r.q.CreateSupplier(r.ctx, p); err != nil {
				return err
			}
		}
		r.summary = "新增 " + itoa(len(todo)) + " 家供應商"
		return nil
	},
}

func itoa(n int) string { return strconv.Itoa(n) }
