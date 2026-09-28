// Command server runs the web application.
package main

//go:generate go tool templ generate -path ../..

import (
	"github.com/arashthr/skeleton/internal/app"
	"github.com/arashthr/skeleton/internal/platform/logger"
	"go.uber.org/fx"
)

func main() {
	fx.New(app.Options(), logger.FxLogger).Run()
}
