// Package database opens the SQL database (Postgres or SQLite) and applies
// migrations.
//
// Queries are written once in portable SQL: use $1-style placeholders, pass
// timestamps from Go (never NOW()), and prefer standard syntax (RETURNING,
// ON CONFLICT) that both engines support.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arashthr/skeleton/internal/platform/config"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	"go.uber.org/fx"
	"go.uber.org/zap"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var Module = fx.Module("database", fx.Provide(New))

type Dialect string

const (
	Postgres Dialect = "postgres"
	SQLite   Dialect = "sqlite"
)

// DB wraps *sql.DB and remembers which engine it talks to.
type DB struct {
	*sql.DB
	Dialect Dialect
}

// New opens the configured database, runs pending migrations and closes the
// pool when the app stops.
func New(lc fx.Lifecycle, cfg *config.Config, log *zap.Logger) (*DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := Open(ctx, Dialect(cfg.Database.Driver), cfg.Database.URL)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, log); err != nil {
		db.Close()
		return nil, err
	}
	lc.Append(fx.StopHook(db.Close))
	log.Info("database ready", zap.String("driver", string(db.Dialect)))
	return db, nil
}

// Open connects to the database and verifies the connection.
func Open(ctx context.Context, dialect Dialect, url string) (*DB, error) {
	var (
		sqlDB *sql.DB
		err   error
	)
	switch dialect {
	case Postgres:
		sqlDB, err = sql.Open("pgx", url)
		if err != nil {
			return nil, fmt.Errorf("open postgres: %w", err)
		}
		sqlDB.SetMaxOpenConns(25)
		sqlDB.SetMaxIdleConns(5)
		sqlDB.SetConnMaxLifetime(time.Hour)
	case SQLite:
		sqlDB, err = sql.Open("sqlite", sqliteDSN(url))
		if err != nil {
			return nil, fmt.Errorf("open sqlite: %w", err)
		}
		// SQLite allows a single writer. One connection avoids "database is locked"
		// errors; WAL mode keeps it fast enough for most apps.
		sqlDB.SetMaxOpenConns(1)
	default:
		return nil, fmt.Errorf("unsupported database dialect %q", dialect)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping %s: %w", dialect, err)
	}
	return &DB{DB: sqlDB, Dialect: dialect}, nil
}

// sqliteDSN turns a file path into a DSN with sane pragmas. Full DSNs
// (starting with "file:") are passed through untouched.
func sqliteDSN(path string) string {
	if strings.HasPrefix(path, "file:") {
		return path
	}
	if path != ":memory:" {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	return "file:" + path +
		"?_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_time_format=sqlite"
}

// InTx runs fn inside a transaction, committing on success and rolling back on error.
// fn must only use tx: with SQLite the pool has a single connection, so
// querying db inside fn would wait forever.
func (db *DB) InTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// IsUniqueViolation reports whether err was caused by a unique constraint.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		code := sqliteErr.Code()
		return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return false
}
