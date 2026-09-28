package handlers

import (
	"tablescore-api/config"
	"tablescore-api/mailer"
	"tablescore-api/services"
	"tablescore-api/ws"

	"gorm.io/gorm"
)

// Deps is everything the HTTP layer needs. main builds one and hands it to
// NewRouter; tests build one around a rolled-back transaction, a throwaway
// hub and a recording mailer, so they exercise exactly the wiring main uses.
type Deps struct {
	DB       *gorm.DB
	Cfg      *config.Config
	Hub      *ws.Hub
	Settings *services.Settings
	Mailer   mailer.Mailer
}
