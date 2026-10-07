package config

import (
	"reflect"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("APP_ENV", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("TRUSTED_PROXIES", " 10.0.0.0/8 , ,172.16.0.1 ")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppEnv != "development" || cfg.HTTPAddr != ":8080" {
		t.Fatalf("預設值錯誤: %+v", cfg)
	}
	if want := []string{"10.0.0.0/8", "172.16.0.1"}; !reflect.DeepEqual(cfg.TrustedProxies, want) {
		t.Fatalf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
	}
	if cfg.IsProduction() {
		t.Fatal("development 不應為 production")
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("缺少 DATABASE_URL 應回傳錯誤")
	}
}

func TestLoadTrustedProxiesEmpty(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("TRUSTED_PROXIES", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TrustedProxies != nil {
		t.Fatalf("未設定時應為 nil(不信任任何 proxy),got %v", cfg.TrustedProxies)
	}
}
