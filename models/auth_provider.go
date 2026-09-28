package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Schema lives in migrations/; tags here only guide queries.
type AuthProvider struct {
	ID             string         `gorm:"primaryKey" json:"id"`
	UserID         string         `json:"user_id"`
	Provider       string         `json:"provider"`
	ProviderUserID string         `json:"provider_user_id"`
	PasswordHash   string         `json:"-"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `json:"-"`
}

func (a *AuthProvider) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	return nil
}
