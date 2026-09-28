package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Schema lives in migrations/; tags here only guide queries.
//
// EmailToken is a single-use, purpose-bound token delivered by email
// (address verification or password reset). Only the SHA-256 of the raw token
// is stored; the raw value exists in the link and nowhere else. A consumed
// token keeps its row with used_at set, so the table doubles as an audit
// trail.
type EmailToken struct {
	ID        string     `gorm:"primaryKey" json:"id"`
	UserID    string     `json:"user_id"`
	Purpose   string     `json:"purpose"` // "verify" | "reset"
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func (e *EmailToken) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	return nil
}
