package config

import (
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestLoadEnvironmentOnlyWithLegacyDatabaseURL(t *testing.T) {
	t.Setenv("AUTH_JWT_SECRET", "test-only-jwt-key")
	t.Setenv("APP_ENV", "dev")
	t.Setenv("DATABASE_URL", "postgres://example/test_db")
	t.Setenv("SERVER_PORT", "9090")
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	require.NoError(t, err)
	require.Equal(t, "postgres://example/test_db", cfg.DatabaseURL)
	require.Equal(t, 9090, cfg.Server.Port)
	require.Empty(t, cfg.Server.TrustedProxies)
}

func TestValidatePositiveTokenLifetimes(t *testing.T) {
	for _, value := range []string{"0s", "-1m", "invalid"} {
		cfg := &Config{}
		cfg.Auth.JWTSecret = "test-key"
		cfg.Auth.AccessTokenExpiry = value
		require.Error(t, cfg.Validate())
		cfg.Auth.AccessTokenExpiry = "15m"
		cfg.Auth.RefreshTokenExpiry = value
		require.Error(t, cfg.Validate())
	}
}

func TestProductionRequiresConfiguredCredentials(t *testing.T) {
	cfg := &Config{}
	cfg.Env = "production"
	cfg.Auth.JWTSecret = DefaultJWTSecretPlaceholder
	require.Error(t, cfg.Validate())
	cfg.Auth.JWTSecret = "0123456789abcdefghijklmnopqrstuvwxyz"
	cfg.Server.BaseURL = "https://api.example.com"
	require.NoError(t, cfg.Validate())
	require.False(t, cfg.EmailEnabled())
	cfg.SMTP.Host = "smtp.example.com"
	require.NoError(t, cfg.Validate())
	require.True(t, cfg.EmailEnabled())
}
