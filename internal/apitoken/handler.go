package apitoken

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/arashthr/skeleton/internal/account"
	"github.com/arashthr/skeleton/internal/auth"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/platform/logger"
	"github.com/arashthr/skeleton/internal/platform/server"
	"github.com/arashthr/skeleton/internal/platform/web"
	"github.com/arashthr/skeleton/internal/user"
	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var Module = fx.Module("apitoken",
	fx.Provide(
		NewRepository,
		server.AsRoutes(NewHandler),
		server.AsMiddleware(NewMiddleware),
		account.AsSection(newSection),
	),
)

// NewMiddleware authenticates requests that carry a bearer token, on any
// route. Other requests (no header, or another scheme such as Basic) pass
// through untouched.
func NewMiddleware(repo *Repository) server.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme, raw, _ := strings.Cut(r.Header.Get("Authorization"), " ")
			if !strings.EqualFold(scheme, "Bearer") {
				next.ServeHTTP(w, r)
				return
			}
			u, err := repo.User(r.Context(), strings.TrimSpace(raw))
			if err != nil {
				if errors.Is(err, errs.ErrUnauthorized) {
					err = errs.Public(err, "Invalid API token")
				}
				web.Error(w, r, err)
				return
			}
			ctx := user.WithContext(r.Context(), u)
			ctx = logger.WithContext(ctx, logger.FromContext(ctx).With(zap.Int64("user_id", u.ID)))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type Handler struct {
	repo *Repository
}

func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

func (h *Handler) Register(r chi.Router) {
	// Example API endpoint: works with a bearer token or a browser session.
	r.With(auth.RequireUser).Get("/api/v1/me", web.Handle(h.me))

	r.Route("/account/tokens", func(r chi.Router) {
		r.Use(auth.RequireUser)
		r.Post("/", web.Handle(h.create))
		r.Delete("/{id}", web.Handle(h.delete))
	})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) error {
	u := user.FromContext(r.Context())
	web.JSON(w, http.StatusOK, map[string]any{
		"id":             u.ID,
		"email":          u.Email,
		"email_verified": u.EmailVerified(),
		"created_at":     u.CreatedAt,
	})
	return nil
}

// create and delete are called with htmx and respond with the refreshed section.

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	u := user.FromContext(r.Context())
	raw, err := h.repo.Create(r.Context(), u.ID, r.FormValue("name"))
	if err != nil {
		msg, ok := errs.PublicMessage(err)
		if !ok {
			return err
		}
		return h.renderSection(w, r, u, "", msg)
	}
	logger.FromContext(r.Context()).Info("api token created")
	return h.renderSection(w, r, u, raw, "")
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	u := user.FromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return errs.ErrNotFound
	}
	if err := h.repo.Delete(r.Context(), u.ID, id); err != nil {
		return err
	}
	logger.FromContext(r.Context()).Info("api token revoked", zap.Int64("token_id", id))
	return h.renderSection(w, r, u, "", "")
}

func (h *Handler) renderSection(w http.ResponseWriter, r *http.Request, u *user.User, created, errMsg string) error {
	tokens, err := h.repo.List(r.Context(), u.ID)
	if err != nil {
		return err
	}
	return web.Render(w, r, http.StatusOK, Section(tokens, created, errMsg))
}

// section shows the tokens on the account page.
type section struct {
	repo *Repository
}

func newSection(repo *Repository) *section { return &section{repo: repo} }

func (s *section) Order() int { return 40 }
func (s *section) Render(ctx context.Context, u *user.User) (templ.Component, error) {
	tokens, err := s.repo.List(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return Section(tokens, "", ""), nil
}
