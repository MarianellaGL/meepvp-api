package bgg

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultBaseURL = "https://boardgamegeek.com/xmlapi2"

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type CollectionGame struct {
	BGGID         int    `json:"bggId"`
	Name          string `json:"name"`
	YearPublished int    `json:"yearPublished,omitempty"`
	ThumbnailURL  string `json:"thumbnailUrl,omitempty"`
	ImageURL      string `json:"imageUrl,omitempty"`
	MinPlayers    int    `json:"minPlayers,omitempty"`
	MaxPlayers    int    `json:"maxPlayers,omitempty"`
	PlayingTime   int    `json:"playingTime,omitempty"`
}

type CollectionResult struct {
	Status            string           `json:"status"`
	RetryAfterSeconds int              `json:"retryAfterSeconds,omitempty"`
	Games             []CollectionGame `json:"games,omitempty"`
}

func NewFromEnvironment() *Client {
	baseURL := strings.TrimRight(os.Getenv("BGG_API_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return New(baseURL, os.Getenv("BGG_API_TOKEN"), &http.Client{Timeout: 15 * time.Second})
}

func New(baseURL, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, httpClient: httpClient}
}

func (c *Client) Collection(ctx context.Context, username string) (CollectionResult, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return CollectionResult{}, fmt.Errorf("username is required")
	}

	endpoint, err := url.Parse(c.baseURL + "/collection")
	if err != nil {
		return CollectionResult{}, fmt.Errorf("create BGG request: %w", err)
	}
	query := endpoint.Query()
	query.Set("username", username)
	query.Set("own", "1")
	query.Set("stats", "1")
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return CollectionResult{}, fmt.Errorf("create BGG request: %w", err)
	}
	req.Header.Set("Accept", "application/xml")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.httpClient.Do(req)
	if err != nil {
		return CollectionResult{}, fmt.Errorf("request BGG collection: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusAccepted {
		return CollectionResult{Status: "processing", RetryAfterSeconds: retryAfter(response)}, nil
	}
	if response.StatusCode != http.StatusOK {
		return CollectionResult{}, fmt.Errorf("BGG collection request failed with status %d", response.StatusCode)
	}

	var payload collectionXML
	if err := xml.NewDecoder(response.Body).Decode(&payload); err != nil {
		return CollectionResult{}, fmt.Errorf("decode BGG collection XML: %w", err)
	}
	games := make([]CollectionGame, 0, len(payload.Items))
	for _, item := range payload.Items {
		games = append(games, CollectionGame{BGGID: item.ObjectID, Name: item.Name.Value, YearPublished: item.YearPublished.Value, ThumbnailURL: item.Thumbnail, ImageURL: item.Image, MinPlayers: item.Stats.MinPlayers, MaxPlayers: item.Stats.MaxPlayers, PlayingTime: item.Stats.PlayingTime})
	}
	return CollectionResult{Status: "ready", Games: games}, nil
}

func retryAfter(response *http.Response) int {
	if value, err := time.ParseDuration(response.Header.Get("Retry-After") + "s"); err == nil && value > 0 {
		return int(value.Seconds())
	}
	return 5
}

type collectionXML struct {
	Items []collectionItemXML `xml:"item"`
}
type collectionItemXML struct {
	ObjectID int `xml:"objectid,attr"`
	Name     struct {
		Value string `xml:",chardata"`
	} `xml:"name"`
	YearPublished struct {
		Value int `xml:"value,attr"`
	} `xml:"yearpublished"`
	Thumbnail string `xml:"thumbnail"`
	Image     string `xml:"image"`
	Stats     struct {
		MinPlayers  int `xml:"minplayers,attr"`
		MaxPlayers  int `xml:"maxplayers,attr"`
		PlayingTime int `xml:"playingtime,attr"`
	} `xml:"stats"`
}
