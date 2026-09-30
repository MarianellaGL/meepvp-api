package httpapi

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/pdfreader"
	"tablescore-api/internal/rulebooks"
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
	cached := false
	if query != "" {
		books, err := a.rulebooks.Search(c.Request.Context(), query, language)
		if err != nil {
			cached = true
		} else if err = a.store.SaveRulebooks(books); err != nil {
			slog.Error("could not save rulebook catalog", "error", err)
			writeError(c.Writer, http.StatusInternalServerError, "could not save rulebook catalog")
			return
		}
	}
	books, err := a.store.FindRulebooks(query, language)
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
