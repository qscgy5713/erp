package auth

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/response"
)

const (
	refreshCookie = "erp_refresh"
	cookiePath    = "/api/v1/auth"
)

type Handler struct {
	svc          *Service
	tokens       *TokenIssuer
	secureCookie bool
	loginLimit   gin.HandlerFunc
	refreshLimit gin.HandlerFunc
}

// NewHandler loginLimit / refreshLimit 為登入與刷新的限流中介層(尚未登入,以 IP 計算)。
// 兩者分開:刷新在每次整頁載入都會發生,共用 IP 的辦公室流量大,不能跟登入擠同一份額度。
func NewHandler(svc *Service, tokens *TokenIssuer, secureCookie bool, loginLimit, refreshLimit gin.HandlerFunc) *Handler {
	return &Handler{svc: svc, tokens: tokens, secureCookie: secureCookie, loginLimit: loginLimit, refreshLimit: refreshLimit}
}

// Register 掛上 /auth 路由。protected 為已套用 Authenticate 的群組。
func (h *Handler) Register(public, protected *gin.RouterGroup) {
	g := public.Group("/auth")
	g.POST("/login", h.loginLimit, h.login)
	g.POST("/refresh", h.refreshLimit, h.refresh)
	g.POST("/logout", h.logout)

	p := protected.Group("/auth")
	p.GET("/me", h.me)
	p.POST("/change-password", h.changePassword)
}

type loginRequest struct {
	Username string `json:"username" binding:"required,max=50"`
	Password string `json:"password" binding:"required,max=200"`
}

type sessionResponse struct {
	AccessToken string     `json:"access_token"`
	ExpiresAt   time.Time  `json:"expires_at"`
	User        meResponse `json:"user"`
}

type meResponse struct {
	ID                 int64    `json:"id"`
	CompanyID          int64    `json:"company_id"`
	Username           string   `json:"username"`
	Name               string   `json:"name"`
	Email              *string  `json:"email"`
	DepartmentID       *int64   `json:"department_id"`
	IsSuperadmin       bool     `json:"is_superadmin"`
	MustChangePassword bool     `json:"must_change_password"`
	DataScope          string   `json:"data_scope"`
	Permissions        []string `json:"permissions"`
}

func (h *Handler) login(c *gin.Context) {
	var req loginRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	sess, err := h.svc.Login(c.Request.Context(), strings.TrimSpace(req.Username), req.Password)
	if err != nil {
		response.Error(c, err)
		return
	}
	h.respondSession(c, sess)
}

func (h *Handler) refresh(c *gin.Context) {
	raw, _ := c.Cookie(refreshCookie)
	sess, err := h.svc.Refresh(c.Request.Context(), raw)
	if err != nil {
		h.clearCookie(c)
		response.Error(c, err)
		return
	}
	h.respondSession(c, sess)
}

func (h *Handler) logout(c *gin.Context) {
	raw, _ := c.Cookie(refreshCookie)
	if err := h.svc.Logout(c.Request.Context(), raw); err != nil {
		response.Error(c, err)
		return
	}
	h.clearCookie(c)
	response.NoContent(c)
}

func (h *Handler) me(c *gin.Context) {
	a := authctx.ActorFrom(c.Request.Context())
	_, u, err := h.svc.ActorForUser(c.Request.Context(), a.UserID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toMe(a, u.Email))
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required,max=200"`
	NewPassword string `json:"new_password" binding:"required,max=200"`
}

func (h *Handler) changePassword(c *gin.Context) {
	var req changePasswordRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	a := authctx.ActorFrom(c.Request.Context())
	sess, err := h.svc.ChangePassword(c.Request.Context(), a.UserID, req.OldPassword, req.NewPassword)
	if err != nil {
		response.Error(c, err)
		return
	}
	h.respondSession(c, sess)
}

func (h *Handler) respondSession(c *gin.Context, sess Session) {
	a, u, err := h.svc.ActorForUser(c.Request.Context(), sess.UserID)
	if err != nil {
		response.Error(c, err)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshCookie,
		Value:    sess.RefreshToken,
		Path:     cookiePath,
		Expires:  sess.RefreshExpiresAt,
		MaxAge:   int(time.Until(sess.RefreshExpiresAt).Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteStrictMode, // 跨站請求不會帶 cookie,防 CSRF
	})
	response.OK(c, sessionResponse{AccessToken: sess.AccessToken, ExpiresAt: sess.AccessExpiresAt, User: toMe(a, u.Email)})
}

func (h *Handler) clearCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: refreshCookie, Value: "", Path: cookiePath, MaxAge: -1,
		HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteStrictMode,
	})
}

func toMe(a *authctx.Actor, email *string) meResponse {
	perms := make([]string, 0, len(a.Permissions))
	for p := range a.Permissions {
		perms = append(perms, p)
	}
	sort.Strings(perms)
	return meResponse{
		ID: a.UserID, CompanyID: a.CompanyID, Username: a.Username, Name: a.Name, Email: email,
		DepartmentID: a.DepartmentID, IsSuperadmin: a.IsSuperadmin,
		MustChangePassword: a.MustChangePassword, DataScope: a.Scope(), Permissions: perms,
	}
}

// Authenticate 驗證 Bearer access token 並把登入者放進 context。
// 必須變更密碼的使用者只能存取 allowWhenMustChange 中的路徑。
func (h *Handler) Authenticate() gin.HandlerFunc {
	allowWhenMustChange := map[string]bool{
		"/api/v1/auth/me":              true,
		"/api/v1/auth/change-password": true,
	}
	return func(c *gin.Context) {
		token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || token == "" {
			response.Error(c, apperr.ErrUnauthorized)
			return
		}
		claims, err := h.tokens.Parse(token)
		if err != nil {
			response.Error(c, apperr.ErrUnauthorized)
			return
		}
		actor, err := h.svc.LoadActor(c.Request.Context(), claims)
		if err != nil {
			response.Error(c, err)
			return
		}
		if actor.MustChangePassword && !allowWhenMustChange[c.FullPath()] {
			response.Error(c, ErrMustChangePassword)
			return
		}
		c.Request = c.Request.WithContext(authctx.WithActor(c.Request.Context(), actor))
		c.Next()
	}
}

// Require 要求具備任一指定權限。
func Require(perms ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		a := authctx.ActorFrom(c.Request.Context())
		for _, p := range perms {
			if a.Can(p) {
				c.Next()
				return
			}
		}
		response.Error(c, apperr.ErrForbidden)
	}
}
