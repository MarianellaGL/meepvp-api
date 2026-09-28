package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthLinksRenderConfirmationAndEscapeTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/reset-password", AuthLinkPage(true))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/reset-password?token=%22%3E%3Cscript%3Eevil%3C/script%3E", nil))
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), "<script>evil")
	require.Contains(t, w.Body.String(), "&lt;script&gt;evil")
	require.Contains(t, w.Body.String(), "/api/auth/reset-password")
	require.True(t, strings.Contains(w.Header().Get("Content-Security-Policy"), "sha256-"))
	require.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}
