// Package tenant 多租戶(分庫不分表,D92):每個租戶一個獨立的 PostgreSQL 資料庫,表結構完全相同;
// 另有一個「控制庫」只記錄有哪些租戶。請求經過驗證後,租戶代碼放在 context,
// 資料庫層依 context 把查詢送到該租戶的連線池——應用程式碼完全不需要知道租戶。
//
// 單租戶部署(沒設 CONTROL_DATABASE_URL)用 Fixed,行為與以前完全相同。
package tenant

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"erp/internal/platform/database"
	"erp/internal/platform/migrate"
)

type ctxKey struct{}

// With 把租戶代碼放進 context。
func With(ctx context.Context, code string) context.Context {
	return context.WithValue(ctx, ctxKey{}, code)
}

// From 取出租戶代碼;沒有時回傳空字串。
func From(ctx context.Context) string {
	s, _ := ctx.Value(ctxKey{}).(string)
	return s
}

var (
	ErrNoTenant = errors.New("tenant: context 沒有租戶代碼")
	ErrUnknown  = errors.New("tenant: 租戶不存在")
	ErrDisabled = errors.New("tenant: 租戶已停用")
)

// codePattern 租戶代碼:小寫英數與連字號,2–31 字元,開頭須為英數。同時是資料庫名稱的一部分,所以限制很嚴。
var codePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}$`)

func ValidCode(code string) bool { return codePattern.MatchString(code) }

// DBName 租戶代碼對應的預設資料庫名稱。
func DBName(code string) string { return "erp_" + strings.ReplaceAll(code, "-", "_") }

var dbNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

func ValidDBName(name string) bool { return dbNamePattern.MatchString(name) }

// Resolver 依 context 的租戶選出連線池。
type Resolver interface {
	Pool(ctx context.Context) (*pgxpool.Pool, error)
}

// Fixed 單租戶:永遠回傳同一個連線池,忽略 context。
type Fixed struct{ P *pgxpool.Pool }

func (f Fixed) Pool(context.Context) (*pgxpool.Pool, error) { return f.P, nil }

// Info 控制庫中的一筆租戶。
type Info struct {
	ID       int64
	Code     string
	Name     string
	DBName   string
	IsActive bool
}

const controlSchema = `
CREATE TABLE IF NOT EXISTS tenants (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code       VARCHAR(31)  NOT NULL UNIQUE,
    name       VARCHAR(100) NOT NULL,
    db_name    VARCHAR(63)  NOT NULL UNIQUE,
    is_active  BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
)`

// Manager 控制庫 + 各租戶連線池(第一次用到才建立,閒置連線自動回收)。
type Manager struct {
	control    *pgxpool.Pool
	controlURL string
	maxConns   int32

	mu    sync.Mutex
	pools map[string]*pgxpool.Pool
	cache map[string]cacheEntry
}

type cacheEntry struct {
	info Info
	err  error
	at   time.Time
}

// infoTTL 停用租戶最慢這麼久後生效(同一個程序內的停用 / 啟用指令會立即清除快取)。
const infoTTL = 15 * time.Second

// NewManager controlURL 為控制庫;租戶資料庫與控制庫在同一個 PostgreSQL 伺服器(同帳密),只是資料庫名稱不同。
func NewManager(ctx context.Context, controlURL string, tenantMaxConns int32) (*Manager, error) {
	cp, err := database.Connect(ctx, controlURL)
	if err != nil {
		return nil, fmt.Errorf("連線控制庫: %w", err)
	}
	if tenantMaxConns <= 0 {
		tenantMaxConns = 8
	}
	return &Manager{control: cp, controlURL: controlURL, maxConns: tenantMaxConns,
		pools: map[string]*pgxpool.Pool{}, cache: map[string]cacheEntry{}}, nil
}

// InitControl 建立控制庫的 tenants 表(可重複執行)。
func (m *Manager) InitControl(ctx context.Context) error {
	_, err := m.control.Exec(ctx, controlSchema)
	return err
}

func (m *Manager) Control() *pgxpool.Pool { return m.control }

// Ping 健康檢查:控制庫是否可連線。
func (m *Manager) Ping(ctx context.Context) error { return m.control.Ping(ctx) }

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pools {
		p.Close()
	}
	m.pools = map[string]*pgxpool.Pool{}
	m.control.Close()
}

// DSN 租戶資料庫的連線字串:控制庫的連線字串換掉資料庫名稱。
func (m *Manager) DSN(dbName string) (string, error) {
	u, err := url.Parse(m.controlURL)
	if err != nil {
		return "", err
	}
	u.Path = "/" + dbName
	return u.String(), nil
}

// AdminDSN 連到維護用資料庫(postgres),建立 / 刪除租戶資料庫時使用。
func (m *Manager) AdminDSN() (string, error) { return m.DSN("postgres") }

// Lookup 以代碼查租戶(含短暫快取);不存在回傳 ErrUnknown。
func (m *Manager) Lookup(ctx context.Context, code string) (Info, error) {
	m.mu.Lock()
	if e, ok := m.cache[code]; ok && time.Since(e.at) < infoTTL {
		m.mu.Unlock()
		return e.info, e.err
	}
	m.mu.Unlock()
	var info Info
	err := m.control.QueryRow(ctx, `SELECT id, code, name, db_name, is_active FROM tenants WHERE code = $1`, code).
		Scan(&info.ID, &info.Code, &info.Name, &info.DBName, &info.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrUnknown
	}
	if err != nil && !errors.Is(err, ErrUnknown) {
		return Info{}, err // 暫時性錯誤不快取
	}
	m.mu.Lock()
	m.cache[code] = cacheEntry{info, err, time.Now()}
	m.mu.Unlock()
	return info, err
}

// Active 租戶存在且啟用;否則回傳 ErrUnknown / ErrDisabled。
func (m *Manager) Active(ctx context.Context, code string) (Info, error) {
	info, err := m.Lookup(ctx, code)
	if err != nil {
		return Info{}, err
	}
	if !info.IsActive {
		return Info{}, ErrDisabled
	}
	return info, nil
}

// List 全部租戶。
func (m *Manager) List(ctx context.Context) ([]Info, error) {
	rows, err := m.control.Query(ctx, `SELECT id, code, name, db_name, is_active FROM tenants ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Info
	for rows.Next() {
		var i Info
		if err := rows.Scan(&i.ID, &i.Code, &i.Name, &i.DBName, &i.IsActive); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// ActiveCodes 啟用中的租戶代碼(背景工作逐一處理用)。
func (m *Manager) ActiveCodes(ctx context.Context) ([]string, error) {
	all, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, i := range all {
		if i.IsActive {
			out = append(out, i.Code)
		}
	}
	return out, nil
}

// Register 把租戶記進控制庫。
func (m *Manager) Register(ctx context.Context, code, name, dbName string) error {
	if !ValidCode(code) {
		return fmt.Errorf("租戶代碼須為小寫英數與連字號,2–31 字元:%q", code)
	}
	if !ValidDBName(dbName) {
		return fmt.Errorf("資料庫名稱不合法:%q", dbName)
	}
	_, err := m.control.Exec(ctx, `INSERT INTO tenants (code, name, db_name) VALUES ($1, $2, $3)`, code, name, dbName)
	m.forget(code)
	return err
}

// SetActive 停用 / 啟用租戶;停用後該租戶的請求立即(本程序)或最慢 15 秒內(其他程序)被拒。
func (m *Manager) SetActive(ctx context.Context, code string, active bool) error {
	tag, err := m.control.Exec(ctx, `UPDATE tenants SET is_active = $2 WHERE code = $1`, code, active)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUnknown
	}
	m.forget(code)
	return nil
}

