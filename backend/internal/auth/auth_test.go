package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestValidatePassword(t *testing.T) {
	cases := map[string]bool{
		"abc12345":               true,
		"密碼abcd1234":             true,
		"short1":                 false, // 太短
		"onlyletters":            false,
		"12345678":               false,
		strings.Repeat("a1", 37): false, // 74 位元組,超過 bcrypt 上限
	}
	for pw, ok := range cases {
		if err := ValidatePassword("password", pw); (err == nil) != ok {
			t.Errorf("%q: err=%v, want ok=%v", pw, err, ok)
		}
	}
}

func TestTokenRoundTripAndTampering(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	iss := NewTokenIssuer(secret, time.Minute)
	tok, _, err := iss.Issue(42, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	c, err := iss.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if uid, _ := c.UserID(); uid != 42 || c.CompanyID != 1 || c.TokenVersion != 3 {
		t.Fatalf("claims = %+v", c)
	}

	other := NewTokenIssuer([]byte("another-secret-another-secret-xx"), time.Minute)
	if _, err := other.Parse(tok); err == nil {
		t.Fatal("不同密鑰簽的 token 應無效")
	}

	// alg=none 攻擊
	none := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer: issuer, Subject: "1", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}})
	s, _ := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, err := iss.Parse(s); err == nil {
		t.Fatal("alg=none 的 token 應被拒絕")
	}

	// 過期
	past := NewTokenIssuer(secret, time.Minute)
	past.now = func() time.Time { return time.Now().Add(-2 * time.Minute) }
	old, _, _ := past.Issue(1, 1, 1)
	if _, err := iss.Parse(old); err == nil {
		t.Fatal("過期 token 應無效")
	}
}

func TestRefreshTokenHash(t *testing.T) {
	raw, hash, err := newRefreshToken()
	if err != nil || len(raw) < 40 || len(hash) != 32 {
		t.Fatalf("raw=%q hash=%d err=%v", raw, len(hash), err)
	}
	if string(hashToken(raw)) != string(hash) {
		t.Fatal("雜湊不一致")
	}
}
