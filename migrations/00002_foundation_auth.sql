-- +goose Up
ALTER TABLE users ADD COLUMN email text;
ALTER TABLE users ADD COLUMN name text;
ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user';
ALTER TABLE users ADD COLUMN email_verified_at timestamptz;
ALTER TABLE users ADD COLUMN updated_at timestamptz;
ALTER TABLE users ADD COLUMN deleted_at timestamptz;
ALTER TABLE users ALTER COLUMN password_hash SET DEFAULT '';
UPDATE users SET name = username, updated_at = created_at;
CREATE UNIQUE INDEX idx_users_email_lower ON users (lower(email)) WHERE email IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX idx_users_deleted_at ON users (deleted_at);
CREATE TABLE auth_providers (
    id                text PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id           text NOT NULL REFERENCES users (id),
    provider          text NOT NULL,
    provider_user_id  text,
    password_hash     text,
    created_at        timestamptz,
    updated_at        timestamptz,
    deleted_at        timestamptz
);
CREATE UNIQUE INDEX idx_auth_providers_provider_user ON auth_providers (provider, provider_user_id);
CREATE INDEX idx_auth_providers_user_id ON auth_providers (user_id);
CREATE INDEX idx_auth_providers_deleted_at ON auth_providers (deleted_at);

CREATE TABLE refresh_tokens (
    id          text PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id     text NOT NULL REFERENCES users (id),
    token       text NOT NULL,
    expires_at  timestamptz NOT NULL,
    revoked     boolean NOT NULL DEFAULT false,
    created_at  timestamptz,
    updated_at  timestamptz,
    deleted_at  timestamptz
);
CREATE UNIQUE INDEX idx_refresh_tokens_token ON refresh_tokens (token);
CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens (user_id);
CREATE INDEX idx_refresh_tokens_deleted_at ON refresh_tokens (deleted_at);

CREATE TABLE email_tokens (
    id          text PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id     text NOT NULL REFERENCES users (id),
    purpose     text NOT NULL,
    token_hash  text NOT NULL,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz
);
CREATE UNIQUE INDEX idx_email_tokens_token_hash ON email_tokens (token_hash);
CREATE INDEX idx_email_tokens_user_id ON email_tokens (user_id);
CREATE INDEX idx_email_tokens_purpose ON email_tokens (purpose);

CREATE TABLE app_configs (
    id     text PRIMARY KEY DEFAULT gen_random_uuid()::text,
    key    text NOT NULL,
    name   text NOT NULL,
    value  text NOT NULL
);
CREATE UNIQUE INDEX idx_app_configs_key ON app_configs (key);


-- +goose Down
DROP TABLE email_tokens;
DROP TABLE app_configs;
DROP TABLE refresh_tokens;
DROP TABLE auth_providers;
DROP INDEX idx_users_email_lower;
DROP INDEX idx_users_deleted_at;
ALTER TABLE users DROP COLUMN email, DROP COLUMN name, DROP COLUMN role, DROP COLUMN email_verified_at, DROP COLUMN updated_at, DROP COLUMN deleted_at;
ALTER TABLE users ALTER COLUMN password_hash DROP DEFAULT;
