package httpapi_test

import (
	"bytes"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"tablescore-api/internal/httpapi"
	"tablescore-api/internal/store"
)

func signupToken(t *testing.T, h http.Handler, username string) string {
	t.Helper()
	response := request(t, h, http.MethodPost, "/v1/auth/signup", map[string]string{"username": username, "password": "correct horse battery staple"}, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("signup %s: %d %s", username, response.Code, response.Body.String())
	}
	var account struct{ Token string }
	decode(t, response, &account)
	return account.Token
}

func TestAccountRecoversOwnedTablesAndJoinedPlayers(t *testing.T) {
	h := httpapi.New(store.NewMemoryStore()).Handler()
	ana := signupToken(t, h, "ana")
	leo := signupToken(t, h, "leo")
	zoe := signupToken(t, h, "zoe")
	max := signupToken(t, h, "max")
	created := requestAuth(t, h, http.MethodPost, "/v1/tables", map[string]string{"name": "Campaign"}, ana)
	if created.Code != http.StatusCreated {
		t.Fatalf("create table: %d %s", created.Code, created.Body.String())
	}
	var table struct{ Code, HostToken string }
	decode(t, created, &table)
	owned := requestAuth(t, h, http.MethodGet, "/v1/me/tables", nil, ana)
	var tables []struct{ Code, HostToken string }
	decode(t, owned, &tables)
	if len(tables) != 1 || tables[0].Code != table.Code || tables[0].HostToken != table.HostToken {
		t.Fatalf("owner cannot recover table: %#v", tables)
	}
	other := requestAuth(t, h, http.MethodGet, "/v1/me/tables", nil, leo)
	decode(t, other, &tables)
	if len(tables) != 0 {
		t.Fatalf("other account can see host token: %#v", tables)
	}

	rule := request(t, h, http.MethodPost, "/v1/scoring-rules", map[string]any{"gameName": "Campaign", "name": "Points", "fields": []map[string]any{{"name": "Coins", "kind": "counter", "pointsPerUnit": 2}}}, "")
	var scoring struct {
		ID     string
		Fields []struct{ ID string }
	}
	decode(t, rule, &scoring)
	game := requestBoth(t, h, http.MethodPost, "/v1/tables/"+table.Code+"/sessions", map[string]any{"ruleId": scoring.ID, "players": []map[string]string{{"name": "Ana"}, {"name": "Zoe"}}}, ana, table.HostToken)
	var session struct {
		ID      string
		Players []struct{ ID string }
	}
	decode(t, game, &session)
	precreated := requestAuth(t, h, http.MethodPost, "/v1/sessions/"+session.ID+"/players", map[string]string{"name": "Zoe"}, zoe)
	if precreated.Code != http.StatusOK {
		t.Fatalf("precreated player could not join: %d %s", precreated.Code, precreated.Body.String())
	}
	zoeSessions := requestAuth(t, h, http.MethodGet, "/v1/me/sessions", nil, zoe)
	var zoeLinked []struct{ MyPlayerID string }
	decode(t, zoeSessions, &zoeLinked)
	if len(zoeLinked) != 1 || zoeLinked[0].MyPlayerID != session.Players[1].ID {
		t.Fatalf("precreated player not linked: %#v", zoeLinked)
	}
	if stolen := requestAuth(t, h, http.MethodPost, "/v1/sessions/"+session.ID+"/players", map[string]string{"name": "Zoe"}, max); stolen.Code != http.StatusConflict {
		t.Fatalf("another account claimed Zoe: %d", stolen.Code)
	}
	join := requestAuth(t, h, http.MethodPost, "/v1/sessions/"+session.ID+"/players", map[string]string{"name": "Leo"}, leo)
	if join.Code != http.StatusOK {
		t.Fatalf("join account: %d %s", join.Code, join.Body.String())
	}
	var joined struct{ Players []struct{ ID, Name string } }
	decode(t, join, &joined)
	if len(joined.Players) != 3 {
		t.Fatalf("joined players: %#v", joined.Players)
	}
	leoSessions := requestAuth(t, h, http.MethodGet, "/v1/me/sessions", nil, leo)
	var linked []struct{ ID, MyPlayerID string }
	decode(t, leoSessions, &linked)
	if len(linked) != 1 || linked[0].ID != session.ID || linked[0].MyPlayerID != joined.Players[2].ID {
		t.Fatalf("joined player not linked to account: %#v", linked)
	}
	if duplicate := requestAuth(t, h, http.MethodPost, "/v1/sessions/"+session.ID+"/players", map[string]string{"name": "Ana"}, leo); duplicate.Code != http.StatusConflict {
		t.Fatalf("existing player could be claimed: %d", duplicate.Code)
	}
	if repeat := requestAuth(t, h, http.MethodPost, "/v1/sessions/"+session.ID+"/players", map[string]string{"name": "Leo"}, leo); repeat.Code != http.StatusOK {
		t.Fatalf("same player retry: %d", repeat.Code)
	}

	path := "/v1/sessions/" + session.ID
	request(t, h, http.MethodPatch, path+"/scores", map[string]any{"playerId": session.Players[0].ID, "fieldId": scoring.Fields[0].ID, "value": 3}, "")
	request(t, h, http.MethodPost, path+"/points", map[string]any{"playerId": session.Players[0].ID, "delta": 4}, "")
	paused := request(t, h, http.MethodPost, path+"/pause", nil, table.HostToken)
	var state struct {
		DurationSeconds int64
		ManualPoints    map[string]int
		Values          map[string]map[string]int
		Totals          []struct {
			PlayerID string
			Total    int
		}
	}
	decode(t, paused, &state)
	if state.ManualPoints[session.Players[0].ID] != 4 || state.Values[session.Players[0].ID][scoring.Fields[0].ID] != 3 || state.Totals[0].Total != 10 {
		t.Fatalf("points changed on pause: %#v", state)
	}
	pausedDuration := state.DurationSeconds
	current := request(t, h, http.MethodGet, path, nil, "")
	decode(t, current, &state)
	if state.DurationSeconds != pausedDuration || state.Totals[0].Total != 10 {
		t.Fatalf("paused session lost score or changed duration: %#v", state)
	}
	resumed := request(t, h, http.MethodPost, path+"/resume", nil, tablesToken(t, h, ana, table.Code))
	decode(t, resumed, &state)
	if state.Totals[0].Total != 10 {
		t.Fatalf("points changed on resume: %#v", state)
	}
}

