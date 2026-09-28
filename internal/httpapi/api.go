package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"tablescore-api/handlers"
	"tablescore-api/internal/bgg"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/pdfreader"
	"tablescore-api/internal/store"
)

type API struct {
	foundation *handlers.Deps
	store      store.Repository
	bgg        *bgg.Client
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
	return &API{store: s, bgg: client}
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
	writeJSON(w, http.StatusOK, result)
}

func (a *API) getBGGCollection(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/bgg/collections/")
	collection, err := a.bgg.Collection(r.Context(), username)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	status := http.StatusOK
	if collection.Status == "processing" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, collection)
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
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	status := http.StatusOK
	if result.Status == "processing" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, result)
}

func (a *API) createTable(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	table, err := a.store.CreateTable(input.Name)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"code": table.Code, "name": table.Name, "hostToken": table.HostToken, "createdAt": table.CreatedAt})
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

func (a *API) listRules(w http.ResponseWriter) {
	rules, err := a.store.ListRules()
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
	if input.IsPublic {
		if _, ok := a.requireUser(w, r); !ok {
			return
		}
	}
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
	session, err := a.store.AddPlayer(id, input.Name)
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

func (a *API) createPDFImport(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(path.Clean(r.URL.Path), "/")
	if len(parts) != 5 {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "a PDF file up to 20 MB is required")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer func() { _ = file.Close() }()
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".pdf") {
		writeError(w, http.StatusBadRequest, "file must be a PDF")
		return
	}
	job, err := a.store.CreatePDFImport(parts[3], header.Filename)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (a *API) getPDFImport(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/pdf-imports/")
	job, err := a.store.GetPDFImport(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
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
