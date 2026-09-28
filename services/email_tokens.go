package services

import (
	"errors"
	"fmt"
	"time"

	"tablescore-api/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Email token purposes. A token only works for the purpose it was issued for.
const (
	TokenPurposeVerify = "verify"
	TokenPurposeReset  = "reset"
)

// ErrEmailTokenInvalid covers every way an email token can fail: unknown,
// expired, already used, or issued for a different purpose. Callers do not
// learn which, and neither does the person holding the link.
var ErrEmailTokenInvalid = errors.New("invalid or expired token")

// IssueEmailToken creates a token for userID and purpose that expires after
// ttl, and returns the raw value to embed in a link. Any earlier unused token
// for the same user and purpose is marked used, so only the newest link works.
func IssueEmailToken(db *gorm.DB, userID string, purpose string, ttl time.Duration) (string, error) {
	raw, err := newRawToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.EmailToken{}).
			Where("user_id = ? AND purpose = ? AND used_at IS NULL", userID, purpose).
			Update("used_at", now).Error; err != nil {
			return fmt.Errorf("failed to supersede email tokens: %w", err)
		}
		tok := models.EmailToken{
			UserID:    userID,
			Purpose:   purpose,
			TokenHash: HashToken(raw),
			ExpiresAt: now.Add(ttl),
		}
		if err := tx.Create(&tok).Error; err != nil {
			return fmt.Errorf("failed to save email token: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return raw, nil
}

// ConsumeEmailToken validates raw for purpose and marks it used. It locks the
// row so two concurrent presentations cannot both succeed, and is meant to be
// called inside the caller's transaction so the side effect (verifying the
// address, changing the password) commits together with the consumption.
func ConsumeEmailToken(tx *gorm.DB, raw, purpose string) (*models.EmailToken, error) {
	var tok models.EmailToken
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("token_hash = ?", HashToken(raw)).
		First(&tok).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEmailTokenInvalid
		}
		return nil, fmt.Errorf("email token lookup failed: %w", err)
	}
	now := time.Now()
	if tok.Purpose != purpose || tok.UsedAt != nil || now.After(tok.ExpiresAt) {
		return nil, ErrEmailTokenInvalid
	}
	if err := tx.Model(&tok).Update("used_at", now).Error; err != nil {
		return nil, fmt.Errorf("failed to mark email token used: %w", err)
	}
	tok.UsedAt = &now
	return &tok, nil
}
