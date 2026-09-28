package services

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"tablescore-api/auth"
	"tablescore-api/models"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// Sentinel errors returned by the user services. Callers (handlers, CLI) map
// these onto their own transport-level responses.
var (
	ErrUserNotFound = errors.New("user not found")
	ErrEmailTaken   = errors.New("email already in use")
	ErrNoChanges    = errors.New("no fields to update")
)

// pgUniqueViolation is the SQLSTATE code Postgres reports for a unique
// constraint violation.
const pgUniqueViolation = "23505"

// isUniqueViolation reports whether err is a Postgres unique constraint
// violation, however deeply it is wrapped by GORM/pgx.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

// NormalizeEmail canonicalises an email address for storage and comparison.
// Emails are case-insensitive in practice, so every boundary that reads or
// writes one must funnel through this.
func NormalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// GetUserByID retrieves a user by their UUID. A missing row is reported as
// ErrUserNotFound so callers never have to import gorm to tell "no such user"
// apart from "the database is broken".
func GetUserByID(db *gorm.DB, id string) (*models.User, error) {
	var user models.User
	if err := db.Preload("AuthProviders").First(&user, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %v", ErrUserNotFound, err)
		}
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}
	return &user, nil
}

// GetUserByEmail retrieves a user by their email address. Returns
// ErrUserNotFound when no row matches.
func GetUserByEmail(db *gorm.DB, email string) (*models.User, error) {
	var user models.User
	if err := db.Preload("AuthProviders").Where("email = ?", NormalizeEmail(email)).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %v", ErrUserNotFound, err)
		}
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}
	return &user, nil
}

// DeleteUserByEmail permanently deletes a user and their associated auth providers and refresh tokens.
func DeleteUserByEmail(db *gorm.DB, email string) error {
	user, err := GetUserByEmail(db, email)
	if err != nil {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("user_id = ?", user.ID).Delete(&models.RefreshToken{}).Error; err != nil {
			return fmt.Errorf("failed to delete refresh tokens: %w", err)
		}
		if err := tx.Unscoped().Where("user_id = ?", user.ID).Delete(&models.AuthProvider{}).Error; err != nil {
			return fmt.Errorf("failed to delete auth providers: %w", err)
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&models.EmailToken{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Delete(user).Error; err != nil {
			return fmt.Errorf("failed to delete user: %w", err)
		}
		return tx.Exec("DELETE FROM auth_sessions WHERE user_id = ?", user.ID).Error
	})
}

// ListUsers returns a page of users ordered newest first, plus the total
// number of rows matching the search. Page and perPage are assumed to be
// already clamped by the caller (that is an HTTP concern).
func ListUsers(db *gorm.DB, page, perPage int, search string) ([]models.User, int64, error) {
	query := db.Model(&models.User{})
	if search != "" {
		like := "%" + search + "%"
		query = query.Where("name ILIKE ? OR email ILIKE ?", like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	var users []models.User
	offset := (page - 1) * perPage
	if err := query.Order("created_at desc").Offset(offset).Limit(perPage).Find(&users).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to fetch users: %w", err)
	}

	return users, total, nil
}

// UpdateUserProfile updates a user's email and/or name. An empty string leaves
// the field unchanged; if both are empty it returns ErrNoChanges. The email is
// normalized before storage, and the email auth provider row, which keeps the
// address in provider_user_id under a unique index with provider, is updated in
// the same transaction: a stale row would block whoever registers the old
// address next. Changing the email also clears email_verified_at. Returns
// ErrUserNotFound if the user does not exist and ErrEmailTaken if either table
// already holds the address. Login resolves through users.email, not through
// that provider row.
func UpdateUserProfile(db *gorm.DB, id string, email, name string) error {
	updates := map[string]any{}
	if email != "" {
		updates["email"] = NormalizeEmail(email)
		// Changing the address invalidates any prior verification: the new
		// address hasn't been confirmed yet, so the flag must not carry over.
		updates["email_verified_at"] = nil
	}
	if name != "" {
		updates["name"] = name
	}
	if len(updates) == 0 {
		return ErrNoChanges
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.User{}).Where("id = ?", id).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrUserNotFound
		}

		newEmail, ok := updates["email"].(string)
		if !ok {
			return nil
		}
		return tx.Model(&models.AuthProvider{}).
			Where("user_id = ? AND provider = ?", id, auth.ProviderEmail).
			Update("provider_user_id", newEmail).Error
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrUserNotFound):
		return ErrUserNotFound
	case isUniqueViolation(err):
		return ErrEmailTaken
	default:
		return fmt.Errorf("failed to update user: %w", err)
	}
}

