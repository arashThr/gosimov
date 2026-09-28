package web

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/arashthr/skeleton/internal/platform/logger"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

// RequestLogger attaches a request-scoped logger (carrying the request id)
// to the context and logs every request once it completes.
func RequestLogger(base *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			log := base.With(zap.String("request_id", middleware.GetReqID(r.Context())))
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() {
				fields := []zap.Field{
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.Int("status", ww.Status()),
					zap.Int("bytes", ww.BytesWritten()),
					zap.Duration("duration", time.Since(start)),
					zap.String("ip", r.RemoteAddr),
				}
				if ww.Status() >= 500 {
					log.Warn("http request", fields...)
				} else {
					log.Debug("http request", fields...)
				}
			}()

			next.ServeHTTP(ww, r.WithContext(logger.WithContext(r.Context(), log)))
		})
	}
}

// Recoverer turns panics into 500 responses and logs the stack trace.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec) // deliberate abort, let net/http handle it
			}
			logger.FromContext(r.Context()).Error("panic recovered",
				zap.Any("panic", rec),
				zap.ByteString("stack", debug.Stack()))
			Error(w, r, fmt.Errorf("panic: %v", rec))
		}()
		next.ServeHTTP(w, r)
	})
}

// SecureHeaders sets conservative security headers on every response.
// HSTS is added by Caddy in production.
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
