package cases

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var supportedActions = map[string]struct{}{
	"goto": {}, "click": {}, "input": {}, "wait_for": {},
	"assert_text": {}, "assert_url_contains": {}, "capture_text": {},
}

type PostgresStore struct {
	db *sql.DB
}

type rowScanner interface {
	Scan(...any) error
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) List(
	ctx context.Context,
	actorUserID int64,
	projectID *int64,
	search string,
	page, pageSize int,
) (Page, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	pattern := "%" + strings.ReplaceAll(strings.ReplaceAll(search, "%", "\\%"), "_", "\\_") + "%"
	var total int64
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM test_cases tc
		JOIN project_members pm ON pm.project_id = tc.project_id
		WHERE pm.user_id = $1
		  AND ($2::bigint IS NULL OR tc.project_id = $2)
		  AND ($3 = '' OR tc.name ILIKE $4 ESCAPE '\'
		       OR COALESCE(tc.description, '') ILIKE $4 ESCAPE '\')`,
		actorUserID, projectID, search, pattern,
	).Scan(&total)
	if err != nil {
		return Page{}, fmt.Errorf("count cases: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tc.id, tc.project_id, tc.name, tc.description, tc.dsl,
		       tc.created_by, tc.updated_by, tc.created_at, tc.updated_at
		FROM test_cases tc
		JOIN project_members pm ON pm.project_id = tc.project_id
		WHERE pm.user_id = $1
		  AND ($2::bigint IS NULL OR tc.project_id = $2)
		  AND ($3 = '' OR tc.name ILIKE $4 ESCAPE '\'
		       OR COALESCE(tc.description, '') ILIKE $4 ESCAPE '\')
		ORDER BY tc.created_at DESC, tc.id DESC
		OFFSET $5 LIMIT $6`,
		actorUserID, projectID, search, pattern, (page-1)*pageSize, pageSize,
	)
	if err != nil {
		return Page{}, fmt.Errorf("list cases: %w", err)
	}
	defer rows.Close()
	items := make([]Stored, 0)
	for rows.Next() {
		item, err := scanCase(rows)
		if err != nil {
			return Page{}, err
		}
		items = append(items, item)
	}
	totalPages := (total + int64(pageSize) - 1) / int64(pageSize)
	return Page{
		Items: items, Total: total, Page: page, PageSize: pageSize,
		TotalPages: totalPages, HasNext: int64(page) < totalPages, HasPrev: page > 1,
	}, rows.Err()
}

func (s *PostgresStore) Get(ctx context.Context, caseID, actorUserID int64) (Stored, error) {
	return scanCase(s.db.QueryRowContext(ctx, `
		SELECT tc.id, tc.project_id, tc.name, tc.description, tc.dsl,
		       tc.created_by, tc.updated_by, tc.created_at, tc.updated_at
		FROM test_cases tc
		JOIN project_members pm ON pm.project_id = tc.project_id
		WHERE tc.id = $1 AND pm.user_id = $2`, caseID, actorUserID))
}

func (s *PostgresStore) Create(
	ctx context.Context,
	actorUserID int64,
	request Mutation,
) (Stored, error) {
	if err := validateMutation(request); err != nil {
		return Stored{}, err
	}
	if err := s.requireMembership(ctx, request.ProjectID, actorUserID); err != nil {
		return Stored{}, err
	}
	dsl, err := encodeDSL(request)
	if err != nil {
		return Stored{}, err
	}
	return scanCase(s.db.QueryRowContext(ctx, `
		INSERT INTO test_cases (project_id, created_by, updated_by, name, description, dsl)
		VALUES ($1, $2, $2, $3, $4, $5)
		RETURNING id, project_id, name, description, dsl,
		          created_by, updated_by, created_at, updated_at`,
		request.ProjectID, actorUserID, request.Name, request.Description, string(dsl)))
}

func (s *PostgresStore) Update(
	ctx context.Context,
	caseID, actorUserID int64,
	request Mutation,
) (Stored, error) {
	if err := validateMutation(request); err != nil {
		return Stored{}, err
	}
	if err := s.requireMembership(ctx, request.ProjectID, actorUserID); err != nil {
		return Stored{}, err
	}
	dsl, err := encodeDSL(request)
	if err != nil {
		return Stored{}, err
	}
	return scanCase(s.db.QueryRowContext(ctx, `
		UPDATE test_cases tc
		SET project_id = $3, updated_by = $2, name = $4,
		    description = $5, dsl = $6, updated_at = now()
		WHERE tc.id = $1
		  AND EXISTS (
		    SELECT 1 FROM project_members pm
		    WHERE pm.project_id = tc.project_id AND pm.user_id = $2
		  )
		RETURNING id, project_id, name, description, dsl,
		          created_by, updated_by, created_at, updated_at`,
		caseID, actorUserID, request.ProjectID, request.Name, request.Description, string(dsl)))
}

func (s *PostgresStore) Delete(ctx context.Context, caseID, actorUserID int64) error {
	count, err := s.DeleteBatch(ctx, actorUserID, []int64{caseID})
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteBatch(
	ctx context.Context,
	actorUserID int64,
	caseIDs []int64,
) (int64, error) {
	if len(caseIDs) == 0 {
		return 0, errors.New("case_ids must not be empty")
	}
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM test_cases tc
		WHERE tc.id = ANY($1)
		  AND EXISTS (
		    SELECT 1 FROM project_members pm
		    WHERE pm.project_id = tc.project_id AND pm.user_id = $2
		  )`, caseIDs, actorUserID)
	if err != nil {
		return 0, fmt.Errorf("delete cases: %w", err)
	}
	return result.RowsAffected()
}

func (s *PostgresStore) requireMembership(ctx context.Context, projectID, actorUserID int64) error {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
		  SELECT 1 FROM project_members WHERE project_id = $1 AND user_id = $2
		)`, projectID, actorUserID).Scan(&exists); err != nil {
		return fmt.Errorf("check project membership: %w", err)
	}
	if !exists {
		return ErrAccessDenied
	}
	return nil
}

func validateMutation(request Mutation) error {
	if strings.TrimSpace(request.Name) == "" {
		return errors.New("case name is required")
	}
	var steps []struct {
		Action string `json:"action"`
	}
	if len(request.Steps) == 0 || json.Unmarshal(request.Steps, &steps) != nil || len(steps) == 0 {
		return errors.New("steps must be a non-empty array")
	}
	for _, step := range steps {
		if _, ok := supportedActions[step.Action]; !ok {
			return fmt.Errorf("unsupported DSL action: %s", step.Action)
		}
	}
	return nil
}

func encodeDSL(request Mutation) ([]byte, error) {
	return json.Marshal(map[string]any{
		"name": request.Name, "description": request.Description, "base_url": request.BaseURL,
		"input_contract":  rawOrEmptyArray(request.InputContract),
		"output_contract": rawOrEmptyArray(request.OutputContract),
		"steps":           request.Steps,
	})
}

func scanCase(row rowScanner) (Stored, error) {
	var item Stored
	var raw []byte
	err := row.Scan(
		&item.ID, &item.ProjectID, &item.Name, &item.Description, &raw,
		&item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Stored{}, ErrNotFound
	}
	if err != nil {
		return Stored{}, fmt.Errorf("scan case: %w", err)
	}
	var dsl struct {
		BaseURL        *string         `json:"base_url"`
		InputContract  json.RawMessage `json:"input_contract"`
		OutputContract json.RawMessage `json:"output_contract"`
		Steps          json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(raw, &dsl); err != nil {
		return Stored{}, fmt.Errorf("decode case DSL: %w", err)
	}
	item.BaseURL = dsl.BaseURL
	item.InputContract = rawOrEmptyArray(dsl.InputContract)
	item.OutputContract = rawOrEmptyArray(dsl.OutputContract)
	item.Steps = rawOrEmptyArray(dsl.Steps)
	return item, nil
}

func rawOrEmptyArray(value json.RawMessage) json.RawMessage {
	if len(value) == 0 || string(value) == "null" {
		return json.RawMessage(`[]`)
	}
	return value
}
