package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"erp/internal/shared/authctx"
)

func TestAllowBurstAndRefill(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := New(60, 2) // 每秒補 1 次
	l.now = func() time.Time { return now }

	for i := range 2 {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatalf("第 %d 次應允許", i+1)
		}
	}
	ok, wait := l.Allow("k")
	if ok || wait <= 0 || wait > time.Second {
		t.Fatalf("超過 burst 應拒絕並回傳等待時間,got ok=%v wait=%v", ok, wait)
	}
	// 被拒絕的請求不應消耗額度:等 1 秒後應恰好可再一次
	now = now.Add(time.Second)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("補充後應允許")
	}
	if ok, _ := l.Allow("other"); !ok {
		t.Fatal("不同 key 應各自計算")
	}
}

func TestIdleEntriesPruned(t *testing.T) {
	now := time.Now()
	l := New(60, 1)
	l.now = func() time.Time { return now }
	l.Allow("old")
	now = now.Add(idleTTL + time.Minute)
	for i := range 999 {
		l.Allow("k" + string(rune('a'+i%26)))
	}
	if _, ok := l.entries["old"]; ok {
		t.Fatal("閒置過久的 key 應被清除")
	}
}

func TestIPKeyGroupsIPv6By64(t *testing.T) {
	cases := map[string]string{
		"203.0.113.7":             "203.0.113.7",
		"::ffff:203.0.113.7":      "203.0.113.7", // IPv4-mapped 視同 IPv4
		"2001:db8:1:2:aaaa::1":    "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff::9999": "2001:db8:1:2::/64",
		"2001:db8:1:3::1":         "2001:db8:1:3::/64",
		"not-an-ip":               "not-an-ip",
	}
	for in, want := range cases {
		if got := ipKey(in); got != want {
			t.Errorf("ipKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEntriesCappedUnderPressure(t *testing.T) {
	now := time.Now()
	l := New(60, 1)
	l.now = func() time.Time { return now }
	for i := range maxEntries {
		l.entries[strconv.Itoa(i)] = &entry{lim: rate.NewLimiter(1, 1), lastSeen: now.Add(-time.Minute)}
	}
	l.Allow("new")
	if len(l.entries) != 1 {
		t.Fatalf("超過上限時應清除閒置的 key,剩 %d", len(l.entries))
	}
}

func TestRetryAfterHeader(t *testing.T) {
	l := New(6, 1) // 每 10 秒補 1 次
	r := newRouter(l, nil)
	get(r, "10.0.0.1:1")
	w := get(r, "10.0.0.1:1")
	if got := w.Header().Get("Retry-After"); got != "10" {
		t.Fatalf("Retry-After = %q, want 10", got)
	}
}

func newRouter(l *Limiter, actor *authctx.Actor) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if actor != nil {
			c.Request = c.Request.WithContext(authctx.WithActor(c.Request.Context(), actor))
		}
	}, Middleware(l))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func get(r *gin.Engine, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = remote
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMiddlewareKeysByUserWhenLoggedIn(t *testing.T) {
	l := New(1, 1)
	alice := newRouter(l, &authctx.Actor{UserID: 1})
	bob := newRouter(l, &authctx.Actor{UserID: 2})

	if w := get(alice, "10.0.0.1:1"); w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	// 同一使用者換 IP 仍共用額度
	w := get(alice, "10.0.0.2:1")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("同一使用者應被限制並帶 Retry-After,got %d %v", w.Code, w.Header())
	}
	// 同一 IP 的其他使用者不受影響
	if w := get(bob, "10.0.0.1:1"); w.Code != http.StatusOK {
		t.Fatalf("其他使用者不應受影響,got %d", w.Code)
	}
}

func TestMiddlewareKeysByIPWhenAnonymous(t *testing.T) {
	l := New(1, 1)
	r := newRouter(l, nil)
	if w := get(r, "10.0.0.1:1"); w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	if w := get(r, "10.0.0.1:2"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("同一 IP 應被限制,got %d", w.Code)
	}
	if w := get(r, "10.0.0.9:1"); w.Code != http.StatusOK {
		t.Fatalf("不同 IP 不應受影響,got %d", w.Code)
	}
}
