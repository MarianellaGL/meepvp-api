package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"tablescore-api/internal/domain"
)

var (
	ErrNotFound   = errors.New("resource not found")
	ErrForbidden  = errors.New("forbidden")
	ErrValidation = errors.New("invalid input")
)

type MemoryStore struct {
	mu       sync.RWMutex
	tables   map[string]domain.Table
	rules    map[string]domain.ScoringRule
	sessions map[string]domain.ScoreSession
	imports  map[string]domain.PDFImport
	plans    map[string]domain.ScheduledGame
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tables: map[string]domain.Table{}, rules: map[string]domain.ScoringRule{},
		sessions: map[string]domain.ScoreSession{}, imports: map[string]domain.PDFImport{}, plans: map[string]domain.ScheduledGame{},
	}
}

func (s *MemoryStore) CreateTable(name string) (domain.Table, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	table := domain.Table{Code: shortCode(), Name: strings.TrimSpace(name), HostToken: randomID(), CreatedAt: time.Now().UTC()}
	s.tables[table.Code] = table
	return table, nil
}

func (s *MemoryStore) GetTable(code string) (domain.Table, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	table, ok := s.tables[strings.ToUpper(code)]
	if !ok {
		return domain.Table{}, ErrNotFound
	}
	return table, nil
}

func (s *MemoryStore) CreateRule(rule domain.ScoringRule) (domain.ScoringRule, error) {
	if rule.BGGID < 0 || strings.TrimSpace(rule.GameName) == "" || strings.TrimSpace(rule.Name) == "" || len(rule.Fields) == 0 {
		return domain.ScoringRule{}, ErrValidation
	}
	if rule.WinCondition == "" {
		rule.WinCondition = domain.WinConditionHighest
	}
	if rule.WinCondition != domain.WinConditionHighest && rule.WinCondition != domain.WinConditionLowest {
		return domain.ScoringRule{}, ErrValidation
	}
	for i := range rule.Fields {
		if strings.TrimSpace(rule.Fields[i].Name) == "" {
			return domain.ScoringRule{}, ErrValidation
		}
		kind := rule.Fields[i].Kind
		if kind != domain.FieldKindCheckbox && kind != domain.FieldKindCounter && kind != domain.FieldKindManual {
			return domain.ScoringRule{}, ErrValidation
		}
		rule.Fields[i].ID = randomID()
	}
	rule.ID, rule.CreatedAt = randomID(), time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules[rule.ID] = rule
	return rule, nil
}

func (s *MemoryStore) ListRules() ([]domain.ScoringRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rules := make([]domain.ScoringRule, 0, len(s.rules))
	for _, rule := range s.rules {
		rules = append(rules, rule)
	}
	return rules, nil
}

func (s *MemoryStore) SearchPublicRules(query string, bggID int) ([]domain.ScoringRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	query = strings.ToLower(strings.TrimSpace(query))
	rules := []domain.ScoringRule{}
	for _, rule := range s.rules {
		if !rule.IsPublic || (bggID > 0 && rule.BGGID != bggID) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(rule.GameName+" "+rule.Name), query) {
			continue
		}
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].CreatedAt.After(rules[j].CreatedAt) })
	if len(rules) > 50 {
		rules = rules[:50]
	}
	return rules, nil
}

func (s *MemoryStore) GetRule(id string) (domain.ScoringRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rule, ok := s.rules[id]
	if !ok {
		return domain.ScoringRule{}, ErrNotFound
	}
	return rule, nil
}

