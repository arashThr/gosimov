// Package home serves the public landing page and the signed-in dashboard.
// It is the natural place to start building your product.
package home

import (
	"net/http"

	"github.com/arashthr/skeleton/internal/auth"
	"github.com/arashthr/skeleton/internal/platform/server"
	"github.com/arashthr/skeleton/internal/platform/web"
	"github.com/arashthr/skeleton/internal/user"
	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"
)

var Module = fx.Module("home", fx.Provide(server.AsRoutes(NewHandler)))

type Handler struct{}

func NewHandler() *Handler {
	return &Handler{}
}

func (h *Handler) Register(r chi.Router) {
	r.Get("/", web.Handle(h.landing))
	r.With(auth.RequireUser).Get("/dashboard", web.Handle(h.dashboard))
}

func (h *Handler) landing(w http.ResponseWriter, r *http.Request) error {
	if user.FromContext(r.Context()) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return nil
	}
	return web.Render(w, r, http.StatusOK, LandingPage())
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) error {
	return web.Render(w, r, http.StatusOK, DashboardPage(user.FromContext(r.Context())))
}
