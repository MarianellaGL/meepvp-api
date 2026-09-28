package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tablescore-api/config"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestForgotPasswordReportsDisabledEmailWithoutAccountLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handlers{Deps: Deps{Cfg: &config.Config{Env: "production"}}}
	r := gin.New()
	r.POST("/api/auth/forgot-password", h.ForgotPassword)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password", strings.NewReader(`{"email":"user@example.com"}`)))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "email delivery is disabled")
}
