-- +goose Up
CREATE TABLE stripe_customers (
    user_id     BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    customer_id TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL
);

-- Latest known state of each Stripe subscription, kept in sync by webhooks.
CREATE TABLE subscriptions (
    id                     BIGSERIAL PRIMARY KEY,
    user_id                BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    stripe_subscription_id TEXT NOT NULL UNIQUE,
    status                 TEXT NOT NULL,
    price_id               TEXT NOT NULL DEFAULT '',
    current_period_end     TIMESTAMPTZ,
    cancel_at_period_end   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at             TIMESTAMPTZ NOT NULL,
    updated_at             TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_subscriptions_user_id ON subscriptions (user_id);

-- Processed webhook events, for idempotency.
CREATE TABLE stripe_events (
    id           TEXT PRIMARY KEY,
    type         TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE stripe_events;
DROP TABLE subscriptions;
DROP TABLE stripe_customers;
