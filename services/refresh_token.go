package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"tablescore-api/models"

	"gorm.io/gorm"
)

// ErrRefreshTokenReused signals that a refresh token was presented after it had
// already been rotated away, which means the raw token leaked.
var ErrRefreshTokenReused = errors.New("refresh token reuse detected")

// ErrRefreshTokenInvalid signals that a refresh token is unknown, expired, or was
// rotated away so recently that reuse cannot be distinguished from a benign retry.
var ErrRefreshTokenInvalid = errors.New("invalid or expired refresh token")

// refreshReuseGrace is how long a freshly revoked refresh token is treated as merely
// invalid rather than as reuse. Browsers share one cookie jar across tabs, so several
// tabs can fire /api/auth/refresh with the same raw token at once; the losers of that
// race arrive just after rotation and must not revoke the whole session.
const refreshReuseGrace = 30 * time.Second

// HashToken returns the hex SHA-256 of a raw bearer token (refresh token or
// email token). Only the hash is ever stored; the input already carries 256
// bits of entropy, so a slow hash would add latency without security.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// newRawToken returns 32 random bytes as unpadded base64url, safe to place in
// a cookie or a URL query string.
func newRawToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CreateRefreshToken creates a new refresh token for a user and returns the raw
// token. Only the hash of the token is persisted.
func CreateRefreshToken(db *gorm.DB, userID string, expiry time.Duration) (string, error) {
	token, err := newRawToken()
	if err != nil {
		return "", err
	}

	rt := models.RefreshToken{
		UserID:    userID,
		Token:     HashToken(token),
		ExpiresAt: time.Now().Add(expiry),
		Revoked:   false,
	}
	if err := db.Create(&rt).Error; err != nil {
		return "", fmt.Errorf("failed to save refresh token: %w", err)
	}

	return token, nil
}

// classifyRefreshToken decides the fate of a stored refresh token row. It returns nil
// when the token may be used, ErrRefreshTokenInvalid when it is expired or was revoked
// within refreshReuseGrace, and ErrRefreshTokenReused when it was revoked earlier than
// that — which means the raw token was replayed after rotation.
func classifyRefreshToken(rt *models.RefreshToken, now time.Time) error {
	if rt.Revoked {
		if now.Sub(rt.UpdatedAt) < refreshReuseGrace {
			return ErrRefreshTokenInvalid
		}
		return ErrRefreshTokenReused
	}
	if now.After(rt.ExpiresAt) {
		return ErrRefreshTokenInvalid
	}
	return nil
}

// ValidateRefreshToken checks if a raw refresh token is valid (exists, not revoked, not expired).
// A hash that matches a row revoked longer ago than refreshReuseGrace means the token was
// replayed after rotation: every refresh token for that user is revoked and
// ErrRefreshTokenReused is returned.
func ValidateRefreshToken(db *gorm.DB, raw string) (*models.RefreshToken, error) {
	var rt models.RefreshToken
	if err := db.Where("token = ?", HashToken(raw)).First(&rt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRefreshTokenInvalid
		}
		return nil, fmt.Errorf("refresh token lookup failed: %w", err)
	}

	switch err := classifyRefreshToken(&rt, time.Now()); {
	case errors.Is(err, ErrRefreshTokenReused):
		if err := RevokeAllUserRefreshTokens(db, rt.UserID); err != nil {
			slog.Error("failed to revoke user refresh tokens after reuse", "user_id", rt.UserID, "error", err)
		}
		return nil, ErrRefreshTokenReused
	case err != nil:
		return nil, err
	}

	return &rt, nil
}

// RevokeRefreshToken marks the refresh token matching the raw value as revoked.
// It returns ErrRefreshTokenInvalid when no row matched, so callers that rotate a
// token can tell whether they really won the race.
func RevokeRefreshToken(db *gorm.DB, raw string) error {
	result := db.Model(&models.RefreshToken{}).Where("token = ? AND revoked = ?", HashToken(raw), false).Update("revoked", true)
	if result.Error != nil {
		return fmt.Errorf("failed to revoke refresh token: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRefreshTokenInvalid
	}
	return nil
}

// RevokeAllUserRefreshTokens revokes every active refresh token belonging to a user.
func RevokeAllUserRefreshTokens(db *gorm.DB, userID string) error {
	if err := db.Exec("DELETE FROM auth_sessions WHERE user_id = ?", userID).Error; err != nil {
		return err
	}
	result := db.Model(&models.RefreshToken{}).Where("user_id = ? AND revoked = ?", userID, false).Update("revoked", true)
	if result.Error != nil {
		return fmt.Errorf("failed to revoke user refresh tokens: %w", result.Error)
	}
	return nil
}
