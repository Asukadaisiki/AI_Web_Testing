package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const EnvironmentCommandsTopic = "agenttrace.environment.commands.v1"
const EnvironmentCommandConsumer = "agenttrace-environments"

// Set the flag once at startup; do not change it while requests are in flight.
func (s *Store) SetManagementEnabled(enabled bool) {
	if s != nil {
		s.managementEnabled.Store(enabled)
	}
}

func (s *Store) ManagementEnabled() bool {
	return s != nil && s.managementEnabled.Load()
}

// This is deployment policy, supplied once at startup. Metadata and uploaded
// Harness content cannot grant additional browser domains.
func (s *Store) SetEnvironmentDefaultPolicy(domains []string) error {
	host := regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	if len(domains) > 100 {
		return errors.New("browser policy exceeds domain limit")
	}
	for _, domain := range domains {
		if len(domain) > 253 || !host.MatchString(domain) || strings.Contains(domain, "..") {
			return errors.New("browser policy requires lowercase exact domain names")
		}
	}
	if domains == nil {
		domains = []string{}
	}
	raw, err := json.Marshal(map[string]any{"allowed_domains": domains})
	if err != nil {
		return err
	}
	policy := json.RawMessage(raw)
	s.environmentPolicy.Store(&policy)
	return nil
}

func (s *Store) defaultEnvironmentPolicy() json.RawMessage {
	if policy := s.environmentPolicy.Load(); policy != nil {
		return append(json.RawMessage(nil), (*policy)...)
	}
	return json.RawMessage(`{"allowed_domains":[]}`)
}

type ProjectBinding struct {
	AgenttraceProjectID string    `json:"agenttrace_project_id"`
	LangfuseProjectID   string    `json:"langfuse_project_id"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Environment struct {
	ID                string          `json:"id"`
	ProjectID         string          `json:"project_id"`
	ConversationID    string          `json:"conversation_id"`
	State             string          `json:"state"`
	ProfileGeneration int64           `json:"profile_generation"`
	FencingToken      int64           `json:"fencing_token"`
	Policy            json.RawMessage `json:"policy"`
	ProfileRef        string          `json:"profile_ref,omitempty"`
	LastUsedAt        *time.Time      `json:"last_used_at,omitempty"`
	CreatedBy         string          `json:"created_by,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type EnvironmentCommand struct {
	EnvironmentID     string          `json:"environment_id"`
	ProjectID         string          `json:"project_id"`
	ConversationID    string          `json:"conversation_id"`
	Kind              string          `json:"kind"`
	ProfileGeneration int64           `json:"profile_generation"`
	FencingToken      int64           `json:"fencing_token"`
	Policy            json.RawMessage `json:"policy"`
	ProfileRef        string          `json:"profile_ref,omitempty"`
	ActorID           string          `json:"actor_id,omitempty"`
}

type EnvironmentCommandClaim struct {
	EventID string
	Token   string
	Command EnvironmentCommand
}

type EnvironmentProviderResult struct {
	ProfileRef string
}

// Providers must be idempotent by Environment identity and profile generation.
// Commands never carry plaintext browser storage state or provider endpoints.
type EnvironmentProvider interface {
	ApplyEnvironment(context.Context, EnvironmentCommand) (EnvironmentProviderResult, error)
}

func (s *Store) requireManagement() error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	if !s.ManagementEnabled() {
		return ErrConflict
	}
	return nil
}

