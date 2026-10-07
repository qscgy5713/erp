// Command cli 為維運指令。
//
//	create-admin -username admin [-name 系統管理員] [-company HQ]
//	    建立超級管理員;密碼由環境變數 ADMIN_PASSWORD 提供(避免留在 shell 歷史)。
//	    首次登入需變更密碼。
//	cleanup-tokens [-keep-days 30]
//	    清除過期或已撤銷的 refresh token 紀錄。
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
	"erp/internal/shared/apperr"
	"erp/internal/system/audit"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx := context.Background()
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

	switch os.Args[1] {
	case "create-admin":
		err = createAdmin(ctx, store, os.Args[2:])
	case "cleanup-tokens":
		err = cleanupTokens(ctx, store, os.Args[2:])
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

	company, err := store.GetCompanyByCode(ctx, *companyCode)
	if err != nil {
		return fmt.Errorf("找不到公司 %s: %w", *companyCode, err)
	}
	return store.InTx(ctx, func(q *db.Queries) error {
		u, err := q.CreateUser(ctx, db.CreateUserParams{
			CompanyID: company.ID, Username: *username, Name: *name, PasswordHash: hash,
			IsSuperadmin: true, MustChangePassword: true,
		})
		if database.IsUniqueViolation(err, "users_username_key") {
			return fmt.Errorf("帳號 %s 已存在", *username)
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
	fmt.Fprintln(os.Stderr, "用法: cli <create-admin|cleanup-tokens> [選項]")
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "錯誤:", err)
	os.Exit(1)
}
