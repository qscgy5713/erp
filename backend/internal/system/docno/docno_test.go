package docno

import (
	"testing"
	"time"
)

func TestFormatAndPeriod(t *testing.T) {
	d := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		format, wantKey, want string
	}{
		{"YYYYMMDD", "20261007", "PO202610070012"},
		{"YYYYMM", "202610", "PO2026100012"},
		{"YYYY", "2026", "PO20260012"},
		{"NONE", "", "PO0012"},
	}
	for _, tc := range cases {
		key, err := PeriodKey(tc.format, d)
		if err != nil || key != tc.wantKey {
			t.Fatalf("%s: key=%q err=%v", tc.format, key, err)
		}
		if got := Format("PO", key, 4, 12); got != tc.want {
			t.Errorf("%s: got %s want %s", tc.format, got, tc.want)
		}
	}
	if _, err := PeriodKey("YYMMDD", d); err == nil {
		t.Fatal("未知格式應回傳錯誤")
	}
	if got := Format("X", "", 3, 12345); got != "X12345" {
		t.Fatalf("超過長度時不應截斷: %s", got)
	}
}
