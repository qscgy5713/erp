// Package notify 通知信(D88):交易內只把信寫進佇列(notifications),
// 由背景程式(Worker)透過 SMTP 寄出;寄信失敗只會重試,不影響單據交易。
package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"sync/atomic"
	"time"

	"erp/internal/db"
	"erp/internal/platform/tenant"
)

var (
	enabled atomic.Bool
	baseURL atomic.Value // string
)

// Configure 啟動時呼叫一次:是否啟用通知,以及信件內連結的網址。
func Configure(on bool, url string) {
	enabled.Store(on)
	baseURL.Store(strings.TrimRight(url, "/"))
}

func Enabled() bool { return enabled.Load() }

// Link 組出系統內連結(未設定網址時只回傳路徑)。
func Link(path string) string {
	b, _ := baseURL.Load().(string)
	return b + path
}

// Enqueue 在交易內對指定使用者排入通知信;沒啟用通知、沒有收件人時什麼都不做。
func Enqueue(ctx context.Context, q *db.Queries, companyID int64, userIDs []int64, event, subject, body, dedupeKey string) error {
	if !Enabled() || len(userIDs) == 0 {
		return nil
	}
	_, err := q.EnqueueNotifications(ctx, db.EnqueueNotificationsParams{
		CompanyID: companyID, UserIds: userIDs, Event: event, Subject: subject, Body: body, DedupeKey: dedupeKey,
	})
	return err
}

// Sender 寄出一封信。
type Sender interface {
	Send(to, subject, body string) error
}

// SMTPSender 以 STARTTLS(587)或隱式 TLS(465)連線寄信,適用 Gmail 應用程式密碼。
type SMTPSender struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
}

func (s SMTPSender) Send(to, subject, body string) error {
	if strings.ContainsAny(to+subject+s.From, "\r\n") {
		return errors.New("信件標頭不可含換行")
	}
	msg := "From: " + s.From + "\r\nTo: " + to + "\r\nSubject: =?UTF-8?B?" + b64(subject) + "?=\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n\r\n" + wrap76(b64(body))
	addr := net.JoinHostPort(s.Host, fmt.Sprint(s.Port))
	auth := smtp.PlainAuth("", s.User, s.Password, s.Host)
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var c *smtp.Client
	if s.Port == 465 {
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.Host})
		if err != nil {
			return err
		}
		if c, err = smtp.NewClient(conn, s.Host); err != nil {
			return err
		}
	} else {
		conn, err := dialer.Dial("tcp", addr)
		if err != nil {
			return err
		}
		if c, err = smtp.NewClient(conn, s.Host); err != nil {
			return err
		}
		if err := c.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
			_ = c.Close()
			return err
		}
	}
	defer func() { _ = c.Close() }()
	_ = c.Hello("localhost")
	if err := c.Auth(auth); err != nil {
		return err
	}
	if err := c.Mail(s.From); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// Worker 背景寄信與定期產生通知。
type Worker struct {
	Q      *db.Queries
	Sender Sender
	// CheckDueDays 票據在幾天內到期就通知
	CheckDueDays int
	// Tenants 多租戶(D92):回傳要處理的租戶代碼,逐一以該租戶的 context 執行;nil = 單租戶(context 不帶租戶)
	Tenants func(ctx context.Context) ([]string, error)
}

// each 對每個租戶(單租戶只有一次)執行 fn;某個租戶失敗不影響其他租戶。
func (w *Worker) each(ctx context.Context, what string, fn func(ctx context.Context) error) {
	if w.Tenants == nil {
		if err := fn(ctx); err != nil && ctx.Err() == nil {
			slog.Warn(what+"失敗", "err", err)
		}
		return
	}
	codes, err := w.Tenants(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn(what+":取得租戶清單失敗", "err", err)
		}
		return
	}
	for _, code := range codes {
		if err := fn(tenant.With(ctx, code)); err != nil && ctx.Err() == nil {
			slog.Warn(what+"失敗", "tenant", code, "err", err)
		}
	}
}

// Flush 寄出一批到期的佇列,回傳成功寄出的封數。
func (w *Worker) Flush(ctx context.Context) (int, error) {
	rows, err := w.Q.ClaimNotifications(ctx, 20)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, n := range rows {
		if err := w.Sender.Send(n.ToEmail, n.Subject, n.Body); err != nil {
			slog.Warn("通知信寄送失敗", "id", n.ID, "attempts", n.Attempts, "err", err)
			if e := w.Q.MarkNotificationFailed(ctx, db.MarkNotificationFailedParams{ID: n.ID, LastError: err.Error()}); e != nil {
				return sent, e
			}
			continue
		}
		if err := w.Q.MarkNotificationSent(ctx, n.ID); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

// Scan 產生定期通知(目前:票據到期)並清除舊紀錄。
func (w *Worker) Scan(ctx context.Context) error {
	days := w.CheckDueDays
	if days <= 0 {
		days = 3
	}
	if _, err := w.Q.EnqueueCheckDueNotifications(ctx, db.EnqueueCheckDueNotificationsParams{Days: int32(days), BaseUrl: Link("")}); err != nil {
		return err
	}
	_, err := w.Q.PurgeOldNotifications(ctx)
	return err
}

// Run 每 30 秒寄信、每小時掃描一次,直到 ctx 結束。
func (w *Worker) Run(ctx context.Context) {
	flush := time.NewTicker(30 * time.Second)
	scan := time.NewTicker(time.Hour)
	defer flush.Stop()
	defer scan.Stop()
	w.each(ctx, "通知掃描", w.Scan)
	for {
		select {
		case <-ctx.Done():
			return
		case <-flush.C:
			w.each(ctx, "通知寄送", func(ctx context.Context) error { _, err := w.Flush(ctx); return err })
		case <-scan.C:
			w.each(ctx, "通知掃描", w.Scan)
		}
	}
}
