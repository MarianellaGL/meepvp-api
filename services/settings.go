package services

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"tablescore-api/auth"
	"tablescore-api/models"

	"gorm.io/gorm"
)

// Setting keys. Values are stored as strings; booleans are "true"/"false".
const (
	SettingEnableWSBeacons  = "enable_websocket_beacons"
	SettingBeaconIntervalMS = "beacon_interval_ms"
	SettingEnableGoogleAuth = "enable_google_auth"
	SettingEnableGitHubAuth = "enable_github_auth"
	SettingEnableAppleAuth  = "enable_apple_auth"

	DefaultBeaconIntervalMS int64 = 50
	MinBeaconIntervalMS     int64 = 10
)

// ErrSettingNotFound is returned by Set for a key that was never seeded.
// Settings are created by seeding, never through the API.
var ErrSettingNotFound = errors.New("setting not found")

var defaultSettings = []models.AppConfig{
	{Key: SettingEnableWSBeacons, Name: "WebSocket Beacons", Value: "true"},
	{Key: SettingEnableGoogleAuth, Name: "Google Authentication", Value: "true"},
	{Key: SettingEnableAppleAuth, Name: "Apple Authentication", Value: "true"},
	{Key: SettingEnableGitHubAuth, Name: "GitHub Authentication", Value: "true"},
	{Key: SettingBeaconIntervalMS, Name: "Beacon Interval (ms)", Value: "50"},
}

// Settings is the in-process view of the app_configs table. Reads are served
// from memory; Set writes the database first, then the cache. The application
// runs as a single process (the WebSocket hub is in-process for the same
// reason), so there is no second writer to invalidate against.
type Settings struct {
	db   *gorm.DB
	mu   sync.RWMutex
	rows map[string]models.AppConfig
}

// NewSettings seeds any missing default rows and loads the whole table.
func NewSettings(db *gorm.DB) (*Settings, error) {
	for _, def := range defaultSettings {
		row := def
		if err := db.Where("key = ?", def.Key).FirstOrCreate(&row).Error; err != nil {
			return nil, fmt.Errorf("failed to seed setting %s: %w", def.Key, err)
		}
	}

	var all []models.AppConfig
	if err := db.Find(&all).Error; err != nil {
		return nil, fmt.Errorf("failed to load settings: %w", err)
	}

	s := &Settings{db: db, rows: make(map[string]models.AppConfig, len(all))}
	for _, row := range all {
		s.rows[row.Key] = row
	}
	return s, nil
}

// All returns every setting sorted by key.
func (s *Settings) All() []models.AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.AppConfig, 0, len(s.rows))
	for _, row := range s.rows {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Get returns the raw value; ok is false for an unknown key.
func (s *Settings) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row, ok := s.rows[key]
	return row.Value, ok
}

// Set persists a new value and returns the updated row.
func (s *Settings) Set(key, value string) (models.AppConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[key]
	if !ok {
		return models.AppConfig{}, ErrSettingNotFound
	}
	row.Value = value
	if err := s.db.Save(&row).Error; err != nil {
		return models.AppConfig{}, fmt.Errorf("failed to update setting %s: %w", key, err)
	}
	s.rows[key] = row
	return row, nil
}

// Bool reports whether the setting is literally "true". Unknown keys are false.
func (s *Settings) Bool(key string) bool {
	v, _ := s.Get(key)
	return v == "true"
}

// ProviderEnabled reports whether an OAuth provider is switched on. A provider
// is only off when its toggle is literally "false"; providers without
// a toggle, and unknown names, are enabled.
// Whether the provider exists at all is the router's concern.
func (s *Settings) ProviderEnabled(provider string) bool {
	var key string
	switch provider {
	case auth.ProviderGoogle:
		key = SettingEnableGoogleAuth
	case auth.ProviderGitHub:
		key = SettingEnableGitHubAuth
	case auth.ProviderApple:
		key = SettingEnableAppleAuth
	default:
		return true
	}
	v, _ := s.Get(key)
	return v != "false"
}

// BeaconsEnabled satisfies ws.BeaconSettings.
func (s *Settings) BeaconsEnabled() bool { return s.Bool(SettingEnableWSBeacons) }

// BeaconInterval satisfies ws.BeaconSettings: the interval setting parsed and
// clamped to MinBeaconIntervalMS, or the default when missing or not a number.
func (s *Settings) BeaconInterval() time.Duration {
	ms := DefaultBeaconIntervalMS
	if v, ok := s.Get(SettingBeaconIntervalMS); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			ms = n
		}
	}
	if ms < MinBeaconIntervalMS {
		ms = MinBeaconIntervalMS
	}
	return time.Duration(ms) * time.Millisecond
}
