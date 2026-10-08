package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // RFC 6238
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"erp/internal/auth"
)

// 測試用的 TOTP 產生器(RFC 6238,與驗證器 App 相同)。
func totpAt(t *testing.T, secretB32 string, step int64) string {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretB32)
	if err != nil {
		t.Fatal(err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off]&0x7f) << 24) | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%06d", bin%1000000)
}

func nowStep() int64 { return time.Now().Unix() / 30 }

type enrolled struct {
	secret   string
	recovery []string
}

func (e *env) resetTOTPStep(username string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), "UPDATE users SET totp_last_step = 0 WHERE lower(username) = lower($1)", username); err != nil {
		e.t.Fatal(err)
	}
}

// enroll 設定並啟用雙因素驗證,回傳密鑰與備援碼。
func (c *client) enroll() enrolled {
	c.e.t.Helper()
	res := c.do(http.MethodPost, "/auth/2fa/setup", nil)
	expect(c.e.t, res, http.StatusOK, "")
	setup := decode[struct {
		Secret     string `json:"secret"`
		OtpauthURI string `json:"otpauth_uri"`
	}](c.e.t, res.Data)
	if !strings.HasPrefix(setup.OtpauthURI, "otpauth://totp/") || !strings.Contains(setup.OtpauthURI, setup.Secret) {
		c.e.t.Fatalf("otpauth uri = %s", setup.OtpauthURI)
	}
	res = c.do(http.MethodPost, "/auth/2fa/enable", map[string]string{"code": totpAt(c.e.t, setup.Secret, nowStep())})
	expect(c.e.t, res, http.StatusOK, "")
	codes := decode[struct {
		Codes []string `json:"recovery_codes"`
	}](c.e.t, res.Data).Codes
	return enrolled{secret: setup.Secret, recovery: codes}
}

type loginChallenge struct {
	Required  bool   `json:"two_factor_required"`
	Challenge string `json:"challenge"`
}

func (c *client) pwLogin(username, password string) (loginChallenge, apiResp) {
	c.e.t.Helper()
	res := c.do(http.MethodPost, "/auth/login", map[string]string{"username": username, "password": password})
	if res.status != http.StatusOK {
		return loginChallenge{}, res
	}
	return decode[loginChallenge](c.e.t, res.Data), res
}

func (c *client) twoFactorLogin(challenge, code string) apiResp {
	c.e.t.Helper()
	res := c.do(http.MethodPost, "/auth/login/2fa", map[string]string{"challenge": challenge, "code": code})
	if res.status == http.StatusOK {
		c.token = decode[struct {
			AccessToken string `json:"access_token"`
		}](c.e.t, res.Data).AccessToken
	}
	return res
}

