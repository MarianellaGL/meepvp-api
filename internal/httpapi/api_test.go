package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

	updated := request(t, h, http.MethodPut, "/v1/sessions/"+sessionBody.ID+"/scores", map[string]any{"values": map[string]any{sessionBody.Players[0].ID: map[string]int{ruleBody.Fields[0].ID: 12}}}, "")
	if updated.Code != http.StatusOK {
		t.Fatalf("update scores: got %d", updated.Code)
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
