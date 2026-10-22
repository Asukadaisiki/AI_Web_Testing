// Package testpostgres protects destructive PostgreSQL integration test setup.
package testpostgres

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DatabaseName accepts only databases reserved for destructive test resets.
func DatabaseName(dsn string) (string, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return "", fmt.Errorf("parse PostgreSQL test DSN: %w", err)
	}
	database := config.ConnConfig.Database
	if database != "agenttrace_test" && !strings.HasPrefix(database, "agenttrace_test_") {
		return "", fmt.Errorf(
			"refusing destructive PostgreSQL reset for database %q: require agenttrace_test or agenttrace_test_*",
			database,
		)
	}
	return database, nil
}