func (s *Store) createBoundProject(ctx context.Context, p CreateProjectParams) (Project, error) {
	if strings.TrimSpace(p.LangfuseProjectID) == "" {
		return Project{}, errors.New("Langfuse project id is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Project{}, err
	}
	defer tx.Rollback(ctx)
	id, err := newID("prj")
	if err != nil {
		return Project{}, err
	}
	project, err := scanProject(tx.QueryRow(ctx, `INSERT INTO projects (id, name, metadata) VALUES ($1, $2, $3::jsonb) RETURNING id, name, metadata, created_at, updated_at`, id, p.Name, requiredJSON(p.Metadata, `{}`)))
	if err != nil {
		return Project{}, err
	}
	if _, err := bindProject(ctx, tx, id, p.LangfuseProjectID); err != nil {
		return Project{}, err
	}
	return project, tx.Commit(ctx)
}

// BindProject is an internal namespace operation, not a user membership API.
func (s *Store) BindProject(ctx context.Context, projectID, langfuseProjectID string) (ProjectBinding, error) {
	if s == nil || s.pool == nil {
		return ProjectBinding{}, errors.New("postgres store is not configured")
	}
	if strings.TrimSpace(langfuseProjectID) == "" {
		return ProjectBinding{}, errors.New("Langfuse project id is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ProjectBinding{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockProject(ctx, tx, projectID); err != nil {
		return ProjectBinding{}, err
	}
	binding, err := bindProject(ctx, tx, projectID, langfuseProjectID)
	if err != nil {
		return ProjectBinding{}, err
	}
	return binding, tx.Commit(ctx)
}

func bindProject(ctx context.Context, tx pgx.Tx, projectID, langfuseProjectID string) (ProjectBinding, error) {
	_, err := tx.Exec(ctx, `INSERT INTO project_bindings (agenttrace_project_id, langfuse_project_id) VALUES ($1, $2) ON CONFLICT (agenttrace_project_id) DO NOTHING`, projectID, langfuseProjectID)
	if err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) && pgerr.Code == "23505" {
			return ProjectBinding{}, ErrConflict
		}
		return ProjectBinding{}, err
	}
	binding, err := scanProjectBinding(tx.QueryRow(ctx, `SELECT agenttrace_project_id, langfuse_project_id, status, created_at, updated_at FROM project_bindings WHERE agenttrace_project_id = $1`, projectID))
	if err == nil && binding.LangfuseProjectID != langfuseProjectID {
		return ProjectBinding{}, ErrConflict
	}
	return binding, err
}

func (s *Store) GetProjectBinding(ctx context.Context, projectID string) (ProjectBinding, error) {
	if s == nil || s.pool == nil {
		return ProjectBinding{}, errors.New("postgres store is not configured")
	}
	return scanProjectBinding(s.pool.QueryRow(ctx, `SELECT agenttrace_project_id, langfuse_project_id, status, created_at, updated_at FROM project_bindings WHERE agenttrace_project_id = $1`, projectID))
}

func scanProjectBinding(row rowScanner) (ProjectBinding, error) {
	var binding ProjectBinding
	err := row.Scan(&binding.AgenttraceProjectID, &binding.LangfuseProjectID, &binding.Status, &binding.CreatedAt, &binding.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectBinding{}, ErrNotFound
	}
	return binding, err
}

const environmentColumns = `id, project_id, conversation_id, state, profile_generation, fencing_token, policy, COALESCE(profile_ref, ''), last_used_at, created_by, created_at, updated_at`

