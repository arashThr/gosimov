// Package home serves the public landing page and the signed-in dashboard.
package home

import (
	"net/http"

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
}

func (h *Handler) landing(w http.ResponseWriter, r *http.Request) error {
	if user.FromContext(r.Context()) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return nil
	}
	return web.Render(w, r, http.StatusOK, LandingPage())
}
