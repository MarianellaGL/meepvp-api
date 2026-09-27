package store

import (
	"database/sql"
	"encoding/json"
	"errors"
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
CREATE TABLE IF NOT EXISTS scheduled_games (id TEXT PRIMARY KEY, table_code TEXT NOT NULL REFERENCES game_tables(code), data JSONB NOT NULL, scheduled_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL);
CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, username TEXT NOT NULL, username_key TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL);
CREATE TABLE IF NOT EXISTS auth_sessions (token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, expires_at TIMESTAMPTZ NOT NULL);
CREATE TABLE IF NOT EXISTS user_game_sessions (user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, session_id TEXT NOT NULL REFERENCES score_sessions(id) ON DELETE CASCADE, player_id TEXT NOT NULL, PRIMARY KEY (user_id, session_id), UNIQUE (session_id, player_id));
CREATE TABLE IF NOT EXISTS session_board_photos (session_id TEXT PRIMARY KEY REFERENCES score_sessions(id) ON DELETE CASCADE, image_data BYTEA NOT NULL, content_type TEXT NOT NULL, uploaded_at TIMESTAMPTZ NOT NULL);
CREATE INDEX IF NOT EXISTS score_sessions_table_code_idx ON score_sessions(table_code);
CREATE INDEX IF NOT EXISTS scoring_rules_created_at_idx ON scoring_rules(created_at DESC);
CREATE INDEX IF NOT EXISTS scheduled_games_table_time_idx ON scheduled_games(table_code, scheduled_at);`)
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

func (s *PostgresStore) SearchPublicRules(query string, bggID int) ([]domain.ScoringRule, error) {
	rows, err := s.db.Query(`SELECT data FROM scoring_rules
WHERE data->>'isPublic' = 'true'
  AND ($1 = '' OR data->>'gameName' ILIKE '%' || $1 || '%' OR data->>'name' ILIKE '%' || $1 || '%')
  AND ($2 = 0 OR (data->>'bggId')::int = $2)
