package bgg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCollectionNormalizesXMLAndUsesEnvironmentToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("username"); got != "Ana" {
			t.Fatalf("username = %q", got)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<items><item objectid="13"><name>Example Game</name><yearpublished value="2020"/><thumbnail>https://example.test/thumb.jpg</thumbnail><stats minplayers="2" maxplayers="4" playingtime="60"/></item></items>`))
	}))
	defer server.Close()

	result, err := New(server.URL, "secret", server.Client()).Collection(context.Background(), "Ana")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ready" || len(result.Games) != 1 || result.Games[0].Name != "Example Game" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestCollectionReturnsProcessingForAcceptedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	result, err := New(server.URL, "", server.Client()).Collection(context.Background(), "Ana")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "processing" || result.RetryAfterSeconds != 7 {
		t.Fatalf("unexpected result: %#v", result)
	}
}
