package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const issuer = "erp"

// Claims 為 access token 內容。TokenVersion 與 users.token_version 不符即失效
// (改密碼、停用、重設密碼時遞增)。
type Claims struct {
	CompanyID    int64 `json:"cid"`
	TokenVersion int32 `json:"tv"`
	jwt.RegisteredClaims
}

func (c *Claims) UserID() (int64, error) {
	return strconv.ParseInt(c.Subject, 10, 64)
}

type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewTokenIssuer(secret []byte, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{secret: secret, ttl: ttl, now: time.Now}
}

func (t *TokenIssuer) Issue(userID, companyID int64, tokenVersion int32) (string, time.Time, error) {
	now := t.now()
	exp := now.Add(t.ttl)
	claims := Claims{
		CompanyID:    companyID,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   strconv.FormatInt(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	return s, exp, err
}

var errInvalidToken = errors.New("invalid token")

func (t *TokenIssuer) Parse(s string) (*Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(s, &claims, func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), // 防止 alg=none 等攻擊
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return nil, errInvalidToken
	}
	return &claims, nil
}

// newRefreshToken 產生隨機 refresh token;資料庫只存其 SHA-256。
func newRefreshToken() (raw string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", nil, err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw), nil
}

func hashToken(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}

// ---- 雙因素驗證的登入挑戰 ----

// challengeIssuer 與 access token 的 issuer 不同,所以挑戰憑證不能當 access token 使用(Parse 會拒絕)。
const (
	challengeIssuer = "erp-2fa"
	challengeTTL    = 5 * time.Minute
)

// challengeClaims 密碼驗證通過、等待輸入雙因素驗證碼的憑證。TokenVersion 與帳號不符(改密碼等)即失效。
type challengeClaims struct {
	TokenVersion int32 `json:"tv"`
	jwt.RegisteredClaims
}

func (t *TokenIssuer) IssueChallenge(userID int64, tokenVersion int32) (string, error) {
	now := t.now()
	claims := challengeClaims{
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: challengeIssuer, Subject: strconv.FormatInt(userID, 10),
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(challengeTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
}

func (t *TokenIssuer) ParseChallenge(s string) (userID int64, tokenVersion int32, err error) {
	var claims challengeClaims
	_, err = jwt.ParseWithClaims(s, &claims, func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(challengeIssuer), jwt.WithExpirationRequired(), jwt.WithTimeFunc(t.now))
	if err != nil {
		return 0, 0, errInvalidToken
	}
	userID, err = strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return 0, 0, errInvalidToken
	}
	return userID, claims.TokenVersion, nil
}
