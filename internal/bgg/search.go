package bgg

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type SearchResult struct {
	Status            string           `json:"status"`
	RetryAfterSeconds int              `json:"retryAfterSeconds,omitempty"`
	Games             []CollectionGame `json:"games"`
}

type searchXML struct {
	Items []struct {
		ID    int    `xml:"id,attr"`
		Type  string `xml:"type,attr"`
		Names []struct {
			Type  string `xml:"type,attr"`
			Value string `xml:"value,attr"`
		} `xml:"name"`
		Year struct {
			Value int `xml:"value,attr"`
		} `xml:"yearpublished"`
	} `xml:"item"`
}

func (c *Client) Search(ctx context.Context, query string) (SearchResult, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 || len([]rune(query)) > 100 {
		return SearchResult{}, fmt.Errorf("search query must be 2 to 100 characters")
	}
	endpoint, err := url.Parse(c.baseURL + "/search")
	if err != nil {
		return SearchResult{}, err
	}
	parameters := endpoint.Query()
	parameters.Set("query", query)
	parameters.Set("type", "boardgame")
	endpoint.RawQuery = parameters.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return SearchResult{}, err
	}
	req.Header.Set("Accept", "application/xml")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return SearchResult{}, fmt.Errorf("request BGG search: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusAccepted {
		return SearchResult{Status: "processing", RetryAfterSeconds: retryAfter(res), Games: []CollectionGame{}}, nil
	}
	if res.StatusCode == http.StatusTooManyRequests {
		return SearchResult{}, &RateLimitError{RetryAfterSeconds: retryAfter(res)}
	}
	if res.StatusCode != http.StatusOK {
		return SearchResult{}, fmt.Errorf("BGG search failed with status %d", res.StatusCode)
	}
	var parsed searchXML
	if err := xml.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return SearchResult{}, fmt.Errorf("decode BGG search XML: %w", err)
	}
	games := make([]CollectionGame, 0, min(len(parsed.Items), 30))
	for _, item := range parsed.Items {
		if item.Type != "boardgame" || item.ID <= 0 {
			continue
		}
		name := ""
		for _, candidate := range item.Names {
			if candidate.Type == "primary" {
				name = strings.TrimSpace(candidate.Value)
				break
			}
			if name == "" {
				name = strings.TrimSpace(candidate.Value)
			}
		}
		if name == "" {
			continue
		}
		games = append(games, CollectionGame{BGGID: item.ID, Name: name, YearPublished: item.Year.Value})
		if len(games) == 30 {
			break
		}
	}
	c.enrichSearch(ctx, games)
	return SearchResult{Status: "ready", Games: games}, nil
}

func (c *Client) enrichSearch(ctx context.Context, games []CollectionGame) {
	if len(games) == 0 {
		return
	}
	ids := make([]string, 0, min(len(games), 10))
	for _, game := range games[:min(len(games), 10)] {
		ids = append(ids, strconv.Itoa(game.BGGID))
	}
	endpoint, err := url.Parse(c.baseURL + "/thing")
	if err != nil {
		return
	}
	query := endpoint.Query()
	query.Set("id", strings.Join(ids, ","))
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/xml")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return
	}
	var details struct {
		Items []struct {
			ID         int    `xml:"id,attr"`
			Thumbnail  string `xml:"thumbnail"`
			Image      string `xml:"image"`
			MinPlayers struct {
				Value int `xml:"value,attr"`
			} `xml:"minplayers"`
			MaxPlayers struct {
				Value int `xml:"value,attr"`
			} `xml:"maxplayers"`
			PlayingTime struct {
				Value int `xml:"value,attr"`
			} `xml:"playingtime"`
		} `xml:"item"`
	}
	if xml.NewDecoder(res.Body).Decode(&details) != nil {
		return
	}
	byID := make(map[int]int, len(games))
	for index, game := range games {
		byID[game.BGGID] = index
	}
	for _, item := range details.Items {
		index, ok := byID[item.ID]
		if !ok {
			continue
		}
		games[index].ThumbnailURL = strings.TrimSpace(item.Thumbnail)
		games[index].ImageURL = strings.TrimSpace(item.Image)
		games[index].MinPlayers = item.MinPlayers.Value
		games[index].MaxPlayers = item.MaxPlayers.Value
		games[index].PlayingTime = item.PlayingTime.Value
	}
}
