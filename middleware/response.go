package middleware

import "github.com/gin-gonic/gin"

// abortError ends the request with the API's standard error body. Middleware
// cannot import handlers (handlers import middleware), so the shape is
// repeated here once rather than at every call site.
func abortError(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": msg})
}
