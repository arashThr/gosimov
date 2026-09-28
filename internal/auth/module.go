// Package auth implements sign up and sign in: email and password, magic
// links, OAuth providers (Google, GitHub), sessions, email verification and
// password reset.
package auth

import (
	"github.com/arashthr/skeleton/internal/platform/server"
	"go.uber.org/fx"
)

var Module = fx.Module("auth",
	fx.Provide(
		NewSessionStore,
		NewTokenStore,
		NewSessions,
		NewService,
		NewProviders,
		NewTurnstile,
		newOAuthFlow,
		server.AsRoutes(NewHandler),
		server.AsMiddleware(func(s *Sessions) server.Middleware { return s.Middleware() }),
	),
	fx.Invoke(registerLifecycle),
)
