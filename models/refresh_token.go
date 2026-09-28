package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Schema lives in migrations/; tags here only guide queries.
type RefreshToken struct {
	ID        string         `gorm:"primaryKey" json:"id"`
	UserID    string         `json:"user_id"`
	Token     string         `json:"-"` // hex SHA-256 of the opaque token; the raw value lives only in the cookie
	ExpiresAt time.Time      `json:"expires_at"`
	Revoked   bool           `json:"revoked"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-"`
}

func (r *RefreshToken) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	return nil
}
