// Package auth 處理登入、Refresh Token 輪替、登出、改密碼與 API 驗證中介層。
package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
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
)

func errLocked(until time.Time) error {
	mins := int(time.Until(until).Minutes()) + 1
	return apperr.New(http.StatusLocked, "AUTH-002", fmt.Sprintf("登入失敗次數過多,帳號已鎖定,請 %d 分鐘後再試", mins))
}

type Service struct {
	store      *database.Store
	tokens     *TokenIssuer
	refreshTTL time.Duration
}

func NewService(store *database.Store, tokens *TokenIssuer, refreshTTL time.Duration) *Service {
	return &Service{store: store, tokens: tokens, refreshTTL: refreshTTL}
}

// Session 為登入或刷新後發給前端的憑證。
type Session struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
	UserID           int64
}

func (s *Service) Login(ctx context.Context, username, password string) (Session, error) {
	u, err := s.store.GetUserByUsername(ctx, username)
	if database.IsNoRows(err) {
		burnPasswordCheck(password)
		slog.WarnContext(ctx, "登入失敗:帳號不存在", "username", username, "ip", authctx.MetaFrom(ctx).IP)
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, err
	}
	// 鎖定期間不驗證密碼,避免持續暴力嘗試
	if u.LockedUntil != nil && u.LockedUntil.After(time.Now()) {
		return Session{}, errLocked(*u.LockedUntil)
	}

	if !CheckPassword(u.PasswordHash, password) {
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
				EntityType: "user", EntityID: &u.ID, Summary: "密碼錯誤",
			})
		})
		if err != nil {
			return Session{}, err
		}
		if row.LockedUntil != nil && row.LockedUntil.After(time.Now()) {
			return Session{}, errLocked(*row.LockedUntil)
		}
		return Session{}, ErrInvalidCredentials
	}
	// 密碼正確才透露帳號停用,避免被用來探測帳號
	if !u.IsActive {
		return Session{}, ErrDisabled
	}

	var sess Session
	err = s.store.InTx(ctx, func(q *db.Queries) error {
		if err := q.RecordLoginSuccess(ctx, u.ID); err != nil {
			return err
		}
		if sess, err = s.newSession(ctx, q, u, uuid.New()); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			CompanyID: u.CompanyID, UserID: &u.ID, Action: audit.Login,
			EntityType: "user", EntityID: &u.ID, Summary: "登入",
		})
	})
	return sess, err
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
