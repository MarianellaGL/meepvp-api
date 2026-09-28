package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"tablescore-api/config"

	"github.com/gorilla/sessions"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/apple"
	"github.com/markbates/goth/providers/github"
	"github.com/markbates/goth/providers/google"
)

// oauthStateMaxAge bounds the OAuth state cookie to a single round trip, in seconds.
const oauthStateMaxAge = 600

// RegisterOAuthProviders configures gothic's state-cookie store from config and
// registers every social provider whose credentials are present. Callback
// URLs are derived from server.base_url.
func RegisterOAuthProviders(cfg *config.Config) {
	// gothic keys its cookie store from the SESSION_SECRET env var at init time.
	// Nothing sets it here, so replace the store with one keyed from config.
	if cfg.SessionSecret() == "" {
		slog.Warn("no auth.session_secret or auth.jwt_secret configured — OAuth providers disabled")
		return
	}

	store := sessions.NewCookieStore([]byte(cfg.SessionSecret()))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   oauthStateMaxAge, // OAuth round-trip only
		HttpOnly: true,
		Secure:   cfg.IsProd(),
		SameSite: http.SameSiteLaxMode,
	}
	// NewCookieStore baked its 30-day default into the securecookie codecs; setting
	// Options only changes the Set-Cookie attribute. MaxAge updates both, so a
	// captured state cookie stops decoding once the round trip window closes.
	store.MaxAge(oauthStateMaxAge)
	gothic.Store = store

	var providers []goth.Provider
	baseURL := strings.TrimRight(cfg.Server.BaseURL, "/")

	if cfg.Auth.Google.ClientID != "" && cfg.Auth.Google.ClientSecret != "" {
		providers = append(providers, google.New(
			cfg.Auth.Google.ClientID,
			cfg.Auth.Google.ClientSecret,
			baseURL+"/api/auth/"+ProviderGoogle+"/callback", "email", "profile",
		))
		slog.Info("registered OAuth provider", "provider", ProviderGoogle)
	}

	if cfg.Auth.GitHub.ClientID != "" && cfg.Auth.GitHub.ClientSecret != "" {
		providers = append(providers, github.New(
			cfg.Auth.GitHub.ClientID,
			cfg.Auth.GitHub.ClientSecret,
			baseURL+"/api/auth/"+ProviderGitHub+"/callback", "user:email",
		))
		slog.Info("registered OAuth provider", "provider", ProviderGitHub)
	}

	if cfg.Auth.Apple.ClientID != "" && cfg.Auth.Apple.TeamID != "" {
		providers = append(providers, apple.New(
			cfg.Auth.Apple.ClientID,
			cfg.Auth.Apple.PrivateKey,
			baseURL+"/api/auth/"+ProviderApple+"/callback",
			nil,
			apple.ScopeName,
			apple.ScopeEmail,
		))
		slog.Info("registered OAuth provider", "provider", ProviderApple)
	}

	if len(providers) > 0 {
		goth.UseProviders(providers...)
	}
}
