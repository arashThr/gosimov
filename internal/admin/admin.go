// Package admin serves an internal area protected by HTTP basic auth
// (ADMIN_USER / ADMIN_PASS). It is disabled when ADMIN_PASS is empty.
package admin

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"github.com/arashthr/skeleton/internal/platform/config"
	"github.com/arashthr/skeleton/internal/platform/server"
	"github.com/arashthr/skeleton/internal/platform/web"
	"github.com/arashthr/skeleton/internal/user"
	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"
)

var Module = fx.Module("admin", fx.Provide(server.AsRoutes(NewHandler)))

type Handler struct {
	cfg   config.Admin
	users *user.Repository
}

func NewHandler(cfg *config.Config, users *user.Repository) *Handler {
	return &Handler{cfg: cfg.Admin, users: users}
}

func (h *Handler) Register(r chi.Router) {
	if !h.cfg.Enabled() {
		return
	}
	r.Route("/admin", func(r chi.Router) {
		r.Use(h.basicAuth)
		r.Get("/", web.Handle(h.index))
	})
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) error {
	count, err := h.users.Count(r.Context())
	if err != nil {
		return err
	}
	recent, err := h.users.ListRecent(r.Context(), 20)
	if err != nil {
		return err
	}
	return web.Render(w, r, http.StatusOK, IndexPage(count, recent))
}

func (h *Handler) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || !equal(u, h.cfg.User) || !equal(p, h.cfg.Password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="admin", charset="UTF-8"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// equal compares in constant time. Hashing first hides the length.
func equal(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
