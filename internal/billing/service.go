package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/arashthr/skeleton/internal/platform/config"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/user"
	"github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/webhook"
	"go.uber.org/zap"
)

var ErrInvalidSignature = errs.Public(errs.ErrInvalid, "invalid webhook signature")

type Service struct {
	cfg    *config.Config
	stripe *stripe.Client
	repo   *Repository
	log    *zap.Logger
}

func NewService(cfg *config.Config, repo *Repository, log *zap.Logger) *Service {
	s := &Service{cfg: cfg, repo: repo, log: log.Named("billing")}
	if cfg.Stripe.Enabled() {
		s.stripe = stripe.NewClient(cfg.Stripe.SecretKey)
	} else {
		s.log.Info("billing disabled: STRIPE_SECRET_KEY or STRIPE_PRICE_ID not set")
	}
	return s
}

func (s *Service) Enabled() bool { return s.stripe != nil }

// Subscription returns the user's current subscription, or nil.
func (s *Service) Subscription(ctx context.Context, userID int64) (*Subscription, error) {
	return s.repo.Current(ctx, userID)
}

// IsPremium reports whether the user has an active subscription. Use it to
// gate paid features.
func (s *Service) IsPremium(ctx context.Context, userID int64) (bool, error) {
	sub, err := s.repo.Current(ctx, userID)
	if err != nil {
		return false, err
	}
	return sub != nil && sub.Active(), nil
}

// CheckoutURL creates a Stripe Checkout session for the configured price.
func (s *Service) CheckoutURL(ctx context.Context, u *user.User) (string, error) {
	customerID, err := s.ensureCustomer(ctx, u)
	if err != nil {
		return "", err
	}
	userID := strconv.FormatInt(u.ID, 10)
	sess, err := s.stripe.V1CheckoutSessions.Create(ctx, &stripe.CheckoutSessionCreateParams{
		Mode:                stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		Customer:            stripe.String(customerID),
		ClientReferenceID:   stripe.String(userID),
		AllowPromotionCodes: stripe.Bool(true),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{
			{Price: stripe.String(s.cfg.Stripe.PriceID), Quantity: stripe.Int64(1)},
		},
		SubscriptionData: &stripe.CheckoutSessionCreateSubscriptionDataParams{
			Metadata: map[string]string{"user_id": userID},
		},
		SuccessURL: stripe.String(s.cfg.URL("/billing/success") + "?session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:  stripe.String(s.cfg.URL("/account")),
	})
	if err != nil {
		return "", fmt.Errorf("create checkout session: %w", err)
	}
	return sess.URL, nil
}

// PortalURL creates a Stripe customer portal session where users update
// payment methods, download invoices and cancel.
func (s *Service) PortalURL(ctx context.Context, u *user.User) (string, error) {
	customerID, err := s.repo.CustomerID(ctx, u.ID)
	if errors.Is(err, errs.ErrNotFound) {
		return "", errs.Public(err, "You don't have a billing account yet")
	}
	if err != nil {
		return "", err
	}
	sess, err := s.stripe.V1BillingPortalSessions.Create(ctx, &stripe.BillingPortalSessionCreateParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(s.cfg.URL("/account")),
	})
	if err != nil {
		return "", fmt.Errorf("create portal session: %w", err)
	}
	return sess.URL, nil
}

// CompleteCheckout syncs the subscription created by a checkout session, so
// the user sees it right away without waiting for the webhook.
func (s *Service) CompleteCheckout(ctx context.Context, u *user.User, sessionID string) error {
	sess, err := s.stripe.V1CheckoutSessions.Retrieve(ctx, sessionID, nil)
	if err != nil {
		return fmt.Errorf("retrieve checkout session: %w", err)
	}
	if sess.ClientReferenceID != strconv.FormatInt(u.ID, 10) {
		return errs.ErrForbidden
	}
	if sess.Subscription == nil {
		return nil
	}
	return s.SyncSubscription(ctx, sess.Subscription.ID)
}

