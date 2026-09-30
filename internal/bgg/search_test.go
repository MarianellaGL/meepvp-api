package bgg

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestSearchKeepsRealBGGIDsAndPrimaryNames(t *testing.T) {
	client := New("https://example.test/xmlapi2", "bgg-token", &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer bgg-token" {
			t.Fatal("missing BGG token")
		}
		if req.URL.Path == "/xmlapi2/thing" {
			if req.URL.Query().Get("id") != "266192" {
				t.Fatalf("wrong thing request: %s", req.URL)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<items><item type="boardgame" id="266192"><thumbnail>https://example.test/wingspan.jpg</thumbnail><minplayers value="1"/><maxplayers value="5"/></item></items>`))}, nil
		}
		if req.URL.Path != "/xmlapi2/search" || req.URL.Query().Get("query") != "Wingspan" || req.URL.Query().Get("type") != "boardgame" {
			t.Fatalf("wrong search request: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<items><item type="boardgame" id="266192"><name type="alternate" value="Alias"/><name type="primary" value="Wingspan"/><yearpublished value="2019"/></item><item type="boardgameexpansion" id="1"><name type="primary" value="Expansion"/></item></items>`))}, nil
	})})
	result, err := client.Search(context.Background(), "Wingspan")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Games) != 1 || result.Games[0].BGGID != 266192 || result.Games[0].Name != "Wingspan" || result.Games[0].YearPublished != 2019 || result.Games[0].ThumbnailURL == "" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
