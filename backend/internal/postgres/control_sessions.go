package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const DefaultControlSessionTTL = 5 * time.Minute
const MaxControlSessionTTL = 10 * time.Minute

type ControlSession struct {
	ID                string    `json:"id"`
	EnvironmentID     string    `json:"environment_id"`
	ExpiresAt         time.Time `json:"expires_at"`
	ControlURL        string    `json:"control_url"`
	State             string    `json:"state"`
	ProjectID         string    `json:"-"`
	ActorID           string    `json:"-"`
	ProfileGeneration int64     `json:"-"`
	FencingToken      int64     `json:"-"`
	ProviderSessionID string    `json:"-"`
	OperationToken    string    `json:"-"`
	ReviewRequired    bool      `json:"-"`
	tokenHash         string
	unexpired         bool
}

type CreateControlSessionParams struct {
	EnvironmentID string
	ProjectID     string
	ActorID       string
	TTL           time.Duration
}

const controlSessionColumns = `id, environment_id, expires_at, state, project_id, actor_id,
	profile_generation, fencing_token, provider_session_id, operation_token, token_hash,
	expires_at > clock_timestamp(), review_required`

func scanControlSession(row rowScanner) (ControlSession, error) {
	var c ControlSession
	err := row.Scan(&c.ID, &c.EnvironmentID, &c.ExpiresAt, &c.State, &c.ProjectID, &c.ActorID,
		&c.ProfileGeneration, &c.FencingToken, &c.ProviderSessionID, &c.OperationToken, &c.tokenHash, &c.unexpired, &c.ReviewRequired)
	if errors.Is(err, pgx.ErrNoRows) {
		return ControlSession{}, ErrNotFound
	}
	c.ControlURL = "/api/v1/control-sessions/" + c.ID
	return c, err
}

