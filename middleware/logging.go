package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestID adds a unique request ID to each request context and response header.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := uuid.NewString()
		c.Set(ctxRequestID, id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

// RequestLogger logs each request with structured fields using slog.
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		c.Next()

		status := c.Writer.Status()
		latency := time.Since(start)
		requestID, _ := c.Get(ctxRequestID)

		attrs := []any{
			"method", method,
			"path", path,
			"status", status,
			"latency", latency,
			"ip", c.ClientIP(),
		}
		if rid, ok := requestID.(string); ok && rid != "" {
			attrs = append(attrs, "request_id", rid)
		}

		if status >= 500 {
			slog.Error("request completed", attrs...)
		} else if status >= 400 {
			slog.Warn("request completed", attrs...)
		} else {
			slog.Info("request completed", attrs...)
		}
	}
}
