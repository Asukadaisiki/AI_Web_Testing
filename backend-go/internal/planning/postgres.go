package planning

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var requiredSlots = []string{
	"app_under_test",
	"business_goal",
	"entry_url_or_page",
	"core_user_flow",
	"main_assertions",
	"test_data_or_account",
	"scope_limits",
}

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) CreateSession(
	ctx context.Context,
	actorUserID int64,
	request CreateSessionRequest,
) (SessionDetail, error) {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SessionDetail{}, fmt.Errorf("begin planning session transaction: %w", err)
	}
	defer transaction.Rollback()

	requirements := []byte(`{"app_under_test":null,"business_goal":null,"entry_url_or_page":null,"core_user_flow":null,"main_assertions":[],"test_data_or_account":null,"scope_limits":null,"test_context":null}`)
	missingSlots, _ := json.Marshal(requiredSlots)
	var sessionID int64
	err = transaction.QueryRowContext(
		ctx,
		`INSERT INTO ai_planning_sessions (
			actor_user_id, runtime_owner, active_project_id, case_id, status,
			requirements_json, missing_slots_json
		) VALUES ($1, 'go', NULL, NULLIF($2, 0), 'collecting', $3, $4)
		RETURNING id`,
		actorUserID,
		request.CaseID,
		string(requirements),
		string(missingSlots),
	).Scan(&sessionID)
	if err != nil {
		return SessionDetail{}, fmt.Errorf("insert planning session: %w", err)
	}

	projectID, err := selectProjectForSession(
		ctx,
		transaction,
		actorUserID,
		request.ProjectID,
		sessionID,
	)
	if err != nil {
		return SessionDetail{}, err
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO session_projects (session_id, project_id)
		 VALUES ($1, $2)
		 ON CONFLICT (session_id, project_id) DO NOTHING`,
		sessionID,
		projectID,
	); err != nil {
		return SessionDetail{}, fmt.Errorf("link initial planning project: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE ai_planning_sessions SET active_project_id = $2, updated_at = now() WHERE id = $1`,
		sessionID,
		projectID,
	); err != nil {
		return SessionDetail{}, fmt.Errorf("activate initial planning project: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return SessionDetail{}, fmt.Errorf("commit planning session: %w", err)
	}
	return s.GetSession(ctx, actorUserID, sessionID)
}

