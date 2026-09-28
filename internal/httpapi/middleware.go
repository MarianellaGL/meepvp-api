package httpapi

import (
	"crypto/rand"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Request-ID", rand.Text())
		c.Next()
	}
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		level := slog.LevelInfo
		if c.Writer.Status() >= 500 {
			level = slog.LevelError
		} else if c.Writer.Status() >= 400 {
			level = slog.LevelWarn
		}
		// Log the route template, excluding query strings, bodies and auth tokens.
		slog.Log(c.Request.Context(), level, "request completed",
			"method", c.Request.Method, "route", c.FullPath(),
			"status", c.Writer.Status(), "latency", time.Since(start),
			"ip", c.ClientIP(), "request_id", c.Writer.Header().Get("X-Request-ID"))
	}
}

func recovery() gin.HandlerFunc {
	// Avoid Gin's default request dump, which can include private table tokens.
	return gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, _ any) {
		slog.ErrorContext(c.Request.Context(), "request panic", "request_id", c.Writer.Header().Get("X-Request-ID"))
		c.Abort()
		if !c.Writer.Written() {
			writeError(c.Writer, http.StatusInternalServerError, "internal server error")
		}
	})
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Content-Type, X-Table-Token, Authorization")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, OPTIONS")
		c.Header("Access-Control-Expose-Headers", "X-Request-ID, Retry-After")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
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
			writeError(c.Writer, http.StatusTooManyRequests, "too many requests, please try again later")
			return
		}
		v.count++
		mu.Unlock()
		c.Next()
	}
}
