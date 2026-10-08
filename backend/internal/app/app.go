// Package app 組裝所有模組與路由,供 cmd/api 與整合測試共用。
package app

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"erp/internal/auth"
	"erp/internal/finance"
	"erp/internal/inventory"
	"erp/internal/masterdata"
	"erp/internal/platform/config"
	"erp/internal/platform/database"
	"erp/internal/platform/httpserver"
	"erp/internal/platform/ratelimit"
	"erp/internal/purchase"
	"erp/internal/sales"
	"erp/internal/system"
)

func NewRouter(cfg config.Config, pool *pgxpool.Pool) (*gin.Engine, error) {
	store := database.NewStore(pool)
	tokens := auth.NewTokenIssuer(cfg.JWTSecret, cfg.AccessTokenTTL)

	// 限流:登入/刷新尚未登入,以 IP 計算;其餘 API 掛在驗證之後,以使用者計算
	limiter := func(perMinute int) gin.HandlerFunc {
		return ratelimit.Middleware(ratelimit.New(perMinute, max(perMinute/2, 1)))
	}
	authHandler := auth.NewHandler(auth.NewService(store, tokens, cfg.RefreshTokenTTL), tokens,
		cfg.IsProduction(), limiter(cfg.LoginRateLimitPerMinute), limiter(cfg.RefreshRateLimitPerMinute))
	apiLimit := limiter(cfg.RateLimitPerMinute)
	systemModule := system.New(store)
	masterdataModule := masterdata.New(store)
	inventoryModule := inventory.New(store)
	purchaseModule := purchase.New(store)
	salesModule := sales.New(store)
	financeModule := finance.New(store)

	return httpserver.NewRouter(httpserver.Deps{
		DB:             pool,
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
		},
	})
}
