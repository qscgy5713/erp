// Package auth 處理登入、Refresh Token 輪替、登出、改密碼與 API 驗證中介層。
package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/system/audit"
)

const (
	MaxLoginAttempts = 5
	LockDuration     = 15 * time.Minute
	// reuseGrace 已輪替的 refresh token 在此時間內被再次使用,視為同一使用者的併發請求
	// (例如兩個分頁同時刷新),只拒絕不撤銷;超過則視為被竊用,撤銷整串。
	reuseGrace = 30 * time.Second
)

var (
	ErrInvalidCredentials = apperr.Unauthorized("AUTH-001", "帳號或密碼錯誤")
	ErrDisabled           = apperr.Unauthorized("AUTH-003", "帳號已停用,請洽系統管理員")
	ErrSessionExpired     = apperr.Unauthorized("AUTH-004", "登入已逾時,請重新登入")
	ErrMustChangePassword = apperr.Forbidden("AUTH-005", "請先變更密碼")
	ErrWrongOldPassword   = apperr.New(http.StatusUnprocessableEntity, "AUTH-006", "目前密碼錯誤")
	ErrSamePassword       = apperr.New(http.StatusUnprocessableEntity, "AUTH-007", "新密碼不可與目前密碼相同")
	ErrBadTwoFactorCode   = apperr.Unauthorized("AUTH-008", "驗證碼錯誤,請輸入驗證器 App 目前顯示的 6 位數,或一組備援碼")
	ErrChallengeExpired   = apperr.Unauthorized("AUTH-009", "登入逾時,請重新輸入帳號密碼")
	ErrTwoFactorOn        = apperr.Conflict("AUTH-010", "已啟用雙因素驗證")
	ErrTwoFactorOff       = apperr.Conflict("AUTH-011", "尚未啟用雙因素驗證")
	ErrNoPendingSetup     = apperr.Conflict("AUTH-012", "請先產生密鑰(設定雙因素驗證)")
	ErrMustSetup2FA       = apperr.Forbidden("AUTH-014", "公司要求使用雙因素驗證,請先完成設定")
	ErrTwoFactorRequired  = apperr.Conflict("AUTH-013", "公司要求所有使用者使用雙因素驗證,不能停用")
)

func errLocked(until time.Time) error {
	mins := int(time.Until(until).Minutes()) + 1
	return apperr.New(http.StatusLocked, "AUTH-002", fmt.Sprintf("登入失敗次數過多,帳號已鎖定,請 %d 分鐘後再試", mins))
}

type Service struct {
	store      *database.Store
	tokens     *TokenIssuer
	refreshTTL time.Duration
	totpKey    [32]byte
	now        func() time.Time
}

func NewService(store *database.Store, tokens *TokenIssuer, refreshTTL time.Duration) *Service {
	return &Service{store: store, tokens: tokens, refreshTTL: refreshTTL, now: time.Now}
}

// WithTOTPKey 設定雙因素驗證密鑰的加密金鑰:優先用 TOTP_ENCRYPTION_KEY,沒設定就由 JWT_SECRET 衍生。
func (s *Service) WithTOTPKey(key, jwtSecret []byte) *Service {
	if len(key) == 0 {
		key = jwtSecret
	}
	s.totpKey = deriveTOTPKey(key)
	return s
}

// Session 為登入或刷新後發給前端的憑證。
type Session struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
	UserID           int64
}

// LoginResult 密碼驗證後的結果:一般帳號直接取得 Session;啟用雙因素驗證的帳號只拿到挑戰憑證,
// 須再以驗證碼呼叫 LoginTwoFactor 才會取得 Session。
type LoginResult struct {
	Session   Session
	Challenge string // 非空表示需要雙因素驗證
}

func (s *Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	u, err := s.store.GetUserByUsername(ctx, username)
	if database.IsNoRows(err) {
		burnPasswordCheck(password)
		slog.WarnContext(ctx, "登入失敗:帳號不存在", "username", username, "ip", authctx.MetaFrom(ctx).IP)
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}
	// 鎖定期間不驗證密碼,避免持續暴力嘗試
	if u.LockedUntil != nil && u.LockedUntil.After(time.Now()) {
		return LoginResult{}, errLocked(*u.LockedUntil)
	}

	if !CheckPassword(u.PasswordHash, password) {
		return LoginResult{}, s.recordFailure(ctx, u, "密碼錯誤", ErrInvalidCredentials)
	}
	// 密碼正確才透露帳號停用,避免被用來探測帳號
	if !u.IsActive {
		return LoginResult{}, ErrDisabled
	}

	if u.TotpEnabled {
		ch, err := s.tokens.IssueChallenge(u.ID, u.TokenVersion)
		if err != nil {
			return LoginResult{}, err
		}
		return LoginResult{Challenge: ch}, nil
	}
	sess, err := s.completeLogin(ctx, u, "登入")
	return LoginResult{Session: sess}, err
}

