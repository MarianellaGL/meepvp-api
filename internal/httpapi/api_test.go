package httpapi_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tablescore-api/internal/bgg"
	"tablescore-api/internal/httpapi"
	"tablescore-api/internal/store"
)

func TestAnonymousTableScoringFlow(t *testing.T) {
	h := httpapi.New(store.NewMemoryStore()).Handler()
	table := request(t, h, http.MethodPost, "/v1/tables", map[string]any{"name": "Friday table"}, "")
	if table.Code != http.StatusCreated {
		t.Fatalf("create table: got %d", table.Code)
	}
	var tableBody struct {
		Code      string `json:"code"`
		HostToken string `json:"hostToken"`
	}
	decode(t, table, &tableBody)

	rule := request(t, h, http.MethodPost, "/v1/scoring-rules", map[string]any{"gameName": "Example", "name": "Standard", "fields": []map[string]any{{"name": "Coins", "kind": "counter", "pointsPerUnit": 1}}}, "")
	if rule.Code != http.StatusCreated {
		t.Fatalf("create rule: got %d", rule.Code)
	}
	var ruleBody struct {
		ID     string `json:"id"`
		Fields []struct {
			ID string `json:"id"`
		} `json:"fields"`
	}
	decode(t, rule, &ruleBody)

	session := request(t, h, http.MethodPost, "/v1/tables/"+tableBody.Code+"/sessions", map[string]any{"ruleId": ruleBody.ID, "players": []map[string]string{{"name": "Ana"}}}, tableBody.HostToken)
	if session.Code != http.StatusCreated {
		t.Fatalf("create session: got %d", session.Code)
	}
	var sessionBody struct {
		ID      string `json:"id"`
		Players []struct {
			ID string `json:"id"`
		} `json:"players"`
	}
	decode(t, session, &sessionBody)
	joined := request(t, h, http.MethodPost, "/v1/sessions/"+sessionBody.ID+"/players", map[string]string{"name": "Host"}, "")
	if joined.Code != http.StatusOK {
		t.Fatalf("join session: got %d: %s", joined.Code, joined.Body.String())
	}
	var joinedBody struct {
		Players []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"players"`
		Totals []struct {
			Total int `json:"total"`
		} `json:"totals"`
	}
	decode(t, joined, &joinedBody)
	if len(joinedBody.Players) != 2 || joinedBody.Players[1].Name != "Host" || len(joinedBody.Totals) != 2 || joinedBody.Totals[1].Total != 0 {
		t.Fatalf("host was not added with a zero score: %#v", joinedBody)
	}
	rejoined := request(t, h, http.MethodPost, "/v1/sessions/"+sessionBody.ID+"/players", map[string]string{"name": "host"}, "")
	var rejoinedBody struct {
		Players []any `json:"players"`
	}
	decode(t, rejoined, &rejoinedBody)
	if len(rejoinedBody.Players) != 2 {
		t.Fatalf("duplicate player added: %#v", rejoinedBody)
	}
	points := request(t, h, http.MethodPost, "/v1/sessions/"+sessionBody.ID+"/points", map[string]any{"playerId": joinedBody.Players[1].ID, "delta": 7}, "")
	if points.Code != http.StatusOK {
		t.Fatalf("add points: got %d: %s", points.Code, points.Body.String())
	}
	var pointsBody struct {
		ManualPoints map[string]int `json:"manualPoints"`
		Totals       []struct {
			PlayerID string `json:"playerId"`
			Total    int    `json:"total"`
		} `json:"totals"`
	}
	decode(t, points, &pointsBody)
	if pointsBody.ManualPoints[joinedBody.Players[1].ID] != 7 || pointsBody.Totals[1].Total != 7 {
		t.Fatalf("manual points missing from total: %#v", pointsBody)
	}
	removed := request(t, h, http.MethodPost, "/v1/sessions/"+sessionBody.ID+"/points", map[string]any{"playerId": joinedBody.Players[1].ID, "delta": -2}, "")
	decode(t, removed, &pointsBody)
	if pointsBody.ManualPoints[joinedBody.Players[1].ID] != 5 || pointsBody.Totals[1].Total != 5 {
		t.Fatalf("subtracted points missing from total: %#v", pointsBody)
	}

	updated := request(t, h, http.MethodPut, "/v1/sessions/"+sessionBody.ID+"/scores", map[string]any{"values": map[string]any{sessionBody.Players[0].ID: map[string]int{ruleBody.Fields[0].ID: 12}}}, "")
	if updated.Code != http.StatusOK {
		t.Fatalf("update scores: got %d", updated.Code)
	}
	fieldUpdate := request(t, h, http.MethodPatch, "/v1/sessions/"+sessionBody.ID+"/scores", map[string]any{"playerId": joinedBody.Players[1].ID, "fieldId": ruleBody.Fields[0].ID, "value": 3}, "")
	if fieldUpdate.Code != http.StatusOK {
		t.Fatalf("set one score: got %d: %s", fieldUpdate.Code, fieldUpdate.Body.String())
	}
	var fieldBody struct {
		Values map[string]map[string]int `json:"values"`
	}
	decode(t, fieldUpdate, &fieldBody)
	if fieldBody.Values[sessionBody.Players[0].ID][ruleBody.Fields[0].ID] != 12 || fieldBody.Values[joinedBody.Players[1].ID][ruleBody.Fields[0].ID] != 3 {
		t.Fatalf("single-field update overwrote another player's score: %#v", fieldBody.Values)
	}
	if denied := request(t, h, http.MethodPost, "/v1/sessions/"+sessionBody.ID+"/finish", nil, ""); denied.Code != http.StatusForbidden {
		t.Fatalf("finish without host token: got %d", denied.Code)
	}
	finished := request(t, h, http.MethodPost, "/v1/sessions/"+sessionBody.ID+"/finish", nil, tableBody.HostToken)
	if finished.Code != http.StatusOK {
		t.Fatalf("finish session: got %d", finished.Code)
	}
	var finishedBody struct {
		Status     string     `json:"status"`
		FinishedAt *time.Time `json:"finishedAt"`
		Winners    []struct {
			PlayerID string `json:"playerId"`
			Total    int    `json:"total"`
		} `json:"winners"`
	}
	decode(t, finished, &finishedBody)
	if finishedBody.Status != "finished" || finishedBody.FinishedAt == nil || finishedBody.FinishedAt.IsZero() || len(finishedBody.Winners) != 1 || finishedBody.Winners[0].PlayerID != sessionBody.Players[0].ID || finishedBody.Winners[0].Total != 12 {
		t.Fatalf("wrong winner after finishing: %#v", finishedBody)
	}
	denied := request(t, h, http.MethodPost, "/v1/sessions/"+sessionBody.ID+"/reopen", nil, "wrong-token")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("reopen without host token: got %d", denied.Code)
	}
	reopened := request(t, h, http.MethodPost, "/v1/sessions/"+sessionBody.ID+"/reopen", nil, tableBody.HostToken)
	if reopened.Code != http.StatusOK {
		t.Fatalf("reopen session: got %d: %s", reopened.Code, reopened.Body.String())
	}
	var reopenedBody struct {
		Status     string     `json:"status"`
		FinishedAt *time.Time `json:"finishedAt"`
		Totals     []struct {
			PlayerID string `json:"playerId"`
			Total    int    `json:"total"`
		} `json:"totals"`
	}
	decode(t, reopened, &reopenedBody)
	if reopenedBody.Status != "active" || reopenedBody.FinishedAt != nil || len(reopenedBody.Totals) != 2 || reopenedBody.Totals[0].Total != 12 || reopenedBody.Totals[1].Total != 8 {
		t.Fatalf("reopen lost saved scores: %#v", reopenedBody)
	}
}

func TestFinishedSessionReportsLowestScoreTie(t *testing.T) {
	h := httpapi.New(store.NewMemoryStore()).Handler()
	table := request(t, h, http.MethodPost, "/v1/tables", map[string]string{"name": "Friday table"}, "")
	var tableBody struct {
		Code      string `json:"code"`
		HostToken string `json:"hostToken"`
	}
	decode(t, table, &tableBody)
	rule := request(t, h, http.MethodPost, "/v1/scoring-rules", map[string]any{"gameName": "Example", "name": "Low score", "winCondition": "lowest_total", "fields": []map[string]any{{"name": "Penalty", "kind": "counter", "pointsPerUnit": 1}}}, "")
	var ruleBody struct {
		ID string `json:"id"`
	}
	decode(t, rule, &ruleBody)
	session := request(t, h, http.MethodPost, "/v1/tables/"+tableBody.Code+"/sessions", map[string]any{"ruleId": ruleBody.ID, "players": []map[string]string{{"name": "Ana"}, {"name": "Leo"}}}, tableBody.HostToken)
	var sessionBody struct {
		ID      string `json:"id"`
		Players []struct {
			ID string `json:"id"`
		} `json:"players"`
	}
	decode(t, session, &sessionBody)
	finished := request(t, h, http.MethodPost, "/v1/sessions/"+sessionBody.ID+"/finish", nil, tableBody.HostToken)
	var result struct {
		Winners []struct {
			PlayerID string `json:"playerId"`
		} `json:"winners"`
	}
	decode(t, finished, &result)
	if len(result.Winners) != 2 || result.Winners[0].PlayerID != sessionBody.Players[0].ID || result.Winners[1].PlayerID != sessionBody.Players[1].ID {
		t.Fatalf("expected both players to tie on the lowest total: %#v", result.Winners)
	}
}

func TestJoinTableCodeFindsActiveSession(t *testing.T) {
	h := httpapi.New(store.NewMemoryStore()).Handler()
	table := request(t, h, http.MethodPost, "/v1/tables", map[string]string{"name": "Friday"}, "")
	var owner struct{ Code, HostToken string }
	decode(t, table, &owner)
	rule := request(t, h, http.MethodPost, "/v1/scoring-rules", map[string]any{"gameName": "Azul", "name": "Points", "fields": []map[string]any{{"name": "Tiles", "kind": "counter", "pointsPerUnit": 1}}}, "")
	var sheet struct{ ID string }
	decode(t, rule, &sheet)
	if found := request(t, h, http.MethodGet, "/v1/tables/"+owner.Code+"/current-session", nil, ""); found.Code != http.StatusNotFound {
		t.Fatalf("table without an active game: %d", found.Code)
	}
	game := request(t, h, http.MethodPost, "/v1/tables/"+owner.Code+"/sessions", map[string]any{"ruleId": sheet.ID, "players": []map[string]string{{"name": "Ana"}}}, owner.HostToken)
	var started struct{ ID string }
	decode(t, game, &started)
	found := request(t, h, http.MethodGet, "/v1/tables/"+strings.ToLower(owner.Code)+"/current-session", nil, "")
	var active struct{ ID, Status string }
	decode(t, found, &active)
	if found.Code != http.StatusOK || active.ID != started.ID || active.Status != "active" {
		t.Fatalf("QR table code did not resolve to the active game: %d %#v", found.Code, active)
	}
	request(t, h, http.MethodPost, "/v1/sessions/"+started.ID+"/finish", nil, owner.HostToken)
	if finished := request(t, h, http.MethodGet, "/v1/tables/"+owner.Code+"/current-session", nil, ""); finished.Code != http.StatusNotFound {
		t.Fatalf("finished game remained joinable: %d", finished.Code)
	}
}

func TestPauseResumeAndBoardPhoto(t *testing.T) {
	h := httpapi.New(store.NewMemoryStore()).Handler()
	table := request(t, h, http.MethodPost, "/v1/tables", map[string]string{"name": "Long game"}, "")
	var owner struct{ Code, HostToken string }
	decode(t, table, &owner)
	rule := request(t, h, http.MethodPost, "/v1/scoring-rules", map[string]any{"gameName": "Campaign", "name": "Points", "fields": []map[string]any{{"name": "Points", "kind": "counter", "pointsPerUnit": 1}}}, "")
	var sheet struct{ ID string }
	decode(t, rule, &sheet)
	game := request(t, h, http.MethodPost, "/v1/tables/"+owner.Code+"/sessions", map[string]any{"ruleId": sheet.ID, "players": []map[string]string{{"name": "Ana"}}}, owner.HostToken)
	var started struct{ ID string }
	decode(t, game, &started)
	path := "/v1/sessions/" + started.ID
	if denied := request(t, h, http.MethodPost, path+"/pause", nil, ""); denied.Code != http.StatusForbidden {
		t.Fatalf("pause without host token: %d", denied.Code)
	}
	paused := request(t, h, http.MethodPost, path+"/pause", nil, owner.HostToken)
	var state struct {
		Status          string
		DurationSeconds int64
		PausedAt        *time.Time
	}
	decode(t, paused, &state)
	if paused.Code != http.StatusOK || state.Status != "paused" || state.PausedAt == nil {
		t.Fatalf("pause response: %d %#v", paused.Code, state)
	}
	if current := request(t, h, http.MethodGet, "/v1/tables/"+owner.Code+"/current-session", nil, ""); current.Code != http.StatusOK {
		t.Fatalf("paused game disappeared: %d", current.Code)
	}
	if changed := request(t, h, http.MethodPost, path+"/points", map[string]any{"playerId": "any", "delta": 1}, ""); changed.Code == http.StatusOK {
		t.Fatal("scores changed while paused")
	}

	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	upload := func(token string, payload []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "board.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, path+"/board-photo", &body)
		r.Header.Set("Content-Type", writer.FormDataContentType())
		r.Header.Set("X-Table-Token", token)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, r)
		return response
	}
	if denied := upload("", imageBytes.Bytes()); denied.Code != http.StatusForbidden {
		t.Fatalf("photo without host token: %d", denied.Code)
	}
	if invalid := upload(owner.HostToken, []byte("not an image")); invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid image accepted: %d", invalid.Code)
	}
	photo := upload(owner.HostToken, imageBytes.Bytes())
	var photoState struct{ BoardPhotoUpdatedAt *time.Time }
	decode(t, photo, &photoState)
	if photo.Code != http.StatusOK || photoState.BoardPhotoUpdatedAt == nil {
		t.Fatalf("photo upload: %d", photo.Code)
	}
	read := request(t, h, http.MethodGet, path+"/board-photo", nil, "")
	if read.Code != http.StatusOK || read.Header().Get("Content-Type") != "image/png" || !bytes.Equal(read.Body.Bytes(), imageBytes.Bytes()) {
		t.Fatalf("photo read: %d %q", read.Code, read.Header().Get("Content-Type"))
	}
	resumed := request(t, h, http.MethodPost, path+"/resume", nil, owner.HostToken)
	decode(t, resumed, &state)
	if resumed.Code != http.StatusOK || state.Status != "active" {
		t.Fatalf("resume response: %d %#v", resumed.Code, state)
	}
	if twice := request(t, h, http.MethodPost, path+"/resume", nil, owner.HostToken); twice.Code != http.StatusBadRequest {
		t.Fatalf("resumed twice: %d", twice.Code)
	}
}

func TestAccountStatsAreScopedToUser(t *testing.T) {
	h := httpapi.New(store.NewMemoryStore()).Handler()
	signup := request(t, h, http.MethodPost, "/v1/auth/signup", map[string]string{"username": "Ana", "password": "correct horse battery staple"}, "")
	if signup.Code != http.StatusCreated {
		t.Fatalf("signup: %d: %s", signup.Code, signup.Body.String())
	}
	var ana struct {
		Token string `json:"token"`
	}
	decode(t, signup, &ana)
	duplicate := request(t, h, http.MethodPost, "/v1/auth/signup", map[string]string{"username": "ana", "password": "another secure password"}, "")
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate username: %d", duplicate.Code)
	}
	login := request(t, h, http.MethodPost, "/v1/auth/login", map[string]string{"username": "ana", "password": "correct horse battery staple"}, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d: %s", login.Code, login.Body.String())
	}
	wrongPassword := request(t, h, http.MethodPost, "/v1/auth/login", map[string]string{"username": "ana", "password": "wrong password"}, "")
	if wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password accepted: %d", wrongPassword.Code)
	}
	table := request(t, h, http.MethodPost, "/v1/tables", map[string]string{"name": "Friday"}, "")
	var owner struct{ Code, HostToken string }
	decode(t, table, &owner)
	publicRule := map[string]any{"gameName": "Example", "name": "Points", "isPublic": true, "fields": []map[string]any{{"name": "Points", "kind": "counter", "pointsPerUnit": 1}}}
	if response := request(t, h, http.MethodPost, "/v1/scoring-rules", publicRule, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous publishing accepted: %d", response.Code)
	}
	rule := requestAuth(t, h, http.MethodPost, "/v1/scoring-rules", publicRule, ana.Token)
	var ruleBody struct {
		ID     string `json:"id"`
		Fields []struct {
			ID string `json:"id"`
		} `json:"fields"`
	}
	decode(t, rule, &ruleBody)
	session := requestBoth(t, h, http.MethodPost, "/v1/tables/"+owner.Code+"/sessions", map[string]any{"ruleId": ruleBody.ID, "players": []map[string]string{{"name": "Ana"}, {"name": "Leo"}}}, ana.Token, owner.HostToken)
	if session.Code != http.StatusCreated {
		t.Fatalf("create session: %d: %s", session.Code, session.Body.String())
	}
	var game struct {
		ID      string `json:"id"`
		Players []struct {
			ID string `json:"id"`
		} `json:"players"`
	}
	decode(t, session, &game)
	request(t, h, http.MethodPatch, "/v1/sessions/"+game.ID+"/scores", map[string]any{"playerId": game.Players[0].ID, "fieldId": ruleBody.Fields[0].ID, "value": 5}, "")
	request(t, h, http.MethodPost, "/v1/sessions/"+game.ID+"/finish", nil, owner.HostToken)
	stats := requestAuth(t, h, http.MethodGet, "/v1/me/stats", nil, ana.Token)
	var result struct{ FinishedGames, Wins, Ties, TotalPoints int }
	decode(t, stats, &result)
	if result.FinishedGames != 1 || result.Wins != 1 || result.Ties != 0 || result.TotalPoints != 5 {
		t.Fatalf("wrong stats: %#v", result)
	}
	mySessions := requestAuth(t, h, http.MethodGet, "/v1/me/sessions", nil, ana.Token)
	var linked []struct {
		ID         string `json:"id"`
		GameName   string `json:"gameName"`
		MyPlayerID string `json:"myPlayerId"`
		Winners    []struct {
			PlayerID string `json:"playerId"`
		} `json:"winners"`
	}
	decode(t, mySessions, &linked)
	if len(linked) != 1 || linked[0].ID != game.ID || linked[0].GameName != "Example" || linked[0].MyPlayerID != game.Players[0].ID || len(linked[0].Winners) != 1 || linked[0].Winners[0].PlayerID != game.Players[0].ID {
		t.Fatalf("account history omitted the game or winner: %#v", linked)
	}
	other := request(t, h, http.MethodPost, "/v1/auth/signup", map[string]string{"username": "Leo", "password": "another secure password"}, "")
	var leo struct {
		Token string `json:"token"`
	}
	decode(t, other, &leo)
	otherSessions := requestAuth(t, h, http.MethodGet, "/v1/me/sessions", nil, leo.Token)
	var none []any
	decode(t, otherSessions, &none)
	if len(none) != 0 {
		t.Fatalf("another account can see Ana's sessions")
	}
	logout := requestAuth(t, h, http.MethodPost, "/v1/auth/logout", nil, ana.Token)
	if logout.Code != http.StatusOK || requestAuth(t, h, http.MethodGet, "/v1/me", nil, ana.Token).Code != http.StatusUnauthorized {
		t.Fatal("logout did not revoke session")
	}
}

func TestBGGRulesRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/forumlist":
			_, _ = w.Write([]byte(`<forums><forum id="20" title="Rules" numthreads="1"/></forums>`))
		case "/forum":
			_, _ = w.Write([]byte(`<forum><threads><thread id="30" subject="Scoring question" author="Ana" numarticles="2"/></threads></forum>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	h := httpapi.New(store.NewMemoryStore(), bgg.New(server.URL, "", server.Client())).Handler()

	response := request(t, h, http.MethodGet, "/v1/bgg/games/266192/rules", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("get rules: got %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		TotalThreads int `json:"totalThreads"`
		Threads      []struct {
			Title string `json:"title"`
		} `json:"threads"`
	}
	decode(t, response, &body)
	if body.TotalThreads != 1 || len(body.Threads) != 1 || body.Threads[0].Title != "Scoring question" {
		t.Fatalf("unexpected rules response: %#v", body)
	}

	invalid := request(t, h, http.MethodGet, "/v1/bgg/games/nope/rules", nil, "")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid game ID: got %d", invalid.Code)
	}
}

func TestCommunityScoringRulesOnlyShowSharedTemplates(t *testing.T) {
	h := httpapi.New(store.NewMemoryStore()).Handler()
	signup := request(t, h, http.MethodPost, "/v1/auth/signup", map[string]string{"username": "ana", "password": "correct horse battery staple"}, "")
	if signup.Code != http.StatusCreated {
		t.Fatalf("signup: %d: %s", signup.Code, signup.Body.String())
	}
	var account struct {
		Token string `json:"token"`
	}
	decode(t, signup, &account)
	for _, rule := range []map[string]any{
		{"gameName": "Wingspan", "name": "Bird points", "bggId": 266192, "isPublic": true},
		{"gameName": "Wingspan", "name": "Private draft", "bggId": 266192, "isPublic": false},
		{"gameName": "Azul", "name": "Tile points", "bggId": 230802, "isPublic": true},
	} {
		rule["fields"] = []map[string]any{{"name": "Points", "kind": "counter", "pointsPerUnit": 1}}
		response := requestAuth(t, h, http.MethodPost, "/v1/scoring-rules", rule, account.Token)
		if response.Code != http.StatusCreated {
			t.Fatalf("create rule: %d: %s", response.Code, response.Body.String())
		}
	}
	all := request(t, h, http.MethodGet, "/v1/scoring-rules", nil, "")
	var allRules []struct {
		ID string `json:"id"`
	}
	decode(t, all, &allRules)
	if all.Code != http.StatusOK || len(allRules) != 3 {
		t.Fatalf("database sheet list: %d, %d rules", all.Code, len(allRules))
	}
	response := request(t, h, http.MethodGet, "/v1/community/scoring-rules?query=wing&bggId=266192", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("search community: %d: %s", response.Code, response.Body.String())
	}
	var found []struct {
		GameName, Name string
		IsPublic       bool
	}
	decode(t, response, &found)
	if len(found) != 1 || found[0].GameName != "Wingspan" || found[0].Name != "Bird points" || !found[0].IsPublic {
		t.Fatalf("unexpected community rules: %#v", found)
	}
	invalid := request(t, h, http.MethodGet, "/v1/community/scoring-rules?bggId=nope", nil, "")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid filter: %d", invalid.Code)
	}
}

