package middleware

import (
	"errors"
	"log/slog"
	"net/http"

	"tablescore-api/auth"
	"tablescore-api/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RequireAdmin rejects requests whose access token does not carry the admin
// role, then re-confirms the role against the live user row. The JWT is valid
// for 15 minutes, so a demoted or deleted admin would otherwise keep admin API
// access until it expires.
func RequireAdmin(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := IdentityFrom(c)
		if !ok || id.Role != auth.RoleAdmin {
			abortError(c, http.StatusForbidden, "admin access required")
			return
		}

		current, err := services.GetUserRole(db, id.UserID)
		if err != nil && !errors.Is(err, services.ErrUserNotFound) {
			// A failed lookup is not a missing admin — it still denies access,
			// but it must be visible rather than indistinguishable from a demote.
			slog.Error("admin role check failed", "user_id", id.UserID, "error", err)
		}
		if err != nil || current != auth.RoleAdmin {
			abortError(c, http.StatusForbidden, "admin access required")
			return
		}

		c.Next()
	}
}
