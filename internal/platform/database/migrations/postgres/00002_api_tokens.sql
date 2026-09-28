-- +goose Up
CREATE TABLE api_tokens (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ
);
CREATE INDEX idx_api_tokens_user_id ON api_tokens (user_id);

-- +goose Down
DROP TABLE api_tokens;
