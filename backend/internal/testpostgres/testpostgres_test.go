package testpostgres

import "testing"

func TestDatabaseNameRequiresExplicitIsolatedDatabase(t *testing.T) {
	for _, dsn := range []string{
		"",
		"postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace?sslmode=disable",
		"postgres://agenttrace:agenttrace@127.0.0.1:5433/postgres?sslmode=disable",
		"postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace_tests?sslmode=disable",
	} {
		if _, err := DatabaseName(dsn); err == nil {
			t.Fatalf("DatabaseName(%q) accepted a non-isolated target", dsn)
		}
	}

	for _, dsn := range []string{
		"postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace_test?sslmode=disable",
		"postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace_test_runner?sslmode=disable",
	} {
		if _, err := DatabaseName(dsn); err != nil {
			t.Fatalf("DatabaseName(%q): %v", dsn, err)
		}
	}
}
