package middleware

import (
	"github.com/gin-gonic/gin"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

func RateLimit(maxRequests int, window time.Duration) gin.HandlerFunc {
	return rateLimit(maxRequests, window, time.Now)
}

type visitor struct {
	count   int
	resetAt time.Time
}

// rateLimit shares a fixed per-IP window across login and signup. Cleanup runs
// on traffic, so constructing a router does not leave a background goroutine.
func rateLimit(maxRequests int, window time.Duration, clock func() time.Time) gin.HandlerFunc {
	var mu sync.Mutex
	visitors := make(map[string]*visitor)
	nextCleanup := clock().Add(window)
	return func(c *gin.Context) {
		ip := c.ClientIP()
		mu.Lock()
		now := clock()
		if !now.Before(nextCleanup) {
			for key, v := range visitors {
				if !now.Before(v.resetAt) {
					delete(visitors, key)
				}
			}
			nextCleanup = now.Add(window)
		}
		v, ok := visitors[ip]
		if !ok || !now.Before(v.resetAt) {
			v = &visitor{resetAt: now.Add(window)}
			visitors[ip] = v
		}
		if v.count >= maxRequests {
			retry := int64(math.Ceil(v.resetAt.Sub(now).Seconds()))
			mu.Unlock()
			c.Header("Retry-After", strconv.FormatInt(retry, 10))
			c.Abort()
			abortError(c, http.StatusTooManyRequests, "too many requests, please try again later")
			return
		}
		v.count++
		mu.Unlock()
		c.Next()
	}
}
