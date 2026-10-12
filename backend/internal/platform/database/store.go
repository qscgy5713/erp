package database

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"erp/internal/db"
)

// PoolResolver 依 context 選出連線池(單租戶固定一個,多租戶依租戶代碼,見 platform/tenant)。
type PoolResolver interface {
	Pool(ctx context.Context) (*pgxpool.Pool, error)
}

type fixedPool struct{ p *pgxpool.Pool }

func (f fixedPool) Pool(context.Context) (*pgxpool.Pool, error) { return f.p, nil }

// router 讓 sqlc 的查詢在每次執行時才決定連線池;ctx 本來就會傳進每個查詢,所以呼叫端完全不用改。
type router struct{ res PoolResolver }

func (r router) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	p, err := r.res.Pool(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return p.Exec(ctx, sql, args...)
}

func (r router) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	p, err := r.res.Pool(ctx)
	if err != nil {
		return nil, err
	}
	return p.Query(ctx, sql, args...)
}

func (r router) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	p, err := r.res.Pool(ctx)
	if err != nil {
		return errRow{err}
	}
	return p.QueryRow(ctx, sql, args...)
}

type errRow struct{ err error }

func (e errRow) Scan(...any) error { return e.err }

// Store 包裝連線池與 sqlc 查詢;單筆查詢直接用內嵌的 *db.Queries,
// 需要交易時用 InTx。
type Store struct {
	res PoolResolver
	*db.Queries
}

// NewStore 單租戶。
func NewStore(pool *pgxpool.Pool) *Store { return NewResolvedStore(fixedPool{pool}) }

// NewResolvedStore 多租戶:每個查詢 / 交易依 context 中的租戶選擇資料庫。
func NewResolvedStore(res PoolResolver) *Store {
	return &Store{res: res, Queries: db.New(router{res})}
}

// InTx 在交易內執行 fn;fn 回傳錯誤或 panic 時回滾。
func (s *Store) InTx(ctx context.Context, fn func(q *db.Queries) error) error {
	p, err := s.res.Pool(ctx)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
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
