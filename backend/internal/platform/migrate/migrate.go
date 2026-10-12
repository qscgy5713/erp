// Package migrate 套用內嵌的 migration;版本紀錄表 schema_migrations 與 golang-migrate 相容,
// 所以以 migrate 指令建立的資料庫可以接著用這裡升級,反之亦然。
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type file struct {
	version int64
	name    string
}

func list(fsys fs.FS) ([]file, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []file
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		v, err := strconv.ParseInt(strings.SplitN(name, "_", 2)[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("migration 檔名格式錯誤: %s", name)
		}
		out = append(out, file{v, name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// Up 套用尚未套用的 migration,回傳套用後的版本與本次套用的個數。每個檔案一個交易。
func Up(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) (version int64, applied int, err error) {
	files, err := list(fsys)
	if err != nil {
		return 0, 0, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer conn.Release()
	pg := conn.Conn().PgConn()
	if _, err := pg.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL)`).ReadAll(); err != nil {
		return 0, 0, err
	}
	var cur int64
	var dirty bool
	row := conn.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`)
	if err := row.Scan(&cur, &dirty); err != nil && !strings.Contains(err.Error(), "no rows") {
		return 0, 0, err
	}
	if dirty {
		return cur, 0, fmt.Errorf("資料庫停在未完成的 migration %d(dirty),請先人工處理", cur)
	}
	for _, f := range files {
		if f.version <= cur {
			continue
		}
		sql, err := fs.ReadFile(fsys, f.name)
		if err != nil {
			return cur, applied, err
		}
		// 一個檔案含多個敘述,用 simple protocol 在同一個交易內執行
		script := "BEGIN;\n" + string(sql) + "\nDELETE FROM schema_migrations;\nINSERT INTO schema_migrations (version, dirty) VALUES (" +
			strconv.FormatInt(f.version, 10) + ", false);\nCOMMIT;"
		if _, err := pg.Exec(ctx, script).ReadAll(); err != nil {
			_, _ = pg.Exec(ctx, "ROLLBACK").ReadAll()
			return cur, applied, fmt.Errorf("套用 %s: %w", f.name, err)
		}
		cur = f.version
		applied++
	}
	return cur, applied, nil
}
