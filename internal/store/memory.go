package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
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
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tables: map[string]domain.Table{}, rules: map[string]domain.ScoringRule{},
		sessions: map[string]domain.ScoreSession{}, imports: map[string]domain.PDFImport{},
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
	if strings.TrimSpace(rule.GameName) == "" || strings.TrimSpace(rule.Name) == "" || len(rule.Fields) == 0 {
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

func (s *MemoryStore) UpdateScores(sessionID string, values map[string]map[string]int) (domain.ScoreSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return domain.ScoreSession{}, ErrNotFound
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

func randomID() string  { b := make([]byte, 12); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func shortCode() string { return strings.ToUpper(randomID()[:6]) }
