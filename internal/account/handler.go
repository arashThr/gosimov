package account

import (
	"context"
	"net/http"
	"slices"

	"github.com/a-h/templ"
	"github.com/arashthr/skeleton/internal/auth"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/platform/flash"
	"github.com/arashthr/skeleton/internal/platform/logger"
	"github.com/arashthr/skeleton/internal/platform/web"
	"github.com/arashthr/skeleton/internal/user"
	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type HandlerParams struct {
	fx.In

	Sections  []Section      `group:"account_sections"`
	Hooks     []DeletionHook `group:"account_deletion_hooks"`
	Users     *user.Repository
	Auth      *auth.Service
	Sessions  *auth.Sessions
	Providers *auth.Providers
}

type Handler struct {
	sections  []Section
	hooks     []DeletionHook
	users     *user.Repository
	auth      *auth.Service
	sessions  *auth.Sessions
	providers *auth.Providers
}

func NewHandler(p HandlerParams) *Handler {
	sections := slices.Clone(p.Sections)
	slices.SortStableFunc(sections, func(a, b Section) int { return a.Order() - b.Order() })
	return &Handler{
		sections:  sections,
		hooks:     p.Hooks,
		users:     p.Users,
		auth:      p.Auth,
		sessions:  p.Sessions,
		providers: p.Providers,
	}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/account", func(r chi.Router) {
		r.Use(auth.RequireUser)
		r.Get("/", web.Handle(h.show))
		r.Post("/password", web.Handle(h.changePassword))
		r.Post("/identities/{provider}/disconnect", web.Handle(h.disconnect))
		r.Post("/delete", web.Handle(h.delete))
	})
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) error {
	u := user.FromContext(r.Context())
	var components []templ.Component
	for _, s := range h.sections {
		c, err := s.Render(r.Context(), u)
		if err != nil {
			return err
		}
		if c != nil {
			components = append(components, c)
		}
	}
	return web.Render(w, r, http.StatusOK, AccountPage(components))
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) error {
	u := user.FromContext(r.Context())
	err := h.auth.ChangePassword(r.Context(), u, r.FormValue("current_password"), r.FormValue("new_password"))
	if err != nil {
		return flashOrFail(w, r, err)
	}
	// Changing the password signed out every session, including this one.
	if err := h.sessions.Start(w, r, u); err != nil {
		return err
	}
	flash.Set(w, flash.Success, "Password updated. Other devices have been signed out.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
	return nil
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) error {
	u := user.FromContext(r.Context())
	provider := chi.URLParam(r, "provider")
	if err := h.users.UnlinkIdentity(r.Context(), u.ID, provider); err != nil {
		return err
	}
	flash.Set(w, flash.Success, "Account disconnected.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
	return nil
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	u := user.FromContext(ctx)
	if user.NormalizeEmail(r.FormValue("confirm_email")) != u.Email {
		return flashOrFail(w, r, errs.Public(errs.ErrInvalid, "Type your email address to confirm deletion"))
	}
	for _, hook := range h.hooks {
		if err := hook.BeforeUserDelete(ctx, u); err != nil {
			return err
		}
	}
	if err := h.sessions.End(w, r); err != nil {
		return err
	}
	if err := h.users.Delete(ctx, u.ID); err != nil {
		return err
	}
	logger.FromContext(ctx).Info("account deleted", zap.Int64("user_id", u.ID))
	flash.Set(w, flash.Info, "Your account has been deleted.")
	http.Redirect(w, r, "/", http.StatusSeeOther)
	return nil
}

// flashOrFail shows user-facing errors as a flash message on the account
// page and escalates everything else.
func flashOrFail(w http.ResponseWriter, r *http.Request, err error) error {
	msg, ok := errs.PublicMessage(err)
	if !ok || web.StatusFor(err) >= 500 {
		return err
	}
	flash.Set(w, flash.Error, msg)
	http.Redirect(w, r, "/account", http.StatusSeeOther)
	return nil
}

// Built-in sections.

type profileSection struct{}

func newProfileSection() *profileSection { return &profileSection{} }

func (s *profileSection) Order() int { return 10 }
func (s *profileSection) Render(_ context.Context, u *user.User) (templ.Component, error) {
	return ProfileSection(u), nil
}

type signInSection struct {
	users     *user.Repository
	providers *auth.Providers
}

func newSignInSection(users *user.Repository, providers *auth.Providers) *signInSection {
	return &signInSection{users: users, providers: providers}
}

func (s *signInSection) Order() int { return 20 }
func (s *signInSection) Render(ctx context.Context, u *user.User) (templ.Component, error) {
	identities, err := s.users.ListIdentities(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	connected := map[string]user.Identity{}
	for _, i := range identities {
		connected[i.Provider] = i
	}
	return SignInSection(u, s.providers.All(), connected), nil
}

type dangerSection struct{}

func newDangerSection() *dangerSection { return &dangerSection{} }

func (s *dangerSection) Order() int { return 1000 }
func (s *dangerSection) Render(_ context.Context, u *user.User) (templ.Component, error) {
	return DangerSection(u), nil
}
