package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

const DefaultJWTSecretPlaceholder = "change-me-to-a-random-64-char-string"

type Config struct {
	DatabaseURL string `yaml:"database_url" env:"DATABASE_URL"`
	Env         string `yaml:"env" env:"APP_ENV" env-default:"dev"`
	Server      struct {
		Port    int    `yaml:"port" env:"SERVER_PORT" env-default:"8080"`
		BaseURL string `yaml:"base_url" env:"SERVER_BASE_URL" env-default:"http://localhost:8080"`
		// TrustedProxies: CIDRs/IPs whose X-Forwarded-For is honored. Empty = Gin default (trust all).
		TrustedProxies []string `yaml:"trusted_proxies" env:"SERVER_TRUSTED_PROXIES"`
	} `yaml:"server"`
	DB struct {
		Host     string `yaml:"host" env:"DB_HOST" env-default:"localhost"`
		Port     int    `yaml:"port" env:"DB_PORT" env-default:"5432"`
		User     string `yaml:"user" env:"DB_USER" env-default:"tablescore"`
		Password string `yaml:"password" env:"DB_PASSWORD" env-default:"tablescore"`
		DBName   string `yaml:"dbname" env:"DB_NAME" env-default:"tablescore"`
		SSLMode  string `yaml:"sslmode" env:"DB_SSLMODE" env-default:"disable"`
	} `yaml:"db"`
	Log struct {
		Level string `yaml:"level" env:"LOG_LEVEL" env-default:"debug"`
	} `yaml:"log"`
	Auth struct {
		JWTSecret          string `yaml:"jwt_secret" env:"AUTH_JWT_SECRET"`
		SessionSecret      string `yaml:"session_secret" env:"AUTH_SESSION_SECRET"` // OAuth state cookie; falls back to jwt_secret
		AccessTokenExpiry  string `yaml:"access_token_expiry" env:"AUTH_ACCESS_TOKEN_EXPIRY" env-default:"15m"`
		RefreshTokenExpiry string `yaml:"refresh_token_expiry" env:"AUTH_REFRESH_TOKEN_EXPIRY" env-default:"168h"`
		Google             struct {
			ClientID     string `yaml:"client_id" env:"AUTH_GOOGLE_CLIENT_ID"`
			ClientSecret string `yaml:"client_secret" env:"AUTH_GOOGLE_CLIENT_SECRET"`
		} `yaml:"google"`
		GitHub struct {
			ClientID     string `yaml:"client_id" env:"AUTH_GITHUB_CLIENT_ID"`
			ClientSecret string `yaml:"client_secret" env:"AUTH_GITHUB_CLIENT_SECRET"`
		} `yaml:"github"`
		Apple struct {
			ClientID   string `yaml:"client_id" env:"AUTH_APPLE_CLIENT_ID"`
			TeamID     string `yaml:"team_id" env:"AUTH_APPLE_TEAM_ID"`
			KeyID      string `yaml:"key_id" env:"AUTH_APPLE_KEY_ID"`
			PrivateKey string `yaml:"private_key" env:"AUTH_APPLE_PRIVATE_KEY"`
		} `yaml:"apple"`
	} `yaml:"auth"`
	SMTP struct {
		Host     string `yaml:"host" env:"SMTP_HOST"` // empty = emails are logged, not sent
		Port     int    `yaml:"port" env:"SMTP_PORT" env-default:"587"`
		Username string `yaml:"username" env:"SMTP_USERNAME"`
		Password string `yaml:"password" env:"SMTP_PASSWORD"`
		From     string `yaml:"from" env:"SMTP_FROM" env-default:"MeppVP <no-reply@example.com>"`
		TLS      string `yaml:"tls" env:"SMTP_TLS" env-default:"starttls"` // starttls | tls | none
	} `yaml:"smtp"`
}

func (c *Config) IsProd() bool { return c.Env == "prod" || c.Env == "production" }

func (c *Config) SessionSecret() string {
	if c.Auth.SessionSecret != "" {
		return c.Auth.SessionSecret
	}
	return c.Auth.JWTSecret
}

// SMTP TLS modes. starttls upgrades a plain connection (port 587), tls opens an
// implicit TLS connection (port 465), none is for local relays only.
const (
	SMTPTLSStartTLS = "starttls"
	SMTPTLSImplicit = "tls"
	SMTPTLSNone     = "none"
)

// Defaults applied when the SMTP strings are empty (only in Config literals
// built by tests; Load applies env-default tags).
const (
	defaultSMTPFrom = "MeppVP <no-reply@example.com>"
	defaultSMTPTLS  = SMTPTLSStartTLS
	defaultSMTPPort = 587
)

// SMTPConfigured reports whether an SMTP host is set. When it is not, the
// server logs emails instead of sending them.
func (c *Config) SMTPConfigured() bool { return c.SMTP.Host != "" }

// SMTPFrom returns smtp.from, or the default sender when it is empty.
func (c *Config) SMTPFrom() string {
	if c.SMTP.From != "" {
		return c.SMTP.From
	}
	return defaultSMTPFrom
}

// SMTPTLS returns smtp.tls, or starttls when it is empty. Validate has already
// rejected an unknown non-empty value.
func (c *Config) SMTPTLS() string {
	if c.SMTP.TLS != "" {
		return c.SMTP.TLS
	}
	return defaultSMTPTLS
}

