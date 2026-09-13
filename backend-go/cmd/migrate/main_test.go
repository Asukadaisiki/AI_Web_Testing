package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dbschema"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestMigrateAddsGroundingPlansToExistingDatabase(t *testing.T) {
	ctx := context.Background()
	db := newMigrationTestDatabase(t, ctx)
	if _, err := db.ExecContext(ctx, dbschema.SchemaSQL); err != nil {
		t.Fatalf("apply baseline schema: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`DROP TABLE public.grounding_plans`,
	); err != nil {
		t.Fatalf("remove grounding plans from existing schema: %v", err)
	}

	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate existing database: %v", err)
	}

	var exists bool
	if err := db.QueryRowContext(
		ctx,
		`SELECT to_regclass('public.grounding_plans') IS NOT NULL`,
	).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("grounding_plans table does not exist after migration")
	}
}

func newMigrationTestDatabase(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	adminURL := *parsed
	adminURL.Path = "/postgres"
	adminURL.RawPath = ""
	admin, err := sql.Open("pgx", adminURL.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.PingContext(ctx); err != nil {
		admin.Close()
		t.Fatal(err)
	}

	databaseName := fmt.Sprintf(
		"ai_web_testing_migrate_%d_%d",
		os.Getpid(),
		time.Now().UnixNano(),
	)
	if _, err := admin.ExecContext(
		ctx,
		"CREATE DATABASE "+databaseName,
	); err != nil {
		admin.Close()
		t.Fatalf("create migration test database: %v", err)
	}
	testURL := *parsed
	testURL.Path = "/" + databaseName
	testURL.RawPath = ""
	db, err := sql.Open("pgx", testURL.String())
	if err != nil {
		_, _ = admin.ExecContext(ctx, "DROP DATABASE "+databaseName+" WITH (FORCE)")
		admin.Close()
		t.Fatal(err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		_, _ = admin.ExecContext(ctx, "DROP DATABASE "+databaseName+" WITH (FORCE)")
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.ExecContext(
			context.Background(),
			"DROP DATABASE "+databaseName+" WITH (FORCE)",
		); err != nil {
			t.Errorf("drop migration test database: %v", err)
		}
		admin.Close()
	})
	return db
}
