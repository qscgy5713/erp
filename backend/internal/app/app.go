// Package app 組裝所有模組與路由,供 cmd/api 與整合測試共用。
package app

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"erp/internal/approval"
	"erp/internal/auth"
	"erp/internal/costing"
	"erp/internal/dashboard"
	"erp/internal/finance"
	"erp/internal/gl"
	"erp/internal/imports"
	"erp/internal/inventory"
	"erp/internal/masterdata"
	"erp/internal/notify"
	"erp/internal/platform/config"
	"erp/internal/platform/database"
	"erp/internal/platform/httpserver"
	"erp/internal/platform/ratelimit"
	"erp/internal/platform/tenant"
	"erp/internal/production"
	"erp/internal/purchase"
	"erp/internal/sales"
	"erp/internal/system"
)

// NewRouter 單租戶。
func NewRouter(cfg config.Config, pool *pgxpool.Pool) (*gin.Engine, error) {
	return newRouter(cfg, database.NewStore(pool), pool, nil)
}

// NewTenantRouter 多租戶(D92):每個租戶一個資料庫,依登入者的租戶選擇。
func NewTenantRouter(cfg config.Config, m *tenant.Manager) (*gin.Engine, error) {
	return newRouter(cfg, database.NewResolvedStore(m), m, m)
}

func newRouter(cfg config.Config, store *database.Store, health httpserver.Pinger, tenants *tenant.Manager) (*gin.Engine, error) {
	notify.Configure(cfg.SMTPEnabled(), cfg.AppBaseURL)
	tokens := auth.NewTokenIssuer(cfg.JWTSecret, cfg.AccessTokenTTL)

	// 限流:登入/刷新尚未登入,以 IP 計算;其餘 API 掛在驗證之後,以使用者計算
	limiter := func(perMinute int) gin.HandlerFunc {
		return ratelimit.Middleware(ratelimit.New(perMinute, max(perMinute/2, 1)))
	}
	authHandler := auth.NewHandler(auth.NewService(store, tokens, cfg.RefreshTokenTTL).WithTOTPKey(cfg.TOTPKey, cfg.JWTSecret), tokens,
		cfg.IsProduction(), limiter(cfg.LoginRateLimitPerMinute), limiter(cfg.RefreshRateLimitPerMinute))
	if tenants != nil {
		authHandler.WithTenants(tenants)
	}
	apiLimit := limiter(cfg.RateLimitPerMinute)
	systemModule := system.New(store)
	masterdataModule := masterdata.New(store)
	inventoryModule := inventory.New(store)
	purchaseModule := purchase.New(store)
	salesModule := sales.New(store)
	financeModule := finance.New(store)
	glModule := gl.New(store)
	costingModule := costing.New(store)
	importsModule := imports.New(store)
	dashboardModule := dashboard.New(store)
	approvalModule := approval.New(store)
	productionModule := production.New(store)
	notifyHandler := notify.NewHandler(store)

	return httpserver.NewRouter(httpserver.Deps{
		DB:             health,
		Production:     cfg.IsProduction(),
		TrustedProxies: cfg.TrustedProxies,
		Authenticate:   authHandler.Authenticate(),
		RateLimit:      apiLimit,
		Modules: func(public, protected *gin.RouterGroup) {
			authHandler.Register(public, protected)
			systemModule.Register(protected)
			masterdataModule.Register(protected)
			inventoryModule.Register(protected)
			purchaseModule.Register(protected)
			salesModule.Register(protected)
			financeModule.Register(protected)
			glModule.Register(protected)
			costingModule.Register(protected)
			importsModule.Register(protected)
			dashboardModule.Register(protected)
			approvalModule.Register(protected)
			productionModule.Register(protected)
			notifyHandler.Register(protected)
		},
	})
}
