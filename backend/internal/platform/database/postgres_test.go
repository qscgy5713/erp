package database

import (
	"context"
	"os"
	"testing"
	"time"
)

// 整合測試:需要已套用 migration 的資料庫。
// 在 docker compose 的 api 容器內(make test)會自動帶入 DATABASE_URL;未設定則略過。
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("未設定 DATABASE_URL,略過資料庫整合測試")
	}
	return dsn
}

func TestConnect(t *testing.T) {
	ctx := context.Background()
	pool, err := Connect(ctx, testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var tz string
	if err := pool.QueryRow(ctx, "SHOW timezone").Scan(&tz); err != nil {
		t.Fatal(err)
	}
	if tz != "UTC" {
		t.Fatalf("連線時區 = %s, want UTC", tz)
	}
}

func TestConnectInvalidURL(t *testing.T) {
	if _, err := Connect(context.Background(), "::not a url"); err == nil {
		t.Fatal("無效的 URL 應回傳錯誤")
	}
}

// 驗證 000001_init:預設公司、updated_at trigger、統編格式約束。
// 在交易內執行並回滾,不留下資料。
func TestInitMigration(t *testing.T) {
	ctx := context.Background()
	pool, err := Connect(ctx, testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currency string
	if err := tx.QueryRow(ctx, "SELECT currency FROM companies WHERE code = 'HQ'").Scan(&currency); err != nil {
		t.Fatalf("預設公司不存在: %v", err)
	}
	if currency != "TWD" {
		t.Fatalf("本位幣 = %s, want TWD", currency)
	}

	// now() 在同一交易內不變,因此先把 updated_at 設成過去,再確認 trigger 會改回 now()
	var before, after time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO companies (code, name, created_at, updated_at)
		VALUES ('T1', '測試', now() - interval '1 day', now() - interval '1 day')
		RETURNING updated_at`).Scan(&before)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, "UPDATE companies SET name = '測試2' WHERE code = 'T1' RETURNING updated_at").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.After(before) {
		t.Fatalf("trigger 未更新 updated_at: before=%v after=%v", before, after)
	}

	if _, err := tx.Exec(ctx, "SAVEPOINT bad_tax_id"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO companies (code, name, tax_id) VALUES ('T2', 'x', '12AB5678')"); err == nil {
		t.Fatal("非 8 位數字的統編應被 CHECK 約束擋下")
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT bad_tax_id"); err != nil {
		t.Fatal(err)
	}
}