// SMTPPort returns smtp.port, or 587 when it is 0. Validate has already
// rejected a value outside 1-65535.
func (c *Config) SMTPPort() int {
	if c.SMTP.Port != 0 {
		return c.SMTP.Port
	}
	return defaultSMTPPort
}

// FromName is the display name of the sender address, which doubles as the
// product name inside email templates. It falls back to the address's local
// part when the sender has no display name.
func (c *Config) FromName() string {
	addr, err := mail.ParseAddress(c.SMTPFrom())
	if err != nil {
		return "MeppVP"
	}
	if addr.Name != "" {
		return addr.Name
	}
	// i > 0: an address starting with "@" has no local part to use.
	if i := strings.Index(addr.Address, "@"); i > 0 {
		return addr.Address[:i]
	}
	return addr.Address
}

// AbsoluteURL joins a path (starting with "/") onto server.base_url. Links in
// emails are built with it so they point at the public origin.
func (c *Config) AbsoluteURL(path string) string {
	return strings.TrimRight(c.Server.BaseURL, "/") + path
}

// Default token lifetimes, used when the expiry strings are empty (only in
// Config literals built by tests; Load applies env-default tags).
const (
	defaultAccessTokenTTL  = 15 * time.Minute
	defaultRefreshTokenTTL = 168 * time.Hour
)

// AccessTokenTTL returns auth.access_token_expiry as a duration. Validate has
// already rejected an unparsable non-empty value.
func (c *Config) AccessTokenTTL() time.Duration {
	return durationOr(c.Auth.AccessTokenExpiry, defaultAccessTokenTTL)
}

// RefreshTokenTTL returns auth.refresh_token_expiry as a duration.
func (c *Config) RefreshTokenTTL() time.Duration {
	return durationOr(c.Auth.RefreshTokenExpiry, defaultRefreshTokenTTL)
}

func durationOr(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

// WSOrigins returns the WebSocket origin allowlist: nil (any origin) outside
// production, otherwise the single "scheme://host" origin derived from base_url.
func (c *Config) WSOrigins() []string {
	if !c.IsProd() {
		return nil
	}
	u, err := url.Parse(c.Server.BaseURL)
	if err != nil {
		return nil
	}
	return []string{u.Scheme + "://" + u.Host}
}

// Validate checks the JWT secret, the token expiries and the smtp.* values in
// every environment, then additionally enforces production-only invariants.
func (c *Config) Validate() error {
	if c.Auth.JWTSecret == "" {
		return errors.New("auth.jwt_secret must be set (see config.example.yaml)")
	}
	if s := c.Auth.AccessTokenExpiry; s != "" {
		if d, err := time.ParseDuration(s); err != nil || d <= 0 {
			return fmt.Errorf("auth.access_token_expiry %q must be a positive duration", s)
		}
	}
	if s := c.Auth.RefreshTokenExpiry; s != "" {
		if d, err := time.ParseDuration(s); err != nil || d <= 0 {
			return fmt.Errorf("auth.refresh_token_expiry %q must be a positive duration", s)
		}
	}
	switch c.SMTP.TLS {
	case "", SMTPTLSStartTLS, SMTPTLSImplicit, SMTPTLSNone:
	default:
		return fmt.Errorf("smtp.tls must be one of starttls, tls or none, got %q", c.SMTP.TLS)
	}
	// 0 is allowed and means the default (587); anything else must be a valid port.
	if p := c.SMTP.Port; p < 0 || p > 65535 {
		return fmt.Errorf("smtp.port must be 0 (the default, 587) or between 1 and 65535, got %d", p)
	}
	if c.SMTP.From != "" {
		if _, err := mail.ParseAddress(c.SMTP.From); err != nil {
			return fmt.Errorf("smtp.from %q is not a valid address: %w", c.SMTP.From, err)
		}
	}
	if !c.IsProd() {
		return nil
	}
	if c.Auth.JWTSecret == DefaultJWTSecretPlaceholder {
		return errors.New("auth.jwt_secret must be set to a random value in production")
	}
	if len(c.Auth.JWTSecret) < 32 {
		return errors.New("auth.jwt_secret must be at least 32 characters in production")
	}
	// The WebSocket origin allowlist is derived from base_url, so it must be absolute.
	if u, err := url.Parse(c.Server.BaseURL); err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("server.base_url must be an absolute URL (scheme://host) in production, got %q", c.Server.BaseURL)
	}
	if !c.SMTPConfigured() {
		return errors.New("smtp.host must be set in production (emails cannot be logged instead of sent)")
	}
	return nil
}

// normalize cleans up values that pick up incidental whitespace when supplied
// as a comma-separated environment variable.
func (c *Config) normalize() {
	proxies := c.Server.TrustedProxies[:0]
	for _, p := range c.Server.TrustedProxies {
		if p = strings.TrimSpace(p); p != "" {
			proxies = append(proxies, p)
		}
	}
	c.Server.TrustedProxies = proxies
}

// Load reads config.yaml when present (env vars override file values),
// otherwise reads from environment only. Then validates.
func Load(path string) (*Config, error) {
	var cfg Config
	switch _, err := os.Stat(path); {
	case err == nil:
		if err := cleanenv.ReadConfig(path, &cfg); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		slog.Info("config loaded from file", "path", path)
	case errors.Is(err, fs.ErrNotExist):
		if err := cleanenv.ReadEnv(&cfg); err != nil {
			return nil, fmt.Errorf("read env: %w", err)
		}
		slog.Info("config file not found, using environment only", "path", path)
	default:
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}
