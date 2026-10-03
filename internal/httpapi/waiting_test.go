package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"tablescore-api/internal/httpapi"
	"tablescore-api/internal/store"
)

type waitingSession struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Players []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Joined bool   `json:"joined"`
	} `json:"players"`
}

func TestGameWaitsForListedPlayersAndHostCanStartEarly(t *testing.T) {
	h := httpapi.New(store.NewMemoryStore()).Handler()
	var table struct{ Code, HostToken string }
	decode(t, request(t, h, http.MethodPost, "/v1/tables", map[string]string{"name": "Viernes"}, ""), &table)
	var rule struct{ ID string }
	decode(t, request(t, h, http.MethodPost, "/v1/scoring-rules", map[string]any{"gameName": "Brass: Birmingham", "name": "Base", "fields": []map[string]any{{"name": "Enlaces", "kind": "manual", "pointsPerUnit": 0}}}, ""), &rule)
	create := func(wait bool) waitingSession {
		w := request(t, h, http.MethodPost, "/v1/tables/"+table.Code+"/sessions", map[string]any{"ruleId": rule.ID, "players": []map[string]string{{"name": "Mariana"}, {"name": "Lucía"}}, "waitForPlayers": wait}, table.HostToken)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		var session waitingSession
		decode(t, w, &session)
		return session
	}

	session := create(true)
	require.Equal(t, "waiting", session.Status)
	var current waitingSession
	w := request(t, h, http.MethodGet, "/v1/tables/"+table.Code+"/current-session", nil, "")
	require.Equal(t, http.StatusOK, w.Code, "players find a waiting game by its table code")
	decode(t, w, &current)
	require.Equal(t, session.ID, current.ID)
	require.True(t, session.Players[0].Joined, "the host is already at the table")
	require.False(t, session.Players[1].Joined)
	score := map[string]any{"playerId": session.Players[0].ID, "fieldId": "x", "value": 3}
	require.Equal(t, http.StatusBadRequest, request(t, h, http.MethodPatch, "/v1/sessions/"+session.ID+"/scores", score, "").Code, "no scoring while waiting")

	w = request(t, h, http.MethodPost, "/v1/sessions/"+session.ID+"/players", map[string]string{"name": "lucía"}, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	decode(t, w, &session)
	require.Equal(t, "active", session.Status, "the game starts when the last listed player joins")
	require.Len(t, session.Players, 2)

	early := create(true)
	require.Equal(t, http.StatusForbidden, request(t, h, http.MethodPost, "/v1/sessions/"+early.ID+"/start", nil, "").Code)
	w = request(t, h, http.MethodPost, "/v1/sessions/"+early.ID+"/start", nil, table.HostToken)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	decode(t, w, &early)
	require.Equal(t, "active", early.Status)
	require.Equal(t, http.StatusBadRequest, request(t, h, http.MethodPost, "/v1/sessions/"+early.ID+"/start", nil, table.HostToken).Code)

	require.Equal(t, "active", create(false).Status, "older apps keep starting right away")
}
