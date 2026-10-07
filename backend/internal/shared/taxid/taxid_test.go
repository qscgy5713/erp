package taxid

import "testing"

func TestValid(t *testing.T) {
	cases := map[string]bool{
		"04595257": true,  // 一般合法統編
		"10458575": true,  // 第 7 位為 7,總和 30 本身可被 5 整除
		"10458574": true,  // 第 7 位為 7,總和 29,僅「+1」規則成立
		"10458573": false, // 第 7 位為 7,總和 28,兩種算法都不成立
		"22099131": true,
		"12345678": false, // 檢查碼錯誤
		"0459525":  false, // 長度不足
		"0459525A": false, // 非數字
		"":         false,
	}
	for in, want := range cases {
		if got := Valid(in); got != want {
			t.Errorf("Valid(%q) = %v, want %v", in, got, want)
		}
	}
}
