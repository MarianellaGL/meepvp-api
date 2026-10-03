package httpapi_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"tablescore-api/internal/bgg"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/httpapi"
	"tablescore-api/internal/rulebooks"
	"tablescore-api/internal/store"
)

func TestDiscoveryCombinesSourcesAndKeepsPartialResults(t *testing.T) {
	repo := store.NewMemoryStore()
	_, err := repo.CreateRule(domain.ScoringRule{BGGID: 13, GameName: "Catan", Name: "Puntos", IsPublic: true, Fields: []domain.ScoreField{{Name: "Aldeas", Kind: domain.FieldKindCounter, PointsPerUnit: 1}}})
	require.NoError(t, err)
	gameTransport := rulebookTransport(func(r *http.Request) (*http.Response, error) {
		body := `<items><item type="boardgame" id="13"><name type="primary" value="Catan"/><yearpublished value="1995"/></item></items>`
		if r.URL.Path == "/thing" {
			body = `<items/>`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	bookTransport := rulebookTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[{"id":"catan","name":"Catan Rulebook","language":"en","link":"https://cdn.1j1ju.com/medias/catan.pdf"}]}`)), Header: make(http.Header)}, nil
	})
	h := httpapi.New(repo, bgg.New("https://bgg.example", "", &http.Client{Transport: gameTransport})).WithRulebookClient(rulebooks.NewWithTransport(bookTransport)).Handler()
	w := request(t, h, "GET", "/v1/discovery/search?query=Catan", nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var result struct {
		Status string `json:"status"`
		Games  []struct {
			BGGID int `json:"bggId"`
		} `json:"games"`
		CommunityRules     []domain.ScoringRule `json:"communityRules"`
		Rulebooks          []domain.Rulebook    `json:"rulebooks"`
		UnavailableSources []string             `json:"unavailableSources"`
	}
	decode(t, w, &result)
	require.Equal(t, "ready", result.Status)
	require.Equal(t, 13, result.Games[0].BGGID)
	require.Len(t, result.CommunityRules, 1)
	require.Len(t, result.Rulebooks, 1)
	require.Empty(t, result.UnavailableSources)
	require.Equal(t, 400, request(t, h, "GET", "/v1/discovery/search?query=C", nil, "").Code)

	failedBGG := rulebookTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable")), Header: make(http.Header)}, nil
	})
	h = httpapi.New(repo, bgg.New("https://bgg.example", "", &http.Client{Transport: failedBGG})).WithRulebookClient(rulebooks.NewWithTransport(bookTransport)).Handler()
	w = request(t, h, "GET", "/v1/discovery/search?query=Catan", nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	decode(t, w, &result)
	require.Equal(t, []string{"bgg"}, result.UnavailableSources)
	require.Len(t, result.CommunityRules, 1)
	require.Len(t, result.Rulebooks, 1)

	processingBGG := rulebookTransport(func(*http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Retry-After", "8")
		return &http.Response{StatusCode: 202, Body: io.NopCloser(strings.NewReader("")), Header: header}, nil
	})
	h = httpapi.New(repo, bgg.New("https://bgg.example", "", &http.Client{Transport: processingBGG})).WithRulebookClient(rulebooks.NewWithTransport(bookTransport)).Handler()
	w = request(t, h, "GET", "/v1/discovery/search?query=Catan", nil, "")
	require.Equal(t, 202, w.Code, w.Body.String())
	var processing struct {
		Status            string               `json:"status"`
		RetryAfterSeconds int                  `json:"retryAfterSeconds"`
		CommunityRules    []domain.ScoringRule `json:"communityRules"`
	}
	decode(t, w, &processing)
	require.Equal(t, "processing", processing.Status)
	require.Equal(t, 8, processing.RetryAfterSeconds)
	require.Len(t, processing.CommunityRules, 1)
}

func TestDiscoveryCorrectsTyposFromRulebooksWithoutAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	repo := store.NewMemoryStore()
	_, err := repo.CreateRule(domain.ScoringRule{GameName: "Schotten Totten", Name: "Mojones", IsPublic: true, Fields: []domain.ScoreField{{Name: "Mojones", Kind: domain.FieldKindCounter, PointsPerUnit: 1}}})
	require.NoError(t, err)
	bggQueries := []string{}
	gameTransport := rulebookTransport(func(r *http.Request) (*http.Response, error) {
		body := `<items/>`
		if r.URL.Path != "/search" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		bggQueries = append(bggQueries, r.URL.Query().Get("query"))
		if r.URL.Query().Get("query") == "Schotten Totten" {
			body = `<items><item type="boardgame" id="2"><name type="primary" value="Schotten Totten 2"/></item><item type="boardgame" id="1"><name type="primary" value="Schotten Totten"/></item></items>`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	// rule-book.org's real answer to "shoten toten": the game plus fuzzy noise.
	bookTransport := rulebookTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"results":[]}`
		if r.URL.Query().Get("search") == "shoten toten" {
			body = `{"results":[
{"id":"ghost","name":"Ghost Stories: White Moon - Scénario Green Roots Plateau","language":"en","link":"https://cdn.1j1ju.com/medias/a.pdf"},
{"id":"schotten","name":"Schotten Totten Rulebook","language":"en","link":"https://cdn.1j1ju.com/medias/b.pdf"},
{"id":"schotten-2","name":"Schotten Totten 2 Rulebook","language":"en","link":"https://cdn.1j1ju.com/medias/c.pdf"},
{"id":"harry","name":"Harry Potter: Hogwarts Battle Rulebook","language":"en","link":"https://cdn.1j1ju.com/medias/d.pdf"}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	h := httpapi.New(repo, bgg.New("https://bgg.example", "", &http.Client{Transport: gameTransport})).WithRulebookClient(rulebooks.NewWithTransport(bookTransport)).Handler()

	w := request(t, h, "GET", "/v1/discovery/search?query=shoten%20toten", nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var result struct {
		Games []struct {
			BGGID int `json:"bggId"`
		} `json:"games"`
		CommunityRules []domain.ScoringRule `json:"communityRules"`
		Rulebooks      []domain.Rulebook    `json:"rulebooks"`
		SearchedAs     string               `json:"searchedAs"`
		SuggestedQuery string               `json:"suggestedQuery"`
	}
	decode(t, w, &result)
	require.Equal(t, "Schotten Totten", result.SearchedAs)
	require.Equal(t, []string{"Schotten Totten"}, bggQueries)
	require.Len(t, result.Games, 2)
	require.Equal(t, 1, result.Games[0].BGGID, "exact title first")
	require.Len(t, result.CommunityRules, 1)
	require.Len(t, result.Rulebooks, 2)
	require.Equal(t, "Schotten Totten Rulebook", result.Rulebooks[0].Name)
	noise, err := repo.FindRulebooks("Harry", "en")
	require.NoError(t, err)
	require.Empty(t, noise, "fuzzy noise must not enter the catalog")

	bggQueries = nil
	w = request(t, h, "GET", "/v1/discovery/search?query=zzqx", nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	result.SearchedAs, result.SuggestedQuery = "", ""
	decode(t, w, &result)
	require.Empty(t, result.SearchedAs)
	require.Empty(t, result.SuggestedQuery)
	require.Equal(t, []string{"zzqx"}, bggQueries, "nothing is searched again on the model's behalf")
}
