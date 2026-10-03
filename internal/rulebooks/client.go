// Package rulebooks accesses the public catalog and bounded, allowlisted PDFs.
package rulebooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"tablescore-api/internal/domain"
	"tablescore-api/internal/pdfreader"
)

var ErrUnavailable = errors.New("rulebook catalog unavailable")
var errRulebookTooLarge = fmt.Errorf("rulebook exceeds %d MB", pdfreader.MaxRulebookBytes>>20)
var slug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,159}$`)

type cachedSearch struct {
	books   []domain.Rulebook
	expires time.Time
}
type Client struct {
	http     *http.Client
	mu       sync.Mutex
	searches map[string]cachedSearch
}

func New() *Client { return NewWithTransport(http.DefaultTransport) }

// NewWithTransport permits fixture transports in tests without allowing clients
// to supply a download host. Every URL and redirect still uses the same policy.
func NewWithTransport(transport http.RoundTripper) *Client {
	return &Client{http: &http.Client{Transport: transport, Timeout: 3 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !AllowedPDFURL(req.URL.String()) {
			return errors.New("PDF redirect not allowed")
		}
		return nil
	}}, searches: make(map[string]cachedSearch)}
}

func AllowedPDFURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "cdn.1j1ju.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && strings.HasPrefix(u.Path, "/medias/") && strings.HasSuffix(strings.ToLower(u.Path), ".pdf")
}

func (c *Client) Search(ctx context.Context, query, language string) ([]domain.Rulebook, error) {
	key := language + "\x00" + query
	c.mu.Lock()
	cached, ok := c.searches[key]
	c.mu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return append([]domain.Rulebook(nil), cached.books...), nil
	}
	u := "https://api.rule-book.org/games?" + url.Values{"search": {query}, "language": {language}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	var payload struct {
		Results []struct{ ID, Name, Link, Language string } `json:"results"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, ErrUnavailable
	}
	if json.Unmarshal(data, &payload) != nil {
		return nil, ErrUnavailable
	}
	books := []domain.Rulebook{}
	seen := map[string]bool{}
	now := time.Now().UTC()
	for _, item := range payload.Results {
		if !slug.MatchString(item.ID) || item.Language != language || len(item.Name) > 300 || strings.TrimSpace(item.Name) == "" || !AllowedPDFURL(item.Link) {
			continue
		}
		id := "rule-book:" + language + ":" + item.ID
		if seen[id] {
			continue
		}
		seen[id] = true
		books = append(books, domain.Rulebook{ID: id, Source: "rule-book.org", SourceID: item.ID, Name: item.Name, Language: language, PDFURL: item.Link, CreatedAt: now, UpdatedAt: now})
		if len(books) >= 50 {
			break
		}
	}
	c.mu.Lock()
	// Bound the in-process cache; persistent metadata remains in PostgreSQL.
	if len(c.searches) >= 100 {
		clear(c.searches)
	}
	c.searches[key] = cachedSearch{books: append([]domain.Rulebook(nil), books...), expires: now.Add(15 * time.Minute)}
	c.mu.Unlock()
	return books, nil
}

func (c *Client) Download(ctx context.Context, book domain.Rulebook) ([]byte, error) {
	if !AllowedPDFURL(book.PDFURL) {
		return nil, errors.New("PDF source is not allowed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, book.PDFURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("could not download rulebook")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rulebook source returned status %d", resp.StatusCode)
	}
	if resp.ContentLength > pdfreader.MaxRulebookBytes {
		return nil, errRulebookTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, pdfreader.MaxRulebookBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > pdfreader.MaxRulebookBytes {
		return nil, errRulebookTooLarge
	}
	return data, nil
}