func scanEnvironment(row rowScanner) (Environment, error) {
	var environment Environment
	err := row.Scan(&environment.ID, &environment.ProjectID, &environment.ConversationID, &environment.State,
		&environment.ProfileGeneration, &environment.FencingToken, &environment.Policy, &environment.ProfileRef,
		&environment.LastUsedAt, &environment.CreatedBy, &environment.CreatedAt, &environment.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	return environment, err
}

func (s *Store) GetEnvironment(ctx context.Context, id string) (Environment, error) {
	if err := s.requireManagement(); err != nil {
		return Environment{}, err
	}
	return scanEnvironment(s.pool.QueryRow(ctx, `SELECT `+environmentColumns+` FROM environments WHERE id = $1`, id))
}

func (s *Store) GetConversationEnvironment(ctx context.Context, conversationID string) (Environment, error) {
	if err := s.requireManagement(); err != nil {
		return Environment{}, err
	}
	return scanEnvironment(s.pool.QueryRow(ctx, `SELECT `+environmentColumns+` FROM environments WHERE conversation_id = $1`, conversationID))
}

func createConversationEnvironment(ctx context.Context, tx pgx.Tx, conversation Conversation, actor string, defaultPolicy ...json.RawMessage) error {
	id, err := newID("env")
	if err != nil {
		return err
	}
	policy := json.RawMessage(`{"allowed_domains":[]}`)
	if len(defaultPolicy) > 0 {
		policy = defaultPolicy[0]
	}
	environment, err := scanEnvironment(tx.QueryRow(ctx, `INSERT INTO environments (id, project_id, conversation_id, created_by, fencing_token, policy) VALUES ($1, $2, $3, $4, 1,$5) RETURNING `+environmentColumns,
		id, conversation.ProjectID, conversation.ID, actor, policy))
	if err != nil {
		return err
	}
	if err := enqueueEnvironmentCommand(ctx, tx, environment, "provision", actor); err != nil {
		return err
	}
	return appendManagementAudit(ctx, tx, environment, actor, "conversation.created")
}

func enqueueEnvironmentCommand(ctx context.Context, tx pgx.Tx, environment Environment, kind, actor string) error {
	id, err := newID("evt")
	if err != nil {
		return err
	}
	command := EnvironmentCommand{
		EnvironmentID: environment.ID, ProjectID: environment.ProjectID, ConversationID: environment.ConversationID,
		Kind: kind, ProfileGeneration: environment.ProfileGeneration, FencingToken: environment.FencingToken,
		Policy: environment.Policy, ProfileRef: environment.ProfileRef, ActorID: actor,
	}
	envelope := event.Envelope[EnvironmentCommand]{
		SchemaVersion: "agenttrace.environment-command.v1", EventID: id, EventType: "environment." + kind + "_requested",
		AggregateID: environment.ID, CorrelationID: environment.ConversationID, OccurredAt: time.Now().UTC(), Payload: command,
	}
	hash, err := payloadHash(envelope)
	if err != nil {
		return err
	}
	commandHash, err := payloadHash(command)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO environment_commands (event_id, environment_id, conversation_id, project_id, kind, profile_generation, payload_hash, command_hash) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, environment.ID, environment.ConversationID, environment.ProjectID, kind, environment.ProfileGeneration, hash, commandHash); err != nil {
		return err
	}
	inserted, err := insertOutbox(ctx, tx, id, EnvironmentCommandsTopic, environment.ID, envelope, hash)
	if err == nil && !inserted {
		return ErrConflict
	}
	return err
}