func TestTwoFactorEnrollmentAndLogin(t *testing.T) {
	e := newEnv(t)
	e.seedUser("alice", pw, false, false)
	c := e.loggedIn("alice", pw)

	// 設定前:狀態未啟用;未產生密鑰不能啟用
	st := decode[struct {
		Enabled bool `json:"enabled"`
	}](t, c.do(http.MethodGet, "/auth/2fa", nil).Data)
	if st.Enabled {
		t.Fatal("一開始不應啟用")
	}
	expect(t, c.do(http.MethodPost, "/auth/2fa/enable", map[string]string{"code": "123456"}), http.StatusConflict, "AUTH-012")
	// 驗證碼錯誤不能啟用;正確才啟用,並拿到 10 組備援碼
	res := c.do(http.MethodPost, "/auth/2fa/setup", nil)
	secret := decode[struct {
		Secret string `json:"secret"`
	}](t, res.Data).Secret
	expect(t, c.do(http.MethodPost, "/auth/2fa/enable", map[string]string{"code": "000000"}), http.StatusUnauthorized, "AUTH-008")
	res = c.do(http.MethodPost, "/auth/2fa/enable", map[string]string{"code": totpAt(t, secret, nowStep())})
	expect(t, res, http.StatusOK, "")
	codes := decode[struct {
		Codes []string `json:"recovery_codes"`
	}](t, res.Data).Codes
	if len(codes) != 10 {
		t.Fatalf("備援碼 %d 組", len(codes))
	}
	// 已啟用:不能再產生密鑰或重複啟用
	expect(t, c.do(http.MethodPost, "/auth/2fa/setup", nil), http.StatusConflict, "AUTH-010")
	st2 := decode[struct {
		Enabled      bool `json:"enabled"`
		RecoveryLeft int  `json:"recovery_codes_left"`
	}](t, c.do(http.MethodGet, "/auth/2fa", nil).Data)
	if !st2.Enabled || st2.RecoveryLeft != 10 {
		t.Fatalf("status = %+v", st2)
	}

	// 密鑰不以明文存在資料庫
	var raw []byte
	if err := e.pool.QueryRow(context.Background(), "SELECT totp_secret_enc FROM users WHERE username = 'alice'").Scan(&raw); err != nil || strings.Contains(string(raw), secret) || len(raw) < 30 {
		t.Fatalf("密鑰應加密儲存 err=%v len=%d", err, len(raw))
	}

	// 登入:密碼正確只拿到挑戰憑證,沒有 access token、沒有 refresh cookie
	anon := &client{e: e}
	ch, res := anon.pwLogin("alice", pw)
	expect(t, res, http.StatusOK, "")
	if !ch.Required || ch.Challenge == "" || len(res.cookies) != 0 || strings.Contains(string(res.Data), "access_token") {
		t.Fatalf("登入回應 = %s cookies=%d", res.Data, len(res.cookies))
	}
	// 挑戰憑證不能當 access token 用
	anon.token = ch.Challenge
	expect(t, anon.do(http.MethodGet, "/auth/me", nil), http.StatusUnauthorized, "SYS-401")
	anon.token = ""

	// 驗證碼錯誤 / 格式錯 / 挑戰無效
	expect(t, anon.twoFactorLogin(ch.Challenge, "000000"), http.StatusUnauthorized, "AUTH-008")
	expect(t, anon.twoFactorLogin(ch.Challenge, "abc"), http.StatusUnauthorized, "AUTH-008")
	expect(t, anon.twoFactorLogin("garbage", "123456"), http.StatusUnauthorized, "AUTH-009")
	// 啟用時用過的時間步不能再用(重放);前一步已過期的碼(超出窗口)也不行
	expect(t, anon.twoFactorLogin(ch.Challenge, totpAt(t, secret, nowStep())), http.StatusUnauthorized, "AUTH-008")
	expect(t, anon.twoFactorLogin(ch.Challenge, totpAt(t, secret, nowStep()-3)), http.StatusUnauthorized, "AUTH-008")
	// 下一步的碼(窗口內)通過;同一個碼不能再用
	code := totpAt(t, secret, nowStep()+1)
	expect(t, anon.twoFactorLogin(ch.Challenge, code), http.StatusOK, "")
	expect(t, anon.do(http.MethodGet, "/auth/me", nil), http.StatusOK, "")
	ch2, _ := anon.pwLogin("alice", pw)
	expect(t, (&client{e: e}).twoFactorLogin(ch2.Challenge, code), http.StatusUnauthorized, "AUTH-008")

	// 備援碼:不分大小寫、可省略連字號,只能用一次;其餘碼不受影響
	other := &client{e: e}
	chR, _ := other.pwLogin("alice", pw)
	rc := strings.ToUpper(strings.ReplaceAll(codes[0], "-", " "))
	expect(t, other.twoFactorLogin(chR.Challenge, rc), http.StatusOK, "")
	chR2, _ := (&client{e: e}).pwLogin("alice", pw)
	expect(t, (&client{e: e}).twoFactorLogin(chR2.Challenge, codes[0]), http.StatusUnauthorized, "AUTH-008")
	expect(t, (&client{e: e}).twoFactorLogin(chR2.Challenge, codes[1]), http.StatusOK, "")
	left := decode[struct {
		RecoveryLeft int `json:"recovery_codes_left"`
	}](t, c.do(http.MethodGet, "/auth/2fa", nil).Data).RecoveryLeft
	if left != 8 {
		t.Fatalf("剩餘備援碼 = %d", left)
	}
}

func TestTwoFactorLockoutSharedWithPassword(t *testing.T) {
	e := newEnv(t)
	e.seedUser("alice", pw, false, false)
	c := e.loggedIn("alice", pw)
	c.enroll()
	anon := &client{e: e}
	ch, _ := anon.pwLogin("alice", pw)
	for i := 1; i < auth.MaxLoginAttempts; i++ {
		expect(t, anon.twoFactorLogin(ch.Challenge, "000000"), http.StatusUnauthorized, "AUTH-008")
	}
	expect(t, anon.twoFactorLogin(ch.Challenge, "000000"), http.StatusLocked, "AUTH-002")
	// 鎖定後連正確的驗證碼與密碼都不能用
	e.resetTOTPStep("alice")
	expect(t, (&client{e: e}).twoFactorLogin(ch.Challenge, "000000"), http.StatusLocked, "AUTH-002")
	_, res := (&client{e: e}).pwLogin("alice", pw)
	expect(t, res, http.StatusLocked, "AUTH-002")
}

