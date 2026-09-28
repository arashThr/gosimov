// Package server builds the HTTP router and runs the HTTP server.
//
// Features plug in through fx value groups:
//   - Routes (group "routes"): each feature registers its own endpoints.
//   - Middleware (group "middleware"): runs on every request, after the core
//     middleware. Order between group members is not guaranteed, so they must
//     not depend on each other.
package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/arashthr/skeleton/internal/platform/config"
	"github.com/arashthr/skeleton/internal/platform/database"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/platform/flash"
	"github.com/arashthr/skeleton/internal/platform/web"
	"github.com/arashthr/skeleton/internal/ui"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var Module = fx.Module("server",
	fx.Provide(NewRouter, NewHTTPServer),
	fx.Invoke(func(*http.Server) {}), // start the server
)

// Routes is implemented by every feature that exposes HTTP endpoints.
type Routes interface {
	Register(r chi.Router)
}

// Middleware is applied to every request.
type Middleware func(http.Handler) http.Handler

// AsRoutes annotates a constructor so its result joins the "routes" group.
func AsRoutes(constructor any) any {
	return fx.Annotate(constructor, fx.As(new(Routes)), fx.ResultTags(`group:"routes"`))
}

// AsMiddleware annotates a constructor returning a Middleware so it joins
// the "middleware" group.
func AsMiddleware(constructor any) any {
	return fx.Annotate(constructor, fx.ResultTags(`group:"middleware"`))
}

type RouterParams struct {
	fx.In

	Log         *zap.Logger
	DB          *database.DB
	Routes      []Routes     `group:"routes"`
	Middlewares []Middleware `group:"middleware"`
}

func NewRouter(p RouterParams) http.Handler {
	r := chi.NewRouter()

	// Browser CSRF protection based on Fetch metadata / Origin headers.
	// Requests without those headers (curl, webhooks, API clients) pass.
	csrf := http.NewCrossOriginProtection()
	csrf.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		web.Error(w, r, errs.Public(errs.ErrForbidden, "Cross-origin request blocked"))
	}))

	r.Use(
		middleware.RequestID,
		middleware.RealIP, // Caddy sets X-Forwarded-For; do not expose the app port directly
		web.RequestLogger(p.Log),
		web.Recoverer,
		web.SecureHeaders,
		csrf.Handler,
		flash.Middleware,
	)
	for _, mw := range p.Middlewares {
		r.Use(mw)
	}

	r.Handle("/static/*", ui.StaticHandler())
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, ui.Asset("favicon.svg"), http.StatusMovedPermanently)
	})
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := p.DB.PingContext(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})

	for _, routes := range p.Routes {
		routes.Register(r)
	}

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		web.Error(w, r, errs.Public(errs.ErrNotFound, "Page not found"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		web.JSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	})
	return r
}

// NewHTTPServer creates the server and ties it to the fx lifecycle for
// graceful startup and shutdown.
func NewHTTPServer(lc fx.Lifecycle, cfg *config.Config, handler http.Handler, log *zap.Logger) *http.Server {
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          zap.NewStdLog(log.Named("http")),
	}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return err
			}
			log.Info("http server listening", zap.String("addr", srv.Addr), zap.String("url", cfg.BaseURL))
			go func() {
				if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Error("http server stopped", zap.Error(err))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("http server shutting down")
			return srv.Shutdown(ctx)
		},
	})
	return srv
}