func (s *Store) ControlSessionOwner(ctx context.Context, id string) (projectID, environmentID string, err error) {
	if err = s.requireManagement(); err != nil {
		return "", "", err
	}
	err = s.pool.QueryRow(ctx, `SELECT project_id, environment_id FROM control_sessions WHERE id=$1`, id).Scan(&projectID, &environmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

// Reservation commits before the one-time link is returned. Pending sessions
// exclude Run claims just as an open browser does.
func (s *Store) CreateControlSession(ctx context.Context, p CreateControlSessionParams) (ControlSession, string, error) {
	if err := s.requireManagement(); err != nil {
		return ControlSession{}, "", err
	}
	if p.ProjectID == "" || strings.TrimSpace(p.ActorID) == "" {
		return ControlSession{}, "", ErrConflict
	}
	if p.TTL == 0 {
		p.TTL = DefaultControlSessionTTL
	}
	if p.TTL <= 0 || p.TTL > MaxControlSessionTTL {
		return ControlSession{}, "", ErrConflict
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ControlSession{}, "", err
	}
	defer tx.Rollback(ctx)
	env, err := lockEnvironment(ctx, tx, p.EnvironmentID)
	if err != nil {
		return ControlSession{}, "", err
	}
	if env.ProjectID != p.ProjectID || (env.State != "ready" && env.State != "needs_review") {
		return ControlSession{}, "", ErrConflict
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT state='active' FROM conversations WHERE id=$1`, env.ConversationID).Scan(&active); err != nil {
		return ControlSession{}, "", err
	}
	if !active {
		return ControlSession{}, "", ErrConflict
	}
	if err := managementConversationSafe(ctx, tx, env, ""); err != nil {
		return ControlSession{}, "", err
	}
	var blocked bool
	err = tx.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM environment_leases WHERE environment_id=$1 AND state IN ('active','releasing'))
		OR EXISTS (SELECT 1 FROM control_sessions WHERE environment_id=$1 AND
			(state IN ('pending','opening','leased','busy','closing') OR
			(state='needs_review' AND profile_generation=$2)))`, env.ID, env.ProfileGeneration).Scan(&blocked)
	if err != nil {
		return ControlSession{}, "", err
	}
	if blocked {
		return ControlSession{}, "", ErrConflict
	}
	id, err := newID("ctrl")
	if err != nil {
		return ControlSession{}, "", err
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return ControlSession{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	sum := sha256.Sum256([]byte(token))
	reviewRequired := env.State == "needs_review"
	env, err = scanEnvironment(tx.QueryRow(ctx, `UPDATE environments SET state='leased', fencing_token=fencing_token+1,
		last_used_at=now(), updated_at=now() WHERE id=$1 RETURNING `+environmentColumns, env.ID))
	if err != nil {
		return ControlSession{}, "", err
	}
	c, err := scanControlSession(tx.QueryRow(ctx, `INSERT INTO control_sessions
		(id,project_id,environment_id,actor_id,token_hash,profile_generation,fencing_token,expires_at,review_required)
		VALUES ($1,$2,$3,$4,$5,$6,$7,clock_timestamp()+$8::bigint*interval '1 millisecond',$9)
		RETURNING `+controlSessionColumns, id, p.ProjectID, env.ID, p.ActorID, hex.EncodeToString(sum[:]),
		env.ProfileGeneration, env.FencingToken, p.TTL.Milliseconds(), reviewRequired))
	if err != nil {
		return ControlSession{}, "", err
	}
	if err := appendManagementAudit(ctx, tx, env, p.ActorID, "control_session.created"); err != nil {
		return ControlSession{}, "", err
	}
	return c, token, tx.Commit(ctx)
}

// All state changes follow Project -> Conversation -> Environment -> Session.
func (s *Store) lockControlSession(ctx context.Context, id, project, actor string) (pgx.Tx, ControlSession, Environment, error) {
	if err := s.requireManagement(); err != nil {
		return nil, ControlSession{}, Environment{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, ControlSession{}, Environment{}, err
	}
	fail := func(err error) (pgx.Tx, ControlSession, Environment, error) {
		_ = tx.Rollback(ctx)
		return nil, ControlSession{}, Environment{}, err
	}
	var environmentID string
	err = tx.QueryRow(ctx, `SELECT environment_id FROM control_sessions WHERE id=$1`, id).Scan(&environmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(ErrNotFound)
	}
	if err != nil {
		return fail(err)
	}
	env, err := lockEnvironment(ctx, tx, environmentID)
	if err != nil {
		return fail(err)
	}
	c, err := scanControlSession(tx.QueryRow(ctx, `SELECT `+controlSessionColumns+` FROM control_sessions WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return fail(err)
	}
	if c.ProjectID != project || c.ActorID != actor || env.ProjectID != project {
		return fail(ErrConflict)
	}
	return tx, c, env, nil
}

func controlIdentityMatches(c ControlSession, env Environment) bool {
	return c.EnvironmentID == env.ID && c.ProfileGeneration == env.ProfileGeneration && c.FencingToken == env.FencingToken
}

func (s *Store) GetControlSession(ctx context.Context, id, project, actor string) (ControlSession, error) {
	tx, c, _, err := s.lockControlSession(ctx, id, project, actor)
	if err != nil {
		return ControlSession{}, err
	}
	defer tx.Rollback(ctx)
	return c, tx.Commit(ctx)
}

func (s *Store) ConsumeControlSession(ctx context.Context, id, project, actor, token string) (ControlSession, Environment, error) {
	tx, c, env, err := s.lockControlSession(ctx, id, project, actor)
	if err != nil {
		return ControlSession{}, Environment{}, err
	}
	defer tx.Rollback(ctx)
	hash := sha256.Sum256([]byte(token))
	expected := hex.EncodeToString(hash[:])
	if c.State != "pending" || !c.unexpired || !controlIdentityMatches(c, env) || env.State != "leased" ||
		subtle.ConstantTimeCompare([]byte(expected), []byte(c.tokenHash)) != 1 {
		return ControlSession{}, Environment{}, ErrConflict
	}
	c, err = scanControlSession(tx.QueryRow(ctx, `UPDATE control_sessions SET state='opening', consumed_at=clock_timestamp(),
		updated_at=now() WHERE id=$1 RETURNING `+controlSessionColumns, id))
	if err != nil {
		return ControlSession{}, Environment{}, err
	}
	if err := appendManagementAudit(ctx, tx, env, actor, "control_session.consumed"); err != nil {
		return ControlSession{}, Environment{}, err
	}
	return c, env, tx.Commit(ctx)
}

func (s *Store) ActivateControlSession(ctx context.Context, id, project, actor, handle string) (ControlSession, error) {
	tx, c, env, err := s.lockControlSession(ctx, id, project, actor)
	if err != nil {
		return ControlSession{}, err
	}
	defer tx.Rollback(ctx)
	if !strings.HasPrefix(handle, "bsess_") || c.State != "opening" || !c.unexpired || env.State != "leased" || !controlIdentityMatches(c, env) {
		return ControlSession{}, ErrConflict
	}
	c, err = scanControlSession(tx.QueryRow(ctx, `UPDATE control_sessions SET state='leased', provider_session_id=$2,
		updated_at=now() WHERE id=$1 RETURNING `+controlSessionColumns, id, handle))
	if err != nil {
		return ControlSession{}, err
	}
	return c, tx.Commit(ctx)
}

// A durable operation claim serializes input, screenshot and stop without
// retaining a database connection across any provider operation.
func (s *Store) ClaimControlSessionOperation(ctx context.Context, id, project, actor string) (ControlSession, Environment, error) {
	tx, c, env, err := s.lockControlSession(ctx, id, project, actor)
	if err != nil {
		return ControlSession{}, Environment{}, err
	}
	defer tx.Rollback(ctx)
	if c.State != "leased" || !c.unexpired || c.ProviderSessionID == "" || env.State != "leased" || !controlIdentityMatches(c, env) {
		return ControlSession{}, Environment{}, ErrConflict
	}
	token, err := newID("cop")
	if err != nil {
		return ControlSession{}, Environment{}, err
	}
	c, err = scanControlSession(tx.QueryRow(ctx, `UPDATE control_sessions SET state='busy', operation_token=$2,
		updated_at=now() WHERE id=$1 RETURNING `+controlSessionColumns, id, token))
	if err != nil {
		return ControlSession{}, Environment{}, err
	}
	return c, env, tx.Commit(ctx)
}

func (s *Store) CompleteControlSessionOperation(ctx context.Context, claim ControlSession, succeeded bool) error {
	tx, c, env, err := s.lockControlSession(ctx, claim.ID, claim.ProjectID, claim.ActorID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if c.State != "busy" || c.OperationToken == "" || c.OperationToken != claim.OperationToken ||
		!controlIdentityMatches(claim, env) || !controlIdentityMatches(c, env) || env.State != "leased" {
		return ErrConflict
	}
	state := "leased"
	if !succeeded || !c.unexpired {
		state = "needs_review"
		if _, err := tx.Exec(ctx, `UPDATE environments SET state='needs_review', updated_at=now() WHERE id=$1`, env.ID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE control_sessions SET state=$2,
		review_required=review_required OR $2='needs_review', operation_token='', updated_at=now() WHERE id=$1`, c.ID, state); err != nil {
		return err
	}
	if err := appendManagementAudit(ctx, tx, env, c.ActorID, "control_session.operation_"+state); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if !c.unexpired {
		return ErrConflict
	}
	return nil
}

func (s *Store) FailControlSession(ctx context.Context, id, project, actor string) error {
	tx, c, env, err := s.lockControlSession(ctx, id, project, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if c.State == "closed" || !controlIdentityMatches(c, env) || (env.State != "leased" && env.State != "needs_review") {
		return ErrConflict
	}
	if err := markControlNeedsReview(ctx, tx, c, env); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func markControlNeedsReview(ctx context.Context, tx pgx.Tx, c ControlSession, env Environment) error {
	if _, err := tx.Exec(ctx, `UPDATE control_sessions SET state='needs_review', review_required=true, operation_token='', updated_at=now() WHERE id=$1`, c.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE environments SET state='needs_review', updated_at=now() WHERE id=$1`, env.ID); err != nil {
		return err
	}
	return appendManagementAudit(ctx, tx, env, c.ActorID, "control_session.needs_review")
}

func (s *Store) BeginCloseControlSession(ctx context.Context, id, project, actor string) (ControlSession, error) {
	tx, c, env, err := s.lockControlSession(ctx, id, project, actor)
	if err != nil {
		return ControlSession{}, err
	}
	defer tx.Rollback(ctx)
	if !controlIdentityMatches(c, env) || (env.State != "leased" && env.State != "needs_review") ||
		(c.State != "pending" && c.State != "leased" && c.State != "needs_review") {
		return ControlSession{}, ErrConflict
	}
	// Unknown Open outcome must be recovered by a generation-fenced reset.
	if c.State == "needs_review" && c.ProviderSessionID == "" {
		return ControlSession{}, ErrConflict
	}
	token, err := newID("cclose")
	if err != nil {
		return ControlSession{}, err
	}
	c, err = scanControlSession(tx.QueryRow(ctx, `UPDATE control_sessions SET state='closing', operation_token=$2,
		updated_at=now() WHERE id=$1 RETURNING `+controlSessionColumns, id, token))
	if err != nil {
		return ControlSession{}, err
	}
	return c, tx.Commit(ctx)
}

// Only call after Close acknowledges context shutdown and profile persistence.
// An unconsumed reservation has no provider work and needs no external Close.
func (s *Store) CompleteCloseControlSession(ctx context.Context, claim ControlSession) (ControlSession, error) {
	tx, c, env, err := s.lockControlSession(ctx, claim.ID, claim.ProjectID, claim.ActorID)
	if err != nil {
		return ControlSession{}, err
	}
	defer tx.Rollback(ctx)
	if c.State != "closing" || c.OperationToken == "" || c.OperationToken != claim.OperationToken ||
		!controlIdentityMatches(claim, env) || !controlIdentityMatches(c, env) ||
		(env.State != "leased" && env.State != "needs_review") {
		return ControlSession{}, ErrConflict
	}
	c, err = scanControlSession(tx.QueryRow(ctx, `UPDATE control_sessions SET state='closed',
		review_required=review_required OR expires_at<=clock_timestamp(), provider_session_id='',
		operation_token='', closed_at=clock_timestamp(), updated_at=now() WHERE id=$1 RETURNING `+controlSessionColumns, c.ID))
	if err != nil {
		return ControlSession{}, err
	}
	state := "ready"
	if c.ReviewRequired {
		state = "needs_review"
	}
	if _, err := tx.Exec(ctx, `UPDATE environments SET state=$2, updated_at=now() WHERE id=$1`, env.ID, state); err != nil {
		return ControlSession{}, err
	}
	if err := appendManagementAudit(ctx, tx, env, c.ActorID, "control_session.closed"); err != nil {
		return ControlSession{}, err
	}
	return c, tx.Commit(ctx)
}

// TTL is recovery, never permission to replay an interrupted browser action.
func (s *Store) RecoverExpiredControlSessions(ctx context.Context) (int, error) {
	if err := s.requireManagement(); err != nil {
		return 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,project_id,actor_id FROM control_sessions
		WHERE expires_at <= clock_timestamp() AND state NOT IN ('closed','needs_review') ORDER BY id LIMIT 100`)
	if err != nil {
		return 0, err
	}
	var expired []ControlSession
	for rows.Next() {
		var c ControlSession
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.ActorID); err != nil {
			rows.Close()
			return 0, err
		}
		expired = append(expired, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, candidate := range expired {
		tx, c, env, err := s.lockControlSession(ctx, candidate.ID, candidate.ProjectID, candidate.ActorID)
		if err != nil {
			return n, err
		}
		if !c.unexpired && c.State != "closed" && c.State != "needs_review" && controlIdentityMatches(c, env) &&
			(env.State == "leased" || env.State == "needs_review") {
			err = markControlNeedsReview(ctx, tx, c, env)
			if err == nil {
				err = tx.Commit(ctx)
			}
			if err == nil {
				n++
			}
		}
		_ = tx.Rollback(ctx)
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