func appendManagementAudit(ctx context.Context, tx pgx.Tx, environment Environment, actor, action string) error {
	id, err := newID("aud")
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO management_audit (id, project_id, conversation_id, environment_id, actor_id, action) VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6)`,
		id, environment.ProjectID, environment.ConversationID, environment.ID, actor, action)
	return err
}

// lockEnvironment follows the shared Project -> Conversation -> Environment order.
func lockEnvironment(ctx context.Context, tx pgx.Tx, id string) (Environment, error) {
	var conversationID string
	err := tx.QueryRow(ctx, `SELECT conversation_id FROM environments WHERE id = $1`, id).Scan(&conversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	if err != nil {
		return Environment{}, err
	}
	if _, err := lockConversation(ctx, tx, conversationID); err != nil {
		return Environment{}, err
	}
	return scanEnvironment(tx.QueryRow(ctx, `SELECT `+environmentColumns+` FROM environments WHERE id = $1 FOR UPDATE`, id))
}

func (s *Store) ResetEnvironment(ctx context.Context, id, actor string) (Environment, error) {
	if err := s.requireManagement(); err != nil {
		return Environment{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Environment{}, err
	}
	defer tx.Rollback(ctx)
	environment, err := lockEnvironment(ctx, tx, id)
	if err != nil {
		return Environment{}, err
	}
	if environment.State != "ready" && environment.State != "needs_review" && environment.State != "broken" {
		return Environment{}, ErrConflict
	}
	if err := managementConversationSafe(ctx, tx, environment, ""); err != nil {
		return Environment{}, err
	}
	environment, err = scanEnvironment(tx.QueryRow(ctx, `UPDATE environments SET state = 'resetting', profile_generation = profile_generation + 1, fencing_token=fencing_token+1, updated_at = now() WHERE id = $1 RETURNING `+environmentColumns, id))
	if err != nil {
		return Environment{}, err
	}
	if err := enqueueEnvironmentCommand(ctx, tx, environment, "reset", actor); err != nil {
		return Environment{}, err
	}
	if err := appendManagementAudit(ctx, tx, environment, actor, "environment.reset_requested"); err != nil {
		return Environment{}, err
	}
	return environment, tx.Commit(ctx)
}

// DeleteEnvironment deletes its owning Conversation as well: the 1:1 invariant
// never leaves an active Conversation without a browser identity.
func (s *Store) DeleteEnvironment(ctx context.Context, id, actor string) error {
	environment, err := s.GetEnvironment(ctx, id)
	if err != nil {
		return err
	}
	return s.deleteManagedConversation(ctx, environment.ConversationID, actor)
}

// ProcessEnvironmentCommand intentionally holds no transaction during provider execution.
func (s *Store) ProcessEnvironmentCommand(ctx context.Context, envelope event.Envelope[EnvironmentCommand], provider EnvironmentProvider) error {
	if provider == nil {
		return errors.New("environment provider is required")
	}
	claim, acquired, err := s.ClaimEnvironmentCommand(ctx, envelope)
	if err != nil {
		return err
	}
	if !acquired {
		var done bool
		if err := s.pool.QueryRow(ctx, `SELECT state = 'succeeded' FROM environment_commands WHERE event_id = $1`, envelope.EventID).Scan(&done); err != nil {
			return err
		}
		if !done {
			return ErrConflict
		}
		return nil
	}
	result, err := provider.ApplyEnvironment(ctx, claim.Command)
	if err != nil {
		// Provider lifecycle operations reconcile by identity/generation on
		// retry. A cancelled DB context leaves the claim running, fail closed.
		return errors.Join(err, s.ReleaseEnvironmentCommand(ctx, claim))
	}
	return s.CompleteEnvironmentCommand(ctx, claim, result)
}

// ReleaseEnvironmentCommand makes a failed lifecycle call retryable. After a
// process crash, only release once the previous provider call is quiescent.
// Running claims are never reclaimed automatically on a wall-clock timeout.
func (s *Store) ReleaseEnvironmentCommand(ctx context.Context, claim EnvironmentCommandClaim) error {
	if err := s.requireManagement(); err != nil {
		return err
	}
	hash, err := payloadHash(claim.Command)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE environment_commands SET state = 'pending', claim_token = NULL, claimed_at = NULL
		WHERE event_id = $1 AND state = 'running' AND claim_token = $2 AND command_hash = $3`,
		claim.EventID, claim.Token, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// GetEnvironmentCommandClaim lets a replacement consumer reconcile an
