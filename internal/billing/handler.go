package billing

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/a-h/templ"
	"github.com/arashthr/skeleton/internal/account"
	"github.com/arashthr/skeleton/internal/auth"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/platform/flash"
	"github.com/arashthr/skeleton/internal/platform/logger"
	"github.com/arashthr/skeleton/internal/platform/server"
	"github.com/arashthr/skeleton/internal/platform/web"
	"github.com/arashthr/skeleton/internal/user"
	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var Module = fx.Module("billing",
	fx.Provide(
		NewRepository,
		NewService,
		server.AsRoutes(NewHandler),
		account.AsSection(newSection),
		account.AsDeletionHook(func(s *Service) *Service { return s }),
	),
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Register(r chi.Router) {
	if !h.service.Enabled() {
		return
	}
	r.Post("/webhooks/stripe", web.Handle(h.webhook))
	r.Route("/billing", func(r chi.Router) {
		r.Use(auth.RequireUser)
		r.Post("/checkout", web.Handle(h.checkout))
		r.Post("/portal", web.Handle(h.portal))
		r.Get("/success", web.Handle(h.success))
	})
}

func (h *Handler) checkout(w http.ResponseWriter, r *http.Request) error {
	url, err := h.service.CheckoutURL(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		return err
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
	return nil
}

func (h *Handler) portal(w http.ResponseWriter, r *http.Request) error {
	url, err := h.service.PortalURL(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		return err
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
	return nil
}

func (h *Handler) success(w http.ResponseWriter, r *http.Request) error {
	if err := h.service.CompleteCheckout(r.Context(), user.FromContext(r.Context()), r.FormValue("session_id")); err != nil {
		// The webhook will catch up; don't fail the user's redirect.
		logger.FromContext(r.Context()).Error("complete checkout", zap.Error(err))
	}
	flash.Set(w, flash.Success, "Thank you! Your subscription is active.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
	return nil
}

// webhook answers 2xx only when the event was processed, so Stripe retries
// failed deliveries.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) error {
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
	if err != nil {
		return errs.Public(errs.ErrInvalid, "request body too large")
	}
	err = h.service.HandleWebhook(r.Context(), payload, r.Header.Get("Stripe-Signature"))
	if errors.Is(err, errs.ErrNotFound) {
		// Subscription of a customer we don't know (e.g. created in the dashboard): nothing to do.
		logger.FromContext(r.Context()).Warn("stripe webhook for unknown customer", zap.Error(err))
		err = nil
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

// section shows the plan on the account page.
type section struct {
	service *Service
}

func newSection(service *Service) *section { return &section{service: service} }

func (s *section) Order() int { return 30 }
func (s *section) Render(ctx context.Context, u *user.User) (templ.Component, error) {
	if !s.service.Enabled() {
		return nil, nil
	}
	sub, err := s.service.Subscription(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return Section(sub), nil
}
