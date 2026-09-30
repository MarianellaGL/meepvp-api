package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"tablescore-api/internal/domain"
)

func (s *PostgresStore) CreateUser(username, passwordHash string) (domain.User, error) {
	user := domain.User{ID: randomID(), Username: username, CreatedAt: time.Now().UTC()}
	_, err := s.db.Exec(`INSERT INTO users (id, username, username_key, password_hash, created_at) VALUES ($1, $2, $3, $4, $5)`, user.ID, user.Username, strings.ToLower(username), passwordHash, user.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.User{}, ErrConflict
	}
	return user, err
}

func (s *PostgresStore) FindUser(username string) (domain.User, string, error) {
	var user domain.User
	var passwordHash string
	err := s.db.QueryRow(`SELECT id, username, password_hash, created_at FROM users WHERE username_key = $1 AND deleted_at IS NULL`, strings.ToLower(username)).Scan(&user.ID, &user.Username, &passwordHash, &user.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, "", ErrNotFound
	}
	return user, passwordHash, err
}

func (s *PostgresStore) SaveAuthSession(userID, tokenHash string, expiresAt int64) error {
	_, err := s.db.Exec(`INSERT INTO auth_sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`, tokenHash, userID, time.Unix(expiresAt, 0).UTC())
	return err
}

func (s *PostgresStore) UserByAuthSession(tokenHash string) (domain.User, error) {
	var user domain.User
	err := s.db.QueryRow(`SELECT u.id, u.username, u.created_at FROM auth_sessions a JOIN users u ON u.id = a.user_id WHERE a.token_hash = $1 AND a.expires_at > now() AND u.deleted_at IS NULL`, tokenHash).Scan(&user.ID, &user.Username, &user.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, ErrNotFound
	}
	return user, err
}

func (s *PostgresStore) DeleteAuthSession(tokenHash string) error {
	_, err := s.db.Exec(`DELETE FROM auth_sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (s *PostgresStore) LinkUserTable(userID, tableCode string) error {
	tableCode = strings.ToUpper(tableCode)
	result, err := s.db.Exec(`INSERT INTO user_game_tables (table_code, user_id) VALUES ($1, $2) ON CONFLICT (table_code) DO NOTHING`, tableCode, userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrNotFound
		}
		return err
	}
	if count, _ := result.RowsAffected(); count > 0 {
		return nil
	}
	var owner string
	if err := s.db.QueryRow(`SELECT user_id FROM user_game_tables WHERE table_code = $1`, tableCode).Scan(&owner); err != nil {
		return err
	}
	if owner != userID {
		return ErrConflict
	}
	return nil
}

func (s *PostgresStore) ListUserTables(userID string) ([]domain.Table, error) {
	rows, err := s.db.Query(`SELECT t.data, t.host_token FROM user_game_tables ut JOIN game_tables t ON t.code = ut.table_code WHERE ut.user_id = $1 ORDER BY t.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	tables := []domain.Table{}
	for rows.Next() {
		var payload []byte
		var token string
		if err := rows.Scan(&payload, &token); err != nil {
			return nil, err
		}
		var table domain.Table
		if err := json.Unmarshal(payload, &table); err != nil {
			return nil, err
		}
		table.HostToken = token
		tables = append(tables, table)
	}
	return tables, rows.Err()
}

func (s *PostgresStore) SaveUserAvatar(userID string, data []byte, contentType string) error {
	_, err := s.db.Exec(`INSERT INTO user_avatars (user_id, image_data, content_type, updated_at) VALUES ($1, $2, $3, now()) ON CONFLICT (user_id) DO UPDATE SET image_data = EXCLUDED.image_data, content_type = EXCLUDED.content_type, updated_at = now()`, userID, data, contentType)
	return err
}

func (s *PostgresStore) GetUserAvatar(userID string) ([]byte, string, error) {
	var data []byte
	var contentType string
	err := s.db.QueryRow(`SELECT image_data, content_type FROM user_avatars WHERE user_id = $1`, userID).Scan(&data, &contentType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return data, contentType, err
}

func (s *PostgresStore) DeleteUserAvatar(userID string) error {
	_, err := s.db.Exec(`DELETE FROM user_avatars WHERE user_id = $1`, userID)
	return err
}

func (s *PostgresStore) LinkUserSession(userID, sessionID, playerID string) error {
	var payload []byte
	err := s.db.QueryRow(`SELECT data FROM score_sessions WHERE id = $1`, sessionID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var session domain.ScoreSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return err
	}
	valid := false
	for _, player := range session.Players {
		if player.ID == playerID {
			valid = true
			break
		}
	}
	if !valid {
		return ErrValidation
	}
	_, err = s.db.Exec(`INSERT INTO user_game_sessions (user_id, session_id, player_id) VALUES ($1, $2, $3) ON CONFLICT (user_id, session_id) DO NOTHING`, userID, sessionID, playerID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}

func (s *PostgresStore) ListUserSessions(userID string) ([]domain.UserSession, error) {
	rows, err := s.db.Query(`SELECT s.data, us.player_id FROM user_game_sessions us JOIN score_sessions s ON s.id = us.session_id WHERE us.user_id = $1 ORDER BY s.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []domain.UserSession{}
	for rows.Next() {
		var payload []byte
		var item domain.UserSession
		if err := rows.Scan(&payload, &item.PlayerID); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &item.Session); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
