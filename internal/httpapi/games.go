package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/search"
)

type gameRulebook struct {
	domain.Rulebook
	// TextReady means the rulebook was already read and its pages are stored.
	TextReady bool `json:"textReady"`
}

type gameResponse struct {
	BGGID        int                  `json:"bggId"`
	Rulebooks    []gameRulebook       `json:"rulebooks"`
	ScoringRules []domain.ScoringRule `json:"scoringRules"`
}

// getGame gathers everything the base holds for one game: its rulebooks and the
// sheets the caller may use. A game is identified by its BGG ID. When no
// rulebook is linked yet, the name hint links catalog rulebooks whose title is
// exactly that name, so later visits need no hint.
func (a *API) getGame(c *gin.Context) {
	bggID, err := strconv.Atoi(c.Param("bggId"))
	if err != nil || bggID <= 0 {
		writeError(c.Writer, http.StatusBadRequest, "game ID must be positive")
		return
	}
	name := strings.TrimSpace(c.Query("name"))
	if len([]rune(name)) > 100 {
		writeError(c.Writer, http.StatusBadRequest, "game name is too long")
		return
	}
	user, _, err := a.optionalUser(c.Request)
	if err != nil {
		writeError(c.Writer, http.StatusUnauthorized, "invalid session")
		return
	}
	books, err := a.store.GameRulebooks(bggID)
	if err != nil {
		writeStoreError(c.Writer, err)
		return
	}
	if len(books) == 0 && name != "" {
		books = a.linkRulebooks(c, bggID, name)
	}
	result := gameResponse{BGGID: bggID, Rulebooks: make([]gameRulebook, 0, len(books))}
	for _, book := range books {
		pages, err := a.store.RulebookPages(book.ID)
		if err != nil {
			writeStoreError(c.Writer, err)
			return
		}
		result.Rulebooks = append(result.Rulebooks, gameRulebook{Rulebook: book, TextReady: len(pages) > 0})
	}
	if result.ScoringRules, err = a.gameRules(bggID, user.ID); err != nil {
		writeStoreError(c.Writer, err)
		return
	}
	writeJSON(c.Writer, http.StatusOK, result)
}

// linkRulebooks only links exact title matches; a near match such as an
// expansion must not become the base game's rulebook.
func (a *API) linkRulebooks(c *gin.Context, bggID int, name string) []domain.Rulebook {
	found, _, err := a.findRulebooks(c.Request.Context(), name, "en")
	if err != nil {
		slog.Warn("game rulebooks unavailable", "error", err)
		return nil
	}
	linked := []domain.Rulebook{}
	for _, book := range found {
		if search.Normalize(search.Title(book.Name)) != search.Normalize(name) {
			continue
		}
		if err := a.store.LinkRulebook(book.ID, bggID); err != nil {
			slog.Warn("could not link rulebook to game", "rulebook", book.ID, "error", err)
			continue
		}
		book.BGGID = &bggID
		linked = append(linked, book)
	}
	return linked
}

// gameRules returns the game's public sheets plus the caller's own.
func (a *API) gameRules(bggID int, userID string) ([]domain.ScoringRule, error) {
	rules, err := a.store.SearchPublicRules("", bggID)
	if err != nil || userID == "" {
		return rules, err
	}
	own, err := a.store.ListUserRules(userID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, rule := range rules {
		seen[rule.ID] = true
	}
	for _, rule := range own {
		if rule.BGGID == bggID && !seen[rule.ID] {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}
