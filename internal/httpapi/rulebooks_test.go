package httpapi_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/httpapi"
	"tablescore-api/internal/rulebooks"
	"tablescore-api/internal/store"
)

type rulebookTransport func(*http.Request) (*http.Response, error)

func (f rulebookTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCatalogFallbackAndScoringSource(t *testing.T) {
	repo := store.NewMemoryStore()
	book := domain.Rulebook{ID: "rule-book:en:catan", Source: "rule-book.org", SourceID: "catan", Name: "Catan Rulebook", Language: "en", Edition: "Base game", PDFURL: "https://cdn.1j1ju.com/medias/a.pdf"}
	require.NoError(t, repo.SaveRulebooks([]domain.Rulebook{book}))
	client := rulebooks.NewWithTransport(rulebookTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
	}))
	h := httpapi.New(repo).WithRulebookClient(client).Handler()
	w := request(t, h, "GET", "/v1/rulebooks?query=Catan&language=en", nil, "")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"cached":true`)
	require.Contains(t, w.Body.String(), book.ID)
	require.Equal(t, 502, request(t, h, "GET", "/v1/rulebooks?query=unknown", nil, "").Code)
	require.Equal(t, 400, request(t, h, "GET", "/v1/rulebooks?language=es", nil, "").Code)
	require.Equal(t, 404, request(t, h, "POST", "/v1/rulebooks/missing/extract", nil, "").Code)
	require.Equal(t, 502, request(t, h, "POST", "/v1/rulebooks/rule-book:en:catan/extract", nil, "").Code)
	input := map[string]any{"rulebookId": book.ID, "gameName": "Catan", "name": "Reviewed", "fields": []map[string]any{{"name": "Settlements", "kind": "counter", "pointsPerUnit": 1}}}
	w = request(t, h, "POST", "/v1/scoring-rules", input, "")
	require.Equal(t, 201, w.Code, w.Body.String())
	var rule domain.ScoringRule
	decode(t, w, &rule)
	saved, err := repo.GetRule(rule.ID)
	require.NoError(t, err)
	require.Equal(t, book.ID, saved.RulebookID)
	input["rulebookId"] = "missing"
	require.Equal(t, 404, request(t, h, "POST", "/v1/scoring-rules", input, "").Code)
}

func TestCatalogSearchPersistsMetadataWithoutChangingReviewedEdition(t *testing.T) {
	repo := store.NewMemoryStore()
	book := domain.Rulebook{ID: "rule-book:en:catan", Source: "rule-book.org", SourceID: "catan", Name: "Catan Rulebook", Language: "en", Edition: "Base game"}
	require.NoError(t, repo.SaveRulebooks([]domain.Rulebook{book}))
	client := rulebooks.NewWithTransport(rulebookTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[{"id":"catan","name":"Catan Rulebook","language":"en","link":"https://cdn.1j1ju.com/medias/new.pdf"}]}`))}, nil
	}))
	h := httpapi.New(repo).WithRulebookClient(client).Handler()
	w := request(t, h, "GET", "/v1/rulebooks?query=Catan", nil, "")
	require.Equal(t, 200, w.Code)
	saved, err := repo.GetRulebook(book.ID)
	require.NoError(t, err)
	require.Equal(t, "Base game", saved.Edition)
	require.Equal(t, "https://cdn.1j1ju.com/medias/new.pdf", saved.PDFURL)
}

func TestScoringDraftEndpointReportsUnavailableAssistant(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	h := httpapi.New(store.NewMemoryStore()).Handler()
	input := map[string]any{"gameName": "Viticulture", "text": "Victory points are tracked during the game. The game ends according to the victory point track."}
	require.Equal(t, 503, request(t, h, "POST", "/v1/ai/scoring-suggestion", input, "").Code)
	input["text"] = "short"
	require.Equal(t, 400, request(t, h, "POST", "/v1/ai/scoring-suggestion", input, "").Code)
}
