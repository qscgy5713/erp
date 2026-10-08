// Package database 建立 PostgreSQL 連線池。
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("解析 DATABASE_URL: %w", err)
	}
	// DB 一律以 UTC 存放時間,顯示時再轉 Asia/Taipei。
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	// 許多列表查詢有一串「@參數 IS NULL OR 條件」的選填篩選。預備陳述式執行 5 次後 PostgreSQL 可能改用通用計畫,
	// 通用計畫無法依實際參數簡化這些條件,壓測實測列表查詢由 22 ms 退化到 68 ms(25,000 筆出貨單)。
	// 強制每次依實際參數規劃,多花的規劃時間(約 1 ms)遠小於退化的代價。
	cfg.ConnConfig.RuntimeParams["plan_cache_mode"] = "force_custom_plan"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("建立連線池: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("連線資料庫: %w", err)
	}
	return pool, nil
}
