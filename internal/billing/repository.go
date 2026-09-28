// Package billing sells a subscription through Stripe Checkout, lets users
// manage it in the Stripe customer portal and keeps a local copy of the
// subscription state in sync through webhooks.
//
// The feature is disabled unless STRIPE_SECRET_KEY and STRIPE_PRICE_ID are set.
package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arashthr/skeleton/internal/platform/database"
	"github.com/arashthr/skeleton/internal/platform/errs"
)

type Subscription struct {
	UserID            int64
	StripeID          string
	Status            string
	PriceID           string
	CurrentPeriodEnd  *time.Time
	CancelAtPeriodEnd bool
	UpdatedAt         time.Time
}

// Active reports whether the subscription grants access. past_due keeps
// access while Stripe retries the payment.
func (s *Subscription) Active() bool {
	switch s.Status {
	case "active", "trialing", "past_due":
		return true
	}
	return false
}

type Repository struct {
	db *database.DB
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CustomerID(ctx context.Context, userID int64) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `SELECT customer_id FROM stripe_customers WHERE user_id = $1`, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errs.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get stripe customer: %w", err)
	}
	return id, nil
}

func (r *Repository) SaveCustomer(ctx context.Context, userID int64, customerID string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO stripe_customers (user_id, customer_id, created_at) VALUES ($1, $2, $3)`,
		userID, customerID, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("save stripe customer: %w", err)
	}
	return nil
}

func (r *Repository) UserIDByCustomer(ctx context.Context, customerID string) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `SELECT user_id FROM stripe_customers WHERE customer_id = $1`, customerID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errs.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("get user by stripe customer: %w", err)
	}
	return id, nil
}

// UpsertSubscription stores the latest state of a subscription.
func (r *Repository) UpsertSubscription(ctx context.Context, s Subscription) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO subscriptions
			(user_id, stripe_subscription_id, status, price_id, current_period_end, cancel_at_period_end, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		ON CONFLICT (stripe_subscription_id) DO UPDATE SET
			status = excluded.status,
			price_id = excluded.price_id,
			current_period_end = excluded.current_period_end,
			cancel_at_period_end = excluded.cancel_at_period_end,
			updated_at = excluded.updated_at`,
		s.UserID, s.StripeID, s.Status, s.PriceID, s.CurrentPeriodEnd, s.CancelAtPeriodEnd, now)
	if err != nil {
		return fmt.Errorf("upsert subscription: %w", err)
	}
	return nil
}

// Current returns the user's most relevant subscription: an active one if
// any, otherwise the most recently updated. It returns nil when the user
// never subscribed.
func (r *Repository) Current(ctx context.Context, userID int64) (*Subscription, error) {
	var s Subscription
	err := r.db.QueryRowContext(ctx, `
		SELECT user_id, stripe_subscription_id, status, price_id, current_period_end, cancel_at_period_end, updated_at
		FROM subscriptions
		WHERE user_id = $1
		ORDER BY CASE WHEN status IN ('active', 'trialing', 'past_due') THEN 0 ELSE 1 END, updated_at DESC
		LIMIT 1`, userID,
	).Scan(&s.UserID, &s.StripeID, &s.Status, &s.PriceID, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("current subscription: %w", err)
	}
	return &s, nil
}
