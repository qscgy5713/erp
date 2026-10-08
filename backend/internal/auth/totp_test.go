package auth

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 附錄 B 的測試向量(SHA-1,密鑰 "12345678901234567890",8 位數取後 6 位)。
func TestTOTPRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"},
		{1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"},
	}
	for _, c := range cases {
		if got := totpCode(secret, c.unix/30); got != c.want {
			t.Errorf("t=%d code=%s want %s", c.unix, got, c.want)
		}
	}
}

func TestVerifyTOTPWindow(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111109, 0)
	step := totpStep(now)
	for d, ok := range map[int64]bool{-2: false, -1: true, 0: true, 1: true, 2: false} {
		got, valid := verifyTOTP(secret, totpCode(secret, step+d), now)
		if valid != ok || (valid && got != step+d) {
			t.Errorf("偏移 %d:valid=%v step=%d", d, valid, got)
		}
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef", " 000000x"} {
		if _, ok := verifyTOTP(secret, bad, now); ok {
			t.Errorf("%q 不應通過", bad)
		}
	}
}

func TestSealOpenSecret(t *testing.T) {
	key := deriveTOTPKey([]byte("some-jwt-secret-material-0123456789"))
	secret, _ := newTOTPSecret()
	sealed, err := sealSecret(key, secret)
	if err != nil {
		t.Fatal(err)
	}
	got, err := openSecret(key, sealed)
	if err != nil || string(got) != string(secret) {
		t.Fatalf("round trip 失敗 err=%v", err)
	}
	// 密文不含明文;換金鑰 / 竄改都無法解開
	if strings.Contains(string(sealed), string(secret)) {
		t.Fatal("密文不應含明文")
	}
	other := deriveTOTPKey([]byte("another-material-0123456789-abcdefgh"))
	if _, err := openSecret(other, sealed); err == nil {
		t.Fatal("換金鑰應解不開")
	}
	sealed[len(sealed)-1] ^= 0xff
	if _, err := openSecret(key, sealed); err == nil {
		t.Fatal("竄改應解不開")
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := newRecoveryCodes()
	if err != nil || len(codes) != recoveryCodeCount {
		t.Fatalf("codes=%v err=%v", codes, err)
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if seen[c] || len(c) != 11 || c[5] != '-' {
			t.Fatalf("格式或重複: %q", c)
		}
		seen[c] = true
		// 使用者輸入大寫、少連字號、多空白都能對上
		in := strings.ToUpper(strings.ReplaceAll(c, "-", " ")) + " "
		if normalizeRecoveryCode(in) != c {
			t.Fatalf("normalize(%q) = %q, want %q", in, normalizeRecoveryCode(in), c)
		}
	}
	if normalizeRecoveryCode("short") != "" || normalizeRecoveryCode("") != "" {
		t.Fatal("長度不對應回傳空字串")
	}
	if string(hashRecoveryCode(codes[0])) == string(hashRecoveryCode(codes[1])) {
		t.Fatal("不同備援碼雜湊不應相同")
	}
}

func TestOtpauthURI(t *testing.T) {
	u := otpauthURI("ERP", "admin", []byte("12345678901234567890"))
	for _, want := range []string{"otpauth://totp/ERP:admin?", "secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", "issuer=ERP", "digits=6", "period=30"} {
		if !strings.Contains(u, want) {
			t.Errorf("URI 缺少 %q: %s", want, u)
		}
	}
}