// interrupted provider operation using canonical command bytes and its token.
func (s *Store) GetEnvironmentCommandClaim(ctx context.Context, eventID string) (EnvironmentCommandClaim, error) {
	if err := s.requireManagement(); err != nil {
		return EnvironmentCommandClaim{}, err
	}
	var token, state, expectedHash string
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(command.claim_token, ''), command.state, command.command_hash, inbox.payload
		FROM environment_commands command JOIN inbox_events inbox
		ON inbox.event_id = command.event_id AND inbox.consumer = $2
		WHERE command.event_id = $1`, eventID, EnvironmentCommandConsumer).Scan(&token, &state, &expectedHash, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnvironmentCommandClaim{}, ErrNotFound
	}
	if err != nil {
		return EnvironmentCommandClaim{}, err
	}
	if state != "running" || token == "" {
		return EnvironmentCommandClaim{}, ErrConflict
	}
	var envelope event.Envelope[EnvironmentCommand]
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return EnvironmentCommandClaim{}, err
	}
	hash, err := payloadHash(envelope.Payload)
	if err != nil {
		return EnvironmentCommandClaim{}, err
	}
	if hash != expectedHash || envelope.EventID != eventID {
		return EnvironmentCommandClaim{}, ErrConflict
	}
	return EnvironmentCommandClaim{EventID: eventID, Token: token, Command: envelope.Payload}, nil
}

func (s *Store) ClaimEnvironmentCommand(ctx context.Context, envelope event.Envelope[EnvironmentCommand]) (EnvironmentCommandClaim, bool, error) {
	if err := s.requireManagement(); err != nil {
		return EnvironmentCommandClaim{}, false, err
	}
	if err := envelope.Validate(); err != nil {
		return EnvironmentCommandClaim{}, false, err
	}
	if envelope.SchemaVersion != "agenttrace.environment-command.v1" || envelope.EventID == "" ||
		envelope.AggregateID != envelope.Payload.EnvironmentID || envelope.EventType != "environment."+envelope.Payload.Kind+"_requested" {
		return EnvironmentCommandClaim{}, false, ErrConflict
	}
	hash, err := payloadHash(envelope)
	if err != nil {
		return EnvironmentCommandClaim{}, false, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return EnvironmentCommandClaim{}, false, err
	}
	defer tx.Rollback(ctx)
	var storedHash, state string
	if err := tx.QueryRow(ctx, `SELECT payload_hash, state FROM environment_commands WHERE event_id = $1`, envelope.EventID).Scan(&storedHash, &state); errors.Is(err, pgx.ErrNoRows) {
		return EnvironmentCommandClaim{}, false, ErrNotFound
	} else if err != nil {
		return EnvironmentCommandClaim{}, false, err
	}
	if storedHash != hash {
		return EnvironmentCommandClaim{}, false, ErrConflict
	}
	if state != "pending" {
		return EnvironmentCommandClaim{}, false, tx.Commit(ctx)
	}
	environment, err := lockEnvironment(ctx, tx, envelope.Payload.EnvironmentID)
	if err != nil {
		return EnvironmentCommandClaim{}, false, err
	}
	command := envelope.Payload
	if environment.ProjectID != command.ProjectID || environment.ConversationID != command.ConversationID ||
		environment.ProfileGeneration != command.ProfileGeneration || environment.FencingToken != command.FencingToken ||
		environment.State != environmentCommandState(command.Kind) {
		return EnvironmentCommandClaim{}, false, ErrConflict
	}
	token, err := newID("claim")
	if err != nil {
		return EnvironmentCommandClaim{}, false, err
	}
	tag, err := tx.Exec(ctx, `UPDATE environment_commands SET state = 'running', claim_token = $2, claimed_at = now() WHERE event_id = $1 AND state = 'pending'`, envelope.EventID, token)
	if err != nil || tag.RowsAffected() == 0 {
		return EnvironmentCommandClaim{}, false, err
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return EnvironmentCommandClaim{}, false, err
	}
	inboxID, err := newID("inb")
	if err != nil {
		return EnvironmentCommandClaim{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO inbox_events (id, consumer, event_id, payload) VALUES ($1, $2, $3, $4::jsonb) ON CONFLICT (consumer, event_id) DO NOTHING`,
		inboxID, EnvironmentCommandConsumer, envelope.EventID, raw); err != nil {
		return EnvironmentCommandClaim{}, false, err
	}
	claim := EnvironmentCommandClaim{EventID: envelope.EventID, Token: token, Command: command}
	return claim, true, tx.Commit(ctx)
}

func environmentCommandState(kind string) string {
	switch kind {
	case "provision":
		return "provisioning"
	case "reset":
		return "resetting"
	case "delete":
		return "deleting"
	}
	return ""
}

