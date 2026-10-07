package permission

import "testing"

func TestCodesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, g := range Groups {
		for _, p := range g.Permissions {
			if seen[p.Code] {
				t.Errorf("權限代碼重複: %s", p.Code)
			}
			seen[p.Code] = true
			if !Known(p.Code) {
				t.Errorf("%s 應為已知權限", p.Code)
			}
		}
	}
	if Known("system.nope") {
		t.Fatal("未註冊的權限不應為已知")
	}
}
