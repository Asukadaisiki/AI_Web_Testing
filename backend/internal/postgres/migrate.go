// Package postgres owns PostgreSQL schema initialization for AgentTrace v3.
package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate applies each embedded SQL migration exactly once.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if entry.Name() > "008_harness_catalog.sql" {
			if err := applyMigration(ctx, pool, "008_harness_catalog.sql"); err != nil {
				return err
			}
		}
		if err := applyMigration(ctx, pool, entry.Name()); err != nil {
			return err
		}
	}
	if err := applyMigration(ctx, pool, "008_harness_catalog.sql"); err != nil {
		return err
	}
	return nil
}

// Kept in Go to preserve the backend-only ownership boundary for composition.
const harnessCatalogMigration = `
CREATE TABLE harnesses (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL CHECK (length(trim(name)) > 0),
	revision INTEGER NOT NULL CHECK (revision > 0),
	snapshot JSONB NOT NULL CHECK (jsonb_typeof(snapshot) = 'object'),
	CHECK (snapshot ->> 'id' = id),
	CHECK (snapshot ->> 'name' = name),
	CHECK ((snapshot ->> 'revision')::integer = revision),
	CHECK (jsonb_typeof(snapshot -> 'skills') = 'array'),
	CHECK (jsonb_typeof(snapshot -> 'mcp') = 'object')
);
CREATE TABLE harness_seeds (id TEXT PRIMARY KEY);
`

func applyMigration(ctx context.Context, pool *pgxpool.Pool, version string) error {
	var sql []byte
	var err error
	if version == "008_harness_catalog.sql" {
		sql = []byte(harnessCatalogMigration)
	} else {
		sql, err = migrationFiles.ReadFile("migrations/" + version)
	}
	if err != nil {
		return fmt.Errorf("read migration %s: %w", version, err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", version, err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(24137, hashtext($1))`,
		version,
	); err != nil {
		return fmt.Errorf("lock migration %s: %w", version, err)
	}

	var appliedVersion string
	err = tx.QueryRow(ctx,
		`SELECT version FROM schema_migrations WHERE version = $1 FOR UPDATE`,
		version,
	).Scan(&appliedVersion)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check migration %s: %w", version, err)
	}
	if err == nil {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		return fmt.Errorf("apply migration %s: %w", version, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return fmt.Errorf("record migration %s: %w", version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", version, err)
	}
	return nil
}