func (m *Manager) forget(code string) {
	m.mu.Lock()
	delete(m.cache, code)
	m.mu.Unlock()
}

// Pool 依 context 中的租戶回傳連線池。沒有租戶代碼一律失敗——絕不預設到某個租戶。
func (m *Manager) Pool(ctx context.Context) (*pgxpool.Pool, error) {
	code := From(ctx)
	if code == "" {
		return nil, ErrNoTenant
	}
	info, err := m.Active(ctx, code)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	p, ok := m.pools[code]
	m.mu.Unlock()
	if ok {
		return p, nil
	}
	// 建立連線池(含連線測試)不持有鎖,避免一個租戶的資料庫慢 / 掛掉時擋住所有租戶
	dsn, err := m.DSN(info.DBName)
	if err != nil {
		return nil, err
	}
	np, err := database.ConnectOpts(ctx, dsn, m.maxConns, 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("連線租戶 %s 的資料庫: %w", code, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.pools[code]; ok { // 同時有別的請求先建好了
		np.Close()
		return p, nil
	}
	m.pools[code] = np
	return np, nil
}

// Provision 建立租戶:建立資料庫、套用所有 migration、把公司名稱設成 name、登記到控制庫。
// 任何一步失敗都會刪掉剛建立的資料庫,不留半成品。回傳資料庫名稱與套用的 migration 個數。
func (m *Manager) Provision(ctx context.Context, code, name string, migrations fs.FS) (dbName string, applied int, err error) {
	if !ValidCode(code) {
		return "", 0, fmt.Errorf("租戶代碼須為小寫英數與連字號,2–31 字元:%q", code)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", 0, errors.New("請指定公司名稱")
	}
	if _, err := m.Lookup(ctx, code); err == nil {
		return "", 0, fmt.Errorf("租戶 %s 已存在", code)
	} else if !errors.Is(err, ErrUnknown) {
		return "", 0, err
	}
	dbName = DBName(code)
	adminDSN, err := m.AdminDSN()
	if err != nil {
		return "", 0, err
	}
	ac, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return "", 0, fmt.Errorf("連線維護資料庫: %w", err)
	}
	defer func() { _ = ac.Close(ctx) }()
	if _, err := ac.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize()); err != nil {
		return "", 0, fmt.Errorf("建立資料庫 %s: %w", dbName, err)
	}
	defer func() {
		if err != nil {
			_, _ = ac.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)")
		}
	}()
	dsn, err := m.DSN(dbName)
	if err != nil {
		return "", 0, err
	}
	pool, err := database.Connect(ctx, dsn)
	if err != nil {
		return "", 0, err
	}
	defer pool.Close()
	if _, applied, err = migrate.Up(ctx, pool, migrations); err != nil {
		return "", 0, err
	}
	if _, err = pool.Exec(ctx, `UPDATE companies SET name = $1`, name); err != nil {
		return "", 0, err
	}
	if err = m.Register(ctx, code, name, dbName); err != nil {
		return "", 0, err
	}
	return dbName, applied, nil
}

// DropDatabase 刪除租戶資料庫並關閉其連線池(測試清理用;正式環境請人工處理,不提供 CLI)。
func (m *Manager) DropDatabase(ctx context.Context, code, dbName string) error {
	m.mu.Lock()
	if p, ok := m.pools[code]; ok {
		p.Close()
		delete(m.pools, code)
	}
	m.mu.Unlock()
	adminDSN, err := m.AdminDSN()
	if err != nil {
		return err
	}
	ac, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return err
	}
	defer func() { _ = ac.Close(ctx) }()
	_, err = ac.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)")
	return err
}
