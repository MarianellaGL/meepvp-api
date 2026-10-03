package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"tablescore-api/handlers"
	"tablescore-api/internal/ai"
	"tablescore-api/internal/bgg"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/pdfreader"
	"tablescore-api/internal/rulebooks"
	"tablescore-api/internal/search"
	"tablescore-api/internal/store"
)

type API struct {
	foundation *handlers.Deps
	store      store.Repository
	bgg        *bgg.Client
	rulebooks  *rulebooks.Client
	ai         *ai.Client
}

type playerTotal struct {
	PlayerID string `json:"playerId"`
	Total    int    `json:"total"`
}

type sessionResponse struct {
	domain.ScoreSession
	DurationSeconds int64         `json:"durationSeconds"`
	Totals          []playerTotal `json:"totals"`
	Winners         []playerTotal `json:"winners"`
}

func New(s store.Repository, clients ...*bgg.Client) *API {
	client := bgg.NewFromEnvironment()
	if len(clients) > 0 && clients[0] != nil {
		client = clients[0]
	}
	return &API{store: s, bgg: client, rulebooks: rulebooks.New(), ai: ai.NewFromEnvironment()}
}

func (a *API) extractPDF(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, pdfreader.MaxFileBytes+(1<<20))
	if err := r.ParseMultipartForm(pdfreader.MaxFileBytes); err != nil {
		writeError(w, http.StatusBadRequest, "a PDF file up to 20 MB is required")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, pdfreader.MaxFileBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read PDF")
		return
	}
	result, err := pdfreader.Extract(data, header.Filename)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.addAISuggestion(r, &result, r.FormValue("gameName"))
	writeJSON(w, http.StatusOK, result)
}

func (a *API) extractScoringText(w http.ResponseWriter, r *http.Request) {
	var input struct {
		GameName string `json:"gameName"`
		Text     string `json:"text"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if !decodeJSON(w, r, &input) {
		return
	}
	input.GameName = strings.TrimSpace(input.GameName)
	input.Text = strings.TrimSpace(input.Text)
	if len([]rune(input.GameName)) > 120 || len([]rune(input.Text)) > 30000 || input.Text == "" {
		writeError(w, http.StatusBadRequest, "invalid extracted text")
		return
	}
	result := pdfreader.Result{FileName: "Imagen de tabla de puntos", Pages: 1, Text: input.Text,
		Excerpts: pdfreader.ScoringExcerpts(input.Text)}
	a.addAISuggestion(r, &result, input.GameName)
	writeJSON(w, http.StatusOK, result)
}

func (a *API) addAISuggestion(r *http.Request, result *pdfreader.Result, gameName string) {
	if result.Suggestion != nil || !a.ai.Enabled() {
		return
	}
	suggestion, err := a.ai.SuggestScoring(r.Context(), gameName, result.Text, result.Excerpts)
	if err != nil {
		// OCR is still useful when the optional model is unavailable.
		slog.Warn("AI scoring suggestion unavailable", "error", err)
		return
	}
	result.Suggestion = suggestion
}

func (a *API) suggestScoringDraft(w http.ResponseWriter, r *http.Request) {
	var input struct {
		GameName string `json:"gameName"`
		Text     string `json:"text"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	if !decodeJSON(w, r, &input) {
		return
	}
	input.GameName = strings.TrimSpace(input.GameName)
	input.Text = strings.TrimSpace(input.Text)
	if len([]rune(input.GameName)) > 120 || len([]rune(input.Text)) < 40 || len([]rune(input.Text)) > 120000 {
		writeError(w, http.StatusBadRequest, "invalid rulebook text")
		return
	}
	if !a.ai.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "AI scoring assistant unavailable")
		return
	}
	suggestion, err := a.ai.SuggestScoring(r.Context(), input.GameName, input.Text, pdfreader.ScoringExcerpts(input.Text))
	if err != nil {
		slog.Warn("AI scoring draft unavailable", "error", err)
		var providerErr *ai.ProviderError
		if errors.As(err, &providerErr) {
			if providerErr.QuotaExhausted() {
				writeError(w, http.StatusServiceUnavailable, "AI provider quota exhausted")
				return
			}
			if providerErr.StatusCode == http.StatusTooManyRequests {
				if providerErr.RetryAfterSeconds > 0 {
					w.Header().Set("Retry-After", strconv.Itoa(providerErr.RetryAfterSeconds))
				}
				writeError(w, http.StatusTooManyRequests, "AI provider rate limited")
				return
			}
			if providerErr.StatusCode == http.StatusUnauthorized || providerErr.StatusCode == http.StatusForbidden {
				writeError(w, http.StatusServiceUnavailable, "AI provider authentication failed")
				return
			}
		}
		writeError(w, http.StatusBadGateway, "could not generate scoring suggestion")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Suggestion *pdfreader.ScoringSuggestion `json:"scoringSuggestion"`
	}{Suggestion: suggestion})
}

