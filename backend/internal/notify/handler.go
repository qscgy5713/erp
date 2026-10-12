package notify

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"erp/internal/db"
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
	r.GET("/notifications/prefs", h.listPrefs)
	r.PUT("/notifications/prefs", h.updatePrefs)
}

// Events 可訂閱的事件(順序即畫面順序)。沒有紀錄的事件預設開啟。
var Events = []struct{ Key, Label, Desc string }{
	{"approval_pending", "待我核准", "有單據輪到我核准(含代理他人時)時通知"},
	{"check_due", "票據到期", "未兌現票據 3 天內到期或已逾期(限有收款/付款過帳權限者)"},
	{"low_stock", "低庫存週報", "每週一封:低於安全庫存的料品摘要(限有庫存檢視權限者)"},
	{"lot_expiry", "批號效期預警週報", "每週一封:已過期或 30 天內到期且有庫存的批號(限有庫存檢視權限者)"},
	{"ar_overdue", "逾期應收週報", "每週一封:逾期未收的筆數與金額(限有收款過帳權限且資料範圍為全部者)"},
}

type prefDTO struct {
	Event   string `json:"event"`
	Label   string `json:"label"`
	Desc    string `json:"desc"`
	Enabled bool   `json:"enabled"`
}

// listPrefs GET /notifications/prefs 我的通知訂閱(沒設定過的事件為開啟)。
func (h *Handler) listPrefs(c *gin.Context) {
	ctx := c.Request.Context()
	rows, err := h.store.ListNotificationPrefs(ctx, authctx.ActorFrom(ctx).UserID)
	if err != nil {
		response.Error(c, err)
		return
	}
	off := map[string]bool{}
	for _, r := range rows {
		off[r.Event] = !r.Enabled
	}
	out := make([]prefDTO, len(Events))
	for i, e := range Events {
		out[i] = prefDTO{e.Key, e.Label, e.Desc, !off[e.Key]}
	}
	response.OK(c, out)
}

// updatePrefs PUT /notifications/prefs {"prefs":{"low_stock":false,...}} 只更新有帶的事件。
func (h *Handler) updatePrefs(c *gin.Context) {
	var in struct {
		Prefs map[string]bool `json:"prefs" binding:"required"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Error(c, apperr.Validation(map[string]string{"prefs": "必填"}))
		return
	}
	known := map[string]bool{}
	for _, e := range Events {
		known[e.Key] = true
	}
	for k := range in.Prefs {
		if !known[k] {
			response.Error(c, apperr.Validation(map[string]string{"prefs": "未知的通知事件:" + k}))
			return
		}
	}
	ctx := c.Request.Context()
	uid := authctx.ActorFrom(ctx).UserID
	err := h.store.InTx(ctx, func(q *db.Queries) error {
		for k, on := range in.Prefs {
			if err := q.UpsertNotificationPref(ctx, db.UpsertNotificationPrefParams{UserID: uid, Event: k, Enabled: on}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	h.listPrefs(c)
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
