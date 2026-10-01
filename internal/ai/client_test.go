package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tablescore-api/internal/bgg"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func modelResponse(text string) *http.Response {
	content, _ := json.Marshal(text)
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"status":"completed","output":[{"content":[{"type":"output_text","text":` + string(content) + `}]}]}`))}
}

func TestSuggestScoringUsesStructuredOutputAndValidatesProposal(t *testing.T) {
	client := &Client{key: "test-key", model: "gpt-4o-mini", endpoint: "https://example.test/v1/responses"}
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["store"] != false {
			t.Fatal("AI response must not be stored")
		}
		if instructions, _ := body["instructions"].(string); !strings.Contains(instructions, "nombres de los campos y todas las notas en español") {
			t.Fatal("AI prompt must translate field names and notes to Spanish")
		}
		format := body["text"].(map[string]any)["format"].(map[string]any)
		if format["type"] != "json_schema" || format["strict"] != true {
			t.Fatal("structured output missing")
		}
		return modelResponse(`{"found":true,"gameName":"Juego","fields":[{"name":"Monedas","kind":"counter","pointsPerUnit":2}],"notes":[]}`), nil
	})}
	suggestion, err := client.SuggestScoring(context.Background(), "Juego", "Cada moneda vale 2 puntos", nil)
	if err != nil {
		t.Fatal(err)
	}
	if suggestion == nil || suggestion.Source != "ai" || len(suggestion.Fields) != 1 || suggestion.Fields[0].PointsPerUnit != 2 {
		t.Fatalf("unexpected suggestion: %#v", suggestion)
	}
}

func TestScoringEvidenceKeepsRulesFromMiddleOfLongRulebook(t *testing.T) {
	text := "Viticulture Essential Edition\n" + strings.Repeat("setup and worker placement. ", 450) +
		"Victory points are tracked during the game. Scoring uses the victory point track.\n" +
		strings.Repeat("appendix and credits. ", 450)
	evidence := scoringEvidence(text)
	if !strings.Contains(evidence, "Victory points are tracked during the game") {
		t.Fatal("middle scoring evidence was lost")
	}
	if len([]rune(evidence)) >= len([]rune(text)) {
		t.Fatal("long rulebook was not bounded")
	}
}

func TestRankGamesRejectsInventedIDs(t *testing.T) {
	client := &Client{key: "test-key", model: "gpt-4o-mini", endpoint: "https://example.test/v1/responses",
		http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return modelResponse(`{"ids":[2,999]}`), nil })}}
	games := []bgg.CollectionGame{{BGGID: 1, Name: "Uno"}, {BGGID: 2, Name: "Dos"}}
	ordered := client.RankGames(context.Background(), "Dos", games)
	if ordered[0].BGGID != 1 {
		t.Fatalf("invented ID changed BGG results: %#v", ordered)
	}
}

func TestCodexCLIUsesStructuredOutputForScoring(t *testing.T) {
	t.Setenv("DATABASE_URL", "do-not-pass-to-ai")
	t.Setenv("OPENAI_API_KEY", "server-test-key")
	t.Setenv("CODEX_API_KEY", "")
	bin := filepath.Join(t.TempDir(), "fake-codex")
	script := `#!/bin/sh
[ -z "$DATABASE_URL" ] || exit 2
[ -n "$CODEX_HOME" ] || exit 3
[ "$CODEX_API_KEY" = "server-test-key" ] || exit 4
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then
    shift
    printf '%s' '{"found":true,"gameName":"Juego","fields":[{"name":"Monedas","kind":"counter","pointsPerUnit":2}],"notes":[]}' > "$1"
    exit 0
  fi
  shift
done
exit 1
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	client := &Client{provider: "codex-cli", cliPath: bin}
	suggestion, err := client.SuggestScoring(context.Background(), "Juego", "Cada moneda vale 2 puntos", nil)
	if err != nil {
		t.Fatal(err)
	}
	if suggestion == nil || suggestion.Source != "ai" || suggestion.Fields[0].PointsPerUnit != 2 {
		t.Fatalf("unexpected suggestion: %#v", suggestion)
	}
}

