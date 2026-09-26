package store

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"tablescore-api/internal/domain"
)

type PostgresStore struct{ db *sql.DB }

func OpenPostgres(databaseURL string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Close() error { return s.db.Close() }

func (s *PostgresStore) Migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS game_tables (code TEXT PRIMARY KEY, host_token TEXT NOT NULL, data JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL);
CREATE TABLE IF NOT EXISTS scoring_rules (id TEXT PRIMARY KEY, data JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL);
CREATE TABLE IF NOT EXISTS score_sessions (id TEXT PRIMARY KEY, table_code TEXT NOT NULL REFERENCES game_tables(code), rule_id TEXT NOT NULL REFERENCES scoring_rules(id), data JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL);
CREATE TABLE IF NOT EXISTS pdf_imports (id TEXT PRIMARY KEY, rule_id TEXT NOT NULL REFERENCES scoring_rules(id), data JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL);
CREATE INDEX IF NOT EXISTS score_sessions_table_code_idx ON score_sessions(table_code);
CREATE INDEX IF NOT EXISTS scoring_rules_created_at_idx ON scoring_rules(created_at DESC);`)
	return err
}

func (s *PostgresStore) CreateTable(name string) (domain.Table, error) {
	table := domain.Table{Code: shortCode(), Name: strings.TrimSpace(name), HostToken: randomID(), CreatedAt: time.Now().UTC()}
	payload, _ := json.Marshal(table)
	_, err := s.db.Exec(`INSERT INTO game_tables (code, host_token, data, created_at) VALUES ($1, $2, $3, $4)`, table.Code, table.HostToken, payload, table.CreatedAt)
	return table, err
}

func (s *PostgresStore) GetTable(code string) (domain.Table, error) {
	var payload []byte
	var token string
	err := s.db.QueryRow(`SELECT data, host_token FROM game_tables WHERE code = $1`, strings.ToUpper(code)).Scan(&payload, &token)
	if err == sql.ErrNoRows {
		return domain.Table{}, ErrNotFound
	}
	if err != nil {
		return domain.Table{}, err
	}
	var table domain.Table
	if err := json.Unmarshal(payload, &table); err != nil {
		return domain.Table{}, err
	}
	table.HostToken = token
	return table, nil
}

func (s *PostgresStore) CreateRule(rule domain.ScoringRule) (domain.ScoringRule, error) {
	if err := prepareRule(&rule); err != nil {
		return domain.ScoringRule{}, err
	}
	payload, _ := json.Marshal(rule)
	_, err := s.db.Exec(`INSERT INTO scoring_rules (id, data, created_at) VALUES ($1, $2, $3)`, rule.ID, payload, rule.CreatedAt)
	return rule, err
}

func (s *PostgresStore) ListRules() ([]domain.ScoringRule, error) {
	rows, err := s.db.Query(`SELECT data FROM scoring_rules ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := []domain.ScoringRule{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var rule domain.ScoringRule
		if err := json.Unmarshal(payload, &rule); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *PostgresStore) GetRule(id string) (domain.ScoringRule, error) {
	var payload []byte
	err := s.db.QueryRow(`SELECT data FROM scoring_rules WHERE id = $1`, id).Scan(&payload)
	if err == sql.ErrNoRows {
		return domain.ScoringRule{}, ErrNotFound
	}
	if err != nil {
		return domain.ScoringRule{}, err
	}
	var rule domain.ScoringRule
	if err := json.Unmarshal(payload, &rule); err != nil {
		return domain.ScoringRule{}, err
	}
	return rule, nil
}

func (s *PostgresStore) CreateSession(tableCode, hostToken, ruleID string, players []domain.Player) (domain.ScoreSession, error) {
	if len(players) == 0 {
		return domain.ScoreSession{}, ErrValidation
	}
	table, err := s.GetTable(tableCode)
	if err != nil {
		return domain.ScoreSession{}, err
	}
	if table.HostToken != hostToken {
		return domain.ScoreSession{}, ErrForbidden
	}
	if _, err := s.GetRule(ruleID); err != nil {
		return domain.ScoreSession{}, err
	}
	values := map[string]map[string]int{}
	for i := range players {
		if strings.TrimSpace(players[i].Name) == "" {
			return domain.ScoreSession{}, ErrValidation
		}
		players[i].ID = randomID()
		values[players[i].ID] = map[string]int{}
	}
	now := time.Now().UTC()
	session := domain.ScoreSession{ID: randomID(), TableCode: table.Code, RuleID: ruleID, Players: players, Values: values, Status: "active", CreatedAt: now, LastModified: now}
	payload, _ := json.Marshal(session)
	_, err = s.db.Exec(`INSERT INTO score_sessions (id, table_code, rule_id, data, created_at) VALUES ($1, $2, $3, $4, $5)`, session.ID, session.TableCode, session.RuleID, payload, now)
	return session, err
}

func (s *PostgresStore) GetSession(id string) (domain.ScoreSession, error) {
	var payload []byte
	err := s.db.QueryRow(`SELECT data FROM score_sessions WHERE id = $1`, id).Scan(&payload)
	if err == sql.ErrNoRows {
		return domain.ScoreSession{}, ErrNotFound
	}
	if err != nil {
		return domain.ScoreSession{}, err
	}
	var session domain.ScoreSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return domain.ScoreSession{}, err
	}
	return session, nil
}

