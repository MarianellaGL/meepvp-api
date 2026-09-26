package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"

	"tablescore-api/internal/bgg"
	"tablescore-api/internal/domain"
	"tablescore-api/internal/store"
)

type API struct {
	store store.Repository
	bgg   *bgg.Client
}

type playerTotal struct {
	PlayerID string `json:"playerId"`
	Total    int    `json:"total"`
}

type sessionResponse struct {
	domain.ScoreSession
	Totals []playerTotal `json:"totals"`
}

func New(s store.Repository, clients ...*bgg.Client) *API {
	client := bgg.NewFromEnvironment()
	if len(clients) > 0 && clients[0] != nil {
		client = clients[0]
	}
	return &API{store: s, bgg: client}
}

func (a *API) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Table-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		a.route(w, r)
	})
}

func (a *API) route(w http.ResponseWriter, r *http.Request) {
	p := path.Clean(r.URL.Path)
	switch {
	case r.Method == http.MethodGet && p == "/health":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case r.Method == http.MethodPost && p == "/v1/tables":
		a.createTable(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/v1/bgg/collections/"):
		a.getBGGCollection(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(p, "/v1/tables/") && strings.HasSuffix(p, "/sessions"):
		a.createSession(w, r)
	case r.Method == http.MethodPost && p == "/v1/scoring-rules":
		a.createRule(w, r)
	case r.Method == http.MethodGet && p == "/v1/scoring-rules":
		a.listRules(w)
	case r.Method == http.MethodPost && strings.HasPrefix(p, "/v1/scoring-rules/") && strings.HasSuffix(p, "/pdf-imports"):
		a.createPDFImport(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/v1/sessions/"):
		a.getSession(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(p, "/v1/sessions/") && strings.HasSuffix(p, "/scores"):
		a.updateScores(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(p, "/v1/sessions/") && strings.HasSuffix(p, "/finish"):
		a.finishSession(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/v1/pdf-imports/"):
		a.getPDFImport(w, r)
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
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

func (a *API) listRules(w http.ResponseWriter) {
	rules, err := a.store.ListRules()
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
	session, err := a.store.CreateSession(parts[3], r.Header.Get("X-Table-Token"), input.RuleID, input.Players)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	a.writeSession(w, http.StatusCreated, session)
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

func (a *API) finishSession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(path.Clean(r.URL.Path), "/v1/sessions/"), "/finish")
	session, err := a.store.FinishSession(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	a.writeSession(w, http.StatusOK, session)
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
	defer file.Close()
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
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (a *API) writeSession(w http.ResponseWriter, status int, session domain.ScoreSession) {
	rule, err := a.store.GetRule(session.RuleID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	fields := make(map[string]domain.ScoreField, len(rule.Fields))
	for _, field := range rule.Fields {
		fields[field.ID] = field
	}
	totals := make([]playerTotal, 0, len(session.Players))
	for _, player := range session.Players {
		total := 0
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
	writeJSON(w, status, sessionResponse{ScoreSession: session, Totals: totals})
}
