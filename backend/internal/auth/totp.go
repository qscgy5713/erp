package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 預設的 HMAC-SHA1,所有驗證器 App 都支援
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// 雙因素驗證:TOTP(RFC 6238),HMAC-SHA1、6 位數、30 秒一步,驗證時容許前後各一步的時鐘誤差。
const (
	totpDigits = 6
	totpPeriod = 30
	totpWindow = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret 產生 20 位元組(160 位元)的隨機密鑰。
func newTOTPSecret() ([]byte, error) {
	b := make([]byte, 20)
	_, err := rand.Read(b)
	return b, err
}

func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off]&0x7f) << 24) | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	mod := uint32(1)
	for range totpDigits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, bin%mod)
}

func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// verifyTOTP 驗證碼是否符合目前時間步或前後各一步;回傳符合的時間步。
// 為了不洩漏哪一步符合,三步都比較(常數時間比較)。
func verifyTOTP(secret []byte, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}
	var matched int64
	ok := false
	for d := int64(-totpWindow); d <= totpWindow; d++ {
		step := totpStep(now) + d
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, step)), []byte(code)) == 1 {
			matched, ok = step, true
		}
	}
	return matched, ok
}

// otpauthURI 驗證器 App 掃描 QR Code 用的網址。
func otpauthURI(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", b32.EncodeToString(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// ---- 密鑰加密(AES-256-GCM) ----

// deriveTOTPKey 沒有另外設定 TOTP_ENCRYPTION_KEY 時,由 JWT_SECRET 衍生。
func deriveTOTPKey(material []byte) [32]byte {
	return sha256.Sum256(append([]byte("erp-totp-key:"), material...))
}

func sealSecret(key [32]byte, secret []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, secret, nil), nil
}

var errBadSealed = errors.New("auth: 雙因素密鑰無法解密(TOTP_ENCRYPTION_KEY 或 JWT_SECRET 是否被更換?)")

func openSecret(key [32]byte, sealed []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errBadSealed
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err != nil {
		return nil, errBadSealed
	}
	return plain, nil
}

// ---- 備援碼 ----

const recoveryCodeCount = 10

// newRecoveryCodes 產生一組備援碼(顯示用 xxxxx-xxxxx,10 位小寫英數,去掉容易混淆的字元)。
func newRecoveryCodes() ([]string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	out := make([]string, recoveryCodeCount)
	for i := range out {
		b := make([]byte, 10)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		for j := range b {
			b[j] = alphabet[int(b[j])%len(alphabet)]
		}
		out[i] = string(b[:5]) + "-" + string(b[5:])
	}
	return out, nil
}

// normalizeRecoveryCode 使用者輸入的備援碼不分大小寫、可省略連字號與空白。
func normalizeRecoveryCode(s string) string {
	s = strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(s)))
	if len(s) != 10 {
		return ""
	}
	return s[:5] + "-" + s[5:]
}

func hashRecoveryCode(code string) []byte {
	h := sha256.Sum256([]byte("erp-recovery:" + code))
	return h[:]
}
