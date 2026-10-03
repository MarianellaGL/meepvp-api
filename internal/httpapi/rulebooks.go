package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/pdfreader"
	"tablescore-api/internal/rulebooks"
	"tablescore-api/internal/search"
)

type rulebookSearchResponse struct {
	Results []domain.Rulebook `json:"results"`
	Cached  bool              `json:"cached"`
}

func (a *API) WithRulebookClient(client *rulebooks.Client) *API { a.rulebooks = client; return a }

func (a *API) searchRulebooks(c *gin.Context) {
	query := strings.TrimSpace(c.Query("query"))
	language := c.DefaultQuery("language", "en")
	if len([]rune(query)) > 100 || (language != "en" && language != "fr") {
		writeError(c.Writer, http.StatusBadRequest, "invalid rulebook search")
		return
	}
	var books []domain.Rulebook
	var cached bool
	var err error
	if query == "" {
		books, err = a.store.FindRulebooks("", language)
	} else {
		books, cached, err = a.findRulebooks(c.Request.Context(), query, language)
	}
	if err != nil {
		writeError(c.Writer, http.StatusInternalServerError, "could not read rulebook catalog")
		return
	}
	if cached && len(books) == 0 {
		writeError(c.Writer, http.StatusBadGateway, "rulebook catalog unavailable")
		return
	}
	writeJSON(c.Writer, http.StatusOK, rulebookSearchResponse{Results: books, Cached: cached})
}

// findRulebooks keeps rule-book.org's typo-tolerant hits that resemble the
// query, best first, and saves only those so the catalog does not collect
// noise. When the source is unavailable it ranks the saved catalog instead.
func (a *API) findRulebooks(ctx context.Context, query, language string) ([]domain.Rulebook, bool, error) {
	if remote, err := a.rulebooks.Search(ctx, query, language); err == nil {
		books := search.Rank(query, remote, rulebookName)
		if err := a.store.SaveRulebooks(books); err != nil {
			slog.Warn("could not save rulebook catalog", "error", err)
		}
		return books, false, nil
	}
	saved, err := a.store.FindRulebooks(query, language)
	if err != nil {
		return nil, true, err
	}
	return search.Rank(query, saved, rulebookName), true, nil
}

func rulebookName(book domain.Rulebook) string { return book.Name }

func (a *API) extractRulebook(c *gin.Context) {
	book, err := a.store.GetRulebook(c.Param("id"))
	if err != nil {
		writeStoreError(c.Writer, err)
		return
	}
	data, err := a.rulebooks.Download(c.Request.Context(), book)
	if err != nil {
		writeError(c.Writer, http.StatusBadGateway, "could not download rulebook")
		return
	}
	result, err := pdfreader.Extract(data, book.SourceID+".pdf")
	if err != nil {
		writeError(c.Writer, http.StatusUnprocessableEntity, "could not extract rulebook PDF")
		return
	}
	a.addAISuggestion(c.Request, &result, book.Name)
	writeJSON(c.Writer, http.StatusOK, struct {
		pdfreader.Result
		Rulebook domain.Rulebook `json:"rulebook"`
	}{Result: result, Rulebook: book})
}
