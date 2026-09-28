// Package dbtest provides migrated, isolated databases for tests.
//
// Tests run against a fresh SQLite file by default. Set TEST_POSTGRES_URL
// (e.g. postgres://postgres:postgres@localhost:5432/app?sslmode=disable) to
// run the same tests against Postgres; each test then gets its own schema.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/arashthr/skeleton/internal/platform/database"
	"go.uber.org/zap"
)

// Config returns the driver and URL of a fresh database for t.
func Config(t testing.TB) (driver, dsn string) {
	t.Helper()
	pgURL := os.Getenv("TEST_POSTGRES_URL")
	if pgURL == "" {
		return string(database.SQLite), filepath.Join(t.TempDir(), "test.db")
	}

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "test_" + hex.EncodeToString(b)

	admin, err := database.Open(context.Background(), database.Postgres, pgURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		admin.Close()
	})

	u, err := url.Parse(pgURL)
	if err != nil {
		t.Fatalf("parse TEST_POSTGRES_URL: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return string(database.Postgres), u.String()
}

// New returns a migrated database that is closed when the test ends.
func New(t testing.TB) *database.DB {
	t.Helper()
	driver, dsn := Config(t)
	db, err := database.Open(context.Background(), database.Dialect(driver), dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), zap.NewNop()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}
