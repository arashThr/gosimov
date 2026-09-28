// Package web contains HTTP helpers shared by all features: an
// error-returning handler type, central error handling, rendering and
// common middleware.
package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/platform/logger"
	"github.com/arashthr/skeleton/internal/ui"
	"go.uber.org/zap"
)

// HandlerFunc is an http.HandlerFunc that returns an error. Returned errors
// are logged and turned into a proper response by Handle.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Handle adapts a HandlerFunc to http.HandlerFunc.
func Handle(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			Error(w, r, err)
		}
	}
}

// Error writes an error response. The status code is derived from the error
// kind (see package errs); the message is the public message if one was
// attached with errs.Public, or the generic status text otherwise.
// API requests get JSON, everything else an HTML page.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	status := StatusFor(err)
	msg, ok := errs.PublicMessage(err)
	if !ok {
		msg = http.StatusText(status)
	}

	log := logger.FromContext(r.Context())
	if status >= 500 {
		log.Error("request failed", zap.Error(err))
	} else {
		log.Info("request rejected", zap.Int("status", status), zap.Error(err))
	}

	if IsAPI(r) {
		JSON(w, status, map[string]string{"error": msg})
		return
	}
	_ = Render(w, r, status, ui.ErrorPage(status, msg))
}

// StatusFor maps an error to an HTTP status code.
func StatusFor(err error) int {
	switch {
	case errors.Is(err, errs.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, errs.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, errs.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, errs.ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, errs.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, errs.ErrRateLimited):
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// IsAPI reports whether the request expects a JSON response.
func IsAPI(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/") ||
		strings.Contains(r.Header.Get("Accept"), "application/json")
}

// IsHTMX reports whether the request was made by htmx.
func IsHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// Render writes a templ component with the given status code.
func Render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	return c.Render(r.Context(), w)
}

// JSON writes v as a JSON response.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Redirect sends the client to url. For htmx requests it uses the
// HX-Redirect header so the whole page navigates, not just the target.
func Redirect(w http.ResponseWriter, r *http.Request, url string) {
	if IsHTMX(r) {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}
