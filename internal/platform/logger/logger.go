// Package logger builds the zap logger and carries request-scoped loggers
// through context.
package logger

import (
	"context"
	"fmt"

	"github.com/arashthr/skeleton/internal/platform/config"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Module = fx.Module("logger",
	fx.Provide(New),
	fx.Invoke(func(lc fx.Lifecycle, l *zap.Logger) {
		undo := zap.ReplaceGlobals(l)
		lc.Append(fx.StopHook(func() {
			_ = l.Sync()
			undo()
		}))
	}),
)

// FxLogger routes fx's own lifecycle events through zap.
var FxLogger = fx.WithLogger(func(l *zap.Logger) fxevent.Logger {
	zl := &fxevent.ZapLogger{Logger: l.Named("fx")}
	zl.UseLogLevel(zapcore.DebugLevel)
	return zl
})

// New builds a colored console logger in development and a JSON logger in production.
func New(cfg *config.Config) (*zap.Logger, error) {
	level, err := zapcore.ParseLevel(cfg.LogLevel)
	if err != nil {
		return nil, fmt.Errorf("invalid LOG_LEVEL %q: %w", cfg.LogLevel, err)
	}

	var zc zap.Config
	if cfg.IsProduction() {
		zc = zap.NewProductionConfig()
		zc.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	} else {
		zc = zap.NewDevelopmentConfig()
		zc.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	zc.Level = zap.NewAtomicLevelAt(level)

	l, err := zc.Build(zap.AddStacktrace(zapcore.ErrorLevel))
	if err != nil {
		return nil, fmt.Errorf("build logger: %w", err)
	}
	return l.With(zap.String("app", cfg.AppName)), nil
}

type ctxKey struct{}

// WithContext returns a copy of ctx carrying l.
func WithContext(ctx context.Context, l *zap.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the request-scoped logger, or the global zap logger
// when none was attached.
func FromContext(ctx context.Context) *zap.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*zap.Logger); ok {
		return l
	}
	return zap.L()
}