func TestTwoFactorChallengeDiesWithPasswordChangeAndDeactivation(t *testing.T) {
	e := newEnv(t)
	e.seedUser("alice", pw, false, false)
	e.seedUser("bob", pw, false, false)
	e.seedUser("root", pw, true, false)
	root := e.loggedIn("root", pw)
	for _, name := range []string{"alice", "bob"} {
		c := e.loggedIn(name, pw)
		en := c.enroll()
		e.resetTOTPStep(name)
		anon := &client{e: e}
		ch, _ := anon.pwLogin(name, pw)
		u, _ := e.q.GetUserByUsername(context.Background(), name)
		if name == "alice" { // 管理員重設密碼 → token_version 變 → 挑戰失效
			expect(t, root.do(http.MethodPost, "/system/users/"+itoa(u.ID)+"/reset-password", map[string]string{"password": "Another-Pw-123"}), http.StatusNoContent, "")
		} else { // 停用帳號 → 挑戰失效
			cur := decode[struct {
				Version int32 `json:"version"`
			}](t, root.do(http.MethodGet, "/system/users/"+itoa(u.ID), nil).Data)
			expect(t, root.do(http.MethodPut, "/system/users/"+itoa(u.ID), map[string]any{"name": "bob", "is_active": false, "role_ids": []int64{}, "version": cur.Version}), http.StatusOK, "")
		}
		expect(t, anon.twoFactorLogin(ch.Challenge, totpAt(t, en.secret, nowStep()+1)), http.StatusUnauthorized, "AUTH-009")
	}
}

func TestTwoFactorDisableRegenerateAndAdminReset(t *testing.T) {
	e := newEnv(t)
	e.seedUser("alice", pw, false, false)
	e.seedUser("bob", pw, false, false)
	e.seedUser("root", pw, true, false)
	e.seedUser("nobody", pw, false, false)
	root := e.loggedIn("root", pw)

	a := e.loggedIn("alice", pw)
	en := a.enroll()
	// 重新產生備援碼:須密碼與驗證碼;舊碼作廢
	e.resetTOTPStep("alice")
	expect(t, a.do(http.MethodPost, "/auth/2fa/recovery-codes", map[string]string{"password": "wrong-pass-1", "code": totpAt(t, en.secret, nowStep())}), http.StatusUnprocessableEntity, "AUTH-006")
	expect(t, a.do(http.MethodPost, "/auth/2fa/recovery-codes", map[string]string{"password": pw, "code": "000000"}), http.StatusUnauthorized, "AUTH-008")
	res := a.do(http.MethodPost, "/auth/2fa/recovery-codes", map[string]string{"password": pw, "code": totpAt(t, en.secret, nowStep())})
	expect(t, res, http.StatusOK, "")
	fresh := decode[struct {
		Codes []string `json:"recovery_codes"`
	}](t, res.Data).Codes
	chOld, _ := (&client{e: e}).pwLogin("alice", pw)
	expect(t, (&client{e: e}).twoFactorLogin(chOld.Challenge, en.recovery[0]), http.StatusUnauthorized, "AUTH-008")
	chNew, _ := (&client{e: e}).pwLogin("alice", pw)
	expect(t, (&client{e: e}).twoFactorLogin(chNew.Challenge, fresh[0]), http.StatusOK, "")

	// 停用:密碼與驗證碼缺一不可;停用後登入不再需要驗證碼,備援碼也清掉
	e.resetTOTPStep("alice")
	expect(t, a.do(http.MethodPost, "/auth/2fa/disable", map[string]string{"password": "wrong-pass-1", "code": totpAt(t, en.secret, nowStep())}), http.StatusUnprocessableEntity, "AUTH-006")
	expect(t, a.do(http.MethodPost, "/auth/2fa/disable", map[string]string{"password": pw, "code": totpAt(t, en.secret, nowStep())}), http.StatusNoContent, "")
	expect(t, a.do(http.MethodPost, "/auth/2fa/disable", map[string]string{"password": pw, "code": "123456"}), http.StatusConflict, "AUTH-011")
	_, res = (&client{e: e}).pwLogin("alice", pw)
	expect(t, res, http.StatusOK, "")
	if strings.Contains(string(res.Data), "challenge") {
		t.Fatal("停用後不應再要求驗證碼")
	}
	// 同一個帳號可以重新啟用,而且是新的密鑰
	if en2 := a.enroll(); en2.secret == en.secret {
		t.Fatal("重新啟用應是新密鑰")
	}

	// 管理員重設:bob 啟用後遺失裝置 → 管理員重設,bob 既有登入失效、之後只要密碼
	b := e.loggedIn("bob", pw)
	b.enroll()
	bu, _ := e.q.GetUserByUsername(context.Background(), "bob")
	expect(t, e.loggedIn("nobody", pw).do(http.MethodPost, "/system/users/"+itoa(bu.ID)+"/reset-2fa", nil), http.StatusForbidden, "SYS-403")
	expect(t, root.do(http.MethodPost, "/system/users/"+itoa(bu.ID)+"/reset-2fa", nil), http.StatusNoContent, "")
	expect(t, root.do(http.MethodPost, "/system/users/"+itoa(bu.ID)+"/reset-2fa", nil), http.StatusConflict, "AUTH-011")
	expect(t, b.do(http.MethodGet, "/auth/me", nil), http.StatusUnauthorized, "SYS-401")
	_, res = (&client{e: e}).pwLogin("bob", pw)
	expect(t, res, http.StatusOK, "")
	if strings.Contains(string(res.Data), "challenge") {
		t.Fatal("重設後不應再要求驗證碼")
	}

	// 使用者清單看得到狀態,但任何地方(清單、稽核)都不含密鑰與備援碼
	list := root.do(http.MethodGet, "/system/users?size=50", nil)
	logs := root.do(http.MethodGet, "/system/audit-logs?size=100", nil)
	for _, body := range []string{string(list.Data), string(logs.Data)} {
		for _, secret := range append([]string{en.secret}, fresh...) {
			if strings.Contains(body, secret) {
				t.Fatalf("回應不應含密鑰或備援碼 %s", secret)
			}
		}
		if strings.Contains(body, "totp_secret") {
			t.Fatal("回應不應含 totp_secret")
		}
	}
	if !strings.Contains(string(logs.Data), "enable_2fa") || !strings.Contains(string(logs.Data), "reset_2fa") {
		t.Fatalf("稽核日誌應記錄雙因素驗證的啟用與重設: %s", logs.Data)
	}
}