// recordFailure 記錄一次失敗(密碼或驗證碼錯誤):累計次數、達上限鎖定帳號、寫稽核。
// 鎖定時回傳鎖定錯誤,否則回傳 fallback。
func (s *Service) recordFailure(ctx context.Context, u db.User, summary string, fallback error) error {
	var row db.RecordLoginFailureRow
	err := s.store.InTx(ctx, func(q *db.Queries) error {
		var err error
		if row, err = q.RecordLoginFailure(ctx, db.RecordLoginFailureParams{
			ID: u.ID, MaxAttempts: MaxLoginAttempts, LockSeconds: int32(LockDuration.Seconds()),
		}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			CompanyID: u.CompanyID, UserID: &u.ID, Action: audit.LoginFailed,
			EntityType: "user", EntityID: &u.ID, Summary: summary,
		})
	})
	if err != nil {
		return err
	}
	if row.LockedUntil != nil && row.LockedUntil.After(time.Now()) {
		return errLocked(*row.LockedUntil)
	}
	return fallback
}

func (s *Service) completeLogin(ctx context.Context, u db.User, summary string) (Session, error) {
	var sess Session
	err := s.store.InTx(ctx, func(q *db.Queries) error {
		if err := q.RecordLoginSuccess(ctx, u.ID); err != nil {
			return err
		}
		var err error
		if sess, err = s.newSession(ctx, q, u, uuid.New()); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			CompanyID: u.CompanyID, UserID: &u.ID, Action: audit.Login,
			EntityType: "user", EntityID: &u.ID, Summary: summary,
		})
	})
	return sess, err
}

// LoginTwoFactor 以挑戰憑證加驗證碼(6 位數 TOTP 或備援碼)完成登入。錯誤與密碼錯誤共用失敗計數與鎖定。
func (s *Service) LoginTwoFactor(ctx context.Context, challenge, code string) (Session, error) {
	uid, tv, err := s.tokens.ParseChallenge(challenge)
	if err != nil {
		return Session{}, ErrChallengeExpired
	}
	u, err := s.store.GetUserByID(ctx, uid)
	if database.IsNoRows(err) {
		return Session{}, ErrChallengeExpired
	}
	if err != nil {
		return Session{}, err
	}
	if !u.IsActive || u.TokenVersion != tv || !u.TotpEnabled {
		return Session{}, ErrChallengeExpired
	}
	if u.LockedUntil != nil && u.LockedUntil.After(time.Now()) {
		return Session{}, errLocked(*u.LockedUntil)
	}
	used, err := s.checkSecondFactor(ctx, u, code)
	if err != nil {
		return Session{}, err
	}
	if used == "" {
		return Session{}, s.recordFailure(ctx, u, "雙因素驗證碼錯誤", ErrBadTwoFactorCode)
	}
	return s.completeLogin(ctx, u, used)
}

