package httpapi_test

import (
	"bytes"
	"context"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"tablescore-api/config"
	"tablescore-api/handlers"
	legacyauth "tablescore-api/internal/auth"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/httpapi"
	"tablescore-api/internal/store"
	"tablescore-api/mailer"
	"tablescore-api/migrations"
	"tablescore-api/models"
	"tablescore-api/services"
	"tablescore-api/ws"
)

func TestFoundationAdoptionPreservesDataAndAuth(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL must point to a dedicated *_test database")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(u.Path, "_test"), "integration tests require a dedicated *_test database")
	base, err := models.OpenDSN(dsn)
	require.NoError(t, err)
	basePool, err := base.DB()
	require.NoError(t, err)
	defer func() { _ = basePool.Close() }()
	schema := "adoption_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, base.Exec("CREATE SCHEMA "+schema).Error)
	defer base.Exec("DROP SCHEMA " + schema + " CASCADE")
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := models.OpenDSN(u.String())
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	repo, err := store.NewPostgresStore(db)
	require.NoError(t, err)

	// Simulate a deployed pre-Foundation database, including a PBKDF2 account.
	baseline, err := migrations.FS.ReadFile("00001_tablescore_baseline.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(strings.Split(string(baseline), "-- +goose Down")[0]).Error)
	salt := []byte("1234567890123456")
	key, err := pbkdf2.Key(sha256.New, "old-password-123", salt, 600000, 32)
	require.NoError(t, err)
	oldHash := "pbkdf2_sha256$600000$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key)
	old, err := repo.CreateUser("existing_user", oldHash)
	require.NoError(t, err)
	oldToken := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	require.NoError(t, repo.SaveAuthSession(old.ID, services.HashToken(oldToken), time.Now().Add(time.Hour).Unix()))
	table, err := repo.CreateTable("existing table")
	require.NoError(t, err)
	// Written with the baseline columns: scoring_rules had no owner yet.
	rule := domain.ScoringRule{ID: "existing-rule", GameName: "Existing game", Name: "Standard", WinCondition: domain.WinConditionHighest, Fields: []domain.ScoreField{{ID: "coins", Name: "Coins", Kind: domain.FieldKindCounter, PointsPerUnit: 1}}, CreatedAt: time.Now().UTC()}
	rulePayload, err := json.Marshal(rule)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`INSERT INTO scoring_rules (id, data, created_at) VALUES (?, ?, ?)`, rule.ID, rulePayload, rule.CreatedAt).Error)
	session, err := repo.CreateSession(table.Code, table.HostToken, rule.ID, []domain.Player{{Name: "Ana"}})
	require.NoError(t, err)
	require.NoError(t, repo.LinkUserSession(old.ID, session.ID, session.Players[0].ID))

	require.NoError(t, models.Migrate(db))
	require.NoError(t, models.Migrate(db))
	restored, hash, err := repo.FindUser("existing_user")
	require.NoError(t, err)
	require.Equal(t, old.ID, restored.ID)
	require.Equal(t, oldHash, hash)
	require.True(t, legacyauth.Verify("old-password-123", hash))
	current, err := repo.GetSession(session.ID)
	require.NoError(t, err)
	require.Equal(t, session.ID, current.ID)
	linked, err := repo.ListUserSessions(old.ID)
	require.NoError(t, err)
	require.Len(t, linked, 1)

	cfg := &config.Config{}
	cfg.Env = "dev"
	cfg.Server.BaseURL = "http://localhost:8080"
	cfg.Auth.JWTSecret = "test-only-foundation-integration-secret"
	settings, err := services.NewSettings(db)
	require.NoError(t, err)
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Shutdown()
	mail := &mailer.Recorder{}
	gin.SetMode(gin.TestMode)
	api, err := httpapi.New(services.NewGameRepository(repo, hub)).WithFoundation(handlers.Deps{DB: db, Cfg: cfg, Hub: hub, Settings: settings, Mailer: mail})
	require.NoError(t, err)
	h := api.Handler()
	call := func(method, path string, body any, cookies []*http.Cookie, token string) *httptest.ResponseRecorder {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	require.Equal(t, 200, call("GET", "/v1/me", nil, nil, oldToken).Code)
	oldLogin := call("POST", "/v1/auth/login", map[string]string{"username": "existing_user", "password": "old-password-123"}, nil, "")
	require.Equal(t, 200, oldLogin.Code, oldLogin.Body.String())
	registration := call("POST", "/api/auth/register", map[string]string{"email": "new@example.com", "name": "New user", "password": "NewPassword123!"}, nil, "")
	require.Equal(t, 201, registration.Code, registration.Body.String())
	require.Len(t, mail.Sent(), 1)
	var registered handlers.UserResponse
	require.NoError(t, json.Unmarshal(registration.Body.Bytes(), &registered))
	require.NotEmpty(t, registered.Username)
	cookies := registration.Result().Cookies()
	require.Len(t, cookies, 2)
	// New Foundation identities can use the existing game's API.
	require.Equal(t, 200, call("GET", "/v1/me", nil, cookies, "").Code)
	require.Equal(t, 200, call("GET", "/api/auth/me", nil, cookies, "").Code)
	require.Equal(t, 403, call("GET", "/api/admin/users", nil, cookies, "").Code)
	jwtToken := cookies[0].Value
	require.Equal(t, 200, call("GET", "/v1/me", nil, nil, jwtToken).Code)
	// Refresh rows contain hashes, and rotation cannot succeed twice concurrently.
	var raw string
	for _, c := range cookies {
		if c.Name == "refresh_token" {
			raw = c.Value
			require.Equal(t, "/api/auth", c.Path)
		}
	}
	require.NotEmpty(t, raw)
	var stored models.RefreshToken
	require.NoError(t, db.Where("token = ?", services.HashToken(raw)).First(&stored).Error)
	require.NotEqual(t, raw, stored.Token)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Go(func() { results <- call("POST", "/api/auth/refresh", nil, cookies, "").Code })
	}
	wg.Wait()
	close(results)
	var wins int
	for code := range results {
		require.True(t, code == 200 || code == 401, "refresh race returned %d", code)
		if code == 200 {
			wins++
		}
	}
	require.Equal(t, 1, wins)
	// The actual WS transport authenticates JWTs and publishes committed game writes.
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + jwtToken}}})
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	room := "session:" + session.ID
	require.NoError(t, wsjson.Write(ctx, conn, ws.NewMessage(ws.TypeRoomJoin, ws.RoomPayload{Room: room})))
	require.Eventually(t, func() bool {
		for _, c := range hub.ConnectedClients() {
			if c.UserID == registered.ID {
				for _, r := range c.Rooms {
					if r == room {
						return true
					}
				}
			}
		}
		return false
	}, time.Second, 5*time.Millisecond)
	game := services.NewGameRepository(repo, hub)
	_, err = game.SetScore(session.ID, session.Players[0].ID, rule.Fields[0].ID, 7)
	require.NoError(t, err)
	var event ws.WSMessage
	require.NoError(t, wsjson.Read(ctx, conn, &event))
	require.Equal(t, ws.TypeQueryInvalidate, event.Type)
	var payload ws.QueryInvalidatePayload
	require.NoError(t, json.Unmarshal(event.Payload, &payload))
	require.Equal(t, []string{"sessions", session.ID}, payload.QueryKey)
	// Password reset synchronizes both login paths and revokes legacy sessions.
	require.NoError(t, repo.SaveAuthSession(registered.ID, services.HashToken(oldToken+"x"), time.Now().Add(time.Hour).Unix()))
	reset, err := services.IssueEmailToken(db, registered.ID, services.TokenPurposeReset, time.Hour)
	require.NoError(t, err)
	_, err = services.ResetPassword(db, reset, "ChangedPassword123!")
	require.NoError(t, err)
	_, newHash, err := repo.FindUser(registered.Username)
	require.NoError(t, err)
	require.True(t, legacyauth.Verify("ChangedPassword123!", newHash))
	_, err = repo.UserByAuthSession(services.HashToken(oldToken + "x"))
	require.ErrorIs(t, err, store.ErrNotFound)
	// Soft deletion also denies the legacy session path.
	require.NoError(t, services.SoftDeleteUser(db, old.ID))
	require.Equal(t, 401, call("GET", "/v1/me", nil, nil, oldToken).Code)
	// Down/up removes Foundation additions but keeps original tables and game data.
	_, err = models.MigrateDown(db)
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Raw("SELECT count(*) FROM score_sessions WHERE id = ?", session.ID).Scan(&count).Error)
	require.Equal(t, int64(1), count)
	require.NoError(t, models.Migrate(db))
}
