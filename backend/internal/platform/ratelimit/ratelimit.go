// Package ratelimit 提供限流中介層:已登入以使用者計算,未登入以來源 IP 計算。
// 記憶體內實作(token bucket),適用單一 api 實例;多實例時需改用共享儲存。
package ratelimit

import (
	"math"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/response"
)

const (
	// idleTTL 超過此時間沒有請求的 key 會被清除,避免記憶體無限成長。
	idleTTL = 10 * time.Minute
	// maxEntries key 數量上限;超過時提早清除閒置較久的 key(防止大量不同 IP 灌爆記憶體)。
	maxEntries = 100_000
	// pressureIdle 超過上限時,閒置超過此時間的 key 即清除。
	pressureIdle = 30 * time.Second
)

type Limiter struct {
	mu      sync.Mutex
	entries map[string]*entry
	limit   rate.Limit
	burst   int
	calls   int
	now     func() time.Time
}

type entry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// New 每個 key 每分鐘平均 perMinute 次,瞬間最多 burst 次。
func New(perMinute, burst int) *Limiter {
	return &Limiter{
		entries: map[string]*entry{},
		limit:   rate.Limit(float64(perMinute) / 60),
		burst:   max(burst, 1),
		now:     time.Now,
	}
}

// Allow 回傳是否允許;不允許時一併回傳建議等待時間。
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.calls++
	if l.calls%1000 == 0 {
		l.prune(now, idleTTL)
	}
	e, ok := l.entries[key]
	if !ok {
		if len(l.entries) >= maxEntries {
			l.prune(now, pressureIdle)
		}
		e = &entry{lim: rate.NewLimiter(l.limit, l.burst)}
		l.entries[key] = e
	}
	e.lastSeen = now

	r := e.lim.ReserveN(now, 1)
	if delay := r.DelayFrom(now); delay > 0 {
		r.CancelAt(now) // 不允許就不消耗額度
		return false, delay
	}
	return true, 0
}

func (l *Limiter) prune(now time.Time, idle time.Duration) {
	for k, e := range l.entries {
		if now.Sub(e.lastSeen) > idle {
			delete(l.entries, k)
		}
	}
}

// Key 已登入回傳 "user:<id>",否則回傳 "ip:<來源 IP>"。
// 來源 IP 依 TRUSTED_PROXIES 決定是否採用 X-Forwarded-For。
func Key(c *gin.Context) string {
	if a := authctx.ActorFrom(c.Request.Context()); a != nil {
		return "user:" + strconv.FormatInt(a.UserID, 10)
	}
	return "ip:" + ipKey(c.ClientIP())
}

// ipKey IPv6 以 /64 網段計算:一般用戶可任意使用同一 /64 內的位址,逐一計算等於沒有限制。
func ipKey(s string) string {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return s
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	p, err := addr.Prefix(64)
	if err != nil {
		return s
	}
	return p.String()
}

// Middleware 超過限制時回 429 與 Retry-After(秒)。
// 掛在驗證中介層之後才會以使用者計算;之前或公開路由以 IP 計算。
func Middleware(l *Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, wait := l.Allow(Key(c))
		if !ok {
			c.Header("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
			response.Error(c, apperr.ErrTooManyRequests)
			return
		}
		c.Next()
	}
}
