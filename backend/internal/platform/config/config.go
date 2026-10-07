// Package config 從環境變數載入設定。
package config

import (
	"fmt"
	"os"
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
}

func Load() (Config, error) {
	cfg := Config{
		AppEnv:          getenv("APP_ENV", "development"),
		HTTPAddr:        getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		ShutdownTimeout: 10 * time.Second,
		TrustedProxies:  splitList(os.Getenv("TRUSTED_PROXIES")),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL 未設定")
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

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
