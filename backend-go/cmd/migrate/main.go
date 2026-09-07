package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dbschema"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("configure database: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("connect database: %v", err)
	}
	if err := migrate(ctx, db); err != nil {
		log.Fatalf("migrate database: %v", err)
	}
}

func migrate(ctx context.Context, db *sql.DB) error {
	initialized, err := coreSchemaExists(ctx, db)
	if err != nil {
		return err
	}
	if !initialized {
		if _, err := db.ExecContext(ctx, dbschema.SchemaSQL); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `
		DROP TABLE IF EXISTS public.dsl_anti_patterns;
		DROP TABLE IF EXISTS public.alembic_version;
	`); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO public.users (id, email, display_name)
		VALUES (1, 'seed-owner@example.com', 'Seed Owner')
		ON CONFLICT (id) DO NOTHING;

		INSERT INTO public.projects (id, name, description, is_default)
		VALUES (1, 'Default Project', 'Seed project for local development and tests.', true)
		ON CONFLICT (id) DO NOTHING;

		INSERT INTO public.project_members (id, project_id, user_id, role)
		VALUES (1, 1, 1, 'owner')
		ON CONFLICT (project_id, user_id) DO NOTHING;

		SELECT setval('public.users_id_seq', GREATEST((SELECT COALESCE(MAX(id), 1) FROM public.users), 1), true);
		SELECT setval('public.projects_id_seq', GREATEST((SELECT COALESCE(MAX(id), 1) FROM public.projects), 1), true);
		SELECT setval('public.project_members_id_seq', GREATEST((SELECT COALESCE(MAX(id), 1) FROM public.project_members), 1), true);
	`)
	return err
}

func coreSchemaExists(ctx context.Context, db *sql.DB) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT to_regclass('public.users') IS NOT NULL
	`).Scan(&exists)
	return exists, err
}
