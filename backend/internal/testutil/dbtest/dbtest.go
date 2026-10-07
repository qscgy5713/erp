// Package dbtest 為整合測試建立獨立的暫存資料庫並套用所有 migration,
// 測試結束後刪除,不會汙染開發用資料庫。
// 需要環境變數 DATABASE_URL(用來連線建立暫存庫);未設定時略過測試。
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"erp/internal/platform/database"
)

func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	baseURL := os.Getenv("DATABASE_URL")
	if baseURL == "" {
		t.Skip("未設定 DATABASE_URL,略過資料庫整合測試")
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		t.Fatalf("dbtest: 連線: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "erp_test_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("dbtest: 建立資料庫: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), baseURL)
		if err != nil {
			t.Logf("dbtest: 清理連線失敗: %v", err)
			return
		}
		defer func() { _ = c.Close(context.Background()) }()
		if _, err := c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Logf("dbtest: 刪除 %s 失敗: %v", name, err)
		}
	})

	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("dbtest: DATABASE_URL 須為 URL 格式: %v", err)
	}
	u.Path = "/" + name
	pool, err := database.Connect(ctx, u.String())
	if err != nil {
		t.Fatalf("dbtest: 連線暫存庫: %v", err)
	}
	t.Cleanup(pool.Close)

	applyMigrations(t, ctx, pool)
	return pool
}

func applyMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(moduleRoot(t), "migrations", "*.up.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("dbtest: 找不到 migration: %v", err)
	}
	sort.Strings(files)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// 一個檔案含多個敘述,須用 simple protocol 執行
		if _, err := conn.Conn().PgConn().Exec(ctx, string(sql)).ReadAll(); err != nil {
			t.Fatalf("dbtest: 套用 %s: %v", filepath.Base(f), err)
		}
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("dbtest: 找不到 go.mod")
		}
		dir = parent
	}
}