func TestCompanyRequiresTwoFactor(t *testing.T) {
	e := newEnv(t)
	e.seedUser("root", pw, true, false)
	e.seedUser("alice", pw, false, false)
	root := e.loggedIn("root", pw)
	alice := e.loggedIn("alice", pw)
	expect(t, alice.do(http.MethodGet, "/masterdata/units", nil), http.StatusOK, "")

	co := decode[struct {
		Version int32 `json:"version"`
	}](t, root.do(http.MethodGet, "/system/company", nil).Data)
	put := func(v int32) apiResp {
		return root.do(http.MethodPut, "/system/company", map[string]any{"name": "公司", "tax_id": "", "tax_reg_no": "", "require_2fa": true, "version": v})
	}
	// 操作的人自己沒有啟用雙因素驗證時不能開啟(避免把自己鎖在外面)
	expect(t, put(co.Version), http.StatusConflict, "SYS-022")
	rootEn := root.enroll()
	expect(t, put(co.Version), http.StatusOK, "")

	// 還沒啟用的人:除了設定雙因素驗證需要的 API 之外全部擋下;原本的登入立即受限
	expect(t, alice.do(http.MethodGet, "/masterdata/units", nil), http.StatusForbidden, "AUTH-014")
	expect(t, alice.do(http.MethodGet, "/auth/me", nil), http.StatusOK, "")
	me := decode[struct {
		MustSetup2FA bool `json:"must_setup_2fa"`
	}](t, alice.do(http.MethodGet, "/auth/me", nil).Data)
	if !me.MustSetup2FA {
		t.Fatal("me 應標示必須設定雙因素驗證")
	}
	en := alice.enroll()
	expect(t, alice.do(http.MethodGet, "/masterdata/units", nil), http.StatusOK, "")
	// 公司要求時不能自己停用
	e.resetTOTPStep("alice")
	expect(t, alice.do(http.MethodPost, "/auth/2fa/disable", map[string]string{"password": pw, "code": totpAt(t, en.secret, nowStep())}), http.StatusConflict, "AUTH-013")
	// 管理員重設後,下次登入又必須先設定
	au, _ := e.q.GetUserByUsername(context.Background(), "alice")
	expect(t, root.do(http.MethodPost, "/system/users/"+itoa(au.ID)+"/reset-2fa", nil), http.StatusNoContent, "")
	again := e.loggedIn("alice", pw)
	expect(t, again.do(http.MethodGet, "/masterdata/units", nil), http.StatusForbidden, "AUTH-014")
	// root 自己登入也要輸入驗證碼
	e.resetTOTPStep("root")
	ch, _ := (&client{e: e}).pwLogin("root", pw)
	if !ch.Required {
		t.Fatal("root 已啟用,登入應要求驗證碼")
	}
	_ = rootEn
}
