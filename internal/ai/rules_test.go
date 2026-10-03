package ai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAnswerRuleSendsNumberedPagesAndKeepsOnlyRealCitations(t *testing.T) {
	var sent string
	client := &Client{key: "test-key", model: "gpt-4o-mini", endpoint: "https://example.test/v1/responses",
		http: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(r.Body)
			sent = string(body)
			return modelResponse(`{"found":true,"answer":"Gana quien tenga más eventos.","pages":[2,9,2]}`), nil
		})}}
	answer, err := client.AnswerRule(context.Background(), "Everdell", "¿Cómo se desempata?", []string{"Setup", "In case of a tie, the player with the most events wins."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent, `[Página 2]`) || !strings.Contains(sent, "most events") {
		t.Fatalf("rulebook pages were not sent numbered: %s", sent)
	}
	if !answer.Found || len(answer.Pages) != 1 || answer.Pages[0] != 2 {
		t.Fatalf("invented or repeated pages kept: %#v", answer)
	}
}

func TestAnswerWithoutRealCitationsIsNotFound(t *testing.T) {
	for _, reply := range []string{
		`{"found":true,"answer":"Inventado.","pages":[40]}`,
		`{"found":true,"answer":"Sin cita.","pages":[]}`,
		`{"found":false,"answer":"","pages":[]}`,
	} {
		client := &Client{key: "test-key", model: "gpt-4o-mini", endpoint: "https://example.test/v1/responses",
			http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return modelResponse(reply), nil })}}
		answer, err := client.AnswerRule(context.Background(), "Everdell", "¿Cómo se desempata?", []string{"Setup"})
		if err != nil {
			t.Fatal(err)
		}
		if answer.Found || answer.Answer != "" || len(answer.Pages) != 0 {
			t.Fatalf("ungrounded answer accepted for %s: %#v", reply, answer)
		}
	}
}
