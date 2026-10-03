package httpapi

import (
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
	// rule-book.org tolerates typos, so its best hit names the game for the
	// sources that only match literal text.
	books, cached, err := a.findRulebooks(ctx, query, "en")
	booksFailed := err != nil || (cached && len(books) == 0)
	if err != nil {
		slog.Warn("discovery saved rulebooks unavailable", "error", err)
	} else {
		result.Rulebooks, result.CachedRulebooks = books, cached
	}
	title := query
	if len(books) > 0 && !search.Contains(books[0].Name, query) {
		title = search.Title(books[0].Name)
		result.SearchedAs = title
	}

	var bggFailed, rulesFailed bool
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		found, err := a.bgg.Search(ctx, title)
		if err != nil {
			bggFailed = true
			slog.Warn("discovery BGG search unavailable", "error", err)
			return
		}
		if found.Status == "processing" {
			result.Status = "processing"
			result.RetryAfterSeconds = found.RetryAfterSeconds
			return
		}
		result.Games = search.Sort(title, found.Games, gameName)
	}()
	go func() {
		defer group.Done()
		rules, err := a.store.SearchPublicRules(title, 0)
		if err != nil {
			rulesFailed = true
			slog.Warn("discovery community search unavailable", "error", err)
			return
		}
		result.CommunityRules = rules
	}()
	group.Wait()
	if bggFailed {
		result.UnavailableSources = append(result.UnavailableSources, "bgg")
	}
	if rulesFailed {
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

func gameName(game bgg.CollectionGame) string { return game.Name }
