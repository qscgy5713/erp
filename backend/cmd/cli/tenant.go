package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"erp/internal/platform/database"
	"erp/internal/platform/migrate"
	"erp/internal/platform/tenant"
	"erp/migrations"
)

func runMulti(ctx context.Context, mgr *tenant.Manager, cmd string, args []string) error {
	if cmd != "init-control" {
		if err := mgr.InitControl(ctx); err != nil {
			return err
		}
	}
	switch cmd {
	case "init-control":
		if err := mgr.InitControl(ctx); err != nil {
			return err
		}
		fmt.Println("控制庫已就緒")
		return nil
	case "create-tenant":
		return createTenant(ctx, mgr, args)
	case "register-tenant":
		return registerTenant(ctx, mgr, args)
	case "list-tenants":
		return listTenants(ctx, mgr)
	case "disable-tenant", "enable-tenant":
		return setTenantActive(ctx, mgr, cmd == "enable-tenant", args)
	case "migrate-tenants":
		return migrateTenants(ctx, mgr, args)
	case "create-admin":
		return tenantCreateAdmin(ctx, mgr, args)
	case "cleanup-tokens":
		return tenantCleanupTokens(ctx, mgr, args)
	}
	usage()
	return nil
}

func createTenant(ctx context.Context, mgr *tenant.Manager, args []string) error {
	fs := flag.NewFlagSet("create-tenant", flag.ExitOnError)
	code := fs.String("code", "", "租戶代碼(小寫英數與連字號,必填)")
	name := fs.String("name", "", "公司名稱(必填)")
	admin := fs.String("admin", "", "同時建立的超級管理員帳號(密碼取自 ADMIN_PASSWORD)")
	_ = fs.Parse(args)
	dbName, n, err := mgr.Provision(ctx, *code, *name, migrations.FS)
	if err != nil {
		return err
	}
	fmt.Printf("已建立資料庫 %s,套用 %d 個 migration\n", dbName, n)
	if *admin != "" {
		if err := makeAdmin(tenant.With(ctx, *code), database.NewResolvedStore(mgr), *admin, "系統管理員", "HQ"); err != nil {
			// 租戶本身已建好,只是管理員沒建成:不回滾,讓使用者另外執行 create-admin
			fmt.Printf("提醒:租戶已建立,但管理員建立失敗(%v);請修正 ADMIN_PASSWORD 後執行 create-admin -tenant %s\n", err, *code)
		}
	}
	fmt.Printf("租戶 %s(%s)已建立\n", *code, *name)
	return nil
}

func registerTenant(ctx context.Context, mgr *tenant.Manager, args []string) error {
	fs := flag.NewFlagSet("register-tenant", flag.ExitOnError)
	code := fs.String("code", "", "租戶代碼")
	name := fs.String("name", "", "公司名稱")
	dbName := fs.String("db", "", "既有資料庫名稱")
	_ = fs.Parse(args)
	if *dbName == "" || strings.TrimSpace(*name) == "" {
		return errors.New("請指定 -code、-name、-db")
	}
	if err := mgr.Register(ctx, *code, strings.TrimSpace(*name), *dbName); err != nil {
		return err
	}
	fmt.Printf("已登記租戶 %s → 資料庫 %s\n", *code, *dbName)
	return nil
}

func listTenants(ctx context.Context, mgr *tenant.Manager) error {
	list, err := mgr.List(ctx)
	if err != nil {
		return err
	}
	for _, t := range list {
		state := "啟用"
		if !t.IsActive {
			state = "停用"
		}
		fmt.Printf("%-20s %-6s %-24s %s\n", t.Code, state, t.DBName, t.Name)
	}
	fmt.Printf("共 %d 個租戶\n", len(list))
	return nil
}

func setTenantActive(ctx context.Context, mgr *tenant.Manager, active bool, args []string) error {
	fs := flag.NewFlagSet("tenant-active", flag.ExitOnError)
	code := fs.String("code", "", "租戶代碼")
	_ = fs.Parse(args)
	if err := mgr.SetActive(ctx, *code, active); err != nil {
		return err
	}
	fmt.Printf("租戶 %s 已%s(正在執行的 API 程序最慢 15 秒內生效)\n", *code, map[bool]string{true: "啟用", false: "停用"}[active])
	return nil
}

// targets 指定的租戶,或全部啟用中的租戶。
func targets(ctx context.Context, mgr *tenant.Manager, only string) ([]tenant.Info, error) {
	all, err := mgr.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []tenant.Info
	for _, t := range all {
		if only != "" && t.Code != only {
			continue
		}
		if only == "" && !t.IsActive {
			continue
		}
		out = append(out, t)
	}
	if only != "" && len(out) == 0 {
		return nil, fmt.Errorf("找不到租戶 %s", only)
	}
	return out, nil
}

func migrateTenants(ctx context.Context, mgr *tenant.Manager, args []string) error {
	fs := flag.NewFlagSet("migrate-tenants", flag.ExitOnError)
	only := fs.String("tenant", "", "只升級這個租戶(預設全部啟用中的租戶)")
	_ = fs.Parse(args)
	list, err := targets(ctx, mgr, *only)
	if err != nil {
		return err
	}
	var failed []string
	for _, t := range list {
		dsn, err := mgr.DSN(t.DBName)
		if err != nil {
			return err
		}
		pool, err := database.Connect(ctx, dsn)
		if err == nil {
			var ver int64
			var n int
			ver, n, err = migrate.Up(ctx, pool, migrations.FS)
			pool.Close()
			if err == nil {
				fmt.Printf("%-20s 版本 %d(本次套用 %d 個)\n", t.Code, ver, n)
				continue
			}
		}
		fmt.Printf("%-20s 失敗:%v\n", t.Code, err)
		failed = append(failed, t.Code)
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d 個租戶升級失敗:%s", len(failed), strings.Join(failed, ", "))
	}
	return nil
}

func tenantCreateAdmin(ctx context.Context, mgr *tenant.Manager, args []string) error {
	fs := flag.NewFlagSet("create-admin", flag.ExitOnError)
	code := fs.String("tenant", "", "租戶代碼(必填)")
	username := fs.String("username", "", "登入帳號(必填)")
	name := fs.String("name", "系統管理員", "顯示名稱")
	companyCode := fs.String("company", "HQ", "公司代碼")
	_ = fs.Parse(args)
	if *code == "" || *username == "" {
		return errors.New("多租戶請指定 -tenant 與 -username")
	}
	if _, err := mgr.Active(ctx, *code); err != nil {
		return fmt.Errorf("租戶 %s: %w", *code, err)
	}
	return makeAdmin(tenant.With(ctx, *code), database.NewResolvedStore(mgr), *username, *name, *companyCode)
}

func tenantCleanupTokens(ctx context.Context, mgr *tenant.Manager, args []string) error {
	fs := flag.NewFlagSet("cleanup-tokens", flag.ExitOnError)
	keep := fs.Int("keep-days", 30, "保留天數")
	only := fs.String("tenant", "", "只處理這個租戶(預設全部)")
	_ = fs.Parse(args)
	list, err := targets(ctx, mgr, *only)
	if err != nil {
		return err
	}
	store := database.NewResolvedStore(mgr)
	for _, t := range list {
		n, err := store.DeleteStaleRefreshTokens(tenant.With(ctx, t.Code), int32(*keep))
		if err != nil {
			return fmt.Errorf("租戶 %s: %w", t.Code, err)
		}
		fmt.Printf("%-20s 已清除 %d 筆 refresh token 紀錄\n", t.Code, n)
	}
	return nil
}
