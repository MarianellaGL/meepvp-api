package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
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