func selectProjectForSession(
	ctx context.Context,
	transaction *sql.Tx,
	actorUserID int64,
	requestedProjectID int64,
	sessionID int64,
) (int64, error) {
	var projectID int64
	if requestedProjectID > 0 {
		err := transaction.QueryRowContext(
			ctx,
			`SELECT p.id
			   FROM projects p
			   JOIN project_members pm ON pm.project_id = p.id
			  WHERE p.id = $1 AND pm.user_id = $2`,
			requestedProjectID,
			actorUserID,
		).Scan(&projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrProjectNotFound
		}
		if err != nil {
			return 0, fmt.Errorf("select requested planning project: %w", err)
		}
		return projectID, nil
	}

	err := transaction.QueryRowContext(
		ctx,
		`SELECT p.id
		   FROM projects p
		   JOIN project_members pm ON pm.project_id = p.id
		  WHERE pm.user_id = $1
		  ORDER BY p.is_default DESC, p.id ASC
		  LIMIT 1`,
		actorUserID,
	).Scan(&projectID)
	if err == nil {
		return projectID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("select default planning project: %w", err)
	}

	err = transaction.QueryRowContext(
		ctx,
		`INSERT INTO projects (name, description, is_default)
		 VALUES ($1, 'auto-created temporary project', true)
		 RETURNING id`,
		fmt.Sprintf("default-%d", sessionID),
	).Scan(&projectID)
	if err != nil {
		return 0, fmt.Errorf("create default planning project: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'owner')`,
		projectID,
		actorUserID,
	); err != nil {
		return 0, fmt.Errorf("own default planning project: %w", err)
	}
	return projectID, nil
}

func (s *PostgresStore) ListSessions(
	ctx context.Context,
	actorUserID int64,
) ([]SessionSummary, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, runtime_owner, active_project_id, title, status,
		        requirements_json, created_at, updated_at
		   FROM ai_planning_sessions
		  WHERE actor_user_id = $1
		  ORDER BY updated_at DESC`,
		actorUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("list planning sessions: %w", err)
	}
	defer rows.Close()
	result := make([]SessionSummary, 0)
	for rows.Next() {
		var item SessionSummary
		var activeProjectID sql.NullInt64
		var title sql.NullString
		var requirements []byte
		if err := rows.Scan(
			&item.ID,
			&item.RuntimeOwner,
			&activeProjectID,
			&title,
			&item.Status,
			&requirements,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan planning session summary: %w", err)
		}
		item.ActiveProjectID = nullableInt64(activeProjectID)
		item.Title = nullableString(title)
		if item.Title == nil {
			item.Title = titleFromRequirements(requirements)
		}
		item.Projects, err = s.listProjects(ctx, actorUserID, item.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate planning sessions: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) GetSession(
	ctx context.Context,
	actorUserID int64,
	sessionID int64,
) (SessionDetail, error) {
	record, err := s.getSession(ctx, actorUserID, sessionID)
	if err != nil {
		return SessionDetail{}, err
	}
	record.Projects, err = s.listProjects(ctx, actorUserID, sessionID)
	if err != nil {
		return SessionDetail{}, err
	}
	return SessionDetail{Session: record}, nil
}

func (s *PostgresStore) getSession(
	ctx context.Context,
	actorUserID int64,
	sessionID int64,
) (Session, error) {
	var record Session
	var activeProjectID, caseID sql.NullInt64
	var title, lastError sql.NullString
	var requirements, plan, missingSlots []byte
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, actor_user_id, runtime_owner, active_project_id, case_id,
		        title, status, requirements_json, plan_json, missing_slots_json,
		        last_error_message, created_at, updated_at
		   FROM ai_planning_sessions
		  WHERE id = $1 AND actor_user_id = $2`,
		sessionID,
		actorUserID,
	).Scan(
		&record.ID,
		&record.ActorUserID,
		&record.RuntimeOwner,
		&activeProjectID,
		&caseID,
		&title,
		&record.Status,
		&requirements,
		&plan,
		&missingSlots,
		&lastError,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get planning session: %w", err)
	}
	record.ActiveProjectID = nullableInt64(activeProjectID)
	record.CaseID = nullableInt64(caseID)
	record.Title = nullableString(title)
	record.LastErrorMessage = nullableString(lastError)
	record.Requirements = normalizedJSON(requirements, `{}`)
	record.Plan = normalizedJSON(plan, `null`)
	record.MissingSlots = normalizedJSON(missingSlots, `[]`)
	return record, nil
}

func (s *PostgresStore) DeleteSession(
	ctx context.Context,
	actorUserID int64,
	sessionID int64,
) error {
	result, err := s.db.ExecContext(
		ctx,
		`DELETE FROM ai_planning_sessions WHERE id = $1 AND actor_user_id = $2`,
		sessionID,
		actorUserID,
	)
	if err != nil {
		return fmt.Errorf("delete planning session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted planning session count: %w", err)
	}
	if affected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *PostgresStore) UpdateSession(
	ctx context.Context,
	actorUserID int64,
	sessionID int64,
	request UpdateSessionRequest,
) (SessionDetail, error) {
	if request.Title == nil && request.Status == nil {
		return s.GetSession(ctx, actorUserID, sessionID)
	}
	if request.Title != nil {
		title := strings.TrimSpace(*request.Title)
		if len(title) > 200 {
			return SessionDetail{}, errors.New("planning session title exceeds 200 characters")
		}
		request.Title = &title
	}
	if request.Status != nil && !validSessionStatus(*request.Status) {
		return SessionDetail{}, errors.New("invalid planning session status")
	}
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE ai_planning_sessions
		    SET title = COALESCE($3, title),
		        status = COALESCE($4, status),
		        updated_at = now()
		  WHERE id = $1 AND actor_user_id = $2`,
		sessionID,
		actorUserID,
		request.Title,
		request.Status,
	)
	if err != nil {
		return SessionDetail{}, fmt.Errorf("update planning session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return SessionDetail{}, fmt.Errorf("read updated planning session count: %w", err)
	}
	if affected == 0 {
		return SessionDetail{}, ErrSessionNotFound
	}
	return s.GetSession(ctx, actorUserID, sessionID)
}

func (s *PostgresStore) ListProjects(
	ctx context.Context,
	actorUserID int64,
	sessionID int64,
) ([]ProjectSummary, error) {
	if _, err := s.getSession(ctx, actorUserID, sessionID); err != nil {
		return nil, err
	}
	return s.listProjects(ctx, actorUserID, sessionID)
}

func (s *PostgresStore) listProjects(
	ctx context.Context,
	actorUserID int64,
	sessionID int64,
) ([]ProjectSummary, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT p.id, p.name, p.description,
		        COALESCE(p.id = aps.active_project_id, false)
		   FROM ai_planning_sessions aps
		   JOIN session_projects sp ON sp.session_id = aps.id
		   JOIN projects p ON p.id = sp.project_id
		  WHERE aps.id = $1 AND aps.actor_user_id = $2
		  ORDER BY p.id`,
		sessionID,
		actorUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("list planning session projects: %w", err)
	}
	defer rows.Close()
	result := make([]ProjectSummary, 0)
	for rows.Next() {
		var project ProjectSummary
		var description sql.NullString
		if err := rows.Scan(
			&project.ID,
			&project.Name,
			&description,
			&project.IsActive,
		); err != nil {
			return nil, fmt.Errorf("scan planning session project: %w", err)
		}
		project.Description = nullableString(description)
		result = append(result, project)
	}
	return result, rows.Err()
}

