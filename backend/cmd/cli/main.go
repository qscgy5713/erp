// Command cli 為維運指令。
//
//	create-admin -username admin [-name 系統管理員] [-company HQ]
//	    建立超級管理員;密碼由環境變數 ADMIN_PASSWORD 提供(避免留在 shell 歷史)。
//	    首次登入需變更密碼。
//	cleanup-tokens [-keep-days 30]
//	    清除過期或已撤銷的 refresh token 紀錄。
//
// 多租戶(設定 CONTROL_DATABASE_URL,D92)另有:
//
//	init-control                                        建立控制庫的租戶清單表(api 啟動時也會自動建立)
//	create-tenant -code acme -name 公司名 [-admin 帳號]   建立租戶資料庫、套用所有 migration、(選填)建立管理員
//	register-tenant -code acme -name 公司名 -db 資料庫     把既有資料庫登記為租戶(舊的單租戶資料庫轉入用)
//	list-tenants                                        列出租戶
//	disable-tenant / enable-tenant -code acme           停用 / 啟用租戶(停用後該租戶無法登入與呼叫 API)
//	migrate-tenants [-tenant acme]                      對所有(或指定)租戶資料庫套用尚未套用的 migration
//
// 多租戶時 create-admin 須加 -tenant,cleanup-tokens 預設處理所有租戶(可用 -tenant 限定)。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"erp/internal/auth"
	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/tenant"
	"erp/internal/shared/apperr"
	"erp/internal/system/audit"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx := context.Background()
	cmd, args := os.Args[1], os.Args[2:]

	if controlDSN := os.Getenv("CONTROL_DATABASE_URL"); controlDSN != "" {
		mgr, err := tenant.NewManager(ctx, controlDSN, 2)
		if err != nil {
			fail(err)
		}
		defer mgr.Close()
		if err := runMulti(ctx, mgr, cmd, args); err != nil {
			fail(err)
		}
		return
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fail(errors.New("DATABASE_URL 未設定"))
	}
	pool, err := database.Connect(ctx, dsn)
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	store := database.NewStore(pool)

	switch cmd {
	case "create-admin":
		err = createAdmin(ctx, store, args)
	case "cleanup-tokens":
		err = cleanupTokens(ctx, store, args)
	default:
		usage()
	}
	if err != nil {
		fail(err)
	}
}

func createAdmin(ctx context.Context, store *database.Store, args []string) error {
	fs := flag.NewFlagSet("create-admin", flag.ExitOnError)
	username := fs.String("username", "", "登入帳號(必填)")
	name := fs.String("name", "系統管理員", "顯示名稱")
	companyCode := fs.String("company", "HQ", "公司代碼")
	_ = fs.Parse(args)
	if *username == "" {
		return errors.New("請指定 -username")
	}
	return makeAdmin(ctx, store, *username, *name, *companyCode)
}

// makeAdmin 建立超級管理員;密碼取自環境變數 ADMIN_PASSWORD。
func makeAdmin(ctx context.Context, store *database.Store, username, name, companyCode string) error {
	password := os.Getenv("ADMIN_PASSWORD")
	if err := auth.ValidatePassword("ADMIN_PASSWORD", password); err != nil {
		if e := apperr.As(err); e != nil {
			return fmt.Errorf("密碼不符合政策: %v", e.Details)
		}
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	company, err := store.GetCompanyByCode(ctx, companyCode)
	if err != nil {
		return fmt.Errorf("找不到公司 %s: %w", companyCode, err)
	}
	return store.InTx(ctx, func(q *db.Queries) error {
		u, err := q.CreateUser(ctx, db.CreateUserParams{
			CompanyID: company.ID, Username: username, Name: name, PasswordHash: hash,
			IsSuperadmin: true, MustChangePassword: true,
		})
		if database.IsUniqueViolation(err, "users_username_key") {
			return fmt.Errorf("帳號 %s 已存在", username)
		}
		if err != nil {
			return err
		}
		fmt.Printf("已建立超級管理員 %s(id=%d),首次登入需變更密碼\n", u.Username, u.ID)
		return audit.Record(ctx, q, audit.Entry{
			CompanyID: company.ID, Action: audit.Create, EntityType: "user", EntityID: &u.ID,
			Summary: "以 CLI 建立超級管理員 " + u.Username,
		})
	})
}

func cleanupTokens(ctx context.Context, store *database.Store, args []string) error {
	fs := flag.NewFlagSet("cleanup-tokens", flag.ExitOnError)
	keep := fs.Int("keep-days", 30, "保留天數")
	_ = fs.Parse(args)
	n, err := store.DeleteStaleRefreshTokens(ctx, int32(*keep))
	if err != nil {
		return err
	}
	fmt.Printf("已清除 %d 筆 refresh token 紀錄\n", n)
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: cli <create-admin|cleanup-tokens|create-tenant|register-tenant|list-tenants|disable-tenant|enable-tenant|migrate-tenants|init-control> [選項]")
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "錯誤:", err)
	os.Exit(1)
}