// checkSecondFactor 驗證第二因素。成功回傳登入摘要(用了驗證器或備援碼),失敗回傳空字串。
// TOTP 的時間步只接受一次(擋重放);備援碼用過即作廢。
func (s *Service) checkSecondFactor(ctx context.Context, u db.User, code string) (string, error) {
	code = strings.TrimSpace(code)
	if len(code) == totpDigits && isDigits(code) {
		secret, err := openSecret(s.totpKey, u.TotpSecretEnc)
		if err != nil {
			return "", err
		}
		step, ok := verifyTOTP(secret, code, s.now())
		if !ok {
			return "", nil
		}
		n, err := s.store.AdvanceTOTPStep(ctx, db.AdvanceTOTPStepParams{ID: u.ID, Step: step})
		if err != nil || n == 0 { // n == 0:這個時間步已用過
			return "", err
		}
		return "登入(雙因素驗證)", nil
	}
	norm := normalizeRecoveryCode(code)
	if norm == "" {
		return "", nil
	}
	n, err := s.store.UseRecoveryCode(ctx, db.UseRecoveryCodeParams{UserID: u.ID, CodeHash: hashRecoveryCode(norm)})
	if err != nil || n == 0 {
		return "", err
	}
	return "登入(使用備援碼)", nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// TwoFactorSetup 開始設定:產生新密鑰(尚未啟用,須以驗證碼確認)。已啟用者須先停用。
type TwoFactorSetup struct {
	Secret     string `json:"secret"`      // Base32,手動輸入用
	OtpauthURI string `json:"otpauth_uri"` // 產生 QR Code 用
}

func (s *Service) SetupTwoFactor(ctx context.Context, userID int64) (TwoFactorSetup, error) {
	var out TwoFactorSetup
	err := s.store.InTx(ctx, func(q *db.Queries) error {
		u, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return err
		}
		if u.TotpEnabled {
			return ErrTwoFactorOn
		}
		secret, err := newTOTPSecret()
		if err != nil {
			return err
		}
		sealed, err := sealSecret(s.totpKey, secret)
		if err != nil {
			return err
		}
		if err := q.SetTOTPPending(ctx, db.SetTOTPPendingParams{ID: u.ID, SecretEnc: sealed}); err != nil {
			return err
		}
		out = TwoFactorSetup{Secret: b32.EncodeToString(secret), OtpauthURI: otpauthURI("ERP", u.Username, secret)}
		return nil
	})
	return out, err
}

// EnableTwoFactor 以驗證碼確認後正式啟用,回傳一組備援碼(只會顯示這一次)。
func (s *Service) EnableTwoFactor(ctx context.Context, userID int64, code string) ([]string, error) {
	var codes []string
	err := s.store.InTx(ctx, func(q *db.Queries) error {
		u, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return err
		}
		if u.TotpEnabled {
			return ErrTwoFactorOn
		}
		if len(u.TotpSecretEnc) == 0 {
			return ErrNoPendingSetup
		}
		secret, err := openSecret(s.totpKey, u.TotpSecretEnc)
		if err != nil {
			return err
		}
		step, ok := verifyTOTP(secret, code, s.now())
		if !ok {
			return ErrBadTwoFactorCode
		}
		if err := q.EnableTOTP(ctx, db.EnableTOTPParams{ID: u.ID, LastStep: step}); err != nil {
			return err
		}
		if codes, err = s.replaceRecoveryCodes(ctx, q, u.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			CompanyID: u.CompanyID, UserID: &u.ID, Action: "enable_2fa",
			EntityType: "user", EntityID: &u.ID, Summary: "啟用雙因素驗證",
		})
	})
	return codes, err
}

func (s *Service) replaceRecoveryCodes(ctx context.Context, q *db.Queries, userID int64) ([]string, error) {
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
		return nil, err
	}
	for _, c := range codes {
		if err := q.InsertRecoveryCode(ctx, db.InsertRecoveryCodeParams{UserID: userID, CodeHash: hashRecoveryCode(c)}); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// confirmSensitive 停用、重新產生備援碼這類敏感操作:須再驗證密碼與目前的第二因素。
func (s *Service) confirmSensitive(ctx context.Context, u db.User, password, code string) error {
	if !CheckPassword(u.PasswordHash, password) {
		return ErrWrongOldPassword.WithMessage("密碼錯誤")
	}
	used, err := s.checkSecondFactor(ctx, u, code)
	if err != nil {
		return err
	}
	if used == "" {
		return ErrBadTwoFactorCode
	}
	return nil
}

// DisableTwoFactor 停用雙因素驗證(須密碼與驗證碼);公司要求雙因素驗證時不可停用。
func (s *Service) DisableTwoFactor(ctx context.Context, userID int64, password, code string) error {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if !u.TotpEnabled {
		return ErrTwoFactorOff
	}
	if req, err := s.store.CompanyRequires2FA(ctx, u.CompanyID); err != nil {
		return err
	} else if req {
		return ErrTwoFactorRequired
	}
	if err := s.confirmSensitive(ctx, u, password, code); err != nil {
		return err
	}
	return s.store.InTx(ctx, func(q *db.Queries) error {
		if err := q.DisableTOTP(ctx, u.ID); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, u.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			CompanyID: u.CompanyID, UserID: &u.ID, Action: "disable_2fa",
			EntityType: "user", EntityID: &u.ID, Summary: "停用雙因素驗證",
		})
	})
}

