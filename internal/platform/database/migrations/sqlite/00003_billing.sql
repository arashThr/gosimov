-- +goose Up
CREATE TABLE stripe_customers (
    user_id     INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    customer_id TEXT NOT NULL UNIQUE,
    created_at  DATETIME NOT NULL
);

-- Latest known state of each Stripe subscription, synced from the Stripe API
-- whenever a webhook reports a change.
CREATE TABLE subscriptions (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id                INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    stripe_subscription_id TEXT NOT NULL UNIQUE,
    status                 TEXT NOT NULL,
    price_id               TEXT NOT NULL DEFAULT '',
    current_period_end     DATETIME,
    cancel_at_period_end   BOOLEAN NOT NULL DEFAULT 0,
    created_at             DATETIME NOT NULL,
    updated_at             DATETIME NOT NULL
);
CREATE INDEX idx_subscriptions_user_id ON subscriptions (user_id);

-- +goose Down
DROP TABLE subscriptions;
DROP TABLE stripe_customers;