func tablesToken(t *testing.T, h http.Handler, token, code string) string {
	t.Helper()
	response := requestAuth(t, h, http.MethodGet, "/v1/me/tables", nil, token)
	var tables []struct{ Code, HostToken string }
	decode(t, response, &tables)
	for _, table := range tables {
		if table.Code == code {
			return table.HostToken
		}
	}
	t.Fatal("table not recovered")
	return ""
}

func TestAvatarBelongsOnlyToSignedInUser(t *testing.T) {
	h := httpapi.New(store.NewMemoryStore()).Handler()
	ana := signupToken(t, h, "ana")
	leo := signupToken(t, h, "leo")
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	upload := func(token string, data []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "avatar.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPut, "/v1/me/avatar", &body)
		r.Header.Set("Content-Type", writer.FormDataContentType())
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		h.ServeHTTP(response, r)
		return response
	}
	if response := upload("", pngBytes.Bytes()); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous upload: %d", response.Code)
	}
	if response := upload(ana, []byte("not image")); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid image: %d", response.Code)
	}
	if response := upload(ana, pngBytes.Bytes()); response.Code != http.StatusOK {
		t.Fatalf("avatar upload: %d %s", response.Code, response.Body.String())
	}
	if response := requestAuth(t, h, http.MethodGet, "/v1/me/avatar", nil, leo); response.Code != http.StatusNotFound {
		t.Fatalf("other account sees avatar: %d", response.Code)
	}
	photo := requestAuth(t, h, http.MethodGet, "/v1/me/avatar", nil, ana)
	if photo.Code != http.StatusOK || photo.Header().Get("Content-Type") != "image/png" || !bytes.Equal(photo.Body.Bytes(), pngBytes.Bytes()) {
		t.Fatalf("avatar download: %d", photo.Code)
	}
	if response := requestAuth(t, h, http.MethodDelete, "/v1/me/avatar", nil, ana); response.Code != http.StatusNoContent {
		t.Fatalf("avatar delete: %d", response.Code)
	}
	if response := requestAuth(t, h, http.MethodGet, "/v1/me/avatar", nil, ana); response.Code != http.StatusNotFound {
		t.Fatalf("deleted avatar still exists: %d", response.Code)
	}
}