ORDER BY created_at DESC LIMIT 50`, strings.TrimSpace(query), bggID)
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
	session := domain.ScoreSession{ID: randomID(), TableCode: table.Code, RuleID: ruleID, Players: players, Values: values, Status: "active", RunningSince: &now, CreatedAt: now, LastModified: now}
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

func (s *PostgresStore) ActiveSessionByTable(code string) (domain.ScoreSession, error) {
	var payload []byte
	err := s.db.QueryRow(`SELECT data FROM score_sessions WHERE table_code = $1 AND data->>'status' IN ('active', 'paused') ORDER BY created_at DESC LIMIT 1`, strings.ToUpper(code)).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ScoreSession{}, ErrNotFound
	}
	if err != nil {
		return domain.ScoreSession{}, err
	}
	var session domain.ScoreSession
	return session, json.Unmarshal(payload, &session)
}

func (s *PostgresStore) AddPlayer(sessionID, name string) (domain.ScoreSession, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.ScoreSession{}, ErrValidation
	}
	tx, err := s.db.Begin()
	if err != nil {
		return domain.ScoreSession{}, err
	}
	defer tx.Rollback()
	var payload []byte
	if err := tx.QueryRow(`SELECT data FROM score_sessions WHERE id = $1 FOR UPDATE`, sessionID).Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return domain.ScoreSession{}, ErrNotFound
		}
		return domain.ScoreSession{}, err
	}
	var session domain.ScoreSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return domain.ScoreSession{}, err
	}
	if session.Status != "active" {
		return domain.ScoreSession{}, ErrValidation
	}
	for _, player := range session.Players {
		if strings.EqualFold(player.Name, name) {
			return session, tx.Commit()
		}
	}
	player := domain.Player{ID: randomID(), Name: name}
	session.Players = append(session.Players, player)
	if session.Values == nil {
		session.Values = map[string]map[string]int{}
	}
	session.Values[player.ID] = map[string]int{}
	session.LastModified = time.Now().UTC()
	payload, _ = json.Marshal(session)
	if _, err := tx.Exec(`UPDATE score_sessions SET data = $1 WHERE id = $2`, payload, sessionID); err != nil {
		return domain.ScoreSession{}, err
	}
	return session, tx.Commit()
}

func (s *PostgresStore) UpdateScores(sessionID string, values map[string]map[string]int) (domain.ScoreSession, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return domain.ScoreSession{}, err
	}
	defer tx.Rollback()
	var payload []byte
	if err := tx.QueryRow(`SELECT data FROM score_sessions WHERE id = $1 FOR UPDATE`, sessionID).Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return domain.ScoreSession{}, ErrNotFound
		}
		return domain.ScoreSession{}, err
	}
	var session domain.ScoreSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return domain.ScoreSession{}, err
	}
	if session.Status != "active" {
		return domain.ScoreSession{}, ErrValidation
	}
	rule, err := s.GetRule(session.RuleID)
	if err != nil {
		return domain.ScoreSession{}, err
	}
	if err := validateScores(session, rule, values); err != nil {
		return domain.ScoreSession{}, err
	}
	session.Values, session.LastModified = values, time.Now().UTC()
	payload, _ = json.Marshal(session)
	if _, err := tx.Exec(`UPDATE score_sessions SET data = $1 WHERE id = $2`, payload, sessionID); err != nil {
		return domain.ScoreSession{}, err
	}
	return session, tx.Commit()
}

func (s *PostgresStore) SetScore(sessionID, playerID, fieldID string, value int) (domain.ScoreSession, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return domain.ScoreSession{}, err
	}
	defer tx.Rollback()
	var payload []byte
	if err := tx.QueryRow(`SELECT data FROM score_sessions WHERE id = $1 FOR UPDATE`, sessionID).Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return domain.ScoreSession{}, ErrNotFound
		}
		return domain.ScoreSession{}, err
	}
	var session domain.ScoreSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return domain.ScoreSession{}, err
	}
	if session.Status != "active" {
		return domain.ScoreSession{}, ErrValidation
	}
	rule, err := s.GetRule(session.RuleID)
	if err != nil {
		return domain.ScoreSession{}, err
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
	payload, _ = json.Marshal(session)
	if _, err := tx.Exec(`UPDATE score_sessions SET data = $1 WHERE id = $2`, payload, sessionID); err != nil {
		return domain.ScoreSession{}, err
	}
	return session, tx.Commit()
}

func (s *PostgresStore) AdjustPoints(sessionID, playerID string, delta int) (domain.ScoreSession, error) {
	if delta == 0 || delta < -10000 || delta > 10000 {
		return domain.ScoreSession{}, ErrValidation
	}
	tx, err := s.db.Begin()
	if err != nil {
		return domain.ScoreSession{}, err
	}
	defer tx.Rollback()
	var payload []byte
	if err := tx.QueryRow(`SELECT data FROM score_sessions WHERE id = $1 FOR UPDATE`, sessionID).Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return domain.ScoreSession{}, ErrNotFound
		}
		return domain.ScoreSession{}, err
	}
	var session domain.ScoreSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return domain.ScoreSession{}, err
	}
	if session.Status != "active" {
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
	payload, _ = json.Marshal(session)
	if _, err := tx.Exec(`UPDATE score_sessions SET data = $1 WHERE id = $2`, payload, sessionID); err != nil {
		return domain.ScoreSession{}, err
	}
	return session, tx.Commit()
}

func (s *PostgresStore) FinishSession(id string) (domain.ScoreSession, error) {
	return s.updateSessionState(id, "finish")
}

func (s *PostgresStore) ReopenSession(id string) (domain.ScoreSession, error) {
	return s.updateSessionState(id, "reopen")
}

func (s *PostgresStore) PauseSession(id string) (domain.ScoreSession, error) {
	return s.updateSessionState(id, "pause")
}

func (s *PostgresStore) ResumeSession(id string) (domain.ScoreSession, error) {
	return s.updateSessionState(id, "resume")
}

func (s *PostgresStore) updateSessionState(id, action string) (domain.ScoreSession, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return domain.ScoreSession{}, err
	}
	defer tx.Rollback()
	var payload []byte
	if err := tx.QueryRow(`SELECT data FROM score_sessions WHERE id = $1 FOR UPDATE`, id).Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return domain.ScoreSession{}, ErrNotFound
		}
		return domain.ScoreSession{}, err
	}
	var session domain.ScoreSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return domain.ScoreSession{}, err
	}
	now := time.Now().UTC()
	switch action {
	case "pause":
		if session.Status != "active" {
			return domain.ScoreSession{}, ErrValidation
		}
		session.Pause(now)
	case "resume":
		if session.Status != "paused" {
			return domain.ScoreSession{}, ErrValidation
		}
		session.Resume(now)
	case "finish":
		if session.Status != "finished" {
			session.Finish(now)
		}
	case "reopen":
		if session.Status == "finished" {
			session.Resume(now)
		}
	}
	payload, _ = json.Marshal(session)
	if _, err := tx.Exec(`UPDATE score_sessions SET data = $1 WHERE id = $2`, payload, id); err != nil {
		return domain.ScoreSession{}, err
	}
	return session, tx.Commit()
}

func (s *PostgresStore) SaveBoardPhoto(id string, data []byte, contentType string) (domain.ScoreSession, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return domain.ScoreSession{}, err
	}
	defer tx.Rollback()
	var payload []byte
	if err := tx.QueryRow(`SELECT data FROM score_sessions WHERE id = $1 FOR UPDATE`, id).Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ScoreSession{}, ErrNotFound
		}
		return domain.ScoreSession{}, err
	}
	var session domain.ScoreSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return domain.ScoreSession{}, err
	}
	if session.Status == "finished" {
		return domain.ScoreSession{}, ErrValidation
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(`INSERT INTO session_board_photos (session_id, image_data, content_type, uploaded_at) VALUES ($1,$2,$3,$4) ON CONFLICT (session_id) DO UPDATE SET image_data = EXCLUDED.image_data, content_type = EXCLUDED.content_type, uploaded_at = EXCLUDED.uploaded_at`, id, data, contentType, now); err != nil {
		return domain.ScoreSession{}, err
	}
	session.BoardPhotoUpdatedAt = &now
	session.LastModified = now
	payload, _ = json.Marshal(session)
	if _, err := tx.Exec(`UPDATE score_sessions SET data = $1 WHERE id = $2`, payload, id); err != nil {
		return domain.ScoreSession{}, err
	}
	return session, tx.Commit()
}

func (s *PostgresStore) GetBoardPhoto(id string) ([]byte, string, error) {
	var data []byte
	var contentType string
	err := s.db.QueryRow(`SELECT image_data, content_type FROM session_board_photos WHERE session_id = $1`, id).Scan(&data, &contentType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return data, contentType, err
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

func (s *PostgresStore) CreateScheduledGame(tableCode, hostToken string, game domain.ScheduledGame) (domain.ScheduledGame, error) {
	table, err := s.GetTable(tableCode)
	if err != nil {
		return domain.ScheduledGame{}, err
	}
	if table.HostToken != hostToken {
		return domain.ScheduledGame{}, ErrForbidden
	}
	if err := prepareScheduledGame(&game); err != nil {
		return domain.ScheduledGame{}, err
	}
	if game.RuleID != "" {
		if _, err := s.GetRule(game.RuleID); err != nil {
			return domain.ScheduledGame{}, err
		}
	}
	game.ID, game.TableCode, game.CreatedAt = randomID(), table.Code, time.Now().UTC()
	payload, _ := json.Marshal(game)
	_, err = s.db.Exec(`INSERT INTO scheduled_games (id, table_code, data, scheduled_at, created_at) VALUES ($1, $2, $3, $4, $5)`, game.ID, game.TableCode, payload, game.ScheduledAt, game.CreatedAt)
	return game, err
}

func (s *PostgresStore) ListScheduledGames(tableCode, hostToken string) ([]domain.ScheduledGame, error) {
	table, err := s.GetTable(tableCode)
	if err != nil {
		return nil, err
	}
	if table.HostToken != hostToken {
		return nil, ErrForbidden
	}
	rows, err := s.db.Query(`SELECT data FROM scheduled_games WHERE table_code = $1 ORDER BY scheduled_at`, table.Code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	games := []domain.ScheduledGame{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var game domain.ScheduledGame
		if err := json.Unmarshal(payload, &game); err != nil {
			return nil, err
		}
		games = append(games, game)
	}
	return games, rows.Err()
}

func (s *PostgresStore) SetScheduledGameRule(id, hostToken, ruleID string) (domain.ScheduledGame, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return domain.ScheduledGame{}, err
	}
	defer tx.Rollback()
	var payload []byte
	if err := tx.QueryRow(`SELECT data FROM scheduled_games WHERE id = $1 FOR UPDATE`, id).Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return domain.ScheduledGame{}, ErrNotFound
		}
		return domain.ScheduledGame{}, err
	}
	var game domain.ScheduledGame
	if err := json.Unmarshal(payload, &game); err != nil {
		return domain.ScheduledGame{}, err
	}
	table, err := s.GetTable(game.TableCode)
	if err != nil {
		return domain.ScheduledGame{}, err
	}
	if table.HostToken != hostToken {
		return domain.ScheduledGame{}, ErrForbidden
	}
	if ruleID != "" {
		if _, err := s.GetRule(ruleID); err != nil {
			return domain.ScheduledGame{}, err
		}
	}
	game.RuleID = ruleID
	payload, _ = json.Marshal(game)
	if _, err := tx.Exec(`UPDATE scheduled_games SET data = $1 WHERE id = $2`, payload, id); err != nil {
		return domain.ScheduledGame{}, err
	}
	return game, tx.Commit()
}

func (s *PostgresStore) SetScheduledGameSession(id, hostToken, sessionID string) (domain.ScheduledGame, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return domain.ScheduledGame{}, err
	}
	defer tx.Rollback()
	var payload []byte
	if err := tx.QueryRow(`SELECT data FROM scheduled_games WHERE id = $1 FOR UPDATE`, id).Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return domain.ScheduledGame{}, ErrNotFound
		}
		return domain.ScheduledGame{}, err
	}
	var game domain.ScheduledGame
	if err := json.Unmarshal(payload, &game); err != nil {
		return domain.ScheduledGame{}, err
	}
	table, err := s.GetTable(game.TableCode)
	if err != nil {
		return domain.ScheduledGame{}, err
	}
	if table.HostToken != hostToken {
		return domain.ScheduledGame{}, ErrForbidden
	}
	session, err := s.GetSession(sessionID)
	if err != nil {
		return domain.ScheduledGame{}, err
	}
	if game.RuleID == "" || session.TableCode != game.TableCode || session.RuleID != game.RuleID || (game.SessionID != "" && game.SessionID != sessionID) {
		return domain.ScheduledGame{}, ErrValidation
	}
	game.SessionID = sessionID
	payload, _ = json.Marshal(game)
	if _, err := tx.Exec(`UPDATE scheduled_games SET data = $1 WHERE id = $2`, payload, id); err != nil {
		return domain.ScheduledGame{}, err
	}
	return game, tx.Commit()
}

func prepareRule(rule *domain.ScoringRule) error {
	if rule.BGGID < 0 || strings.TrimSpace(rule.GameName) == "" || strings.TrimSpace(rule.Name) == "" || len(rule.Fields) == 0 {
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

func validateScoreField(session domain.ScoreSession, rule domain.ScoringRule, playerID, fieldID string) error {
	playerFound, fieldFound := false, false
	for _, player := range session.Players {
		if player.ID == playerID {
			playerFound = true
			break
		}
	}
	for _, field := range rule.Fields {
		if field.ID == fieldID {
			fieldFound = true
			break
		}
	}
	if !playerFound || !fieldFound {
		return ErrValidation
	}
	return nil
}

var _ Repository = (*PostgresStore)(nil)
