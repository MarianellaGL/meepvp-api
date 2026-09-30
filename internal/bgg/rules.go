package bgg

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type RulesThread struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Author string `json:"author"`
	Posts  int    `json:"posts"`
	URL    string `json:"url"`
}

type RulesResult struct {
	Status            string        `json:"status"`
	RetryAfterSeconds int           `json:"retryAfterSeconds,omitempty"`
	ForumURL          string        `json:"forumUrl,omitempty"`
	TotalThreads      int           `json:"totalThreads"`
	Threads           []RulesThread `json:"threads"`
}

type cachedRules struct {
	result  RulesResult
	expires time.Time
}

func (c *Client) Rules(ctx context.Context, gameID int) (RulesResult, error) {
	if gameID <= 0 {
		return RulesResult{}, fmt.Errorf("game ID must be positive")
	}
	c.rulesMu.Lock()
	defer c.rulesMu.Unlock()
	if cached, ok := c.rulesCache[gameID]; ok && time.Now().Before(cached.expires) {
		return cached.result, nil
	}

	var forums forumListXML
	status, retry, err := c.readXML(ctx, "/forumlist", url.Values{"id": {strconv.Itoa(gameID)}, "type": {"thing"}}, &forums)
	if err != nil {
		return RulesResult{}, err
	}
	if status == http.StatusAccepted {
		return RulesResult{Status: "processing", RetryAfterSeconds: retry}, nil
	}

	result := RulesResult{Status: "ready", Threads: []RulesThread{}}
	for _, forum := range forums.Forums {
		if !strings.EqualFold(strings.TrimSpace(forum.Title), "Rules") {
			continue
		}
		result.ForumURL = fmt.Sprintf("https://boardgamegeek.com/forum/%d", forum.ID)
		result.TotalThreads = forum.NumThreads
		if forum.NumThreads == 0 {
			c.rulesCache[gameID] = cachedRules{result: result, expires: time.Now().Add(10 * time.Minute)}
			return result, nil
		}

		var threads forumXML
		status, retry, err = c.readXML(ctx, "/forum", url.Values{"id": {strconv.Itoa(forum.ID)}, "page": {"1"}}, &threads)
		if err != nil {
			return RulesResult{}, err
		}
		if status == http.StatusAccepted {
			return RulesResult{Status: "processing", RetryAfterSeconds: retry}, nil
		}
		for _, thread := range threads.Threads {
			result.Threads = append(result.Threads, RulesThread{
				ID: thread.ID, Title: thread.Subject, Author: thread.Author,
				Posts: thread.NumArticles, URL: fmt.Sprintf("https://boardgamegeek.com/thread/%d", thread.ID),
			})
		}
		c.rulesCache[gameID] = cachedRules{result: result, expires: time.Now().Add(10 * time.Minute)}
		return result, nil
	}
	c.rulesCache[gameID] = cachedRules{result: result, expires: time.Now().Add(10 * time.Minute)}
	return result, nil
}

func (c *Client) readXML(ctx context.Context, path string, query url.Values, target any) (int, int, error) {
	endpoint, err := url.Parse(c.baseURL + path)
	if err != nil {
		return 0, 0, fmt.Errorf("create BGG request: %w", err)
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return 0, 0, fmt.Errorf("create BGG request: %w", err)
	}
	req.Header.Set("Accept", "application/xml")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("request BGG %s: %w", strings.TrimPrefix(path, "/"), err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusAccepted {
		return response.StatusCode, retryAfter(response), nil
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return 0, 0, &RateLimitError{RetryAfterSeconds: retryAfter(response)}
	}
	if response.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("BGG %s request failed with status %d", strings.TrimPrefix(path, "/"), response.StatusCode)
	}
	if err := xml.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(target); err != nil {
		return 0, 0, fmt.Errorf("decode BGG %s XML: %w", strings.TrimPrefix(path, "/"), err)
	}
	return response.StatusCode, 0, nil
}

type forumListXML struct {
	Forums []struct {
		ID         int    `xml:"id,attr"`
		Title      string `xml:"title,attr"`
		NumThreads int    `xml:"numthreads,attr"`
	} `xml:"forum"`
}

type forumXML struct {
	Threads []struct {
		ID          int    `xml:"id,attr"`
		Subject     string `xml:"subject,attr"`
		Author      string `xml:"author,attr"`
		NumArticles int    `xml:"numarticles,attr"`
	} `xml:"threads>thread"`
}
