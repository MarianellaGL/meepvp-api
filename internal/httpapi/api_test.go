package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	finished := request(t, h, http.MethodPost, "/v1/sessions/"+sessionBody.ID+"/finish", nil, "")
	if finished.Code != http.StatusOK {
		t.Fatalf("finish session: got %d", finished.Code)
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
		Status string `json:"status"`
		Totals []struct {
			PlayerID string `json:"playerId"`
			Total    int    `json:"total"`
		} `json:"totals"`
	}
	decode(t, reopened, &reopenedBody)
	if reopenedBody.Status != "active" || len(reopenedBody.Totals) != 2 || reopenedBody.Totals[0].Total != 12 || reopenedBody.Totals[1].Total != 8 {
		t.Fatalf("reopen lost saved scores: %#v", reopenedBody)
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
	for _, rule := range []map[string]any{
		{"gameName": "Wingspan", "name": "Bird points", "bggId": 266192, "isPublic": true},
		{"gameName": "Wingspan", "name": "Private draft", "bggId": 266192, "isPublic": false},
		{"gameName": "Azul", "name": "Tile points", "bggId": 230802, "isPublic": true},
	} {
		rule["fields"] = []map[string]any{{"name": "Points", "kind": "counter", "pointsPerUnit": 1}}
		response := request(t, h, http.MethodPost, "/v1/scoring-rules", rule, "")
		if response.Code != http.StatusCreated {
			t.Fatalf("create rule: %d: %s", response.Code, response.Body.String())
		}
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
func decode(t *testing.T, response *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(dst); err != nil {
		t.Fatal(err)
	}
}
