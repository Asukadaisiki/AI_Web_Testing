package projects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) List(ctx context.Context, actorUserID int64) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.description, p.created_at, p.updated_at
		FROM projects p
		JOIN project_members pm ON pm.project_id = p.id
		WHERE pm.user_id = $1
		ORDER BY p.name, p.id`, actorUserID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	result := make([]Project, 0)
	for rows.Next() {
		var project Project
		if err := rows.Scan(
			&project.ID, &project.Name, &project.Description,
			&project.CreatedAt, &project.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		result = append(result, project)
	}
	return result, rows.Err()
}

func (s *PostgresStore) Get(ctx context.Context, projectID, actorUserID int64) (Project, error) {
	return scanProject(s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.description, p.created_at, p.updated_at
		FROM projects p
		JOIN project_members pm ON pm.project_id = p.id
		WHERE p.id = $1 AND pm.user_id = $2`, projectID, actorUserID))
}

func (s *PostgresStore) Create(
	ctx context.Context,
	actorUserID int64,
	request CreateRequest,
) (Project, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return Project{}, errors.New("project name is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin project transaction: %w", err)
	}
	defer tx.Rollback()
	var project Project
	err = tx.QueryRowContext(ctx, `
		INSERT INTO projects (name, description)
		VALUES ($1, $2)
		RETURNING id, name, description, created_at, updated_at`,
		name, request.Description,
	).Scan(&project.ID, &project.Name, &project.Description, &project.CreatedAt, &project.UpdatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO project_members (project_id, user_id, role)
		VALUES ($1, $2, 'owner')`, project.ID, actorUserID); err != nil {
		return Project{}, fmt.Errorf("create project owner: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit project: %w", err)
	}
	return project, nil
}

func (s *PostgresStore) Update(
	ctx context.Context,
	projectID, actorUserID int64,
	request UpdateRequest,
) (Project, error) {
	if request.Name != nil {
		value := strings.TrimSpace(*request.Name)
		if value == "" || len(value) > 200 {
			return Project{}, errors.New("invalid project name")
		}
		request.Name = &value
	}
	row := s.db.QueryRowContext(ctx, `
		UPDATE projects p
		SET name = COALESCE($3, p.name),
		    description = CASE WHEN $4::boolean THEN $5 ELSE p.description END,
		    updated_at = now()
		WHERE p.id = $1
		  AND EXISTS (
		    SELECT 1 FROM project_members pm
		    WHERE pm.project_id = p.id AND pm.user_id = $2
		  )
		RETURNING p.id, p.name, p.description, p.created_at, p.updated_at`,
		projectID, actorUserID, request.Name, request.Description != nil, request.Description)
	return scanProject(row)
}

func (s *PostgresStore) Delete(ctx context.Context, projectID, actorUserID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin project deletion: %w", err)
	}
	defer tx.Rollback()
	var owned bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM project_members
			WHERE project_id = $1 AND user_id = $2 AND role = 'owner'
		)`, projectID, actorUserID).Scan(&owned); err != nil {
		return fmt.Errorf("check project ownership: %w", err)
	}
	if !owned {
		return ErrAccessDenied
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM locator_corrections
		WHERE source_execution_id IN (
			SELECT id FROM test_case_runs WHERE project_id = $1
		)`, projectID); err != nil {
		return fmt.Errorf("delete project corrections: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM test_case_runs WHERE project_id = $1`,
		projectID,
	); err != nil {
		return fmt.Errorf("delete project executions: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM execution_batches WHERE project_id = $1`,
		projectID,
	); err != nil {
		return fmt.Errorf("delete project batches: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM projects WHERE id = $1`,
		projectID,
	); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit project deletion: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanProject(row rowScanner) (Project, error) {
	var project Project
	err := row.Scan(
		&project.ID, &project.Name, &project.Description,
		&project.CreatedAt, &project.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("scan project: %w", err)
	}
	return project, nil
}
