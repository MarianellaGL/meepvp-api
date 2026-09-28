package handlers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"tablescore-api/auth"
	"tablescore-api/middleware"
	"tablescore-api/models"
	"tablescore-api/services"

	"github.com/gin-gonic/gin"
	"github.com/markbates/goth/gothic"
)

// Register handles new user registration with email/password.
func (h *Handlers) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorJSON(c, http.StatusBadRequest, err.Error())
		return
	}

	if msg := auth.ValidatePasswordComplexity(req.Password); msg != "" {
		ErrorJSON(c, http.StatusBadRequest, msg)
		return
	}

	_, err := services.GetUserByEmail(h.DB, req.Email)
	if err == nil {
		ErrorJSON(c, http.StatusConflict, "user with this email already exists")
		return
	}

	user, err := services.CreateEmailUser(h.DB, req.Email, req.Name, req.Password)
	if err != nil {
		serviceError(c, err, "failed to create user")
		return
	}

	// The account exists whether or not the email goes out; the user can resend
	// from the banner, so a mail failure is logged rather than returned.
	if h.Cfg.EmailEnabled() {
		if err := services.SendVerificationEmail(c.Request.Context(), h.DB, h.Mailer, h.Cfg, user); err != nil {
			slog.Error("failed to send verification email", "user_id", user.ID, "error", err)
		}
	}

	if err := h.issueSession(c, user); err != nil {
		slog.Error("failed to issue session", "error", err)
		ErrorJSON(c, http.StatusInternalServerError, "failed to create session")
		return
	}

	c.JSON(http.StatusCreated, toUserResponse(user))
}

