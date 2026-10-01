package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"tablescore-api/internal/bgg"
	"tablescore-api/internal/domain"
)

type discoveryResponse struct {
	Status             string               `json:"status"`
	RetryAfterSeconds  int                  `json:"retryAfterSeconds,omitempty"`
	Games              []bgg.CollectionGame `json:"games"`
	CommunityRules     []domain.ScoringRule `json:"communityRules"`
	Rulebooks          []domain.Rulebook    `json:"rulebooks"`
	CachedRulebooks    bool                 `json:"cachedRulebooks"`
	UnavailableSources []string             `json:"unavailableSources"`
}

// searchDiscovery keeps the three public catalogs behind one mobile request.
// Each source can fail independently so available results remain usable.
func (a *API) searchDiscovery(c *gin.Context) {
	query := strings.TrimSpace(c.Query("query"))
	if n := len([]rune(query)); n < 2 || n > 100 {
		writeError(c.Writer, http.StatusBadRequest, "search query must be 2 to 100 characters")
		return
	}
	result := discoveryResponse{
		Status: "ready", Games: []bgg.CollectionGame{}, CommunityRules: []domain.ScoringRule{},
		Rulebooks: []domain.Rulebook{}, UnavailableSources: []string{},
	}
	var bggFailed, rulesFailed, booksFailed bool
	var group sync.WaitGroup
	group.Add(3)
	go func() {
		defer group.Done()
		found, err := a.bgg.Search(c.Request.Context(), query)
		if err != nil {
			bggFailed = true
			slog.Warn("discovery BGG search unavailable", "error", err)
			return
		}
		if found.Status == "ready" && len(found.Games) == 0 {
			if alternate := a.ai.AlternateBGGQuery(c.Request.Context(), query); alternate != "" {
				if retry, retryErr := a.bgg.Search(c.Request.Context(), alternate); retryErr == nil && retry.Status == "ready" && len(retry.Games) > 0 {
					found = retry
				}
			}
		}
		if found.Status == "processing" {
			result.Status = "processing"
			result.RetryAfterSeconds = found.RetryAfterSeconds
			return
		}
		if ranked := a.ai.RankGames(c.Request.Context(), query, found.Games); ranked != nil {
			result.Games = ranked
		}
	}()
	go func() {
		defer group.Done()
		rules, err := a.store.SearchPublicRules(query, 0)
		if err != nil {
			rulesFailed = true
			slog.Warn("discovery community search unavailable", "error", err)
			return
		}
		result.CommunityRules = rules
	}()
	go func() {
		defer group.Done()
		books, err := a.rulebooks.Search(c.Request.Context(), query, "en")
		if err != nil {
			result.CachedRulebooks = true
		} else if err = a.store.SaveRulebooks(books); err != nil {
			result.CachedRulebooks = true
			slog.Warn("discovery rulebook catalog could not be saved", "error", err)
		}
		books, err = a.store.FindRulebooks(query, "en")
		if err != nil || (result.CachedRulebooks && len(books) == 0) {
			booksFailed = true
			if err != nil {
				slog.Warn("discovery saved rulebooks unavailable", "error", err)
			}
			return
		}
		result.Rulebooks = books
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
	status := http.StatusOK
	if result.Status == "processing" {
		status = http.StatusAccepted
	}
	writeJSON(c.Writer, status, result)
}
