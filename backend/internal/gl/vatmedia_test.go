package gl

import (
	"strings"
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestBuildMediaLayout(t *testing.T) {
	const reg = "A12345678"
	docs := []MediaDoc{
		{Side: "purchase", DocNo: "GR1", DocType: "receipt", Date: day("2026-10-20"), InvoiceNo: "XY11223344", InvoiceKind: "triplicate",
			TaxKind: "taxable", PartnerTaxID: "87654321", Untaxed: d("2000"), Tax: d("100")},
		{Side: "sales", DocNo: "DN2", DocType: "delivery", Date: day("2026-10-16"), InvoiceNo: "AB00000002", TaxKind: "taxable",
			Untaxed: d("500.4"), Tax: d("25.4")}, // 二聯式:買方無統編;金額四捨五入為整數
		{Side: "sales", DocNo: "DN1", DocType: "delivery", Date: day("2026-10-15"), InvoiceNo: "AB12345678", TaxKind: "taxable",
			PartnerTaxID: "12345678", Untaxed: d("1000"), Tax: d("50")},
		{Side: "sales", DocNo: "DN3", DocType: "delivery", Date: day("2026-09-30"), InvoiceNo: "AB00000003", TaxKind: "exempt",
			PartnerTaxID: "12345678", Untaxed: d("300")},
	}
	lines, excluded, tot := BuildMedia(reg, docs)
	if len(excluded) != 0 {
		t.Fatalf("不應有未納入: %+v", excluded)
	}
	want := []string{
		// 銷項依日期:9/30 免稅 31、10/15 應稅 31、10/16 二聯式 32;最後是進項 21
		"31" + reg + "0000001" + "115" + "09" + "12345678" + "        " + "AB" + "00000003" + "000000000300" + "3" + "0000000000" + " " + "     " + "   ",
		"31" + reg + "0000002" + "115" + "10" + "12345678" + "        " + "AB" + "12345678" + "000000001000" + "1" + "0000000050" + " " + "     " + "   ",
		"32" + reg + "0000003" + "115" + "10" + "        " + "        " + "AB" + "00000002" + "000000000500" + "1" + "0000000025" + " " + "     " + "   ",
		"21" + reg + "0000004" + "115" + "10" + "        " + "87654321" + "XY" + "11223344" + "000000002000" + "1" + "0000000100" + "1" + "     " + "   ",
	}
	if len(lines) != len(want) {
		t.Fatalf("筆數 %d,期望 %d", len(lines), len(want))
	}
	for i, l := range lines {
		if len(l) != recordLen {
			t.Errorf("第 %d 筆長度 %d,須為 81", i+1, len(l))
		}
		if l != want[i] {
			t.Errorf("第 %d 筆\n got %q\nwant %q", i+1, l, want[i])
		}
	}
	// 以官方附件五的位置抽查:格式代號 1–2、稅籍 3–11、流水號 12–18、課稅別 62、稅額 63–72、扣抵代號 73
	l := lines[3]
	if l[0:2] != "21" || l[2:11] != reg || l[11:18] != "0000004" || l[61:62] != "1" || l[62:72] != "0000000100" || l[72:73] != "1" {
		t.Errorf("位置不符: %q", l)
	}
	if tot.SalesCount != 3 || !tot.SalesAmount.Equal(d("1800")) || !tot.SalesTax.Equal(d("75")) || tot.PurchaseCount != 1 || !tot.PurchaseTax.Equal(d("100")) {
		t.Errorf("totals = %+v", tot)
	}
}

func TestBuildMediaExclusions(t *testing.T) {
	base := MediaDoc{Side: "sales", DocNo: "X", DocType: "delivery", Date: day("2026-10-01"), InvoiceNo: "AB12345678", TaxKind: "taxable", Untaxed: d("100"), Tax: d("5")}
	pbase := MediaDoc{Side: "purchase", DocNo: "P", DocType: "receipt", Date: day("2026-10-01"), InvoiceNo: "XY12345678", InvoiceKind: "register3",
		TaxKind: "taxable", PartnerTaxID: "87654321", Untaxed: d("100"), Tax: d("5")}
	mut := func(d MediaDoc, f func(*MediaDoc)) MediaDoc { f(&d); return d }
	cases := []struct {
		name string
		doc  MediaDoc
		want string
	}{
		{"銷貨退回", mut(base, func(x *MediaDoc) { x.DocType = "return" }), "折讓證明單"},
		{"沒有發票", mut(base, func(x *MediaDoc) { x.InvoiceNo = "" }), "尚未登錄發票"},
		{"零稅率", mut(base, func(x *MediaDoc) { x.TaxKind = "zero" }), "零稅率"},
		{"發票格式", mut(base, func(x *MediaDoc) { x.InvoiceNo = "12345" }), "格式不符"},
		{"金額過大", mut(base, func(x *MediaDoc) { x.Untaxed = d("10000000000000") }), "超出欄位長度"},
		{"進貨退出", mut(pbase, func(x *MediaDoc) { x.DocType = "return" }), "折讓證明單"},
		{"未選憑證種類", mut(pbase, func(x *MediaDoc) { x.InvoiceKind = "" }), "沒有供應商發票"},
		{"免稅進貨", mut(pbase, func(x *MediaDoc) { x.TaxKind = "exempt" }), "免稅進貨"},
		{"供應商無統編", mut(pbase, func(x *MediaDoc) { x.PartnerTaxID = "" }), "沒有統一編號"},
	}
	for _, c := range cases {
		lines, ex, _ := BuildMedia("A12345678", []MediaDoc{c.doc})
		if len(lines) != 0 || len(ex) != 1 || !strings.Contains(ex[0].Reason, c.want) {
			t.Errorf("%s: lines=%d excluded=%+v", c.name, len(lines), ex)
		}
	}
	// 三種進項憑證對應的格式代號
	for kind, code := range map[string]string{"triplicate": "21", "register2": "22", "register3": "25"} {
		lines, _, _ := BuildMedia("A12345678", []MediaDoc{mut(pbase, func(x *MediaDoc) { x.InvoiceKind = kind })})
		if len(lines) != 1 || lines[0][:2] != code {
			t.Errorf("%s → %v", kind, lines)
		}
	}
	// 負數金額(例如資料異常)不得寫入
	if lines, ex, _ := BuildMedia("A12345678", []MediaDoc{mut(base, func(x *MediaDoc) { x.Untaxed = d("-1") })}); len(lines) != 0 || len(ex) != 1 {
		t.Errorf("負數應被排除: %v %v", lines, ex)
	}
}