// Login handles email/password authentication.
func (h *Handlers) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorJSON(c, http.StatusBadRequest, err.Error())
		return
	}

	user, ap, err := services.FindEmailUser(h.DB, req.Email)
	if err != nil {
		ErrorJSON(c, http.StatusUnauthorized, "invalid email or password")
		return
	}

	valid, err := auth.VerifyPassword(req.Password, ap.PasswordHash)
	if err != nil || !valid {
		ErrorJSON(c, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if err := h.issueSession(c, user); err != nil {
		slog.Error("failed to issue session", "error", err)
		ErrorJSON(c, http.StatusInternalServerError, "failed to create session")
		return
	}

	c.JSON(http.StatusOK, toUserResponse(user))
}

// OAuthBegin initiates the OAuth flow for a given provider.
func (h *Handlers) OAuthBegin(c *gin.Context) {
	provider := c.Param("provider")

	if !h.Settings.ProviderEnabled(provider) {
		ErrorJSON(c, http.StatusForbidden, provider+" authentication is currently disabled")
		return
	}

	q := c.Request.URL.Query()
	q.Set("provider", provider)
	c.Request.URL.RawQuery = q.Encode()

	gothic.BeginAuthHandler(c.Writer, c.Request)
}

// OAuthCallback handles the OAuth callback from a provider.
func (h *Handlers) OAuthCallback(c *gin.Context) {
	provider := c.Param("provider")
	q := c.Request.URL.Query()
	q.Set("provider", provider)
	c.Request.URL.RawQuery = q.Encode()

	gothUser, err := gothic.CompleteUserAuth(c.Writer, c.Request)
	if err != nil {
		slog.Error("oauth callback failed", "provider", provider, "error", err)
		ErrorJSON(c, http.StatusUnauthorized, "authentication failed")
		return
	}

	email := gothUser.Email
	name := gothUser.Name
	if name == "" {
		name = gothUser.NickName
	}

	user, err := services.UpsertSocialUser(h.DB, email, name, provider, gothUser.UserID)
	if err != nil {
		slog.Error("failed to upsert social user", "error", err)
		ErrorJSON(c, http.StatusInternalServerError, "failed to process user")
		return
	}

	if err := h.issueSession(c, user); err != nil {
		slog.Error("failed to issue session", "error", err)
		ErrorJSON(c, http.StatusInternalServerError, "failed to create session")
		return
	}

	c.Redirect(http.StatusTemporaryRedirect, "/api/auth/me")
}

// GetCurrentUser returns the currently authenticated user.
func (h *Handlers) GetCurrentUser(c *gin.Context) {
	id, ok := middleware.IdentityFrom(c)
	if !ok {
		ErrorJSON(c, http.StatusUnauthorized, "not authenticated")
		return
	}

	user, err := services.GetUserByID(h.DB, id.UserID)
	if err != nil {
		ErrorJSON(c, http.StatusNotFound, "user not found")
		return
	}

	c.JSON(http.StatusOK, toUserResponse(user))
}

// RefreshToken validates the refresh token, rotates it, and issues new tokens.
func (h *Handlers) RefreshToken(c *gin.Context) {
	tokenStr, err := c.Cookie(auth.CookieRefreshToken)
	if err != nil || tokenStr == "" {
		ErrorJSON(c, http.StatusUnauthorized, "missing refresh token")
		return
	}

	rt, err := services.ValidateRefreshToken(h.DB, tokenStr)
	if err != nil {
		if errors.Is(err, services.ErrRefreshTokenReused) {
			slog.Warn("refresh token reuse detected", "ip", c.ClientIP())
		}
		if errors.Is(err, services.ErrRefreshTokenReused) {
			auth.ClearSessionCookies(c, h.Cfg.IsProd())
		}
		serviceError(c, err, "failed to validate session")
		return
	}

	// Rotate: the old token must be revoked before a new session is issued.
	if err := services.RevokeRefreshToken(h.DB, tokenStr); err != nil {
		if errors.Is(err, services.ErrRefreshTokenInvalid) {
			ErrorJSON(c, http.StatusUnauthorized, "invalid or expired refresh token")
			return
		}
		slog.Error("failed to revoke old refresh token", "error", err)
		ErrorJSON(c, http.StatusInternalServerError, "failed to refresh session")
		return
	}

	user, err := services.GetUserByID(h.DB, rt.UserID)
	if err != nil {
		if errors.Is(err, services.ErrUserNotFound) {
			auth.ClearSessionCookies(c, h.Cfg.IsProd())
			ErrorJSON(c, http.StatusUnauthorized, "user not found")
			return
		}
		slog.Error("failed to load user on refresh", "error", err)
		ErrorJSON(c, http.StatusInternalServerError, "failed to refresh session")
		return
	}

	if err := h.issueSession(c, user); err != nil {
		// The old refresh token is already revoked; clear the cookies so the client
		// cannot replay it and trip reuse detection.
		auth.ClearSessionCookies(c, h.Cfg.IsProd())
		slog.Error("failed to issue session on refresh", "error", err)
		ErrorJSON(c, http.StatusInternalServerError, "failed to refresh session")
		return
	}

	c.JSON(http.StatusOK, MessageResponse{Message: "tokens refreshed"})
}

// Logout revokes the refresh token and clears auth cookies.
func (h *Handlers) Logout(c *gin.Context) {
	tokenStr, err := c.Cookie(auth.CookieRefreshToken)
	if err == nil && tokenStr != "" {
		// A stale or already-revoked cookie is the normal end of a session, not
		// an operational problem — only a real failure deserves an error line.
		if err := services.RevokeRefreshToken(h.DB, tokenStr); errors.Is(err, services.ErrRefreshTokenInvalid) {
			slog.Debug("refresh token already invalid on logout", "error", err)
		} else if err != nil {
			slog.Error("failed to revoke refresh token on logout", "error", err)
		}
	}

	auth.ClearSessionCookies(c, h.Cfg.IsProd())

	c.JSON(http.StatusOK, MessageResponse{Message: "logged out"})
}

// issueSession signs an access token, stores a new refresh token and sets
// both cookies.
func (h *Handlers) issueSession(c *gin.Context, user *models.User) error {
	accessToken, err := auth.SignAccessToken(
		auth.Claims{UserID: user.ID, Email: user.Email, Role: user.Role},
		h.Cfg.Auth.JWTSecret,
		h.Cfg.AccessTokenTTL(),
	)
	if err != nil {
		return fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err := services.CreateRefreshToken(h.DB, user.ID, h.Cfg.RefreshTokenTTL())
	if err != nil {
		return fmt.Errorf("failed to create refresh token: %w", err)
	}

	auth.SetSessionCookies(c, accessToken, h.Cfg.AccessTokenTTL(), refreshToken, h.Cfg.RefreshTokenTTL(), h.Cfg.IsProd())
	return nil
}
