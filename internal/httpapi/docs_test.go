package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"tablescore-api/config"
	"tablescore-api/handlers"
	"tablescore-api/internal/httpapi"
	"tablescore-api/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDocumentationCoversMountedRoutes(t *testing.T) {
	cfg := &config.Config{}
	cfg.Auth.JWTSecret = "documentation-test-secret"
	api, err := httpapi.New(store.NewMemoryStore()).WithFoundation(handlers.Deps{Cfg: cfg})
	require.NoError(t, err)
	r := api.Handler().(*gin.Engine)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")
	var spec struct {
		OpenAPI string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &spec))
	require.Equal(t, "3.0.3", spec.OpenAPI)
	parameter := regexp.MustCompile(`:(\w+)`)
	routes := map[string]bool{}
	for _, route := range r.Routes() {
		if route.Path == "/docs" || route.Path == "/docs/" || route.Path == "/openapi.json" || route.Path == "/verify-email" || route.Path == "/reset-password" {
			continue
		}
		path := parameter.ReplaceAllString(route.Path, "{$1}")
		method := strings.ToLower(route.Method)
		routes[method+" "+path] = true
		require.Contains(t, spec.Paths[path], method, "undocumented route: %s %s", route.Method, route.Path)
	}
	for path, operations := range spec.Paths {
		for method := range operations {
			require.True(t, routes[method+" "+path], "documented route does not exist: %s %s", method, path)
		}
	}
	for _, path := range []string{"/docs", "/docs/"} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Body.String(), "SwaggerUIBundle")
		require.Contains(t, w.Body.String(), "/openapi.json")
	}
}
