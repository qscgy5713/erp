// Command api 啟動 ERP HTTP API。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"erp/internal/app"
	"erp/internal/db"
	"erp/internal/notify"
	"erp/internal/platform/config"
	"erp/internal/platform/database"
	"erp/internal/platform/tenant"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api 結束", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.IsProduction() {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		router  http.Handler
		queries *db.Queries
		tenants func(context.Context) ([]string, error)
	)
	if cfg.MultiTenant() {
		// 多租戶(D92):每個租戶一個資料庫,控制庫記錄租戶清單
		mgr, err := tenant.NewManager(ctx, cfg.ControlDatabaseURL, int32(cfg.TenantPoolMaxConns))
		if err != nil {
			return err
		}
		defer mgr.Close()
		if err := mgr.InitControl(ctx); err != nil {
			return err
		}
		r, err := app.NewTenantRouter(cfg, mgr)
		if err != nil {
			return err
		}
		router, queries, tenants = r, database.NewResolvedStore(mgr).Queries, mgr.ActiveCodes
		slog.Info("多租戶模式", "tenant_pool_max_conns", cfg.TenantPoolMaxConns)
	} else {
		pool, err := database.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		r, err := app.NewRouter(cfg, pool)
		if err != nil {
			return err
		}
		router, queries = r, db.New(pool)
	}

	if cfg.SMTPEnabled() {
		w := &notify.Worker{Q: queries, Tenants: tenants, Sender: notify.SMTPSender{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort, User: cfg.SMTPUser, Password: cfg.SMTPPassword, From: cfg.SMTPFrom,
		}}
		go w.Run(ctx)
		slog.Info("通知信已啟用", "smtp", cfg.SMTPHost, "from", cfg.SMTPFrom)
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("api 啟動", "addr", cfg.HTTPAddr, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	slog.Info("api 關閉中")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