// RegenerateRecoveryCodes 重新產生備援碼(舊的全部作廢;須密碼與驗證碼)。
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, userID int64, password, code string) ([]string, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !u.TotpEnabled {
		return nil, ErrTwoFactorOff
	}
	if err := s.confirmSensitive(ctx, u, password, code); err != nil {
		return nil, err
	}
	var codes []string
	err = s.store.InTx(ctx, func(q *db.Queries) error {
		var err error
		if codes, err = s.replaceRecoveryCodes(ctx, q, u.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			CompanyID: u.CompanyID, UserID: &u.ID, Action: "regen_recovery_codes",
			EntityType: "user", EntityID: &u.ID, Summary: "重新產生雙因素備援碼",
		})
	})
	return codes, err
}

// TwoFactorStatus 目前使用者的雙因素驗證狀態。
type TwoFactorStatus struct {
	Enabled        bool `json:"enabled"`
	RecoveryLeft   int  `json:"recovery_codes_left"`
	CompanyRequire bool `json:"company_requires"`
}

func (s *Service) TwoFactorStatus(ctx context.Context, userID int64) (TwoFactorStatus, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return TwoFactorStatus{}, err
	}
	req, err := s.store.CompanyRequires2FA(ctx, u.CompanyID)
	if err != nil {
		return TwoFactorStatus{}, err
	}
	n, err := s.store.CountUnusedRecoveryCodes(ctx, u.ID)
	if err != nil {
		return TwoFactorStatus{}, err
	}
	return TwoFactorStatus{Enabled: u.TotpEnabled, RecoveryLeft: int(n), CompanyRequire: req}, nil
}

// Refresh 以 refresh token 換發新的 access token 與 refresh token(輪替)。
func (s *Service) Refresh(ctx context.Context, raw string) (Session, error) {
	if raw == "" {
		return Session{}, ErrSessionExpired
	}
	var (
		sess Session
		fail error // 需要 commit(例如撤銷整串)後才回傳的錯誤
	)
	err := s.store.InTx(ctx, func(q *db.Queries) error {
		rt, err := q.GetRefreshTokenForUpdate(ctx, hashToken(raw))
		if database.IsNoRows(err) {
			fail = ErrSessionExpired
			return nil
		}
		if err != nil {
			return err
		}
		u, err := q.GetUserByID(ctx, rt.UserID)
		if err != nil {
			return err
		}

		switch {
		case rt.RevokedAt != nil:
			fail = ErrSessionExpired
			if time.Since(*rt.RevokedAt) <= reuseGrace {
				return nil
			}
			slog.WarnContext(ctx, "偵測到 refresh token 重放,撤銷整串", "user_id", u.ID, "family", rt.FamilyID)
			if err := q.RevokeRefreshFamily(ctx, rt.FamilyID); err != nil {
				return err
			}
			return audit.Record(ctx, q, audit.Entry{
				CompanyID: u.CompanyID, UserID: &u.ID, Action: audit.TokenReuse,
				EntityType: "user", EntityID: &u.ID, Summary: "偵測到已撤銷的登入憑證被重複使用,已強制登出該裝置",
			})
		case rt.ExpiresAt.Before(time.Now()):
			fail = ErrSessionExpired
			return nil
		case !u.IsActive:
			fail = ErrDisabled
			return q.RevokeRefreshFamily(ctx, rt.FamilyID)
		}

		if err := q.RevokeRefreshToken(ctx, rt.ID); err != nil {
			return err
		}
		sess, err = s.newSession(ctx, q, u, rt.FamilyID)
		return err
	})
	if err != nil {
		return Session{}, err
	}
	if fail != nil {
		return Session{}, fail
	}
	return sess, nil
}

// Logout 撤銷該裝置的整串 refresh token。token 無效時視為已登出。
func (s *Service) Logout(ctx context.Context, raw string) error {
	if raw == "" {
		return nil
	}
	return s.store.InTx(ctx, func(q *db.Queries) error {
		rt, err := q.GetRefreshTokenForUpdate(ctx, hashToken(raw))
		if database.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := q.RevokeRefreshFamily(ctx, rt.FamilyID); err != nil {
			return err
		}
		u, err := q.GetUserByID(ctx, rt.UserID)
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			CompanyID: u.CompanyID, UserID: &u.ID, Action: audit.Logout,
			EntityType: "user", EntityID: &u.ID, Summary: "登出",
		})
	})
}

