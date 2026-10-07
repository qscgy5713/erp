// Package httpserver 組裝 Gin router 與共用中介層。
package httpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/shared/response"
)

// Pinger 用於健康檢查,*pgxpool.Pool 即符合。
type Pinger interface {
	Ping(ctx context.Context) error
}

type Deps struct {
	DB             Pinger
	Production     bool
	TrustedProxies []string // nil 表示不信任任何 proxy,ClientIP 取連線來源位址
}

func NewRouter(d Deps) (*gin.Engine, error) {
	if d.Production {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	// Gin 預設信任所有 proxy,X-Forwarded-For 可被偽造;改為只信任明確設定者。
	if err := r.SetTrustedProxies(d.TrustedProxies); err != nil {
		return nil, fmt.Errorf("TRUSTED_PROXIES 格式錯誤: %w", err)
	}
	r.Use(gin.Recovery(), requestLogger())
	r.NoRoute(func(c *gin.Context) {
		response.Fail(c, http.StatusNotFound, "SYS-404", "找不到資源")
	})

	v1 := r.Group("/api/v1")
	v1.GET("/health", healthHandler(d.DB))

	return r, nil
}

func healthHandler(db Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			slog.Error("health: 資料庫無回應", "err", err)
			response.Fail(c, http.StatusServiceUnavailable, "SYS-503", "資料庫無回應")
			return
		}
		response.OK(c, gin.H{"status": "ok"})
	}
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("http",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}
