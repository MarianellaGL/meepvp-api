package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCtx(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c, w
}

func TestAccessTokenFromRequest(t *testing.T) {
	c, _ := newCtx(t)
	assert.Equal(t, "", AccessTokenFromRequest(c), "nothing present")

	c, _ = newCtx(t)
	c.Request.Header.Set("Authorization", "Bearer from-header")
	assert.Equal(t, "from-header", AccessTokenFromRequest(c), "header fallback")

	c, _ = newCtx(t)
	c.Request.Header.Set("Authorization", "Basic abc")
	assert.Equal(t, "", AccessTokenFromRequest(c), "non-bearer header ignored")

	c, _ = newCtx(t)
	c.Request.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: "from-cookie"})
	c.Request.Header.Set("Authorization", "Bearer from-header")
	assert.Equal(t, "from-cookie", AccessTokenFromRequest(c), "cookie wins")
}

func TestSetSessionCookies(t *testing.T) {
	c, w := newCtx(t)
	SetSessionCookies(c, "acc", 15*time.Minute, "ref", time.Hour, true)

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 2)
	byName := map[string]*http.Cookie{}
	for _, ck := range cookies {
		byName[ck.Name] = ck
	}

	access := byName[CookieAccessToken]
	require.NotNil(t, access)
	assert.Equal(t, "acc", access.Value)
	assert.Equal(t, CookiePath, access.Path)
	assert.Equal(t, 900, access.MaxAge, "the cookie expires with the access token")
	assert.True(t, access.HttpOnly)
	assert.True(t, access.Secure)
	assert.Equal(t, http.SameSiteLaxMode, access.SameSite)

	refresh := byName[CookieRefreshToken]
	require.NotNil(t, refresh)
	assert.Equal(t, "ref", refresh.Value)
	assert.Equal(t, RefreshTokenCookiePath, refresh.Path)
	assert.Equal(t, 3600, refresh.MaxAge, "the cookie expires with the refresh token")
	assert.True(t, refresh.HttpOnly)
	assert.True(t, refresh.Secure)
	assert.Equal(t, http.SameSiteLaxMode, refresh.SameSite)
}

// A cookie that outlives its token leaves the browser sending a token the
// server has already rejected, so the max-age has to track the configured
// lifetime rather than a constant of its own.
func TestSetSessionCookies_MaxAgeFollowsTheLifetimes(t *testing.T) {
	c, w := newCtx(t)
	SetSessionCookies(c, "acc", 5*time.Minute, "ref", 24*time.Hour, false)

	for _, ck := range w.Result().Cookies() {
		switch ck.Name {
		case CookieAccessToken:
			assert.Equal(t, 300, ck.MaxAge)
		case CookieRefreshToken:
			assert.Equal(t, 86400, ck.MaxAge)
		}
	}
}

// A non-positive lifetime must not turn into an expiring or session cookie:
// -1 means "delete me" to the browser and 0 means "until the tab closes".
func TestSetSessionCookies_NonPositiveLifetimeStillSetsTheCookie(t *testing.T) {
	c, w := newCtx(t)
	SetSessionCookies(c, "acc", 0, "ref", -time.Hour, false)

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 2)
	for _, ck := range cookies {
		assert.Equal(t, 1, ck.MaxAge, "%s", ck.Name)
		assert.NotEmpty(t, ck.Value)
	}
}

func TestClearSessionCookies(t *testing.T) {
	c, w := newCtx(t)
	ClearSessionCookies(c, false)

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 2)
	for _, ck := range cookies {
		assert.Empty(t, ck.Value)
		assert.Less(t, ck.MaxAge, 0, "%s must be expired", ck.Name)
		assert.False(t, ck.Secure)
	}
}
