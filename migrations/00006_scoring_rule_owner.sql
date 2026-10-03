-- +goose Up
-- Sheets created before this column have no owner: public ones stay listed,
-- private ones are only reachable by ID (sessions keep working).
ALTER TABLE scoring_rules ADD COLUMN owner_user_id TEXT REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX scoring_rules_owner_user_id_idx ON scoring_rules(owner_user_id, created_at DESC);

-- +goose Down
DROP INDEX scoring_rules_owner_user_id_idx;
ALTER TABLE scoring_rules DROP COLUMN owner_user_id;
