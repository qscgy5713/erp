// Package config 從環境變數載入設定。
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv          string // development / production
	HTTPAddr        string
	DatabaseURL     string
	ShutdownTimeout time.Duration
	// TrustedProxies 為可信任的反向代理 IP/CIDR;只有來自這些位址的
	// X-Forwarded-For 才會被採用,避免使用者偽造來源 IP。
	TrustedProxies []string

	JWTSecret []byte
	// TOTPKey 雙因素驗證密鑰的加密金鑰;未設定時由 JWT_SECRET 衍生(此時更換 JWT_SECRET 會讓已啟用者無法驗證,需重設)
	TOTPKey         []byte
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	// RateLimitPerMinute 已登入 API 每位使用者每分鐘次數
	RateLimitPerMinute int
	// LoginRateLimitPerMinute 登入每個 IP 每分鐘次數(單一帳號的暴力破解另由帳號鎖定處理)
	LoginRateLimitPerMinute int
	// RefreshRateLimitPerMinute 刷新 token 每個 IP 每分鐘次數
	RefreshRateLimitPerMinute int
}

// devSecretMarker 出現在 .env.example 的開發用密鑰中;正式環境禁止使用。
const devSecretMarker = "dev-only"

func Load() (Config, error) {
	cfg := Config{
		AppEnv:          getenv("APP_ENV", "development"),
		HTTPAddr:        getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		ShutdownTimeout: 10 * time.Second,
		TrustedProxies:  splitList(os.Getenv("TRUSTED_PROXIES")),
		JWTSecret:       []byte(os.Getenv("JWT_SECRET")),
		TOTPKey:         []byte(os.Getenv("TOTP_ENCRYPTION_KEY")),
	}

	var errs []error
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL 未設定"))
	}
	if len(cfg.JWTSecret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET 至少 32 個字元"))
	}
	if len(cfg.TOTPKey) > 0 && len(cfg.TOTPKey) < 32 {
		errs = append(errs, errors.New("TOTP_ENCRYPTION_KEY 至少 32 個字元(不設定則由 JWT_SECRET 衍生)"))
	}
	if cfg.IsProduction() && strings.Contains(string(cfg.JWTSecret), devSecretMarker) {
		errs = append(errs, errors.New("正式環境不可使用開發用 JWT_SECRET"))
	}

	var err error
	if cfg.AccessTokenTTL, err = getDuration("ACCESS_TOKEN_TTL", 15*time.Minute); err != nil {
		errs = append(errs, err)
	}
	if cfg.RefreshTokenTTL, err = getDuration("REFRESH_TOKEN_TTL", 7*24*time.Hour); err != nil {
		errs = append(errs, err)
	}
	if cfg.RateLimitPerMinute, err = getPositiveInt("RATE_LIMIT_PER_MINUTE", 600); err != nil {
		errs = append(errs, err)
	}
	if cfg.LoginRateLimitPerMinute, err = getPositiveInt("LOGIN_RATE_LIMIT_PER_MINUTE", 60); err != nil {
		errs = append(errs, err)
	}
	if cfg.RefreshRateLimitPerMinute, err = getPositiveInt("REFRESH_RATE_LIMIT_PER_MINUTE", 300); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) IsProduction() bool { return c.AppEnv == "production" }

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getDuration(key string, def time.Duration) (time.Duration, error) {
	s := os.Getenv(key)
	if s == "" {
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s 格式錯誤(例:15m、168h)", key)
	}
	return d, nil
}

func getPositiveInt(key string, def int) (int, error) {
	s := os.Getenv(key)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s 須為正整數", key)
	}
	return n, nil
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
