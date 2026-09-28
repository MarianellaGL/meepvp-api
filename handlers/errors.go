package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"tablescore-api/mailer"
	"tablescore-api/services"

	"github.com/gin-gonic/gin"
)

// serviceStatus maps a service sentinel error to the status and message the
// API returns for it. Anything else is an internal failure: 500
// with fallback as the message, so no wrapped detail reaches the client.
func serviceStatus(err error, fallback string) (int, string) {
	switch {
	case errors.Is(err, mailer.ErrDisabled):
		return http.StatusServiceUnavailable, "email delivery is disabled"
	case errors.Is(err, services.ErrUserNotFound):
		return http.StatusNotFound, "user not found"
	case errors.Is(err, services.ErrEmailTaken):
		return http.StatusConflict, "email already in use"
	case errors.Is(err, services.ErrNoChanges):
		return http.StatusBadRequest, "no fields to update"
	case errors.Is(err, services.ErrSettingNotFound):
		return http.StatusNotFound, "setting not found"
	case errors.Is(err, services.ErrRefreshTokenReused):
		return http.StatusUnauthorized, "session revoked"
	case errors.Is(err, services.ErrRefreshTokenInvalid):
		return http.StatusUnauthorized, "invalid or expired refresh token"
	case errors.Is(err, services.ErrEmailTokenInvalid):
		return http.StatusBadRequest, "invalid or expired token"
	case errors.Is(err, services.ErrAlreadyVerified):
		return http.StatusBadRequest, "email already verified"
	case errors.Is(err, services.ErrResendTooSoon):
		return http.StatusTooManyRequests, "verification email sent recently, try again in a minute"
	}
	return http.StatusInternalServerError, fallback
}

// serviceError writes the response for a failed service call and logs the
// underlying error when it was not an expected sentinel.
func serviceError(c *gin.Context, err error, fallback string) {
	status, msg := serviceStatus(err, fallback)
	if status == http.StatusInternalServerError {
		slog.Error(fallback, "error", err)
	}
	ErrorJSON(c, status, msg)
}
