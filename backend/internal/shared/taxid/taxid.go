// Package taxid 驗證台灣營利事業統一編號。
//
// 規則:8 位數字,依權數 1,2,1,2,1,2,4,1 相乘後各位數相加,
// 總和能被 5 整除即為合法(財政部 2023 年起由 10 改為 5,舊編號仍相容);
// 第 7 位為 7 時,乘積 28 的位數和可視為 10 或 1,總和或總和+1 能被 5 整除皆可。
package taxid

var weights = [8]int{1, 2, 1, 2, 1, 2, 4, 1}

func Valid(s string) bool {
	if len(s) != 8 {
		return false
	}
	sum := 0
	for i := range 8 {
		d := int(s[i] - '0')
		if d < 0 || d > 9 {
			return false
		}
		p := d * weights[i]
		sum += p/10 + p%10
	}
	if sum%5 == 0 {
		return true
	}
	return s[6] == '7' && (sum+1)%5 == 0
}
