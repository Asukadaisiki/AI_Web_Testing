package postgres

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrateAppliesSchemaOnceAndEnforcesContracts(t *testing.T) {
	dsn := os.Getenv("AGENTTRACE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_TEST_POSTGRES_DSN is not configured")
	}
	if _, err := testpostgres.DatabaseName(dsn); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)
	resetDatabase(t, ctx, pool)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var applied int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}
	if applied != 4 {
		t.Fatalf("applied migrations = %d, want 4", applied)
	}

	for _, unique := range []struct {
		table   string
		columns []string
	}{
		{"messages", []string{"conversation_id", "ordinal"}},
		{"messages", []string{"run_id"}},
		{"run_events", []string{"run_id", "seq"}},
		{"spans", []string{"run_id", "trace_id", "span_id"}},
		{"operation_results", []string{"operation_id"}},
		{"idempotency_keys", []string{"scope", "key"}},
		{"outbox_events", []string{"event_id"}},
		{"inbox_events", []string{"consumer", "event_id"}},
	} {
		assertUniqueConstraint(t, ctx, pool, unique.table, unique.columns)
	}

	for _, column := range []string{"next_attempt_at", "locked_by", "locked_until", "claim_token"} {
		assertColumn(t, ctx, pool, "outbox_events", column)
	}
}

func TestMigrateSerializesConcurrentInitialMigration(t *testing.T) {
	dsn := os.Getenv("AGENTTRACE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_TEST_POSTGRES_DSN is not configured")
	}
	if _, err := testpostgres.DatabaseName(dsn); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse test database DSN: %v", err)
	}
	config.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)

	for attempt := 1; attempt <= 10; attempt++ {
		resetDatabase(t, ctx, pool)
		if _, err := pool.Exec(ctx, `
			CREATE TABLE schema_migrations (
				version TEXT PRIMARY KEY,
				applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)`); err != nil {
			t.Fatalf("create migration ledger for attempt %d: %v", attempt, err)
		}

		start := make(chan struct{})
		errs := make(chan error, 2)
		var group sync.WaitGroup
		for range 2 {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				errs <- Migrate(ctx, pool)
			}()
		}
		close(start)
		group.Wait()
		close(errs)

		for err := range errs {
			if err != nil {
				t.Fatalf("concurrent migration attempt %d: %v", attempt, err)
			}
		}

		var applied int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil {
			t.Fatalf("count applied migrations after attempt %d: %v", attempt, err)
		}
		if applied != 4 {
			t.Fatalf("applied migrations after attempt %d = %d, want 4", attempt, applied)
		}
	}
}

func resetDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset test database: %v", err)
	}
}

func assertUniqueConstraint(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	table string,
	columns []string,
) {
	t.Helper()

	var found bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.table_constraints AS table_constraint
			JOIN information_schema.key_column_usage AS column_use
			  ON table_constraint.constraint_catalog = column_use.constraint_catalog
			 AND table_constraint.constraint_schema = column_use.constraint_schema
			 AND table_constraint.constraint_name = column_use.constraint_name
			WHERE table_constraint.table_schema = current_schema()
			  AND table_constraint.table_name = $1
			  AND table_constraint.constraint_type = 'UNIQUE'
			GROUP BY table_constraint.constraint_name
			HAVING array_agg(column_use.column_name::text ORDER BY column_use.ordinal_position) = $2::text[]
		)`, table, columns).Scan(&found)
	if err != nil {
		t.Fatalf("inspect unique constraint on %s: %v", table, err)
	}
	if !found {
		t.Fatalf("%s does not have unique constraint on %v", table, columns)
	}
}

func assertColumn(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, column string) {
	t.Helper()

	var found bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = $1
			  AND column_name = $2
		)`, table, column).Scan(&found)
	if err != nil {
		t.Fatalf("inspect column %s.%s: %v", table, column, err)
	}
	if !found {
		t.Fatalf("missing column %s.%s", table, column)
	}
}