func (s *Store) CompleteEnvironmentCommand(ctx context.Context, claim EnvironmentCommandClaim, result EnvironmentProviderResult) error {
	if err := s.requireManagement(); err != nil {
		return err
	}
	commandHash, err := payloadHash(claim.Command)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var state, token, environmentID, kind, storedCommandHash string
	var generation int64
	if err := tx.QueryRow(ctx, `SELECT state, COALESCE(claim_token, ''), environment_id, kind, profile_generation, command_hash FROM environment_commands WHERE event_id = $1`,
		claim.EventID).Scan(&state, &token, &environmentID, &kind, &generation, &storedCommandHash); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if storedCommandHash != commandHash || token != claim.Token || token == "" || environmentID != claim.Command.EnvironmentID ||
		kind != claim.Command.Kind || generation != claim.Command.ProfileGeneration {
		return ErrConflict
	}
	if state == "succeeded" {
		return tx.Commit(ctx)
	}
	// Serialize acknowledgements before looking up an Environment that the
	// first successful delete acknowledgement may already have removed.
	if _, err := lockConversation(ctx, tx, claim.Command.ConversationID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT state, COALESCE(claim_token, '') FROM environment_commands WHERE event_id = $1 FOR UPDATE`, claim.EventID).Scan(&state, &token); err != nil {
		return err
	}
	if token != claim.Token {
		return ErrConflict
	}
	if state == "succeeded" {
		return tx.Commit(ctx)
	}
	environment, err := scanEnvironment(tx.QueryRow(ctx, `SELECT `+environmentColumns+` FROM environments WHERE id = $1 FOR UPDATE`, environmentID))
	if err != nil {
		return err
	}
	if state != "running" || environment.State != environmentCommandState(kind) ||
		environment.ProfileGeneration != generation || environment.FencingToken != claim.Command.FencingToken {
		return ErrConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE environment_commands SET state = 'succeeded', completed_at = now() WHERE event_id = $1 AND state = 'running' AND claim_token = $2`, claim.EventID, token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE inbox_events SET processed_at = now() WHERE consumer = $1 AND event_id = $2`, EnvironmentCommandConsumer, claim.EventID); err != nil {
		return err
	}
	if kind == "delete" {
		if err := managementConversationSafe(ctx, tx, environment, claim.EventID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM environments WHERE id = $1`, environmentID); err != nil {
			return err
		}
		if err := deleteConversationData(ctx, tx, []string{environment.ConversationID}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE conversation_id = $1`, environment.ConversationID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE conversations SET state = 'deleted', title = NULL, metadata = '{}', deleted_at = now(), updated_at = now() WHERE id = $1`, environment.ConversationID); err != nil {
			return err
		}
		if err := deleteEnvironmentDeliveryRecords(ctx, tx, environment); err != nil {
			return err
		}
		if err := appendManagementAudit(ctx, tx, environment, claim.Command.ActorID, "conversation.deleted"); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, `UPDATE environments SET state = 'ready', profile_ref = NULLIF($2, ''), updated_at = now() WHERE id = $1`, environmentID, result.ProfileRef); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func managementConversationSafe(ctx context.Context, tx pgx.Tx, environment Environment, exceptEventID string) error {
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM conversations WHERE id = $1`, environment.ConversationID).Scan(&state); err != nil {
		return err
	}
	if state == "deleted" || (state == "deleting" && exceptEventID == "") || environment.State == "leased" {
		return ErrConflict
	}
	var leasesInstalled bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('environment_leases') IS NOT NULL`).Scan(&leasesInstalled); err != nil {
		return err
	}
	if leasesInstalled {
		var leased bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM environment_leases WHERE environment_id = $1 AND state IN ('active', 'releasing'))`, environment.ID).Scan(&leased); err != nil {
			return err
		}
		if leased {
			return ErrConflict
		}
	}
	rows, err := tx.Query(ctx, `SELECT id, status FROM runs WHERE conversation_id = $1 ORDER BY id FOR UPDATE`, environment.ConversationID)
	if err != nil {
		return err
	}
	var runIDs []string
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			rows.Close()
			return err
		}
		if status != "succeeded" && status != "failed" && status != "cancelled" {
			rows.Close()
			return ErrConflict
		}
		runIDs = append(runIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if safe, err := deliverySafeForDeletion(ctx, tx, runIDs); err != nil {
		return err
	} else if !safe {
		return ErrConflict
	}
	var safe bool
	err = tx.QueryRow(ctx, `SELECT
		NOT EXISTS (SELECT 1 FROM outbox_events WHERE
		  (key = $1 OR key = $2 OR payload->'payload'->>'conversation_id' = $2)
		  AND event_id <> $3 AND published_at IS NULL)
		AND NOT EXISTS (SELECT 1 FROM environment_commands WHERE environment_id = $1 AND event_id <> $3 AND state <> 'succeeded')
		AND NOT EXISTS (SELECT 1 FROM inbox_events WHERE processed_at IS NULL AND event_id <> $3 AND (
		  payload->'payload'->>'environment_id' = $1 OR payload->'payload'->>'conversation_id' = $2
		  OR payload->'payload'->>'run_id' = ANY($4::text[])
		  OR event_id IN (SELECT event_id FROM outbox_events WHERE key = $1 OR key = ANY($4::text[]))))`,
		environment.ID, environment.ConversationID, exceptEventID, runIDs).Scan(&safe)
	if err != nil {
		return fmt.Errorf("check environment delivery safety: %w", err)
	}
	if !safe {
		return ErrConflict
	}
	return nil
}
