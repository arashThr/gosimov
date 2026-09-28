-- +goose Up
CREATE TABLE users (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    email             TEXT NOT NULL UNIQUE,
    password_hash     TEXT,
    email_verified_at DATETIME,
    created_at        DATETIME NOT NULL,
    updated_at        DATETIME NOT NULL
);

-- Third-party identities (Google, GitHub, ...) linked to a user.
CREATE TABLE user_identities (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id          INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider         TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    email            TEXT NOT NULL,
    created_at       DATETIME NOT NULL,
    UNIQUE (provider, provider_user_id)
);
CREATE INDEX idx_user_identities_user_id ON user_identities (user_id);

CREATE TABLE sessions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    ip_address   TEXT NOT NULL DEFAULT '',
    user_agent   TEXT NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL,
    last_used_at DATETIME NOT NULL,
    expires_at   DATETIME NOT NULL
);
CREATE INDEX idx_sessions_user_id ON sessions (user_id);
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);

-- Single-use tokens sent by email: magic links, email verification, password reset.
CREATE TABLE auth_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    email      TEXT NOT NULL,
    purpose    TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL
);
CREATE INDEX idx_auth_tokens_expires_at ON auth_tokens (expires_at);

-- +goose Down
DROP TABLE auth_tokens;
DROP TABLE sessions;
DROP TABLE user_identities;
DROP TABLE users;