// HandleWebhook verifies and processes a Stripe webhook. Instead of trusting
// the (possibly stale or out of order) event payload, the subscription is
// fetched from Stripe and stored as is, which makes processing idempotent.
func (s *Service) HandleWebhook(ctx context.Context, payload []byte, signature string) error {
	event, err := webhook.ConstructEventWithOptions(payload, signature, s.cfg.Stripe.WebhookSecret,
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	log := s.log.With(zap.String("event_id", event.ID), zap.String("event_type", string(event.Type)))

	var object struct {
		ID           string `json:"id"`
		Subscription string `json:"subscription"`
	}
	if err := json.Unmarshal(event.Data.Raw, &object); err != nil {
		return fmt.Errorf("decode webhook object: %w", err)
	}

	switch event.Type {
	case "customer.subscription.created",
		"customer.subscription.updated",
		"customer.subscription.deleted",
		"customer.subscription.paused",
		"customer.subscription.resumed":
		log.Info("webhook received")
		return s.SyncSubscription(ctx, object.ID)
	case "checkout.session.completed":
		log.Info("webhook received")
		if object.Subscription == "" {
			return nil
		}
		return s.SyncSubscription(ctx, object.Subscription)
	default:
		log.Debug("webhook ignored")
		return nil
	}
}

// SyncSubscription fetches a subscription from Stripe and stores it.
func (s *Service) SyncSubscription(ctx context.Context, subscriptionID string) error {
	sub, err := s.stripe.V1Subscriptions.Retrieve(ctx, subscriptionID, nil)
	if err != nil {
		return fmt.Errorf("retrieve subscription %s: %w", subscriptionID, err)
	}
	userID, err := s.repo.UserIDByCustomer(ctx, sub.Customer.ID)
	if err != nil {
		return fmt.Errorf("subscription %s: %w", subscriptionID, err)
	}

	local := Subscription{
		UserID:            userID,
		StripeID:          sub.ID,
		Status:            string(sub.Status),
		CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
	}
	if sub.Items != nil && len(sub.Items.Data) > 0 {
		item := sub.Items.Data[0]
		if item.Price != nil {
			local.PriceID = item.Price.ID
		}
		if item.CurrentPeriodEnd > 0 {
			end := time.Unix(item.CurrentPeriodEnd, 0).UTC()
			local.CurrentPeriodEnd = &end
		}
	}
	if err := s.repo.UpsertSubscription(ctx, local); err != nil {
		return err
	}
	s.log.Info("subscription synced",
		zap.Int64("user_id", userID),
		zap.String("subscription_id", sub.ID),
		zap.String("status", local.Status))
	return nil
}

// BeforeUserDelete cancels the user's subscriptions so a deleted account
// is not charged again.
func (s *Service) BeforeUserDelete(ctx context.Context, u *user.User) error {
	if !s.Enabled() {
		return nil
	}
	customerID, err := s.repo.CustomerID(ctx, u.ID)
	if errors.Is(err, errs.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	list := s.stripe.V1Subscriptions.List(ctx, &stripe.SubscriptionListParams{Customer: stripe.String(customerID)})
	for sub, err := range list.All(ctx) {
		if err != nil {
			return fmt.Errorf("list subscriptions: %w", err)
		}
		if sub.Status == stripe.SubscriptionStatusCanceled || sub.Status == stripe.SubscriptionStatusIncompleteExpired {
			continue
		}
		if _, err := s.stripe.V1Subscriptions.Cancel(ctx, sub.ID, nil); err != nil {
			return fmt.Errorf("cancel subscription %s: %w", sub.ID, err)
		}
		s.log.Info("subscription canceled for deleted user", zap.Int64("user_id", u.ID), zap.String("subscription_id", sub.ID))
	}
	return nil
}

// ensureCustomer returns the user's Stripe customer, creating it on first use.
func (s *Service) ensureCustomer(ctx context.Context, u *user.User) (string, error) {
	id, err := s.repo.CustomerID(ctx, u.ID)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return "", err
	}
	params := &stripe.CustomerCreateParams{
		Email:    stripe.String(u.Email),
		Metadata: map[string]string{"user_id": strconv.FormatInt(u.ID, 10)},
	}
	params.SetIdempotencyKey("customer-for-user-" + strconv.FormatInt(u.ID, 10))
	c, err := s.stripe.V1Customers.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create stripe customer: %w", err)
	}
	if err := s.repo.SaveCustomer(ctx, u.ID, c.ID); err != nil {
		return "", err
	}
	s.log.Info("stripe customer created", zap.Int64("user_id", u.ID), zap.String("customer_id", c.ID))
	return c.ID, nil
}
