package models

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Schema lives in migrations/; tags here only guide queries.
type AppConfig struct {
	ID    string `gorm:"primaryKey" json:"id"`
	Key   string `json:"key"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (a *AppConfig) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	return nil
}
