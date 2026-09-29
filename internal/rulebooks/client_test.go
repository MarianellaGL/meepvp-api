package rulebooks

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/pdfreader"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}

func TestSearchFiltersUnsafeMetadataAndCachesIndependentCopies(t *testing.T) {
	calls := 0
	client := NewWithTransport(transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "api.rule-book.org", r.URL.Host)
		require.Equal(t, "Catan & friends", r.URL.Query().Get("search"))
		require.Equal(t, "en", r.URL.Query().Get("language"))
		return response(`{"results":[
   {"id":"catan","name":"Catan Rulebook","language":"en","link":"https://cdn.1j1ju.com/medias/test.pdf"},
   {"id":"catan","name":"Duplicate","language":"en","link":"https://cdn.1j1ju.com/medias/test.pdf"},
   {"id":"evil","name":"Unsafe URL","language":"en","link":"http://127.0.0.1/private.pdf"},
   {"id":"french","name":"French","language":"fr","link":"https://cdn.1j1ju.com/medias/test.pdf"},
   {"id":"../bad","name":"Bad ID","language":"en","link":"https://cdn.1j1ju.com/medias/test.pdf"}
  ]}`), nil
	}))
	books, err := client.Search(context.Background(), "Catan & friends", "en")
	require.NoError(t, err)
	require.Len(t, books, 1)
	require.Equal(t, "rule-book:en:catan", books[0].ID)
	books[0].Name = "mutated"
	books, err = client.Search(context.Background(), "Catan & friends", "en")
	require.NoError(t, err)
	require.Equal(t, "Catan Rulebook", books[0].Name)
	require.Equal(t, 1, calls)
}

func TestDownloadRejectsUnsafeSourcesRedirectsAndOversizedPDFs(t *testing.T) {
	for _, raw := range []string{"http://cdn.1j1ju.com/medias/a.pdf", "https://cdn.1j1ju.com.evil.test/medias/a.pdf", "https://user@cdn.1j1ju.com/medias/a.pdf", "https://cdn.1j1ju.com/private.pdf", "https://cdn.1j1ju.com/medias/a.pdf?url=x", "https://127.0.0.1/medias/a.pdf"} {
		require.False(t, AllowedPDFURL(raw), raw)
	}
	calls := 0
	client := NewWithTransport(transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://127.0.0.1/private.pdf"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))
	_, err := client.Download(context.Background(), domain.Rulebook{PDFURL: "https://cdn.1j1ju.com/medias/a.pdf"})
	require.Error(t, err)
	require.Equal(t, 1, calls)
	_, err = client.Download(context.Background(), domain.Rulebook{PDFURL: "http://localhost/a.pdf"})
	require.Error(t, err)
	require.Equal(t, 1, calls)
	client = NewWithTransport(transportFunc(func(r *http.Request) (*http.Response, error) {
		result := response("tiny")
		result.ContentLength = pdfreader.MaxFileBytes + 1
		return result, nil
	}))
	_, err = client.Download(context.Background(), domain.Rulebook{PDFURL: "https://cdn.1j1ju.com/medias/a.pdf"})
	require.ErrorContains(t, err, "20 MB")
}

func TestSearchRejectsMalformedAndOversizedResponses(t *testing.T) {
	for _, body := range []string{"invalid JSON", strings.Repeat(" ", (1<<20)+1)} {
		client := NewWithTransport(transportFunc(func(r *http.Request) (*http.Response, error) { return response(body), nil }))
		_, err := client.Search(context.Background(), "Catan", "en")
		require.ErrorIs(t, err, ErrUnavailable)
	}
}
