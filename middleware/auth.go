package middleware

import (
	"log/slog"
	"net/http"

	"tablescore-api/auth"

	"github.com/gin-gonic/gin"
)

// RequireAuth validates the access token (cookie, or Bearer header as a
// fallback) and stores the caller as an Identity in the Gin context.
func RequireAuth(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := auth.AccessTokenFromRequest(c)
		if tokenString == "" {
			abortError(c, http.StatusUnauthorized, "missing access token")
			return
		}

		claims, err := auth.ParseAccessToken(tokenString, jwtSecret)
		if err != nil {
			slog.Debug("invalid access token", "error", err)
			abortError(c, http.StatusUnauthorized, "invalid or expired access token")
			return
		}

		userID, err := auth.ParseUserID(claims.UserID)
		if err != nil {
			abortError(c, http.StatusUnauthorized, "invalid token subject")
			return
		}
		setIdentity(c, Identity{UserID: userID, Email: claims.Email, Role: claims.Role})
		c.Next()
	}
}
