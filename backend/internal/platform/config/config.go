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
	AppEnv      string // development / production
	HTTPAddr    string
	DatabaseURL string
	// ControlDatabaseURL 多租戶(D92):控制庫的連線字串。設定後改為「每個租戶一個資料庫」,租戶資料庫與控制庫在同一個
	// PostgreSQL 伺服器;此時 DATABASE_URL 可不設。不設 = 單租戶(行為與以前完全相同)。
	ControlDatabaseURL string
	// TenantPoolMaxConns 每個租戶連線池的連線數上限
	TenantPoolMaxConns int
	ShutdownTimeout    time.Duration
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

	// 通知信(SMTP,例如 Gmail 搭配應用程式密碼);SMTPUser 空白 = 不寄信
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string
	AppBaseURL   string // 信件內連結的網址
}

func (c Config) SMTPEnabled() bool { return c.SMTPUser != "" && c.SMTPPassword != "" }

// devSecretMarker 出現在 .env.example 的開發用密鑰中;正式環境禁止使用。
const devSecretMarker = "dev-only"

func Load() (Config, error) {
	cfg := Config{
		AppEnv:             getenv("APP_ENV", "development"),
		HTTPAddr:           getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		ControlDatabaseURL: os.Getenv("CONTROL_DATABASE_URL"),
		ShutdownTimeout:    10 * time.Second,
		TrustedProxies:     splitList(os.Getenv("TRUSTED_PROXIES")),
		JWTSecret:          []byte(os.Getenv("JWT_SECRET")),
		TOTPKey:            []byte(os.Getenv("TOTP_ENCRYPTION_KEY")),
		SMTPHost:           getenv("SMTP_HOST", "smtp.gmail.com"),
		SMTPUser:           os.Getenv("SMTP_USER"),
		SMTPPassword:       os.Getenv("SMTP_PASSWORD"),
		AppBaseURL:         strings.TrimRight(os.Getenv("APP_BASE_URL"), "/"),
	}
	cfg.SMTPFrom = getenv("SMTP_FROM", cfg.SMTPUser)

	var errs []error
	if cfg.DatabaseURL == "" && cfg.ControlDatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL 未設定(多租戶請設定 CONTROL_DATABASE_URL)"))
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
	if cfg.TenantPoolMaxConns, err = getPositiveInt("TENANT_POOL_MAX_CONNS", 8); err != nil {
		errs = append(errs, err)
	}
	if cfg.SMTPPort, err = getPositiveInt("SMTP_PORT", 587); err != nil {
		errs = append(errs, err)
	}
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

func (c Config) MultiTenant() bool { return c.ControlDatabaseURL != "" }

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