func (s *PostgresStore) UpdateScores(sessionID string, values map[string]map[string]int) (domain.ScoreSession, error) {
	session, err := s.GetSession(sessionID)
	if err != nil {
		return domain.ScoreSession{}, err
	}
	rule, err := s.GetRule(session.RuleID)
	if err != nil {
		return domain.ScoreSession{}, err
	}
	if err := validateScores(session, rule, values); err != nil {
		return domain.ScoreSession{}, err
	}
	session.Values, session.LastModified = values, time.Now().UTC()
	payload, _ := json.Marshal(session)
	result, err := s.db.Exec(`UPDATE score_sessions SET data = $1 WHERE id = $2`, payload, sessionID)
	if err != nil {
		return domain.ScoreSession{}, err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return domain.ScoreSession{}, ErrNotFound
	}
	return session, nil
}

func (s *PostgresStore) FinishSession(id string) (domain.ScoreSession, error) {
	session, err := s.GetSession(id)
	if err != nil {
		return domain.ScoreSession{}, err
	}
	session.Status, session.LastModified = "finished", time.Now().UTC()
	payload, _ := json.Marshal(session)
	result, err := s.db.Exec(`UPDATE score_sessions SET data = $1 WHERE id = $2`, payload, id)
	if err != nil {
		return domain.ScoreSession{}, err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return domain.ScoreSession{}, ErrNotFound
	}
	return session, nil
}

func (s *PostgresStore) CreatePDFImport(ruleID, fileName string) (domain.PDFImport, error) {
	if _, err := s.GetRule(ruleID); err != nil {
		return domain.PDFImport{}, err
	}
	job := domain.PDFImport{ID: randomID(), RuleID: ruleID, Status: "queued", FileName: fileName, CreatedAt: time.Now().UTC()}
	payload, _ := json.Marshal(job)
	_, err := s.db.Exec(`INSERT INTO pdf_imports (id, rule_id, data, created_at) VALUES ($1, $2, $3, $4)`, job.ID, ruleID, payload, job.CreatedAt)
	return job, err
}

func (s *PostgresStore) GetPDFImport(id string) (domain.PDFImport, error) {
	var payload []byte
	err := s.db.QueryRow(`SELECT data FROM pdf_imports WHERE id = $1`, id).Scan(&payload)
	if err == sql.ErrNoRows {
		return domain.PDFImport{}, ErrNotFound
	}
	if err != nil {
		return domain.PDFImport{}, err
	}
	var job domain.PDFImport
	if err := json.Unmarshal(payload, &job); err != nil {
		return domain.PDFImport{}, err
	}
	return job, nil
}

func prepareRule(rule *domain.ScoringRule) error {
	if strings.TrimSpace(rule.GameName) == "" || strings.TrimSpace(rule.Name) == "" || len(rule.Fields) == 0 {
		return ErrValidation
	}
	if rule.WinCondition == "" {
		rule.WinCondition = domain.WinConditionHighest
	}
	if rule.WinCondition != domain.WinConditionHighest && rule.WinCondition != domain.WinConditionLowest {
		return ErrValidation
	}
	for i := range rule.Fields {
		if strings.TrimSpace(rule.Fields[i].Name) == "" {
			return ErrValidation
		}
		kind := rule.Fields[i].Kind
		if kind != domain.FieldKindCheckbox && kind != domain.FieldKindCounter && kind != domain.FieldKindManual {
			return ErrValidation
		}
		rule.Fields[i].ID = randomID()
	}
	rule.ID, rule.CreatedAt = randomID(), time.Now().UTC()
	return nil
}

func validateScores(session domain.ScoreSession, rule domain.ScoringRule, values map[string]map[string]int) error {
	fields, players := map[string]bool{}, map[string]bool{}
	for _, field := range rule.Fields {
		fields[field.ID] = true
	}
	for _, player := range session.Players {
		players[player.ID] = true
	}
	for playerID, scores := range values {
		if !players[playerID] {
			return ErrValidation
		}
		for fieldID := range scores {
			if !fields[fieldID] {
				return ErrValidation
			}
		}
	}
	return nil
}

var _ Repository = (*PostgresStore)(nil)
