package handlers

import (
	"log/slog"
	"net/http"

	"tablescore-api/auth"
	"tablescore-api/mailer"
	"tablescore-api/middleware"
	"tablescore-api/services"

	"github.com/gin-gonic/gin"
)

// forgotPasswordMessage is returned whether or not the address exists, so the
// endpoint cannot be used to enumerate accounts.
const forgotPasswordMessage = "if an account exists for that email, a reset link has been sent"

// VerifyEmail consumes a verification token from an emailed link.
func (h *Handlers) VerifyEmail(c *gin.Context) {
	var req VerifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorJSON(c, http.StatusBadRequest, err.Error())
		return
	}

	if _, err := services.VerifyEmail(h.DB, req.Token); err != nil {
		serviceError(c, err, "failed to verify email")
		return
	}

	c.JSON(http.StatusOK, MessageResponse{Message: "email verified"})
}

// ResendVerification emails a fresh verification link to the caller.
func (h *Handlers) ResendVerification(c *gin.Context) {
	id, ok := middleware.IdentityFrom(c)
	if !ok {
		ErrorJSON(c, http.StatusUnauthorized, "not authenticated")
		return
	}
	if !h.Cfg.EmailEnabled() {
		serviceError(c, mailer.ErrDisabled, "failed to send verification email")
		return
	}

	user, err := services.GetUserByID(h.DB, id.UserID)
	if err != nil {
		serviceError(c, err, "failed to load user")
		return
	}

	if err := services.SendVerificationEmail(c.Request.Context(), h.DB, h.Mailer, h.Cfg, user); err != nil {
		serviceError(c, err, "failed to send verification email")
		return
	}

	c.JSON(http.StatusOK, MessageResponse{Message: "verification email sent"})
}

// ForgotPassword emails a reset link when the address has a password login.
// The response is the same in every case; a failure is logged, not returned,
// because a 500 would reveal that the account exists.
func (h *Handlers) ForgotPassword(c *gin.Context) {
	if !h.Cfg.EmailEnabled() {
		serviceError(c, mailer.ErrDisabled, "failed to process password reset request")
		return
	}
	var req ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorJSON(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := services.RequestPasswordReset(c.Request.Context(), h.DB, h.Mailer, h.Cfg, req.Email); err != nil {
		slog.Error("failed to process password reset request", "error", err)
	}

	c.JSON(http.StatusOK, MessageResponse{Message: forgotPasswordMessage})
}

// ResetPassword sets a new password from an emailed reset link. No session is
// issued; the client signs in with the new password.
func (h *Handlers) ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorJSON(c, http.StatusBadRequest, err.Error())
		return
	}

	if msg := auth.ValidatePasswordComplexity(req.Password); msg != "" {
		ErrorJSON(c, http.StatusBadRequest, msg)
		return
	}

	user, err := services.ResetPassword(h.DB, req.Token, req.Password)
	if err != nil {
		serviceError(c, err, "failed to reset password")
		return
	}

	slog.Info("password reset completed", "user_id", user.ID)
	c.JSON(http.StatusOK, MessageResponse{Message: "password updated"})
}