func (s *PostgresStore) LinkProject(
	ctx context.Context,
	actorUserID int64,
	sessionID int64,
	projectID int64,
) (ProjectSummary, error) {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectSummary{}, fmt.Errorf("begin link project transaction: %w", err)
	}
	defer transaction.Rollback()
	if err := requireOwnedSessionTx(ctx, transaction, actorUserID, sessionID); err != nil {
		return ProjectSummary{}, err
	}
	project, err := getMemberProjectTx(ctx, transaction, actorUserID, projectID)
	if err != nil {
		return ProjectSummary{}, err
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO session_projects (session_id, project_id)
		 VALUES ($1, $2)
		 ON CONFLICT (session_id, project_id) DO NOTHING`,
		sessionID,
		projectID,
	); err != nil {
		return ProjectSummary{}, fmt.Errorf("link planning project: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE ai_planning_sessions SET active_project_id = $2, updated_at = now()
		  WHERE id = $1`,
		sessionID,
		projectID,
	); err != nil {
		return ProjectSummary{}, fmt.Errorf("activate linked project: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return ProjectSummary{}, fmt.Errorf("commit linked project: %w", err)
	}
	project.IsActive = true
	return project, nil
}

func (s *PostgresStore) UnlinkProject(
	ctx context.Context,
	actorUserID int64,
	sessionID int64,
	projectID int64,
) error {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin unlink project transaction: %w", err)
	}
	defer transaction.Rollback()
	if err := requireOwnedSessionTx(ctx, transaction, actorUserID, sessionID); err != nil {
		return err
	}
	result, err := transaction.ExecContext(
		ctx,
		`DELETE FROM session_projects WHERE session_id = $1 AND project_id = $2`,
		sessionID,
		projectID,
	)
	if err != nil {
		return fmt.Errorf("unlink planning project: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read unlinked project count: %w", err)
	}
	if affected == 0 {
		return ErrProjectNotFound
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE ai_planning_sessions
		    SET active_project_id = CASE
		        WHEN active_project_id = $2 THEN (
		            SELECT project_id FROM session_projects
		             WHERE session_id = $1 ORDER BY id LIMIT 1
		        )
		        ELSE active_project_id
		    END,
		        updated_at = now()
		  WHERE id = $1`,
		sessionID,
		projectID,
	); err != nil {
		return fmt.Errorf("select fallback planning project: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit unlinked project: %w", err)
	}
	return nil
}

func (s *PostgresStore) CreateProject(
	ctx context.Context,
	actorUserID int64,
	sessionID int64,
	request CreateProjectRequest,
) (ProjectSummary, error) {
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return ProjectSummary{}, errors.New("project name is required")
	}
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectSummary{}, fmt.Errorf("begin create project transaction: %w", err)
	}
	defer transaction.Rollback()
	if err := requireOwnedSessionTx(ctx, transaction, actorUserID, sessionID); err != nil {
		return ProjectSummary{}, err
	}
	var project ProjectSummary
	var description sql.NullString
	err = transaction.QueryRowContext(
		ctx,
		`INSERT INTO projects (name, description)
		 VALUES ($1, $2)
		 RETURNING id, name, description`,
		request.Name,
		request.Description,
	).Scan(&project.ID, &project.Name, &description)
	if err != nil {
		return ProjectSummary{}, fmt.Errorf("%w: create project: %v", ErrConflict, err)
	}
	project.Description = nullableString(description)
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'owner')`,
		project.ID,
		actorUserID,
	); err != nil {
		return ProjectSummary{}, fmt.Errorf("own created project: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO session_projects (session_id, project_id) VALUES ($1, $2)`,
		sessionID,
		project.ID,
	); err != nil {
		return ProjectSummary{}, fmt.Errorf("link created project: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE ai_planning_sessions SET active_project_id = $2, updated_at = now()
		  WHERE id = $1`,
		sessionID,
		project.ID,
	); err != nil {
		return ProjectSummary{}, fmt.Errorf("activate created project: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return ProjectSummary{}, fmt.Errorf("commit created project: %w", err)
	}
	project.IsActive = true
	return project, nil
}

func (s *PostgresStore) ResolveRunContext(
	ctx context.Context,
	actorUserID int64,
	sessionID int64,
) (string, int64, error) {
	var projectID int64
	err := s.db.QueryRowContext(
		ctx,
		`SELECT chosen.project_id
		   FROM ai_planning_sessions aps
		   JOIN LATERAL (
		       SELECT sp.project_id
		         FROM session_projects sp
		         JOIN project_members pm
		           ON pm.project_id = sp.project_id AND pm.user_id = $2
		        WHERE sp.session_id = aps.id
		        ORDER BY (sp.project_id = aps.active_project_id) DESC, sp.id ASC
		        LIMIT 1
		   ) chosen ON true
		  WHERE aps.id = $1 AND aps.actor_user_id = $2`,
		sessionID,
		actorUserID,
	).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, ErrSessionNotFound
	}
	if err != nil {
		return "", 0, fmt.Errorf("resolve planning run context: %w", err)
	}
	return strconv.FormatInt(sessionID, 10), projectID, nil
}

func requireOwnedSessionTx(
	ctx context.Context,
	transaction *sql.Tx,
	actorUserID int64,
	sessionID int64,
) error {
	var exists bool
	if err := transaction.QueryRowContext(
		ctx,
		`SELECT EXISTS(
		    SELECT 1 FROM ai_planning_sessions WHERE id = $1 AND actor_user_id = $2
		)`,
		sessionID,
		actorUserID,
	).Scan(&exists); err != nil {
		return fmt.Errorf("check planning session owner: %w", err)
	}
	if !exists {
		return ErrSessionNotFound
	}
	return nil
}

func getMemberProjectTx(
	ctx context.Context,
	transaction *sql.Tx,
	actorUserID int64,
	projectID int64,
) (ProjectSummary, error) {
	var project ProjectSummary
	var description sql.NullString
	err := transaction.QueryRowContext(
		ctx,
		`SELECT p.id, p.name, p.description
		   FROM projects p
		   JOIN project_members pm ON pm.project_id = p.id
		  WHERE p.id = $1 AND pm.user_id = $2`,
		projectID,
		actorUserID,
	).Scan(&project.ID, &project.Name, &description)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectSummary{}, ErrProjectNotFound
	}
	if err != nil {
		return ProjectSummary{}, fmt.Errorf("get member project: %w", err)
	}
	project.Description = nullableString(description)
	return project, nil
}

func normalizedJSON(value []byte, fallback string) json.RawMessage {
	if len(value) == 0 || !json.Valid(value) {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(value)
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func titleFromRequirements(raw []byte) *string {
	var requirements struct {
		AppUnderTest *string `json:"app_under_test"`
	}
	if json.Unmarshal(raw, &requirements) != nil {
		return nil
	}
	return requirements.AppUnderTest
}

func validSessionStatus(value string) bool {
	switch value {
	case "collecting", "plan_ready", "drafts_ready", "reviewing", "saving",
		"executing", "completed", "closed", "error":
		return true
	default:
		return false
	}
}
