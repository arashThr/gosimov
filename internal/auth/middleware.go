package auth

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/arashthr/skeleton/internal/platform/config"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/platform/logger"
	"github.com/arashthr/skeleton/internal/platform/server"
	"github.com/arashthr/skeleton/internal/platform/web"
	"github.com/arashthr/skeleton/internal/user"
	"go.uber.org/zap"
)

const sessionCookie = "session"

// Sessions ties browser cookies to server-side sessions.
type Sessions struct {
	store  *SessionStore
	secure bool
}

func NewSessions(cfg *config.Config, store *SessionStore) *Sessions {
	return &Sessions{store: store, secure: cfg.SecureCookies()}
}

// Start creates a session for u and sets the session cookie.
func (s *Sessions) Start(w http.ResponseWriter, r *http.Request, u *user.User) error {
	token, err := s.store.Create(r.Context(), u.ID, r.RemoteAddr, r.UserAgent())
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(SessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
	logger.FromContext(r.Context()).Info("signed in", zap.Int64("user_id", u.ID))
	return nil
}

// End deletes the current session and clears the cookie.
func (s *Sessions) End(w http.ResponseWriter, r *http.Request) error {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secure})
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	return s.store.Delete(r.Context(), c.Value)
}

// Middleware loads the signed-in user (if any) into the request context.
func (s *Sessions) Middleware() server.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(sessionCookie)
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			u, err := s.store.User(r.Context(), c.Value)
			if err != nil {
				if !errors.Is(err, errs.ErrUnauthorized) {
					web.Error(w, r, err)
					return
				}
				next.ServeHTTP(w, r) // expired or unknown session: continue anonymously
				return
			}
			ctx := user.WithContext(r.Context(), u)
			ctx = logger.WithContext(ctx, logger.FromContext(ctx).With(zap.Int64("user_id", u.ID)))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireUser redirects anonymous visitors to the sign-in page, remembering
// where they wanted to go.
func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user.FromContext(r.Context()) == nil {
			if web.IsAPI(r) {
				web.Error(w, r, errs.ErrUnauthorized)
				return
			}
			target := "/signin"
			if r.Method == http.MethodGet {
				target += "?" + url.Values{"next": {r.URL.RequestURI()}}.Encode()
			}
			web.Redirect(w, r, target)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireGuest sends signed-in users away from pages like sign in and sign up.
func RequireGuest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user.FromContext(r.Context()) != nil {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SafeRedirect returns next if it is a local path, or fallback otherwise.
// This prevents open redirects through ?next=https://evil.example.
func SafeRedirect(next, fallback string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return fallback
	}
	return next
}
