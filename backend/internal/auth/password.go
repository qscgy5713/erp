package auth

import (
	"sync"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"erp/internal/shared/apperr"
)

const bcryptCost = 12

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

var (
	dummyOnce sync.Once
	dummyHash string
)

// burnPasswordCheck 帳號不存在時也做一次雜湊比對,讓回應時間一致,
// 避免攻擊者從回應快慢判斷帳號是否存在。
func burnPasswordCheck(pw string) {
	dummyOnce.Do(func() {
		dummyHash, _ = HashPassword("dummy-password-for-timing-1")
	})
	CheckPassword(dummyHash, pw)
}

// ValidatePassword 密碼政策:8–72 字元(bcrypt 上限 72 位元組),需同時包含英文字母與數字。
func ValidatePassword(field, pw string) error {
	msg := ""
	switch {
	case len([]rune(pw)) < 8:
		msg = "至少 8 個字元"
	case len(pw) > 72:
		msg = "過長(最多 72 位元組)"
	default:
		var letter, digit bool
		for _, r := range pw {
			letter = letter || unicode.IsLetter(r)
			digit = digit || unicode.IsDigit(r)
		}
		if !letter || !digit {
			msg = "需同時包含英文字母與數字"
		}
	}
	if msg != "" {
		return apperr.Validation(map[string]string{field: msg})
	}
	return nil
}
