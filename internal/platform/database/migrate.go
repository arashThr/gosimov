package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"go.uber.org/zap"
)

// Each engine has its own migrations folder. Keep them in sync: every
// migration in postgres/ should have a sibling with the same version in sqlite/.
//
//go:embed migrations
var migrationsFS embed.FS

// Migrate applies all pending migrations.
func (db *DB) Migrate(ctx context.Context, log *zap.Logger) error {
	var gooseDialect goose.Dialect
	switch db.Dialect {
	case Postgres:
		gooseDialect = goose.DialectPostgres
	case SQLite:
		gooseDialect = goose.DialectSQLite3
	}

	dir, err := fs.Sub(migrationsFS, "migrations/"+string(db.Dialect))
	if err != nil {
		return fmt.Errorf("migrations dir: %w", err)
	}
	provider, err := goose.NewProvider(gooseDialect, db.DB, dir)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	for _, r := range results {
		log.Info("migration applied",
			zap.String("source", r.Source.Path),
			zap.Duration("duration", r.Duration))
	}
	return nil
}
