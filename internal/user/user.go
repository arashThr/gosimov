// Package user owns the user model and its persistence. Authentication
// concerns (passwords, sessions, OAuth) live in the auth package.
package user

import (
	"context"
	"strings"
	"time"

	"go.uber.org/fx"
)

var Module = fx.Module("user", fx.Provide(NewRepository))

type User struct {
	ID              int64
	Email           string
	PasswordHash    *string // nil for users who only sign in with magic links or OAuth
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (u *User) EmailVerified() bool { return u.EmailVerifiedAt != nil }
func (u *User) HasPassword() bool   { return u.PasswordHash != nil }

// Identity links a user to an account at a third-party provider.
type Identity struct {
	ID             int64
	UserID         int64
	Provider       string
	ProviderUserID string
	Email          string
	CreatedAt      time.Time
}

// NormalizeEmail lowercases and trims an email address.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

type ctxKey struct{}

// WithContext returns a copy of ctx carrying the signed-in user.
func WithContext(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// FromContext returns the signed-in user, or nil for anonymous requests.
func FromContext(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKey{}).(*User)
	return u
}
