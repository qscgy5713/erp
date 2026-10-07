package database

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"erp/internal/db"
)

// Store 包裝連線池與 sqlc 查詢;單筆查詢直接用內嵌的 *db.Queries,
// 需要交易時用 InTx。
type Store struct {
	Pool *pgxpool.Pool
	*db.Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{Pool: pool, Queries: db.New(pool)}
}

// InTx 在交易內執行 fn;fn 回傳錯誤或 panic 時回滾。
func (s *Store) InTx(ctx context.Context, fn func(q *db.Queries) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		return fn(s.WithTx(tx))
	})
}

// IsNoRows 查無資料。
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// IsUniqueViolation 違反唯一約束;constraint 為空時不比對約束名稱。
func IsUniqueViolation(err error, constraint string) bool {
	return pgErrCode(err, "23505", constraint)
}

// IsForeignKeyViolation 違反外鍵約束(例如引用不存在的資料)。
func IsForeignKeyViolation(err error, constraint string) bool {
	return pgErrCode(err, "23503", constraint)
}

// IsCheckViolation 違反 CHECK 約束。
func IsCheckViolation(err error) bool {
	return pgErrCode(err, "23514", "")
}

func pgErrCode(err error, code, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}
