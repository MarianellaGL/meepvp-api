package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"tablescore-api/internal/bgg"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/search"
)

type discoveryResponse struct {
	Status             string               `json:"status"`
	RetryAfterSeconds  int                  `json:"retryAfterSeconds,omitempty"`
	Games              []bgg.CollectionGame `json:"games"`
	CommunityRules     []domain.ScoringRule `json:"communityRules"`
	Rulebooks          []domain.Rulebook    `json:"rulebooks"`
	CachedRulebooks    bool                 `json:"cachedRulebooks"`
	UnavailableSources []string             `json:"unavailableSources"`
	// SearchedAs is the catalog title used for games and sheets when the
	// query was a misspelling of it.
	SearchedAs string `json:"searchedAs,omitempty"`
	// SuggestedQuery is an AI suggestion offered only when nothing matched.
	// It is never searched automatically; the person decides.
	SuggestedQuery string `json:"suggestedQuery,omitempty"`
}

// searchDiscovery keeps the three public catalogs behind one mobile request.
// Search and ordering are deterministic; the model may only suggest a query.
// Each source can fail independently so available results remain usable.
func (a *API) searchDiscovery(c *gin.Context) {
	query := strings.TrimSpace(c.Query("query"))
	if n := len([]rune(query)); n < 2 || n > 100 {
		writeError(c.Writer, http.StatusBadRequest, "search query must be 2 to 100 characters")
		return
	}
	ctx := c.Request.Context()
	result := discoveryResponse{
		Status: "ready", Games: []bgg.CollectionGame{}, CommunityRules: []domain.ScoringRule{},
		Rulebooks: []domain.Rulebook{}, UnavailableSources: []string{},
	}
	var books []domain.Rulebook
	var cached bool
	var booksErr error
	var found gameSearch
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		books, cached, booksErr = a.findRulebooks(ctx, query, "en")
	}()
	go func() {
		defer group.Done()
		found = a.searchGamesAndSheets(ctx, query)
	}()
	group.Wait()
	booksFailed := booksErr != nil || (cached && len(books) == 0)
	if booksErr != nil {
		slog.Warn("discovery saved rulebooks unavailable", "error", booksErr)
	} else {
		result.Rulebooks, result.CachedRulebooks = books, cached
	}
	// rule-book.org tolerates typos. Only when the literal query finds nothing
	// does its best rulebook name the game, so "coven" keeps finding Covenant
	// instead of becoming "Disc Cover".
	if found.empty() && len(books) > 0 && !search.Contains(books[0].Name, query) {
		title := search.Title(books[0].Name)
		if retry := a.searchGamesAndSheets(ctx, title); !retry.empty() {
			found = retry
			result.SearchedAs = title
		}
	}
	result.Games, result.CommunityRules = found.games, found.rules
	if found.processing {
		result.Status, result.RetryAfterSeconds = "processing", found.retryAfterSeconds
	}
	if found.bggFailed {
		result.UnavailableSources = append(result.UnavailableSources, "bgg")
	}
	if found.rulesFailed {
		result.UnavailableSources = append(result.UnavailableSources, "community")
	}
	if booksFailed {
		result.UnavailableSources = append(result.UnavailableSources, "rulebooks")
	}
	if len(result.UnavailableSources) == 3 {
		writeError(c.Writer, http.StatusBadGateway, "game discovery unavailable")
		return
	}
	if result.Status == "ready" && len(result.Games) == 0 && len(result.CommunityRules) == 0 && len(result.Rulebooks) == 0 {
		result.SuggestedQuery = a.ai.SuggestQuery(ctx, query)
	}
	status := http.StatusOK
	if result.Status == "processing" {
		status = http.StatusAccepted
	}
	writeJSON(c.Writer, status, result)
}

type gameSearch struct {
	games                  []bgg.CollectionGame
	rules                  []domain.ScoringRule
	processing             bool
	retryAfterSeconds      int
	bggFailed, rulesFailed bool
}

// empty means BGG answered and neither source matched, so another term may help.
func (g gameSearch) empty() bool {
	return !g.bggFailed && !g.processing && len(g.games) == 0 && len(g.rules) == 0
}

// searchGamesAndSheets queries BGG and community sheets for one term in parallel.
func (a *API) searchGamesAndSheets(ctx context.Context, term string) gameSearch {
	result := gameSearch{games: []bgg.CollectionGame{}, rules: []domain.ScoringRule{}}
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		found, err := a.bgg.Search(ctx, term)
		if err != nil {
			result.bggFailed = true
			slog.Warn("discovery BGG search unavailable", "error", err)
			return
		}
		if found.Status == "processing" {
			result.processing, result.retryAfterSeconds = true, found.RetryAfterSeconds
			return
		}
		result.games = search.Sort(term, found.Games, gameName)
	}()
	go func() {
		defer group.Done()
		rules, err := a.store.SearchPublicRules(term, 0)
		if err != nil {
			result.rulesFailed = true
			slog.Warn("discovery community search unavailable", "error", err)
			return
		}
		result.rules = rules
	}()
	group.Wait()
	return result
}

func gameName(game bgg.CollectionGame) string { return game.Name }
