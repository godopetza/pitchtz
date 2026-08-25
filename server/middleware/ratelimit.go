package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/godopetza/pitchtz/utils"
	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type IPRateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     rate.Limit
	burst    int
	ttl      time.Duration
}

func NewIPRateLimiter(requestsPerMinute, burst int) *IPRateLimiter {
	if requestsPerMinute < 1 {
		requestsPerMinute = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &IPRateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate.Limit(float64(requestsPerMinute) / 60.0),
		burst:    burst,
		ttl:      30 * time.Minute,
	}
}

func (l *IPRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.allow(c.ClientIP(), time.Now()) {
			utils.RespondError(c, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (l *IPRateLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	for key, item := range l.visitors {
		if now.Sub(item.lastSeen) > l.ttl {
			delete(l.visitors, key)
		}
	}
	item, ok := l.visitors[ip]
	if !ok {
		item = &visitor{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.visitors[ip] = item
	}
	item.lastSeen = now
	return item.limiter.Allow()
}
