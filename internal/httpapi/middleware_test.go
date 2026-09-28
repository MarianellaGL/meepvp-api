package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"tablescore-api/internal/store"
)

func TestRecoveryLogsJSONErrorWithRequestID(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	router := New(store.NewMemoryStore()).Handler().(*gin.Engine)
	router.GET("/panic", func(c *gin.Context) { panic("private panic details") })
	r := httptest.NewRequest(http.MethodGet, "/panic?token=private-query", nil)
	r.Header.Set("X-Table-Token", "private-table-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 500 || w.Header().Get("X-Request-ID") == "" {
		t.Fatalf("panic response: %d, %v", w.Code, w.Header())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["error"] != "internal server error" {
		t.Fatalf("unexpected error response: %s", w.Body.String())
	}
	if strings.Contains(logs.String(), "private") {
		t.Fatalf("request secrets appeared in logs: %s", logs.String())
	}
	decoder := json.NewDecoder(&logs)
	var panicEntry, completed map[string]any
	if err := decoder.Decode(&panicEntry); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&completed); err != nil {
		t.Fatal(err)
	}
	if completed["status"] != float64(500) || completed["request_id"] != w.Header().Get("X-Request-ID") || completed["level"] != "ERROR" {
		t.Fatalf("missing panic completion log: %v", completed)
	}
}

func TestAuthRateLimitSharedAcrossLoginAndSignup(t *testing.T) {
	h := New(store.NewMemoryStore()).Handler()
	for i := 0; i < 21; i++ {
		endpoint := "/v1/auth/login"
		if i%2 == 0 {
			endpoint = "/v1/auth/signup"
		}
		r := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", "203.0.113.10")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if i < 20 && w.Code == 429 {
			t.Fatalf("request %d blocked early", i+1)
		}
		if i == 20 && (w.Code != 429 || w.Header().Get("Retry-After") != "60") {
			t.Fatalf("limit was not shared: %d %v", w.Code, w.Header())
		}
	}
	for _, endpoint := range []string{"/health", "/v1/auth/logout"} {
		method := http.MethodGet
		if endpoint != "/health" {
			method = http.MethodPost
		}
		r := httptest.NewRequest(method, endpoint, nil)
		r.Header.Set("X-Forwarded-For", "203.0.113.10")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code == 429 {
			t.Fatalf("%s should remain available", endpoint)
		}
	}
}

func TestRateLimitWindowAndIndependentIPs(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := gin.New()
	r.GET("/limited", rateLimit(1, time.Second, func() time.Time { return now }), func(c *gin.Context) { c.Status(200) })
	request := func(ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/limited", nil)
		req.Header.Set("X-Forwarded-For", ip)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	if request("203.0.113.1").Code != 200 || request("203.0.113.2").Code != 200 {
		t.Fatal("independent IPs should be allowed")
	}
	now = now.Add(900 * time.Millisecond)
	w := request("203.0.113.1")
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("expected positive rounded retry: %d %v", w.Code, w.Header())
	}
	now = now.Add(100 * time.Millisecond)
	if request("203.0.113.1").Code != 200 || request("203.0.113.1").Code != 429 {
		t.Fatal("window should reset exactly at the deadline")
	}
}

func TestRateLimitConcurrentRequests(t *testing.T) {
	r := gin.New()
	r.GET("/limited", rateLimit(10, time.Minute, time.Now), func(c *gin.Context) { c.Status(200) })
	var allowed, blocked atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Go(func() {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/limited", nil))
			switch w.Code {
			case 200:
				allowed.Add(1)
			case 429:
				blocked.Add(1)
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 10 || blocked.Load() != 40 {
		t.Fatalf("concurrent limit: %d allowed, %d blocked", allowed.Load(), blocked.Load())
	}
}

func TestRouterPreflightAndUnknownRoute(t *testing.T) {
	h := New(store.NewMemoryStore()).Handler()
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodOptions, "/v1/auth/login", 204},
		{http.MethodGet, "/v1/sessions/id/unexpected", 404},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status || w.Header().Get("Access-Control-Allow-Origin") != "*" || w.Header().Get("X-Request-ID") == "" {
			t.Fatalf("%s %s: %d %v", tc.method, tc.path, w.Code, w.Header())
		}
	}
}
