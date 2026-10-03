package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"tablescore-api/internal/ai"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/search"
)

const maxCitationRunes = 1500

type citation struct {
	Page int    `json:"page"`
	Text string `json:"text"`
}

type ruleAnswerResponse struct {
	Found     bool            `json:"found"`
	Answer    string          `json:"answer"`
	Citations []citation      `json:"citations"`
	Rulebook  domain.Rulebook `json:"rulebook"`
}

// answerCache keeps answers per rulebook and question so the same question is
// read from the rulebook only once per server process.
type answerCache struct {
	mu      sync.Mutex
	answers map[string]ai.RuleAnswer
}

func (c *answerCache) get(key string) (ai.RuleAnswer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	answer, ok := c.answers[key]
	return answer, ok
}

func (c *answerCache) put(key string, answer ai.RuleAnswer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.answers == nil || len(c.answers) >= 500 {
		c.answers = map[string]ai.RuleAnswer{}
	}
	c.answers[key] = answer
}

// askRule answers a rule question from the game's rulebook. The model reads
// the stored pages and must cite them; each cited page is returned with its
// original text so the answer can be checked.
func (a *API) askRule(c *gin.Context) {
	bggID, err := strconv.Atoi(c.Param("bggId"))
	if err != nil || bggID <= 0 {
		writeError(c.Writer, http.StatusBadRequest, "game ID must be positive")
		return
	}
	var input struct {
		Question string `json:"question"`
	}
	if !decodeJSON(c.Writer, c.Request, &input) {
		return
	}
	question := strings.TrimSpace(input.Question)
	if n := len([]rune(question)); n < 3 || n > 300 {
		writeError(c.Writer, http.StatusBadRequest, "question must be 3 to 300 characters")
		return
	}
	books, err := a.store.GameRulebooks(bggID)
	if err != nil {
		writeStoreError(c.Writer, err)
		return
	}
	if len(books) == 0 {
		writeError(c.Writer, http.StatusNotFound, "no rulebook linked to this game")
		return
	}
	if !a.ai.Enabled() {
		writeError(c.Writer, http.StatusServiceUnavailable, "AI rule answers unavailable")
		return
	}
	book := books[0]
	read, err := a.readRulebook(c.Request.Context(), book)
	if err != nil {
		writeRulebookError(c.Writer, err)
		return
	}
	key := book.ID + "\x00" + search.Normalize(question)
	answer, ok := a.answers.get(key)
	if !ok {
		if answer, err = a.ai.AnswerRule(c.Request.Context(), search.Title(book.Name), question, read.PageTexts); err != nil {
			writeAIError(c.Writer, err, "could not answer rule question")
			return
		}
		a.answers.put(key, answer)
	}
	result := ruleAnswerResponse{Found: answer.Found, Answer: answer.Answer, Citations: []citation{}, Rulebook: book}
	for _, page := range answer.Pages {
		text := []rune(read.PageTexts[page-1])
		if len(text) > maxCitationRunes {
			text = append(text[:maxCitationRunes], '…')
		}
		result.Citations = append(result.Citations, citation{Page: page, Text: string(text)})
	}
	writeJSON(c.Writer, http.StatusOK, result)
}

func (a *API) WithAIClient(client *ai.Client) *API { a.ai = client; return a }