func (a *API) getBGGCollection(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/bgg/collections/")
	collection, err := a.bgg.Collection(r.Context(), username)
	if err != nil {
		writeBGGError(w, err)
		return
	}
	status := http.StatusOK
	if collection.Status == "processing" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, collection)
}

func (a *API) searchBGG(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if len([]rune(query)) < 2 || len([]rune(query)) > 100 {
		writeError(w, http.StatusBadRequest, "search query must be 2 to 100 characters")
		return
	}
	result, err := a.bgg.Search(r.Context(), query)
	if err != nil {
		writeBGGError(w, err)
		return
	}
	status := http.StatusOK
	if result.Status == "processing" {
		status = http.StatusAccepted
	} else {
		result.Games = search.Sort(query, result.Games, gameName)
	}
	writeJSON(w, status, result)
}

func (a *API) getBGGRules(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(path.Clean(r.URL.Path), "/")
	if len(parts) != 6 {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	gameID, err := strconv.Atoi(parts[4])
	if err != nil || gameID <= 0 {
		writeError(w, http.StatusBadRequest, "game ID must be positive")
		return
	}
	result, err := a.bgg.Rules(r.Context(), gameID)
	if err != nil {
		writeBGGError(w, err)
		return
	}
	status := http.StatusOK
	if result.Status == "processing" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, result)
}

func writeBGGError(w http.ResponseWriter, err error) {
	var limit *bgg.RateLimitError
	if errors.As(err, &limit) {
		w.Header().Set("Retry-After", strconv.Itoa(limit.RetryAfterSeconds))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "BGG is limiting requests", "retryAfterSeconds": limit.RetryAfterSeconds})
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}

func (a *API) createTable(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	user, authenticated, err := a.optionalUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid session")
		return
	}
	table, err := a.store.CreateTable(input.Name)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if authenticated {
		if err := a.store.LinkUserTable(user.ID, table.Code); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, tableWithToken(table))
}

