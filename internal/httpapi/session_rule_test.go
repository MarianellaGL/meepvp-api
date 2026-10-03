package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"tablescore-api/internal/httpapi"
	"tablescore-api/internal/store"
)

func TestPlayersGetTheGamesSheetEvenWhenItIsPrivate(t *testing.T) {
	h := httpapi.New(store.NewMemoryStore()).Handler()
	host := signUp(t, h, "anfitriona")
	var rule struct{ ID string }
	decode(t, requestAuth(t, h, http.MethodPost, "/v1/scoring-rules", map[string]any{"gameName": "Brass: Birmingham", "name": "Privada", "fields": []map[string]any{{"name": "Enlaces", "kind": "manual", "pointsPerUnit": 0}}}, host), &rule)
	var table struct{ Code, HostToken string }
	decode(t, request(t, h, http.MethodPost, "/v1/tables", map[string]string{"name": "Viernes"}, ""), &table)
	var session struct{ ID string }
	decode(t, request(t, h, http.MethodPost, "/v1/tables/"+table.Code+"/sessions", map[string]any{"ruleId": rule.ID, "players": []map[string]string{{"name": "Mariana"}}}, table.HostToken), &session)

	// A guest without an account cannot list the private sheet, but gets it with the game.
	var listed []struct{ ID string }
	decode(t, request(t, h, http.MethodGet, "/v1/scoring-rules", nil, ""), &listed)
	require.Empty(t, listed)
	var game struct {
		Rule struct {
			ID     string `json:"id"`
			Fields []struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"rule"`
	}
	w := request(t, h, http.MethodPost, "/v1/sessions/"+session.ID+"/players", map[string]string{"name": "Lucía"}, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	decode(t, w, &game)
	require.Equal(t, rule.ID, game.Rule.ID)
	require.Equal(t, "Enlaces", game.Rule.Fields[0].Name)
}
