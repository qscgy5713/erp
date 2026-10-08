package gl

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// 營業稅進銷項媒體申報檔(D62)。欄位位置取自財政部〈營業稅電子資料申報繳稅作業要點〉附件五,
// 規格筆記見 doc/vat-media-spec.md。第一版只涵蓋:
//
//	銷項:格式 31(買方有統編)/ 32(買方無統編),課稅別為應稅或免稅;
//	進項:格式 21 / 22 / 25,課稅別為應稅、扣抵代號 1(可扣抵之進貨及費用)。
//
// 其餘(退回折讓 23 / 24 / 33 / 34、零稅率、免稅進貨、固定資產、彙總登錄、作廢發票)不產生記錄,
// 而是列入「未納入」清單說明原因,讓使用者知道申報檔與 401 彙總之間差在哪裡。

const recordLen = 81

var mediaInvoiceRe = regexp.MustCompile(`^([A-Z]{2})([0-9]{8})$`)

// MediaDoc 要寫入申報檔的單據(已統一銷項 / 進項的表示)。
type MediaDoc struct {
	Side         string // sales 銷項 / purchase 進項
	DocNo        string
	DocType      string // delivery / receipt / return
	Date         time.Time
	InvoiceNo    string
	InvoiceKind  string // 進項:triplicate / register2 / register3
	TaxKind      string // taxable / zero / exempt
	PartnerTaxID string
	Untaxed, Tax decimal.Decimal
}

// MediaExcluded 沒有納入申報檔的單據與原因。
type MediaExcluded struct {
	Side    string          `json:"side"`
	DocNo   string          `json:"doc_no"`
	Reason  string          `json:"reason"`
	Untaxed decimal.Decimal `json:"untaxed"`
	Tax     decimal.Decimal `json:"tax"`
}

// MediaTotals 申報檔內各類的筆數與金額,供使用者與 401 彙總對照。
type MediaTotals struct {
	SalesCount    int             `json:"sales_count"`
	SalesAmount   decimal.Decimal `json:"sales_amount"`
	SalesTax      decimal.Decimal `json:"sales_tax"`
	PurchaseCount int             `json:"purchase_count"`
	PurchaseAmt   decimal.Decimal `json:"purchase_amount"`
	PurchaseTax   decimal.Decimal `json:"purchase_tax"`
}

func putField(rec []byte, from, to int, v string) {
	// from / to 為 1 起算的位置(含)。v 已是定長內容
	copy(rec[from-1:to], v)
}

