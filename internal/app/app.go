// Package app is the composition root: it lists every module that makes up
// the application. Adding a feature means adding its Module here.
package app

import (
	"github.com/arashthr/skeleton/internal/account"
	"github.com/arashthr/skeleton/internal/apitoken"
	"github.com/arashthr/skeleton/internal/auth"
	"github.com/arashthr/skeleton/internal/home"
	"github.com/arashthr/skeleton/internal/platform/config"
	"github.com/arashthr/skeleton/internal/platform/database"
	"github.com/arashthr/skeleton/internal/platform/email"
	"github.com/arashthr/skeleton/internal/platform/logger"
	"github.com/arashthr/skeleton/internal/platform/server"
	"github.com/arashthr/skeleton/internal/ui"
	"github.com/arashthr/skeleton/internal/user"
	"go.uber.org/fx"
)

func Options() fx.Option {
	return fx.Options(
		logger.FxLogger,

		// Platform
		config.Module,
		logger.Module,
		database.Module,
		email.Module,
		server.Module,
		fx.Invoke(ui.Setup),

		// Features
		user.Module,
		auth.Module,
		account.Module,
		apitoken.Module,
		home.Module,
	)
}