func (a *API) currentTableSession(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSuffix(strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/tables/"), "/current-session")
	if code == "" || strings.Contains(code, "/") {
		writeError(w, http.StatusBadRequest, "invalid table code")
		return
	}
	session, err := a.store.ActiveSessionByTable(code)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	a.writeSession(w, http.StatusOK, session)
}

// listRules returns public sheets plus the caller's own; private sheets of
// other people never leave the server.
func (a *API) listRules(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.optionalUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid session")
		return
	}
	rules, err := a.store.ListRules(user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (a *API) searchPublicRules(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if len([]rune(query)) > 100 {
		writeError(w, http.StatusBadRequest, "search query is too long")
		return
	}
	bggID := 0
	if raw := r.URL.Query().Get("bggId"); raw != "" {
		var err error
		bggID, err = strconv.Atoi(raw)
		if err != nil || bggID <= 0 {
			writeError(w, http.StatusBadRequest, "bggId must be positive")
			return
		}
	}
	rules, err := a.store.SearchPublicRules(query, bggID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (a *API) createRule(w http.ResponseWriter, r *http.Request) {
	var input domain.ScoringRule
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.RulebookID != "" {
		if _, err := a.store.GetRulebook(input.RulebookID); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	user, authenticated, err := a.optionalUser(r)
	if err != nil || (input.IsPublic && !authenticated) {
		writeError(w, http.StatusUnauthorized, "login required")
		return
	}
	input.OwnerID = user.ID
	rule, err := a.store.CreateRule(input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (a *API) createSession(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(path.Clean(r.URL.Path), "/")
	if len(parts) != 5 {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	var input struct {
		RuleID  string          `json:"ruleId"`
		Players []domain.Player `json:"players"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	user, authenticated, err := a.optionalUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid session")
		return
	}
	session, err := a.store.CreateSession(parts[3], r.Header.Get("X-Table-Token"), input.RuleID, input.Players)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if authenticated && len(session.Players) > 0 {
		if err := a.store.LinkUserSession(user.ID, session.ID, session.Players[0].ID); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	a.writeSession(w, http.StatusCreated, session)
}

func (a *API) scheduledGames(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(path.Clean(r.URL.Path), "/")
	if len(parts) != 5 {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	code, token := parts[3], r.Header.Get("X-Table-Token")
	if r.Method == http.MethodGet {
		games, err := a.store.ListScheduledGames(code, token)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, games)
		return
	}
	var input domain.ScheduledGame
	if !decodeJSON(w, r, &input) {
		return
	}
	game, err := a.store.CreateScheduledGame(code, token, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, game)
}

func (a *API) setScheduledGameRule(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(path.Clean(r.URL.Path), "/")
	if len(parts) != 5 {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	var input struct {
		RuleID string `json:"ruleId"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	game, err := a.store.SetScheduledGameRule(parts[3], r.Header.Get("X-Table-Token"), input.RuleID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, game)
}

func (a *API) setScheduledGameSession(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(path.Clean(r.URL.Path), "/")
	if len(parts) != 5 {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	var input struct {
		SessionID string `json:"sessionId"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	game, err := a.store.SetScheduledGameSession(parts[3], r.Header.Get("X-Table-Token"), input.SessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, game)
}

func (a *API) updateScheduledGame(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/scheduled-games/")
	var input domain.ScheduledGame
	if !decodeJSON(w, r, &input) {
		return
	}
	game, err := a.store.UpdateScheduledGame(id, r.Header.Get("X-Table-Token"), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, game)
}

func (a *API) deleteScheduledGame(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/scheduled-games/")
	if err := a.store.DeleteScheduledGame(id, r.Header.Get("X-Table-Token")); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) getSession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/sessions/")
	session, err := a.store.GetSession(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	a.writeSession(w, http.StatusOK, session)
}

func (a *API) addPlayer(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/sessions/"), "/players")
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	user, authenticated, err := a.optionalUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid session")
		return
	}
	userID := ""
	if authenticated {
		userID = user.ID
	}
	session, err := a.store.AddPlayer(id, input.Name, userID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	a.writeSession(w, http.StatusOK, session)
}

func (a *API) updateScores(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/sessions/"), "/scores")
	var input struct {
		Values map[string]map[string]int `json:"values"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	session, err := a.store.UpdateScores(id, input.Values)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	a.writeSession(w, http.StatusOK, session)
}

func (a *API) setScore(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/sessions/"), "/scores")
	var input struct {
		PlayerID string `json:"playerId"`
		FieldID  string `json:"fieldId"`
		Value    int    `json:"value"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	session, err := a.store.SetScore(id, input.PlayerID, input.FieldID, input.Value)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	a.writeSession(w, http.StatusOK, session)
}

func (a *API) adjustPoints(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/sessions/"), "/points")
	var input struct {
		PlayerID string `json:"playerId"`
		Delta    int    `json:"delta"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	session, err := a.store.AdjustPoints(id, input.PlayerID, input.Delta)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	a.writeSession(w, http.StatusOK, session)
}

func (a *API) finishSession(w http.ResponseWriter, r *http.Request) {
	a.changeHostSession(w, r, "finish", a.store.FinishSession)
}

func (a *API) reopenSession(w http.ResponseWriter, r *http.Request) {
	a.changeHostSession(w, r, "reopen", a.store.ReopenSession)
}

func (a *API) pauseSession(w http.ResponseWriter, r *http.Request) {
	a.changeHostSession(w, r, "pause", a.store.PauseSession)
}

func (a *API) resumeSession(w http.ResponseWriter, r *http.Request) {
	a.changeHostSession(w, r, "resume", a.store.ResumeSession)
}

func (a *API) changeHostSession(w http.ResponseWriter, r *http.Request, action string, update func(string) (domain.ScoreSession, error)) {
	id := strings.TrimSuffix(strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/sessions/"), "/"+action)
	current, err := a.store.GetSession(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	table, err := a.store.GetTable(current.TableCode)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(table.HostToken), []byte(r.Header.Get("X-Table-Token"))) != 1 {
		writeError(w, http.StatusForbidden, "host token required")
		return
	}
	session, err := update(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	a.writeSession(w, http.StatusOK, session)
}

const maxBoardPhotoBytes = 5 << 20

func (a *API) saveBoardPhoto(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/sessions/"), "/board-photo")
	session, err := a.store.GetSession(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	table, err := a.store.GetTable(session.TableCode)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(table.HostToken), []byte(r.Header.Get("X-Table-Token"))) != 1 {
		writeError(w, http.StatusForbidden, "host token required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBoardPhotoBytes+(1<<20))
	if err := r.ParseMultipartForm(maxBoardPhotoBytes); err != nil {
		writeError(w, http.StatusBadRequest, "an image up to 5 MB is required")
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxBoardPhotoBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxBoardPhotoBytes {
		writeError(w, http.StatusBadRequest, "an image up to 5 MB is required")
		return
	}
	contentType := http.DetectContentType(data)
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
		writeError(w, http.StatusBadRequest, "JPEG, PNG or WebP image required")
		return
	}
	session, err = a.store.SaveBoardPhoto(id, data, contentType)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	a.writeSession(w, http.StatusOK, session)
}

func (a *API) getBoardPhoto(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/sessions/"), "/board-photo")
	data, contentType, err := a.store.GetBoardPhoto(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (a *API) writeSession(w http.ResponseWriter, status int, session domain.ScoreSession) {
	result, err := a.sessionResult(session)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, status, result)
}

func (a *API) sessionResult(session domain.ScoreSession) (sessionResponse, error) {
	rule, err := a.store.GetRule(session.RuleID)
	if err != nil {
		return sessionResponse{}, err
	}
	fields := make(map[string]domain.ScoreField, len(rule.Fields))
	for _, field := range rule.Fields {
		fields[field.ID] = field
	}
	totals := make([]playerTotal, 0, len(session.Players))
	for _, player := range session.Players {
		total := session.ManualPoints[player.ID]
		for fieldID, value := range session.Values[player.ID] {
			field := fields[fieldID]
			if field.Kind == domain.FieldKindManual {
				total += value
			} else {
				total += value * field.PointsPerUnit
			}
		}
		totals = append(totals, playerTotal{PlayerID: player.ID, Total: total})
	}
	winners := []playerTotal{}
	if session.Status == "finished" {
		for _, result := range totals {
			if len(winners) == 0 || (rule.WinCondition == domain.WinConditionLowest && result.Total < winners[0].Total) || (rule.WinCondition != domain.WinConditionLowest && result.Total > winners[0].Total) {
				winners = []playerTotal{result}
			} else if result.Total == winners[0].Total {
				winners = append(winners, result)
			}
		}
	}
	return sessionResponse{ScoreSession: session, DurationSeconds: session.DurationSeconds(time.Now().UTC()), Totals: totals, Winners: winners}, nil
}
