// Package testpg shares PostgreSQL test connection setup across packages.
//
// BUG-177: the platform treats timestamp columns as UTC wall time (Overview
// windows, agent deadlines). The local server may default to a non-UTC session
// (e.g. Asia/Shanghai), so every PostgreSQL test connection must force a UTC
// session, matching the production DSN normalization, otherwise
// `now()`-written timestamp columns fall outside UTC window queries.
package testpg

import (
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// URL returns TEST_DATABASE_URL with a UTC session enforced. It skips the
// caller's test when the variable is unset.
func URL(t *testing.T) string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if raw == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	return WithUTCSession(raw)
}

// Open opens a pgx connection to TEST_DATABASE_URL with a UTC session and
// registers cleanup. It skips the caller's test when the variable is unset.
func Open(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", URL(t))
	if err != nil {
		t.Fatalf("sql.Open(pgx): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// WithUTCSession appends a timezone=UTC runtime parameter to a postgres DSN.
// pgx v5 passes unrecognized connection parameters as runtime parameters, so
// every pooled connection gets `SET timezone TO 'UTC'` before use.
func WithUTCSession(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return dsn
	}
	query := parsed.Query()
	if query.Get("timezone") != "" {
		return dsn
	}
	query.Set("timezone", "UTC")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