func zeroPad(n decimal.Decimal, width int) (string, bool) {
	s := n.Round(0).Abs().String()
	if n.Round(0).IsNegative() || len(s) > width {
		return "", false
	}
	return strings.Repeat("0", width-len(s)) + s, true
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

// formatCode 格式代號;回傳空字串表示本版不支援,reason 為原因。
func formatCode(d MediaDoc) (code, reason string) {
	if d.Side == "sales" {
		switch {
		case d.DocType == "return":
			return "", "銷貨退回須以折讓證明單(格式 33 / 34)申報,本版尚未支援"
		case d.InvoiceNo == "":
			return "", "尚未登錄發票號碼"
		case d.TaxKind == "zero":
			return "", "零稅率銷售須另附零稅率銷售額資料檔與通關方式註記,本版尚未支援"
		}
		if d.PartnerTaxID != "" {
			return "31", ""
		}
		return "32", ""
	}
	switch {
	case d.DocType == "return":
		return "", "進貨退出須以折讓證明單(格式 23 / 24)申報,本版尚未支援"
	case d.InvoiceKind == "":
		return "", "沒有供應商發票(進貨單未選憑證種類),不可扣抵、不列入申報檔"
	case d.TaxKind != "taxable":
		return "", "零稅率 / 免稅進貨本版尚未支援"
	case len(d.PartnerTaxID) != 8:
		return "", "供應商沒有統一編號"
	}
	switch d.InvoiceKind {
	case "triplicate":
		return "21", ""
	case "register2":
		return "22", ""
	case "register3":
		return "25", ""
	}
	return "", "憑證種類不正確"
}

// BuildMedia 產生申報檔記錄(每筆固定 81 字元,不含換行)與未納入清單。
// taxRegNo 為公司 9 碼稅籍編號;記錄依「銷項、進項」各自按日期與單號排序,流水號連續編號。
func BuildMedia(taxRegNo string, docs []MediaDoc) (lines []string, excluded []MediaExcluded, totals MediaTotals) {
	sorted := append([]MediaDoc(nil), docs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Side != b.Side {
			return a.Side == "sales" // 銷項在前
		}
		if !a.Date.Equal(b.Date) {
			return a.Date.Before(b.Date)
		}
		return a.DocNo < b.DocNo
	})
	exclude := func(d MediaDoc, reason string) {
		excluded = append(excluded, MediaExcluded{Side: d.Side, DocNo: d.DocNo, Reason: reason, Untaxed: signedDoc(d, d.Untaxed), Tax: signedDoc(d, d.Tax)})
	}
	for _, d := range sorted {
		code, reason := formatCode(d)
		if code == "" {
			exclude(d, reason)
			continue
		}
		m := mediaInvoiceRe.FindStringSubmatch(d.InvoiceNo)
		if m == nil {
			exclude(d, "發票號碼格式不符(須為 2 碼英文字軌加 8 碼數字)")
			continue
		}
		roc := d.Date.Year() - 1911
		if roc < 1 || roc > 999 {
			exclude(d, "發票日期超出可申報範圍")
			continue
		}
		amount, ok1 := zeroPad(d.Untaxed, 12)
		tax, ok2 := zeroPad(d.Tax, 10)
		if !ok1 || !ok2 {
			exclude(d, "金額超出欄位長度或為負數")
			continue
		}
		rec := []byte(strings.Repeat(" ", recordLen))
		putField(rec, 1, 2, code)
		putField(rec, 3, 11, padRight(taxRegNo, 9))
		putField(rec, 12, 18, fmt.Sprintf("%07d", len(lines)+1))
		putField(rec, 19, 21, fmt.Sprintf("%03d", roc))
		putField(rec, 22, 23, fmt.Sprintf("%02d", int(d.Date.Month())))
		if d.Side == "sales" {
			putField(rec, 24, 31, padRight(d.PartnerTaxID, 8)) // 買受人統一編號(二聯式為空白)
		} else {
			putField(rec, 32, 39, padRight(d.PartnerTaxID, 8)) // 銷售人(供應商)統一編號
		}
		putField(rec, 40, 41, m[1])
		putField(rec, 42, 49, m[2])
		putField(rec, 50, 61, amount)
		if d.TaxKind == "exempt" {
			putField(rec, 62, 62, "3")
		} else {
			putField(rec, 62, 62, "1")
		}
		putField(rec, 63, 72, tax)
		if d.Side == "purchase" {
			putField(rec, 73, 73, "1") // 進項可扣抵之進貨及費用
		}
		lines = append(lines, string(rec))
		if d.Side == "sales" {
			totals.SalesCount++
			totals.SalesAmount = totals.SalesAmount.Add(d.Untaxed.Round(0))
			totals.SalesTax = totals.SalesTax.Add(d.Tax.Round(0))
		} else {
			totals.PurchaseCount++
			totals.PurchaseAmt = totals.PurchaseAmt.Add(d.Untaxed.Round(0))
			totals.PurchaseTax = totals.PurchaseTax.Add(d.Tax.Round(0))
		}
	}
	return lines, excluded, totals
}

func signedDoc(d MediaDoc, v decimal.Decimal) decimal.Decimal {
	if d.DocType == "return" {
		return v.Neg()
	}
	return v
}