// SetUserRole assigns a role to a user. changed is true only when the stored
// role actually moved, which lets callers skip side effects (such as revoking
// refresh tokens) on a no-op write. Returns ErrUserNotFound if the user does
// not exist.
func SetUserRole(db *gorm.DB, id string, role string) (changed bool, err error) {
	result := db.Model(&models.User{}).Where("id = ? AND role <> ?", id, role).Update("role", role)
	if result.Error != nil {
		return false, fmt.Errorf("failed to update role: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return true, nil
	}

	// No row moved: either the user is gone or it already had this role.
	var count int64
	if err := db.Model(&models.User{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to look up user: %w", err)
	}
	if count == 0 {
		return false, ErrUserNotFound
	}
	return false, nil
}

// MarkEmailVerified sets email_verified_at to now if the address is not yet
// verified; an already verified user keeps its original timestamp. Returns
// ErrUserNotFound if no live row matched.
func MarkEmailVerified(db *gorm.DB, id string) error {
	result := db.Model(&models.User{}).Where("id = ? AND email_verified_at IS NULL", id).Update("email_verified_at", time.Now())
	if result.Error != nil {
		return fmt.Errorf("failed to mark email verified: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}

	var count int64
	if err := db.Model(&models.User{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to look up user: %w", err)
	}
	if count == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SoftDeleteUser marks a user deleted (GORM soft delete sets deleted_at rather
// than removing the row). Returns ErrUserNotFound if no live row matched.
func SoftDeleteUser(db *gorm.DB, id string) error {
	result := db.Where("id = ?", id).Delete(&models.User{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete user: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SeedAdminEmail is the address of the account created by SeedAdmin. It is a
// const so the caller logs the same address the seeder actually wrote.
const SeedAdminEmail = "admin@decoda.ar"

// SeedAdmin creates an admin account with a random password if the users table is empty.
// Returns the generated password if an admin was created, or empty string if users already exist.
func SeedAdmin(db *gorm.DB) (string, error) {
	var count int64
	if err := db.Model(&models.User{}).Count(&count).Error; err != nil {
		return "", fmt.Errorf("failed to count users: %w", err)
	}
	if count > 0 {
		return "", nil
	}

	passwordBytes := make([]byte, 16)
	if _, err := rand.Read(passwordBytes); err != nil {
		return "", fmt.Errorf("failed to generate password: %w", err)
	}
	password := base64.URLEncoding.EncodeToString(passwordBytes)

	user, err := CreateEmailUser(db, SeedAdminEmail, "Admin", password)
	if err != nil {
		return "", fmt.Errorf("failed to create admin user: %w", err)
	}

	if _, err := SetUserRole(db, user.ID, auth.RoleAdmin); err != nil {
		return "", fmt.Errorf("failed to set admin role: %w", err)
	}

	return password, nil
}

// UpsertSocialUser finds or creates a user for a social login.
// If a user with the same email already exists, the social provider is linked to that user.
func UpsertSocialUser(db *gorm.DB, email, name, provider, providerUserID string) (*models.User, error) {
	email = NormalizeEmail(email)

	var user models.User

	var existingProvider models.AuthProvider
	result := db.Where("provider = ? AND provider_user_id = ?", provider, providerUserID).First(&existingProvider)
	if result.Error == nil {
		if err := db.First(&user, "id = ?", existingProvider.UserID).Error; err != nil {
			return nil, fmt.Errorf("failed to find user for existing provider: %w", err)
		}
		return &user, nil
	}

	if email == "" {
		return nil, errors.New("OAuth provider did not supply an email")
	}
	result = db.Where("email = ?", email).First(&user)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("failed to query user: %w", result.Error)
	}

	return &user, db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			user = models.User{
				Email:           email,
				Name:            name,
				EmailVerifiedAt: &now, // the provider has already verified the address
			}
			if err := tx.Create(&user).Error; err != nil {
				return fmt.Errorf("failed to create user: %w", err)
			}
		} else if user.EmailVerifiedAt == nil {
			// Linking a provider to an existing account proves the address too.
			if err := tx.Model(&user).Update("email_verified_at", now).Error; err != nil {
				return fmt.Errorf("failed to mark email verified: %w", err)
			}
			user.EmailVerifiedAt = &now
		}

		ap := models.AuthProvider{
			UserID:         user.ID,
			Provider:       provider,
			ProviderUserID: providerUserID,
		}
		if err := tx.Create(&ap).Error; err != nil {
			return fmt.Errorf("failed to create auth provider: %w", err)
		}

		return nil
	})
}

// CreateEmailUser creates a new user with email/password credentials.
func CreateEmailUser(db *gorm.DB, email, name, password string) (*models.User, error) {
	email = NormalizeEmail(email)

	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}

	var user models.User
	err = db.Transaction(func(tx *gorm.DB) error {
		user = models.User{
			Email:        email,
			Name:         name,
			PasswordHash: hash,
		}
		if err := tx.Create(&user).Error; err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}

		ap := models.AuthProvider{
			UserID:         user.ID,
			Provider:       auth.ProviderEmail,
			ProviderUserID: email,
			PasswordHash:   hash,
		}
		if err := tx.Create(&ap).Error; err != nil {
			return fmt.Errorf("failed to create auth provider: %w", err)
		}

		return nil
	})

	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return &user, nil
}

// FindEmailUser finds a user by email who has an email auth provider.
func FindEmailUser(db *gorm.DB, email string) (*models.User, *models.AuthProvider, error) {
	var user models.User
	if err := db.Where("email = ?", NormalizeEmail(email)).First(&user).Error; err != nil {
		return nil, nil, err
	}

	var ap models.AuthProvider
	if err := db.Where("user_id = ? AND provider = ?", user.ID, auth.ProviderEmail).First(&ap).Error; err != nil {
		return nil, nil, err
	}

	return &user, &ap, nil
}

// GetUserRole returns the live role of a user, bypassing whatever an access
// token claims. GORM's default scope hides soft-deleted rows, so a deleted
// user reports ErrUserNotFound like a missing one.
func GetUserRole(db *gorm.DB, id string) (string, error) {
	var role string
	result := db.Model(&models.User{}).Select("role").Where("id = ?", id).Limit(1).Scan(&role)
	if result.Error != nil {
		return "", fmt.Errorf("failed to look up role: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return "", ErrUserNotFound
	}
	return role, nil
}
