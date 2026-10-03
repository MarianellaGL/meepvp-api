package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"tablescore-api/internal/ai"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/httpapi"
	"tablescore-api/internal/store"
)

func fakeModel(calls *int, reply string) *ai.Client {
	return ai.New("test-key", "gpt-4o-mini", "https://example.test/v1/responses", &http.Client{Transport: rulebookTransport(func(*http.Request) (*http.Response, error) {
		*calls++
		text, _ := json.Marshal(reply)
		body := `{"status":"completed","output":[{"content":[{"type":"output_text","text":` + string(text) + `}]}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})})
}

func linkedEverdell(t *testing.T) *store.MemoryStore {
	t.Helper()
	repo := store.NewMemoryStore()
	book := domain.Rulebook{ID: "rule-book:en:everdell", Source: "rule-book.org", SourceID: "everdell", Name: "Everdell Rulebook", Language: "en", PDFURL: "https://cdn.1j1ju.com/medias/a.pdf"}
	require.NoError(t, repo.SaveRulebooks([]domain.Rulebook{book}))
	require.NoError(t, repo.LinkRulebook(book.ID, 199792))
	require.NoError(t, repo.SaveRulebookPages(book.ID, []string{"Setup the board.", "In case of a tie, the player with the most events wins."}))
	return repo
}

func TestAskRuleCitesRulebookPagesAndCachesAnswers(t *testing.T) {
	calls := 0
	model := fakeModel(&calls, `{"found":true,"answer":"Gana quien tenga más eventos.","pages":[2]}`)
	h := httpapi.New(linkedEverdell(t)).WithAIClient(model).Handler()
	var answer struct {
		Found     bool   `json:"found"`
		Answer    string `json:"answer"`
		Citations []struct {
			Page int    `json:"page"`
			Text string `json:"text"`
		} `json:"citations"`
		Rulebook domain.Rulebook `json:"rulebook"`
	}
	w := request(t, h, "POST", "/v1/games/199792/ask", map[string]string{"question": "¿Cómo se desempata?"}, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	decode(t, w, &answer)
	require.True(t, answer.Found)
	require.Equal(t, "Gana quien tenga más eventos.", answer.Answer)
	require.Len(t, answer.Citations, 1)
	require.Equal(t, 2, answer.Citations[0].Page)
	require.Contains(t, answer.Citations[0].Text, "most events wins")
	require.Equal(t, "rule-book:en:everdell", answer.Rulebook.ID)

	// Same question with other casing and accents: answered from the cache.
	w = request(t, h, "POST", "/v1/games/199792/ask", map[string]string{"question": "¿COMO se desempata?"}, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 1, calls)
}

func TestAskRuleRejectsInventedPages(t *testing.T) {
	calls := 0
	model := fakeModel(&calls, `{"found":true,"answer":"Algo inventado.","pages":[40]}`)
	h := httpapi.New(linkedEverdell(t)).WithAIClient(model).Handler()
	w := request(t, h, "POST", "/v1/games/199792/ask", map[string]string{"question": "¿Cuántas cartas se roban?"}, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, `false`, extractField(t, w.Body.Bytes(), "found"))
	require.JSONEq(t, `[]`, extractField(t, w.Body.Bytes(), "citations"))
}

func TestAskRuleValidatesInputAndAvailability(t *testing.T) {
	calls := 0
	model := fakeModel(&calls, `{"found":false,"answer":"","pages":[]}`)
	h := httpapi.New(linkedEverdell(t)).WithAIClient(model).Handler()
	require.Equal(t, 400, request(t, h, "POST", "/v1/games/199792/ask", map[string]string{"question": "?"}, "").Code)
	require.Equal(t, 400, request(t, h, "POST", "/v1/games/0/ask", map[string]string{"question": "¿Cómo se gana?"}, "").Code)
	require.Equal(t, 404, request(t, h, "POST", "/v1/games/13/ask", map[string]string{"question": "¿Cómo se gana?"}, "").Code)
	disabled := httpapi.New(linkedEverdell(t)).WithAIClient(ai.New("", "", "", nil)).Handler()
	require.Equal(t, 503, request(t, disabled, "POST", "/v1/games/199792/ask", map[string]string{"question": "¿Cómo se gana?"}, "").Code)
	require.Equal(t, 0, calls)
}

func extractField(t *testing.T, body []byte, field string) string {
	t.Helper()
	var object map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &object))
	return string(object[field])
}
