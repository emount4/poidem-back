package ratelimit

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/gin-gonic/gin"
)

type entry struct {
	count int
	reset time.Time
}

// Limiter is a small in-memory fixed-window limiter intended for a single local API instance.
type Limiter struct {
	mu     sync.Mutex
	items  map[string]entry
	limit  int
	window time.Duration
	now    func() time.Time
}

func New(limit int, window time.Duration) *Limiter {
	return &Limiter{items: make(map[string]entry), limit: limit, window: window, now: time.Now}
}

func (l *Limiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		now := l.now()
		key := c.ClientIP()
		if principal, ok := account.PrincipalFromContext(c.Request.Context()); ok {
			key = "user:" + strconv.FormatInt(principal.UserID, 10)
		}
		allowed, retryAfter := l.allow(key, now)
		if !allowed {
			seconds := int((retryAfter + time.Second - 1) / time.Second)
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", strconv.Itoa(seconds))
			apierr.Write(c, http.StatusTooManyRequests, "RATE_LIMITED", "Слишком много запросов, повторите позже", nil)
			return
		}
		c.Next()
	}
}

func (l *Limiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	current, ok := l.items[key]
	if !ok || !now.Before(current.reset) {
		l.items[key] = entry{count: 1, reset: now.Add(l.window)}
		if len(l.items) > 10_000 {
			for itemKey, item := range l.items {
				if !now.Before(item.reset) {
					delete(l.items, itemKey)
				}
			}
		}
		return true, 0
	}
	if current.count >= l.limit {
		return false, current.reset.Sub(now)
	}
	current.count++
	l.items[key] = current
	return true, 0
}
