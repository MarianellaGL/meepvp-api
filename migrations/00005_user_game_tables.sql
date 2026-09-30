-- +goose Up
CREATE TABLE user_game_tables (
    table_code TEXT PRIMARY KEY REFERENCES game_tables(code) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX user_game_tables_user_id_idx ON user_game_tables(user_id);
-- Existing signed-in hosts were linked to the first player of their sessions.
INSERT INTO user_game_tables (table_code, user_id)
SELECT DISTINCT ON (s.table_code) s.table_code, us.user_id
FROM score_sessions s
JOIN user_game_sessions us ON us.session_id = s.id
WHERE us.player_id = s.data->'players'->0->>'id'
ORDER BY s.table_code, s.created_at ASC
ON CONFLICT (table_code) DO NOTHING;
CREATE TABLE user_avatars (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    image_data BYTEA NOT NULL,
    content_type TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE user_avatars;
DROP TABLE user_game_tables;