func (s *MemoryStore) CreateSession(tableCode, hostToken, ruleID string, players []domain.Player) (domain.ScoreSession, error) {
	if len(players) == 0 {
		return domain.ScoreSession{}, ErrValidation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	table, ok := s.tables[strings.ToUpper(tableCode)]
	if !ok {
		return domain.ScoreSession{}, ErrNotFound
	}
	if table.HostToken != hostToken {
		return domain.ScoreSession{}, ErrForbidden
	}
	if _, ok := s.rules[ruleID]; !ok {
		return domain.ScoreSession{}, ErrNotFound
	}
	values := make(map[string]map[string]int, len(players))
	for i := range players {
		if strings.TrimSpace(players[i].Name) == "" {
			return domain.ScoreSession{}, ErrValidation
		}
		players[i].ID = randomID()
		values[players[i].ID] = map[string]int{}
	}
	now := time.Now().UTC()
	session := domain.ScoreSession{ID: randomID(), TableCode: table.Code, RuleID: ruleID, Players: players, Values: values, Status: "active", CreatedAt: now, LastModified: now}
	s.sessions[session.ID] = session
	return session, nil
}

func (s *MemoryStore) GetSession(id string) (domain.ScoreSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	if !ok {
		return domain.ScoreSession{}, ErrNotFound
	}
	return session, nil
}

func (s *MemoryStore) AddPlayer(sessionID, name string) (domain.ScoreSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return domain.ScoreSession{}, ErrNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" || session.Status != "active" {
		return domain.ScoreSession{}, ErrValidation
	}
	for _, player := range session.Players {
		if strings.EqualFold(player.Name, name) {
			return session, nil
		}
	}
	player := domain.Player{ID: randomID(), Name: name}
	session.Players = append(session.Players, player)
	if session.Values == nil {
		session.Values = map[string]map[string]int{}
	}
	session.Values[player.ID] = map[string]int{}
	session.LastModified = time.Now().UTC()
	s.sessions[sessionID] = session
	return session, nil
}

func (s *MemoryStore) UpdateScores(sessionID string, values map[string]map[string]int) (domain.ScoreSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return domain.ScoreSession{}, ErrNotFound
	}
	if session.Status != "active" {
		return domain.ScoreSession{}, ErrValidation
	}
	rule, ok := s.rules[session.RuleID]
	if !ok {
		return domain.ScoreSession{}, ErrNotFound
	}
	fieldIDs, playerIDs := map[string]bool{}, map[string]bool{}
	for _, field := range rule.Fields {
		fieldIDs[field.ID] = true
	}
	for _, player := range session.Players {
		playerIDs[player.ID] = true
	}
	for playerID, scores := range values {
		if !playerIDs[playerID] {
			return domain.ScoreSession{}, ErrValidation
		}
		for fieldID := range scores {
			if !fieldIDs[fieldID] {
				return domain.ScoreSession{}, ErrValidation
			}
		}
	}
	session.Values, session.LastModified = values, time.Now().UTC()
	s.sessions[sessionID] = session
	return session, nil
}

func (s *MemoryStore) SetScore(sessionID, playerID, fieldID string, value int) (domain.ScoreSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return domain.ScoreSession{}, ErrNotFound
	}
	if session.Status != "active" {
		return domain.ScoreSession{}, ErrValidation
	}
	rule, ok := s.rules[session.RuleID]
	if !ok {
		return domain.ScoreSession{}, ErrNotFound
	}
	if err := validateScoreField(session, rule, playerID, fieldID); err != nil {
		return domain.ScoreSession{}, err
	}
	if session.Values == nil {
		session.Values = map[string]map[string]int{}
	}
	if session.Values[playerID] == nil {
		session.Values[playerID] = map[string]int{}
	}
	session.Values[playerID][fieldID] = value
	session.LastModified = time.Now().UTC()
	s.sessions[sessionID] = session
	return session, nil
}

func (s *MemoryStore) AdjustPoints(sessionID, playerID string, delta int) (domain.ScoreSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return domain.ScoreSession{}, ErrNotFound
	}
	if session.Status != "active" || delta == 0 || delta < -10000 || delta > 10000 {
		return domain.ScoreSession{}, ErrValidation
	}
	playerFound := false
	for _, player := range session.Players {
		if player.ID == playerID {
			playerFound = true
			break
		}
	}
	if !playerFound {
		return domain.ScoreSession{}, ErrValidation
	}
	if session.ManualPoints == nil {
		session.ManualPoints = map[string]int{}
	}
	session.ManualPoints[playerID] += delta
	session.LastModified = time.Now().UTC()
	s.sessions[sessionID] = session
	return session, nil
}

func (s *MemoryStore) FinishSession(id string) (domain.ScoreSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return domain.ScoreSession{}, ErrNotFound
	}
	session.Status, session.LastModified = "finished", time.Now().UTC()
	s.sessions[id] = session
	return session, nil
}

func (s *MemoryStore) ReopenSession(id string) (domain.ScoreSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return domain.ScoreSession{}, ErrNotFound
	}
	session.Status, session.LastModified = "active", time.Now().UTC()
	s.sessions[id] = session
	return session, nil
}

