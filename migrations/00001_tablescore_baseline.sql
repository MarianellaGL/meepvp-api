-- +goose Up

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
CREATE INDEX IF NOT EXISTS scheduled_games_table_time_idx ON scheduled_games(table_code, scheduled_at);

-- +goose Down
-- Existing Tablescore data is never dropped by baseline rollback.
SELECT 1;
