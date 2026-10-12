package notify

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/response"
)

type Handler struct{ store *database.Store }

func NewHandler(store *database.Store) *Handler { return &Handler{store: store} }

// Register 掛上 /notifications;r 須已套用驗證。
func (h *Handler) Register(r *gin.RouterGroup) {
	r.POST("/notifications/test", h.test)
}

// test POST /notifications/test 寄一封測試信給自己(經由佇列,約 30 秒內寄出);只能寄給自己的 email。
func (h *Handler) test(c *gin.Context) {
	ctx := c.Request.Context()
	a := authctx.ActorFrom(ctx)
	if !Enabled() {
		response.Error(c, apperr.New(http.StatusConflict, "NTF-001", "通知信尚未啟用:請在伺服器設定 SMTP_USER / SMTP_PASSWORD"))
		return
	}
	u, err := h.store.GetUserByID(ctx, a.UserID)
	if err != nil {
		response.Error(c, err)
		return
	}
	if u.Email == nil || strings.TrimSpace(*u.Email) == "" {
		response.Error(c, apperr.Validation(map[string]string{"email": "你的帳號尚未設定 email,請管理員在使用者資料補上"}))
		return
	}
	if err := Enqueue(ctx, h.store.Queries, a.CompanyID, []int64{a.UserID}, "test", "ERP 測試通知信",
		"這是一封測試信,收到代表通知信設定正常。\n\n送出時間:"+time.Now().Format("2006-01-02 15:04:05"), ""); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, gin.H{"to": *u.Email})
}
