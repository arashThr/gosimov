// Package flash implements one-shot messages that survive a redirect
// ("Password updated", "Check your inbox", ...).
package flash

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
)

type Kind string

const (
	Success Kind = "success"
	Error   Kind = "error"
	Info    Kind = "info"
)

type Message struct {
	Kind Kind   `json:"k"`
	Text string `json:"t"`
}

const cookieName = "flash"

// Set stores a message to be shown on the next rendered page.
func Set(w http.ResponseWriter, kind Kind, text string) {
	b, _ := json.Marshal(Message{Kind: kind, Text: text})
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    base64.RawURLEncoding.EncodeToString(b),
		Path:     "/",
		MaxAge:   60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

type ctxKey struct{}

// Middleware moves a pending flash message from its cookie into the request
// context and clears the cookie.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1})

		var msg Message
		if b, err := base64.RawURLEncoding.DecodeString(c.Value); err == nil && json.Unmarshal(b, &msg) == nil {
			r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &msg))
		}
		next.ServeHTTP(w, r)
	})
}

// FromContext returns the flash message for this request, or nil.
func FromContext(ctx context.Context) *Message {
	msg, _ := ctx.Value(ctxKey{}).(*Message)
	return msg
}
