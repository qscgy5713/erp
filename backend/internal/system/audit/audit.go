// Package audit 寫入稽核日誌。須在與業務資料相同的交易內呼叫,
// 業務失敗回滾時稽核也一併回滾,不會留下不實紀錄。
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"erp/internal/db"
	"erp/internal/shared/authctx"
)

// 動作
const (
	Create         = "create"
	Update         = "update"
	Delete         = "delete"
	Login          = "login"
	LoginFailed    = "login_failed"
	Logout         = "logout"
	PasswordReset  = "password_reset"
	PasswordChange = "password_change"
	Unlock         = "unlock"
	TokenReuse     = "token_reuse"
)

type Entry struct {
	// CompanyID 未登入的事件(例如登入失敗)須自行指定;否則取登入者的公司
	CompanyID  int64
	UserID     *int64 // 未指定時取登入者
	Action     string
	EntityType string
	EntityID   *int64
	Summary    string
	Before     any // 會序列化為 JSON;不可放密碼雜湊等機敏欄位
	After      any
}

func Record(ctx context.Context, q *db.Queries, e Entry) error {
	actor := authctx.ActorFrom(ctx)
	meta := authctx.MetaFrom(ctx)
	if e.CompanyID == 0 && actor != nil {
		e.CompanyID = actor.CompanyID
	}
	if e.UserID == nil && actor != nil {
		e.UserID = &actor.UserID
	}
	if e.CompanyID == 0 {
		return fmt.Errorf("audit: 缺少 company_id")
	}
	before, err := toJSON(e.Before)
	if err != nil {
		return err
	}
	after, err := toJSON(e.After)
	if err != nil {
		return err
	}
	return q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		CompanyID:  e.CompanyID,
		UserID:     e.UserID,
		Action:     e.Action,
		EntityType: e.EntityType,
		EntityID:   e.EntityID,
		Summary:    truncateRunes(e.Summary, 255),
		BeforeData: before,
		AfterData:  after,
		Ip:         meta.IP,
		UserAgent:  meta.UserAgent,
		RequestID:  meta.RequestID,
	})
}

func toJSON(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("audit: 序列化: %w", err)
	}
	return b, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