func TestWorkerRoutesScoringOverUnixSocket(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-codex")
	script := `#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then
    shift
    printf '%s' '{"found":true,"gameName":"Juego","fields":[{"name":"Monedas","kind":"counter","pointsPerUnit":2}],"notes":[]}' > "$1"
    exit 0
  fi
  shift
done
exit 1
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	socketDir, err := os.MkdirTemp("/tmp", "meeple-ai-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(socketDir) }()
	socket := filepath.Join(socketDir, "worker.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ServeWorker(ctx, socket, bin) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not open socket")
		}
		time.Sleep(10 * time.Millisecond)
	}
	client := &Client{provider: "codex-worker", workerSocket: socket}
	suggestion, err := client.SuggestScoring(context.Background(), "Juego", "Cada moneda vale 2 puntos", nil)
	if err != nil {
		t.Fatal(err)
	}
	if suggestion == nil || suggestion.Fields[0].PointsPerUnit != 2 {
		t.Fatalf("unexpected suggestion: %#v", suggestion)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWorkerFallsBackToResponsesWhenUnavailable(t *testing.T) {
	client := &Client{provider: "codex-worker", workerSocket: filepath.Join(t.TempDir(), "missing.sock"),
		key: "fallback-key", model: "gpt-4o-mini", endpoint: "https://example.test/v1/responses"}
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer fallback-key" {
			t.Fatal("fallback did not use the server key")
		}
		return modelResponse(`{"found":true,"gameName":"Juego","fields":[{"name":"Monedas","kind":"counter","pointsPerUnit":2}],"notes":[]}`), nil
	})}
	suggestion, err := client.SuggestScoring(context.Background(), "Juego", "Cada moneda vale 2 puntos de victoria.", nil)
	if err != nil || suggestion == nil || len(suggestion.Fields) != 1 {
		t.Fatalf("fallback suggestion: %#v, %v", suggestion, err)
	}
}

func TestWorkerFallsBackToResponsesWhenCodexFails(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake-codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cli := &Client{provider: "codex-cli", cliPath: bin}
	responses := &Client{key: "test-key", endpoint: "https://example.test/v1/responses",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Authorization") != "Bearer test-key" {
				t.Fatal("fallback did not use the worker key")
			}
			return modelResponse(`{"found":true,"gameName":"Schotten Totten","fields":[{"name":"Piedras reclamadas","kind":"counter","pointsPerUnit":1}],"notes":["Revisá la condición de victoria."]}`), nil
		})}}
	client := &Client{key: "test-key", model: "gpt-4o-mini"}
	payload, _ := json.Marshal(map[string]any{"model": client.model, "instructions": "Responde en español", "input": "Schotten Totten", "text": map[string]any{"format": map[string]any{"schema": suggestionSchema}}})
	result, err := completeWithFallback(context.Background(), payload, cli, responses)
	if err != nil || result.Status != "completed" {
		t.Fatalf("fallback response: %#v, %v", result, err)
	}
}

func TestResponsesRateLimitKeepsProviderCodeAndDelay(t *testing.T) {
	client := &Client{key: "test-key", endpoint: "https://example.test/v1/responses",
		http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"17"}},
				Body: io.NopCloser(strings.NewReader(`{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded"}}`))}, nil
		})}}
	_, err := client.completeResponses(context.Background(), []byte(`{}`))
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != 429 || providerErr.Code != "rate_limit_exceeded" || providerErr.RetryAfterSeconds != 17 || providerErr.QuotaExhausted() {
		t.Fatalf("rate limit classification: %#v, %v", providerErr, err)
	}
	providerErr.Code = "credit_balance_exhausted"
	if !providerErr.QuotaExhausted() {
		t.Fatal("prepaid credit exhaustion must not be treated as a retryable rate limit")
	}
}

func TestCodexFailureCategoryDoesNotExposeStderr(t *testing.T) {
	if got := cliFailureCategory("Error 429: quota exceeded for sk-secret-value"); got != "quota_or_rate_limit" {
		t.Fatalf("unexpected category: %q", got)
	}
}
