package httpapi_test

import (
	"bytes"
	"fmt"
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

func TestGameLinksExactRulebooksAndListsUsableSheets(t *testing.T) {
	repo := store.NewMemoryStore()
	searches := 0
	client := rulebooks.NewWithTransport(rulebookTransport(func(r *http.Request) (*http.Response, error) {
		searches++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[
{"id":"everdell","name":"Everdell Rulebook","language":"en","link":"https://cdn.1j1ju.com/medias/a.pdf"},
{"id":"everdell-pearlbrook","name":"Everdell: Pearlbrook Rulebook","language":"en","link":"https://cdn.1j1ju.com/medias/b.pdf"}]}`)), Header: make(http.Header)}, nil
	}))
	h := httpapi.New(repo).WithRulebookClient(client).Handler()
	ana := signUp(t, h, "ana")
	beto := signUp(t, h, "beto")
	fields := []map[string]any{{"name": "Points", "kind": "counter", "pointsPerUnit": 1}}
	for _, rule := range []map[string]any{
		{"gameName": "Everdell", "name": "Base", "bggId": 199792, "isPublic": true, "fields": fields},
		{"gameName": "Everdell", "name": "Ana draft", "bggId": 199792, "fields": fields},
		{"gameName": "Wingspan", "name": "Birds", "bggId": 266192, "isPublic": true, "fields": fields},
	} {
		require.Equal(t, 201, requestAuth(t, h, "POST", "/v1/scoring-rules", rule, ana).Code)
	}

	var game struct {
		BGGID     int `json:"bggId"`
		Rulebooks []struct {
			ID        string `json:"id"`
			BGGID     int    `json:"bggId"`
			TextReady bool   `json:"textReady"`
		} `json:"rulebooks"`
		ScoringRules []domain.ScoringRule `json:"scoringRules"`
	}
	w := requestAuth(t, h, "GET", "/v1/games/199792?name=Everdell", nil, ana)
	require.Equal(t, 200, w.Code, w.Body.String())
	decode(t, w, &game)
	require.Len(t, game.Rulebooks, 1, "an expansion is not the base game's rulebook")
	require.Equal(t, "rule-book:en:everdell", game.Rulebooks[0].ID)
	require.Equal(t, 199792, game.Rulebooks[0].BGGID)
	require.False(t, game.Rulebooks[0].TextReady)
	require.Len(t, game.ScoringRules, 2, "public sheet plus the owner's draft")

	// The link is stored: no name hint and no catalog search needed anymore.
	searches = 0
	w = requestAuth(t, h, "GET", "/v1/games/199792", nil, beto)
	require.Equal(t, 200, w.Code, w.Body.String())
	game.Rulebooks, game.ScoringRules = nil, nil
	decode(t, w, &game)
	require.Equal(t, 0, searches)
	require.Len(t, game.Rulebooks, 1)
	require.Len(t, game.ScoringRules, 1, "another account sees only the public sheet")

	require.Equal(t, 400, request(t, h, "GET", "/v1/games/nope", nil, "").Code)
	require.Equal(t, 400, request(t, h, "GET", "/v1/games/0", nil, "").Code)
}

func TestRulebookIsDownloadedAndReadOnce(t *testing.T) {
	repo := store.NewMemoryStore()
	book := domain.Rulebook{ID: "rule-book:en:everdell", Source: "rule-book.org", SourceID: "everdell", Name: "Everdell Rulebook", Language: "en", PDFURL: "https://cdn.1j1ju.com/medias/a.pdf"}
	require.NoError(t, repo.SaveRulebooks([]domain.Rulebook{book}))
	downloads := 0
	pdf := minimalPDF("The player with the most points wins")
	client := rulebooks.NewWithTransport(rulebookTransport(func(r *http.Request) (*http.Response, error) {
		downloads++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(pdf)), ContentLength: int64(len(pdf)), Header: make(http.Header)}, nil
	}))
	h := httpapi.New(repo).WithRulebookClient(client).Handler()
	for range 2 {
		w := request(t, h, "POST", "/v1/rulebooks/"+book.ID+"/extract", nil, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "most points wins")
	}
	require.Equal(t, 1, downloads)
	pages, err := repo.RulebookPages(book.ID)
	require.NoError(t, err)
	require.Len(t, pages, 1)
}

func signUp(t *testing.T, h http.Handler, username string) string {
	t.Helper()
	w := request(t, h, "POST", "/v1/auth/signup", map[string]string{"username": username, "password": "correct horse battery staple"}, "")
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var account struct {
		Token string `json:"token"`
	}
	decode(t, w, &account)
	return account.Token
}

func minimalPDF(line string) []byte {
	content := "BT /F1 12 Tf 72 720 Td (" + line + ") Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n")
	offsets := []int{}
	for index, object := range objects {
		offsets = append(offsets, output.Len())
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&output, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return output.Bytes()
}
