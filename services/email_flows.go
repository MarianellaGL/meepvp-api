package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"tablescore-api/auth"
	"tablescore-api/config"
	"tablescore-api/mailer"
	"tablescore-api/models"

	"gorm.io/gorm"
)

// Lifetimes for the two email flows and the per-user resend cooldown.
const (
	verificationTokenTTL = 24 * time.Hour
	passwordResetTTL     = time.Hour
	resendCooldown       = time.Minute
)

var (
	// ErrAlreadyVerified is returned when a verification email is requested
	// for an address that is already confirmed.
	ErrAlreadyVerified = errors.New("email already verified")
	// ErrResendTooSoon is returned when a verification email was sent less
	// than resendCooldown ago.
	ErrResendTooSoon = errors.New("verification email sent recently")
)

// SendVerificationEmail issues a verification token for user and emails the
// link. It refuses when the address is already verified or when a link went
// out within the cooldown, so the endpoint cannot be used to flood a mailbox.
func SendVerificationEmail(ctx context.Context, db *gorm.DB, m mailer.Mailer, cfg *config.Config, user *models.User) error {
	if !cfg.EmailEnabled() {
		return mailer.ErrDisabled
	}
	if user.EmailVerifiedAt != nil {
		return ErrAlreadyVerified
	}
	var last models.EmailToken
	err := db.Where("user_id = ? AND purpose = ?", user.ID, TokenPurposeVerify).
		Order("created_at desc").First(&last).Error
	switch {
	case err == nil:
		if time.Since(last.CreatedAt) < resendCooldown {
			return ErrResendTooSoon
		}
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return fmt.Errorf("failed to look up verification tokens: %w", err)
	}

	raw, err := IssueEmailToken(db, user.ID, TokenPurposeVerify, verificationTokenTTL)
	if err != nil {
		return err
	}
	msg, err := mailer.VerificationMessage(cfg.FromName(), user.Email, user.Name, cfg.AbsoluteURL("/verify-email?token="+raw))
	if err != nil {
		return err
	}
	return m.Send(ctx, msg)
}

// VerifyEmail consumes a verification token and marks the user's address
// confirmed, in one transaction. Returns the updated user.
func VerifyEmail(db *gorm.DB, raw string) (*models.User, error) {
	var user models.User
	err := db.Transaction(func(tx *gorm.DB) error {
		tok, err := ConsumeEmailToken(tx, raw, TokenPurposeVerify)
		if err != nil {
			return err
		}
		// GORM's default scope hides soft-deleted users, so a token for a
		// deleted account updates nothing and is reported invalid.
		result := tx.Model(&models.User{}).Where("id = ?", tok.UserID).Update("email_verified_at", time.Now())
		if result.Error != nil {
			return fmt.Errorf("failed to mark email verified: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrEmailTokenInvalid
		}
		return tx.First(&user, "id = ?", tok.UserID).Error
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// RequestPasswordReset emails a reset link to the account behind email, if
// there is one with a password. Unknown addresses and social-only accounts
// are a silent no-op: the caller must not be able to tell, so the endpoint
// cannot be used to enumerate accounts.
func RequestPasswordReset(ctx context.Context, db *gorm.DB, m mailer.Mailer, cfg *config.Config, email string) error {
	if !cfg.EmailEnabled() {
		return mailer.ErrDisabled
	}
	user, _, err := FindEmailUser(db, email)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			slog.Debug("password reset requested for unknown or passwordless email")
			return nil
		}
		return fmt.Errorf("failed to look up user for password reset: %w", err)
	}

	raw, err := IssueEmailToken(db, user.ID, TokenPurposeReset, passwordResetTTL)
	if err != nil {
		return err
	}
	msg, err := mailer.PasswordResetMessage(cfg.FromName(), user.Email, user.Name, cfg.AbsoluteURL("/reset-password?token="+raw))
	if err != nil {
		return err
	}
	return m.Send(ctx, msg)
}

// ResetPassword consumes a reset token, stores the new password hash, revokes
// every refresh token for the user (any stolen session dies with the old
// password) and marks the address verified, all in one transaction. The
// caller validates password complexity first.
func ResetPassword(db *gorm.DB, raw, newPassword string) (*models.User, error) {
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return nil, err
	}
	var user models.User
	err = db.Transaction(func(tx *gorm.DB) error {
		tok, err := ConsumeEmailToken(tx, raw, TokenPurposeReset)
		if err != nil {
			return err
		}
		// Look the user up first: a token for a user who was soft-deleted after
		// the link was issued must be indistinguishable from an invalid token,
		// not surface as a different error partway through the update.
		if err := tx.First(&user, "id = ?", tok.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrEmailTokenInvalid
			}
			return fmt.Errorf("failed to load user: %w", err)
		}
		result := tx.Model(&models.AuthProvider{}).
			Where("user_id = ? AND provider = ?", tok.UserID, auth.ProviderEmail).
			Update("password_hash", hash)
		if result.Error != nil {
			return fmt.Errorf("failed to update password: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrEmailTokenInvalid
		}
		if err := tx.Model(&models.User{}).Where("id = ?", tok.UserID).Update("password_hash", hash).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM auth_sessions WHERE user_id = ?", tok.UserID).Error; err != nil {
			return err
		}
		if err := RevokeAllUserRefreshTokens(tx, tok.UserID); err != nil {
			return err
		}
		if err := tx.Model(&models.User{}).
			Where("id = ? AND email_verified_at IS NULL", tok.UserID).
			Update("email_verified_at", time.Now()).Error; err != nil {
			return fmt.Errorf("failed to mark email verified: %w", err)
		}
		return tx.First(&user, "id = ?", tok.UserID).Error
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}