func TestScheduleGameWithoutScoringSheetAndAssignLater(t *testing.T) {
	h := httpapi.New(store.NewMemoryStore()).Handler()
	table := request(t, h, http.MethodPost, "/v1/tables", map[string]string{"name": "Saturday"}, "")
	var owner struct{ Code, HostToken string }
	decode(t, table, &owner)
	when := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	created := request(t, h, http.MethodPost, "/v1/tables/"+owner.Code+"/scheduled-games", map[string]any{"gameName": "Wingspan", "scheduledAt": when, "players": []string{"Ana", "Leo"}}, owner.HostToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("schedule: %d: %s", created.Code, created.Body.String())
	}
	var game struct {
		ID, RuleID, GameName string
		Players              []string
	}
	decode(t, created, &game)
	if game.GameName != "Wingspan" || game.RuleID != "" || len(game.Players) != 2 {
		t.Fatalf("unexpected scheduled game: %#v", game)
	}
	denied := request(t, h, http.MethodGet, "/v1/tables/"+owner.Code+"/scheduled-games", nil, "wrong")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("plan list exposed without host token: %d", denied.Code)
	}
	rule := request(t, h, http.MethodPost, "/v1/scoring-rules", map[string]any{"gameName": "Wingspan", "name": "Birds", "fields": []map[string]any{{"name": "Birds", "kind": "counter", "pointsPerUnit": 1}}}, "")
	var ruleBody struct{ ID string }
	decode(t, rule, &ruleBody)
	linked := request(t, h, http.MethodPatch, "/v1/scheduled-games/"+game.ID+"/rule", map[string]string{"ruleId": ruleBody.ID}, owner.HostToken)
	if linked.Code != http.StatusOK {
		t.Fatalf("assign rule: %d: %s", linked.Code, linked.Body.String())
	}
	decode(t, linked, &game)
	if game.RuleID != ruleBody.ID {
		t.Fatalf("rule was not assigned: %#v", game)
	}
	session := request(t, h, http.MethodPost, "/v1/tables/"+owner.Code+"/sessions", map[string]any{"ruleId": ruleBody.ID, "players": []map[string]string{{"name": "Ana"}}}, owner.HostToken)
	var sessionBody struct{ ID string }
	decode(t, session, &sessionBody)
	started := request(t, h, http.MethodPatch, "/v1/scheduled-games/"+game.ID+"/session", map[string]string{"sessionId": sessionBody.ID}, owner.HostToken)
	if started.Code != http.StatusOK {
		t.Fatalf("attach session: %d: %s", started.Code, started.Body.String())
	}
	var startedGame struct{ SessionID string }
	decode(t, started, &startedGame)
	if startedGame.SessionID != sessionBody.ID {
		t.Fatalf("game was not started: %#v", startedGame)
	}
}

func request(t *testing.T, h http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Table-Token", token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func requestAuth(t *testing.T, h http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	return requestBoth(t, h, method, path, body, token, "")
}

func requestBoth(t *testing.T, h http.Handler, method, path string, body any, token, hostToken string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Table-Token", hostToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func decode(t *testing.T, response *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(dst); err != nil {
		t.Fatal(err)
	}
}
