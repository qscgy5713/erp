package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

const goodSecret = "0123456789abcdef0123456789abcdef"

func setBase(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("JWT_SECRET", goodSecret)
	t.Setenv("APP_ENV", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("TRUSTED_PROXIES", "")
	t.Setenv("ACCESS_TOKEN_TTL", "")
	t.Setenv("REFRESH_TOKEN_TTL", "")
	t.Setenv("RATE_LIMIT_PER_MINUTE", "")
	t.Setenv("LOGIN_RATE_LIMIT_PER_MINUTE", "")
	t.Setenv("REFRESH_RATE_LIMIT_PER_MINUTE", "")
}

func TestLoadDefaults(t *testing.T) {
	setBase(t)
	t.Setenv("TRUSTED_PROXIES", " 10.0.0.0/8 , ,172.16.0.1 ")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppEnv != "development" || cfg.HTTPAddr != ":8080" || cfg.IsProduction() {
		t.Fatalf("預設值錯誤: %+v", cfg)
	}
	if want := []string{"10.0.0.0/8", "172.16.0.1"}; !reflect.DeepEqual(cfg.TrustedProxies, want) {
		t.Fatalf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
	}
	if cfg.AccessTokenTTL != 15*time.Minute || cfg.RefreshTokenTTL != 7*24*time.Hour {
		t.Fatalf("TTL 預設值錯誤: %v %v", cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	}
	if cfg.RateLimitPerMinute != 600 || cfg.LoginRateLimitPerMinute != 60 || cfg.RefreshRateLimitPerMinute != 300 {
		t.Fatalf("限流預設值錯誤: %+v", cfg)
	}
}

func TestLoadTrustedProxiesEmpty(t *testing.T) {
	setBase(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TrustedProxies != nil {
		t.Fatalf("未設定時應為 nil(不信任任何 proxy),got %v", cfg.TrustedProxies)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name, key, value, want string
	}{
		{"缺少 DATABASE_URL", "DATABASE_URL", "", "DATABASE_URL"},
		{"JWT_SECRET 太短", "JWT_SECRET", "short", "JWT_SECRET"},
		{"TTL 格式錯誤", "ACCESS_TOKEN_TTL", "15", "ACCESS_TOKEN_TTL"},
		{"TTL 不可為負", "REFRESH_TOKEN_TTL", "-1h", "REFRESH_TOKEN_TTL"},
		{"限流須為正整數", "RATE_LIMIT_PER_MINUTE", "0", "RATE_LIMIT_PER_MINUTE"},
		{"登入限流須為數字", "LOGIN_RATE_LIMIT_PER_MINUTE", "abc", "LOGIN_RATE_LIMIT_PER_MINUTE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setBase(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want contains %s", err, tc.want)
			}
		})
	}
}

func TestProductionRejectsDevSecret(t *testing.T) {
	setBase(t)
	t.Setenv("JWT_SECRET", "dev-only-insecure-jwt-secret-change-me-0123456789")
	if _, err := Load(); err != nil {
		t.Fatalf("開發環境應允許開發用密鑰: %v", err)
	}
	t.Setenv("APP_ENV", "production")
	if _, err := Load(); err == nil {
		t.Fatal("正式環境應拒絕開發用密鑰")
	}
}
