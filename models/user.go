package models

import (
	"strings"
	"time"

	"tablescore-api/auth"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Schema lives in migrations/; tags here only guide queries.
type User struct {
	ID              string         `gorm:"primaryKey" json:"id"`
	Username        string         `json:"username"`
	UsernameKey     string         `json:"-"`
	PasswordHash    string         `json:"-"`
	Email           string         `json:"email"`
	Name            string         `json:"name"`
	Role            string         `json:"role"`                        // auth.RoleAdmin or auth.RoleUser
	EmailVerifiedAt *time.Time     `json:"email_verified_at,omitempty"` // nil until the address is confirmed
	AuthProviders   []AuthProvider `gorm:"foreignKey:UserID" json:"auth_providers,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `json:"-"`
}

// BeforeCreate fills the ID and the role. Both used to come from database
// defaults that GORM only honoured because of schema tags; now the struct
// carries them explicitly so inserts never rely on the database.
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = uuid.NewString()
	}
	if u.Username == "" {
		u.Username = "user_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	}
	u.UsernameKey = strings.ToLower(u.Username)
	if u.Role == "" {
		u.Role = auth.RoleUser
	}
	return nil
}
