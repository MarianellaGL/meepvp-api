package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Session cookie layout. The refresh token is scoped to its own path so the
// browser only ever sends it to the refresh endpoint.
const (
	CookieAccessToken      = "access_token"
	CookieRefreshToken     = "refresh_token"
	CookiePath             = "/"
	RefreshTokenCookiePath = "/api/auth"
	CookieSameSite         = http.SameSiteLaxMode
	CookieHTTPOnly         = true
)

// SetSessionCookies writes both httpOnly session cookies. Each one expires
// with the token it carries, so the lifetimes come from the same config values
// the tokens were signed and stored with (auth.access_token_expiry and
// auth.refresh_token_expiry) and cannot drift from them. secure must be true
// in production so they are never sent over plain HTTP.
func SetSessionCookies(c *gin.Context, accessToken string, accessTTL time.Duration, refreshToken string, refreshTTL time.Duration, secure bool) {
	c.SetSameSite(CookieSameSite)
	c.SetCookie(CookieAccessToken, accessToken, cookieMaxAge(accessTTL), CookiePath, "", secure, CookieHTTPOnly)
	c.SetCookie(CookieRefreshToken, refreshToken, cookieMaxAge(refreshTTL), RefreshTokenCookiePath, "", secure, CookieHTTPOnly)
}

// cookieMaxAge converts a token lifetime to the whole seconds Set-Cookie takes.
// A zero or negative age would expire the cookie or make it a session cookie,
// so it is floored at one second; config's defaults and Validate keep real
// lifetimes far above that.
func cookieMaxAge(ttl time.Duration) int {
	if secs := int(ttl / time.Second); secs > 0 {
		return secs
	}
	return 1
}

// ClearSessionCookies expires both session cookies.
func ClearSessionCookies(c *gin.Context, secure bool) {
	c.SetSameSite(CookieSameSite)
	c.SetCookie(CookieAccessToken, "", -1, CookiePath, "", secure, CookieHTTPOnly)
	c.SetCookie(CookieRefreshToken, "", -1, RefreshTokenCookiePath, "", secure, CookieHTTPOnly)
}

// AccessTokenFromRequest returns the raw access token: the cookie when present,
// otherwise a Bearer Authorization header, otherwise "".
func AccessTokenFromRequest(c *gin.Context) string {
	if tok, err := c.Cookie(CookieAccessToken); err == nil && tok != "" {
		return tok
	}
	if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}