// ChangePassword 變更自己的密碼。成功後其他裝置全部登出,目前裝置換發新憑證繼續使用。
func (s *Service) ChangePassword(ctx context.Context, userID int64, oldPw, newPw string) (Session, error) {
	if err := ValidatePassword("new_password", newPw); err != nil {
		return Session{}, err
	}
	var sess Session
	err := s.store.InTx(ctx, func(q *db.Queries) error {
		u, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return err
		}
		if !CheckPassword(u.PasswordHash, oldPw) {
			return ErrWrongOldPassword
		}
		if oldPw == newPw {
			return ErrSamePassword
		}
		hash, err := HashPassword(newPw)
		if err != nil {
			return err
		}
		if u, err = q.SetPassword(ctx, db.SetPasswordParams{
			ID: u.ID, PasswordHash: hash, MustChangePassword: false, UpdatedBy: &u.ID,
		}); err != nil {
			return err
		}
		if err := q.RevokeUserRefreshTokens(ctx, u.ID); err != nil {
			return err
		}
		if sess, err = s.newSession(ctx, q, u, uuid.New()); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			CompanyID: u.CompanyID, UserID: &u.ID, Action: audit.PasswordChange,
			EntityType: "user", EntityID: &u.ID, Summary: "變更密碼",
		})
	})
	return sess, err
}

func (s *Service) newSession(ctx context.Context, q *db.Queries, u db.User, family uuid.UUID) (Session, error) {
	access, accessExp, err := s.tokens.Issue(u.ID, u.CompanyID, u.TokenVersion)
	if err != nil {
		return Session{}, err
	}
	raw, hash, err := newRefreshToken()
	if err != nil {
		return Session{}, err
	}
	meta := authctx.MetaFrom(ctx)
	refreshExp := time.Now().Add(s.refreshTTL)
	if _, err := q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		UserID: u.ID, FamilyID: family, TokenHash: hash, ExpiresAt: refreshExp,
		Ip: meta.IP, UserAgent: meta.UserAgent,
	}); err != nil {
		return Session{}, err
	}
	return Session{
		AccessToken: access, AccessExpiresAt: accessExp,
		RefreshToken: raw, RefreshExpiresAt: refreshExp,
		UserID: u.ID,
	}, nil
}

// LoadActor 驗證 access token 對應的使用者仍有效,並載入其權限與資料範圍。
// 每個請求都查資料庫,停用或改密碼可立即生效。
func (s *Service) LoadActor(ctx context.Context, claims *Claims) (*authctx.Actor, error) {
	userID, err := claims.UserID()
	if err != nil {
		return nil, apperr.ErrUnauthorized
	}
	u, err := s.store.GetUserByID(ctx, userID)
	if database.IsNoRows(err) {
		return nil, apperr.ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if !u.IsActive || u.TokenVersion != claims.TokenVersion || u.CompanyID != claims.CompanyID {
		return nil, apperr.ErrUnauthorized
	}
	return s.actorFor(ctx, u)
}

// ActorForUser 載入指定使用者的權限(登入後回傳 /me 資料用)。
func (s *Service) ActorForUser(ctx context.Context, userID int64) (*authctx.Actor, db.User, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, db.User{}, err
	}
	a, err := s.actorFor(ctx, u)
	return a, u, err
}

func (s *Service) actorFor(ctx context.Context, u db.User) (*authctx.Actor, error) {
	rows, err := s.store.ListUserAccess(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	a := &authctx.Actor{
		UserID: u.ID, CompanyID: u.CompanyID, DepartmentID: u.DepartmentID,
		Username: u.Username, Name: u.Name, IsSuperadmin: u.IsSuperadmin,
		MustChangePassword: u.MustChangePassword,
		Permissions:        map[string]struct{}{},
	}
	if !u.TotpEnabled {
		req, err := s.store.CompanyRequires2FA(ctx, u.CompanyID)
		if err != nil {
			return nil, err
		}
		a.MustSetup2FA = req
	}
	for _, r := range rows {
		a.DataScope = authctx.WiderScope(a.DataScope, r.DataScope)
		if r.Permission != nil {
			a.Permissions[*r.Permission] = struct{}{}
		}
	}
	if a.DataScope == "" {
		a.DataScope = authctx.ScopeSelf
	}
	return a, nil
}