func (s *MemoryStore) CreatePDFImport(ruleID, fileName string) (domain.PDFImport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rules[ruleID]; !ok {
		return domain.PDFImport{}, ErrNotFound
	}
	job := domain.PDFImport{ID: randomID(), RuleID: ruleID, Status: "queued", FileName: fileName, CreatedAt: time.Now().UTC()}
	s.imports[job.ID] = job
	return job, nil
}

func (s *MemoryStore) GetPDFImport(id string) (domain.PDFImport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.imports[id]
	if !ok {
		return domain.PDFImport{}, ErrNotFound
	}
	return job, nil
}

func (s *MemoryStore) CreateScheduledGame(tableCode, hostToken string, game domain.ScheduledGame) (domain.ScheduledGame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	table, ok := s.tables[strings.ToUpper(tableCode)]
	if !ok {
		return domain.ScheduledGame{}, ErrNotFound
	}
	if table.HostToken != hostToken {
		return domain.ScheduledGame{}, ErrForbidden
	}
	if err := prepareScheduledGame(&game); err != nil {
		return domain.ScheduledGame{}, err
	}
	if game.RuleID != "" {
		if _, ok := s.rules[game.RuleID]; !ok {
			return domain.ScheduledGame{}, ErrNotFound
		}
	}
	game.ID, game.TableCode, game.CreatedAt = randomID(), table.Code, time.Now().UTC()
	s.plans[game.ID] = game
	return game, nil
}

func (s *MemoryStore) ListScheduledGames(tableCode, hostToken string) ([]domain.ScheduledGame, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	table, ok := s.tables[strings.ToUpper(tableCode)]
	if !ok {
		return nil, ErrNotFound
	}
	if table.HostToken != hostToken {
		return nil, ErrForbidden
	}
	games := []domain.ScheduledGame{}
	for _, game := range s.plans {
		if game.TableCode == table.Code {
			games = append(games, game)
		}
	}
	sort.Slice(games, func(i, j int) bool { return games[i].ScheduledAt.Before(games[j].ScheduledAt) })
	return games, nil
}

func (s *MemoryStore) SetScheduledGameRule(id, hostToken, ruleID string) (domain.ScheduledGame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	game, ok := s.plans[id]
	if !ok {
		return domain.ScheduledGame{}, ErrNotFound
	}
	if s.tables[game.TableCode].HostToken != hostToken {
		return domain.ScheduledGame{}, ErrForbidden
	}
	if ruleID != "" {
		if _, ok := s.rules[ruleID]; !ok {
			return domain.ScheduledGame{}, ErrNotFound
		}
	}
	game.RuleID = ruleID
	s.plans[id] = game
	return game, nil
}

func (s *MemoryStore) SetScheduledGameSession(id, hostToken, sessionID string) (domain.ScheduledGame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	game, ok := s.plans[id]
	if !ok {
		return domain.ScheduledGame{}, ErrNotFound
	}
	if s.tables[game.TableCode].HostToken != hostToken {
		return domain.ScheduledGame{}, ErrForbidden
	}
	session, ok := s.sessions[sessionID]
	if !ok {
		return domain.ScheduledGame{}, ErrNotFound
	}
	if game.RuleID == "" || session.TableCode != game.TableCode || session.RuleID != game.RuleID || (game.SessionID != "" && game.SessionID != sessionID) {
		return domain.ScheduledGame{}, ErrValidation
	}
	game.SessionID = sessionID
	s.plans[id] = game
	return game, nil
}

func prepareScheduledGame(game *domain.ScheduledGame) error {
	game.GameName = strings.TrimSpace(game.GameName)
	if game.GameName == "" || len([]rune(game.GameName)) > 120 || game.ScheduledAt.IsZero() || game.ScheduledAt.Before(time.Now().Add(-5*time.Minute)) || game.ScheduledAt.After(time.Now().AddDate(2, 0, 0)) || len(game.Players) > 20 {
		return ErrValidation
	}
	for i, name := range game.Players {
		name = strings.TrimSpace(name)
		if name == "" || len([]rune(name)) > 80 {
			return ErrValidation
		}
		game.Players[i] = name
	}
	game.ScheduledAt = game.ScheduledAt.UTC()
	return nil
}

func randomID() string  { b := make([]byte, 12); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func shortCode() string { return strings.ToUpper(randomID()[:6]) }
