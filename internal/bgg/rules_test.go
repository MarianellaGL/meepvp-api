package bgg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRulesFindsGameRulesForumAndThreads(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("BGG token was not sent")
		}
		w.Header().Set("Content-Type", "application/xml")
		switch r.URL.Path {
		case "/forumlist":
			if r.URL.Query().Get("id") != "266192" || r.URL.Query().Get("type") != "thing" {
				t.Errorf("wrong forum list query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`<forums><forum id="1" title="General" numthreads="100"/><forum id="2740385" title="Rules" numthreads="728"/></forums>`))
		case "/forum":
			if r.URL.Query().Get("id") != "2740385" || r.URL.Query().Get("page") != "1" {
				t.Errorf("wrong forum query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`<forum id="2740385"><threads><thread id="2468030" subject="Looking for an answer? [FAQ]" author="Ana" numarticles="10"/></threads></forum>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, "secret", server.Client())
	result, err := client.Rules(context.Background(), 266192)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || result.Status != "ready" || result.TotalThreads != 728 || len(result.Threads) != 1 {
		t.Fatalf("unexpected result: %#v (requests: %d)", result, requests)
	}
	if result.Threads[0].URL != "https://boardgamegeek.com/thread/2468030" || result.Threads[0].Title != "Looking for an answer? [FAQ]" {
		t.Fatalf("wrong thread: %#v", result.Threads[0])
	}
	if _, err := client.Rules(context.Background(), 266192); err != nil || requests != 2 {
		t.Fatalf("cached request made extra BGG calls: %d, %v", requests, err)
	}
}

func TestRulesReturnsEmptyWhenNoRulesForumExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<forums><forum id="1" title="General" numthreads="100"/></forums>`))
	}))
	defer server.Close()

	result, err := New(server.URL, "", server.Client()).Rules(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ready" || result.TotalThreads != 0 || len(result.Threads) != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}
