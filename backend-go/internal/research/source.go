package research

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dsl"
)

var (
	ErrSourceChanged     = errors.New("research source changed")
	ErrUnsupportedSchema = errors.New("unsupported research source schema")
)

const (
	maxSourceEventPayloadBytes = 8 * 1024 * 1024
	maxSourceReportBytes       = 16 * 1024 * 1024
)

type AgentEventSnapshot struct {
	Ref            SourceRef       `json:"ref"`
	Seq            int64           `json:"seq"`
	Type           string          `json:"type"`
	ConversationID string          `json:"conversation_id"`
	StepID         Slot[string]    `json:"step_id"`
	ToolCallID     Slot[string]    `json:"tool_call_id"`
	ParentID       Slot[string]    `json:"parent_id"`
	CheckpointID   Slot[string]    `json:"checkpoint_id"`
	Payload        json.RawMessage `json:"payload"`
}

type GenerationSnapshot struct {
	Ref              SourceRef       `json:"ref"`
	ID               int64           `json:"id"`
	ProjectID        int64           `json:"project_id"`
	ApprovedBySeq    int64           `json:"approved_by_seq"`
	ApprovalToolCall string          `json:"approval_tool_call_id"`
	DSLCanonical     json.RawMessage `json:"dsl_canonical"`
	DSLSHA256        string          `json:"dsl_sha256"`
	CanonicalVersion string          `json:"canonical_version"`
	RetryFromID      Slot[int64]     `json:"retry_from_generation_id"`
	RetryReasonCode  Slot[string]    `json:"retry_reason_code"`
}

type BatchSnapshot struct {
	Ref          SourceRef     `json:"ref"`
	ID           int64         `json:"id"`
	ProjectID    int64         `json:"project_id"`
	GenerationID int64         `json:"generation_id"`
	Status       string        `json:"status"`
	Jobs         []JobSnapshot `json:"jobs"`
}

type JobSnapshot struct {
	Ref              SourceRef           `json:"ref"`
	ID               int64               `json:"id"`
	OrderIndex       int64               `json:"order_index"`
	Status           string              `json:"status"`
	AttemptCount     int64               `json:"attempt_count"`
	MaxAttempts      int64               `json:"max_attempts"`
	DSLSHA256        string              `json:"dsl_sha256"`
	CanonicalVersion string              `json:"canonical_version"`
	Executions       []ExecutionSnapshot `json:"executions"`
}

type ExecutionSnapshot struct {
	Ref                 SourceRef             `json:"ref"`
	ReportRef           Slot[SourceRef]       `json:"report_ref"`
	ID                  int64                 `json:"id"`
	Attempt             int64                 `json:"attempt"`
	Status              string                `json:"status"`
	DSLSHA256           string                `json:"dsl_sha256"`
	ReportSchemaVersion string                `json:"report_schema_version"`
	Report              Slot[json.RawMessage] `json:"report"`
	FailureSignal       Slot[json.RawMessage] `json:"failure_signal"`
}

type SourceSnapshot struct {
	SchemaVersion  string                `json:"schema_version"`
	ResearchRunID  string                `json:"research_run_id"`
	ProjectID      int64                 `json:"project_id"`
	AgentRunID     string                `json:"agent_run_id"`
	AgentRunStatus string                `json:"agent_run_status"`
	Events         []AgentEventSnapshot  `json:"events"`
	Generations    []GenerationSnapshot  `json:"generations"`
	Batches        []BatchSnapshot       `json:"batches"`
	Reward         Slot[json.RawMessage] `json:"reward"`
	Cursor         SourceCursor          `json:"cursor"`
	SourceSHA256   string                `json:"source_sha256"`
}

type SourceReader interface {
	Read(context.Context, string) (SourceSnapshot, error)
}

type sourceIdentityLinks struct {
	approvedGenerationID sql.NullInt64
	generationID         sql.NullInt64
	batchID              sql.NullInt64
	executionID          sql.NullInt64
	dslSHA256            sql.NullString
}

type PostgresSourceReader struct {
	db *sql.DB
}

func NewPostgresSourceReader(db *sql.DB) *PostgresSourceReader {
	return &PostgresSourceReader{db: db}
}

func (r *PostgresSourceReader) Read(
	ctx context.Context,
	researchRunID string,
) (SourceSnapshot, error) {
	researchRunID = strings.TrimSpace(researchRunID)
	if researchRunID == "" {
		return SourceSnapshot{}, fmt.Errorf("%w: research_run_id", ErrInvalid)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return SourceSnapshot{}, fmt.Errorf("begin source snapshot: %w", err)
	}
	defer tx.Rollback()

	snapshot, links, err := readSourceIdentity(ctx, tx, researchRunID)
	if err != nil {
		return SourceSnapshot{}, err
	}
	snapshot.Events, err = readSourceEvents(ctx, tx, snapshot.AgentRunID)
	if err != nil {
		return SourceSnapshot{}, err
	}
	if len(snapshot.Events) == 0 {
		return SourceSnapshot{}, fmt.Errorf("%w: agent event stream is empty", ErrSourceChanged)
	}
	approved, err := approvedGenerations(snapshot.Events)
	if err != nil {
		return SourceSnapshot{}, err
	}
	if links.approvedGenerationID.Valid {
		if _, exists := approved[links.approvedGenerationID.Int64]; !exists {
			return SourceSnapshot{}, fmt.Errorf(
				"%w: approved generation %d has no approval causation",
				ErrSourceChanged, links.approvedGenerationID.Int64,
			)
		}
	}
	if len(approved) > 0 {
		snapshot.Generations, err = readGenerations(
			ctx, tx, snapshot.ProjectID, approved,
		)
		if err != nil {
			return SourceSnapshot{}, err
		}
		snapshot.Batches, err = readBatches(
			ctx, tx, snapshot.ProjectID, snapshot.AgentRunID,
			snapshot.Generations, snapshot.Events,
		)
		if err != nil {
			return SourceSnapshot{}, err
		}
	} else {
		snapshot.Generations = []GenerationSnapshot{}
		snapshot.Batches = []BatchSnapshot{}
	}
	if err := validateSourceLinks(snapshot, links); err != nil {
		return SourceSnapshot{}, err
	}
	snapshot.Reward = Unavailable[json.RawMessage](
		"independent_oracle_not_persisted",
	)
	snapshot.Cursor = buildSourceCursor(snapshot)
	if err := snapshot.Cursor.NormalizeAndValidate(); err != nil {
		return SourceSnapshot{}, err
	}
	hash, err := sourceSnapshotHash(snapshot)
	if err != nil {
		return SourceSnapshot{}, err
	}
	snapshot.SourceSHA256 = hash
	if err := tx.Commit(); err != nil {
		return SourceSnapshot{}, fmt.Errorf("commit source snapshot: %w", err)
	}
	return snapshot, nil
}

func readSourceIdentity(
	ctx context.Context,
	tx *sql.Tx,
	researchRunID string,
) (SourceSnapshot, sourceIdentityLinks, error) {
	var snapshot SourceSnapshot
	var links sourceIdentityLinks
	var agentRunID sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT rr.id, rr.project_id, rr.agent_run_id, rr.generation_id,
		       rr.batch_id, rr.execution_id, rr.dsl_sha256,
		       ar.status, ar.last_event_seq, ar.approved_generation_id
		FROM research_runs rr
		JOIN agent_runs ar ON ar.id = rr.agent_run_id
		WHERE rr.id = $1`,
		researchRunID,
	).Scan(
		&snapshot.ResearchRunID, &snapshot.ProjectID, &agentRunID,
		&links.generationID, &links.batchID, &links.executionID, &links.dslSHA256,
		&snapshot.AgentRunStatus, &snapshot.Cursor.AgentEventSeq,
		&links.approvedGenerationID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceSnapshot{}, sourceIdentityLinks{}, ErrNotFound
	}
	if err != nil {
		return SourceSnapshot{}, sourceIdentityLinks{}, fmt.Errorf("read source identity: %w", err)
	}
	if !agentRunID.Valid {
		return SourceSnapshot{}, sourceIdentityLinks{}, fmt.Errorf(
			"%w: research run has no agent_run_id",
			ErrBrokenLink,
		)
	}
	snapshot.SchemaVersion = EventSchemaVersion
	snapshot.AgentRunID = agentRunID.String
	return snapshot, links, nil
}

func readSourceEvents(
	ctx context.Context,
	tx *sql.Tx,
	agentRunID string,
) ([]AgentEventSnapshot, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT seq, event_type, conversation_id, step_id, tool_call_id,
		       parent_id, checkpoint_id, payload_json
		FROM agent_events
		WHERE run_id = $1
		ORDER BY seq`,
		agentRunID,
	)
	if err != nil {
		return nil, fmt.Errorf("read source events: %w", err)
	}
	defer rows.Close()
	events := make([]AgentEventSnapshot, 0)
	for rows.Next() {
		var event AgentEventSnapshot
		var stepID, toolCallID, parentID, checkpointID sql.NullString
		var payload []byte
		if err := rows.Scan(
			&event.Seq, &event.Type, &event.ConversationID, &stepID,
			&toolCallID, &parentID, &checkpointID, &payload,
		); err != nil {
			return nil, fmt.Errorf("scan source event: %w", err)
		}
		if event.Seq != int64(len(events)+1) {
			return nil, fmt.Errorf(
				"%w: agent event seq %d after %d",
				ErrSourceChanged, event.Seq, len(events),
			)
		}
		if len(payload) > maxSourceEventPayloadBytes {
			return nil, fmt.Errorf("%w: agent event %d payload too large", ErrInvalid, event.Seq)
		}
		canonical, err := CanonicalJSON(payload)
		if err != nil {
			return nil, fmt.Errorf("agent event %d payload: %w", event.Seq, err)
		}
		var object map[string]any
		if err := json.Unmarshal(canonical, &object); err != nil || object == nil {
			return nil, fmt.Errorf("%w: agent event %d payload", ErrInvalid, event.Seq)
		}
		event.Payload = canonical
		event.StepID = nullableStringSlot(stepID, "agent_event_has_no_step_id")
		event.ToolCallID = nullableStringSlot(toolCallID, "agent_event_has_no_tool_call_id")
		event.ParentID = nullableStringSlot(parentID, "agent_event_has_no_parent_id")
		event.CheckpointID = nullableStringSlot(
			checkpointID, "agent_event_has_no_checkpoint_id",
		)
		if err := validateAgentEventSchema(event); err != nil {
			return nil, err
		}
		ref, err := sourceRef(
			SourceAgentEvent,
			fmt.Sprintf("%s:%d", agentRunID, event.Seq),
			Available(event.Seq),
			eventSchemaSlot(event),
			struct {
				Seq            int64           `json:"seq"`
				Type           string          `json:"type"`
				ConversationID string          `json:"conversation_id"`
				StepID         Slot[string]    `json:"step_id"`
				ToolCallID     Slot[string]    `json:"tool_call_id"`
				ParentID       Slot[string]    `json:"parent_id"`
				CheckpointID   Slot[string]    `json:"checkpoint_id"`
				Payload        json.RawMessage `json:"payload"`
			}{
				event.Seq, event.Type, event.ConversationID, event.StepID,
				event.ToolCallID, event.ParentID, event.CheckpointID, event.Payload,
			},
		)
		if err != nil {
			return nil, err
		}
		event.Ref = ref
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source events: %w", err)
	}
	var lastEventSeq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT last_event_seq FROM agent_runs WHERE id = $1`,
		agentRunID,
	).Scan(&lastEventSeq); err != nil {
		return nil, fmt.Errorf("read source event cursor: %w", err)
	}
	if lastEventSeq != int64(len(events)) {
		return nil, fmt.Errorf(
			"%w: last_event_seq=%d event_count=%d",
			ErrSourceChanged, lastEventSeq, len(events),
		)
	}
	return events, nil
}

type approvalFact struct {
	Seq        int64
	ToolCallID string
}

func approvedGenerations(events []AgentEventSnapshot) (map[int64]approvalFact, error) {
	generationArtifacts := make([]struct {
		seq int64
		id  int64
	}, 0)
	pending := make(map[string]int64)
	approved := make(map[int64]approvalFact)
	for _, event := range events {
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return nil, err
		}
		switch event.Type {
		case "artifact.published":
			if payload["type"] != "dsl_generation" {
				continue
			}
			id, err := strconv.ParseInt(stringValue(payload["id"]), 10, 64)
			if err != nil || id <= 0 {
				return nil, fmt.Errorf(
					"%w: event %d invalid generation artifact",
					ErrSourceChanged, event.Seq,
				)
			}
			generationArtifacts = append(generationArtifacts, struct {
				seq int64
				id  int64
			}{event.Seq, id})
		case "tool.pending":
			if event.ToolCallID.Value == nil || payload["tool"] != "ask_user_question" ||
				!containsApprovalQuestion(payload["questions"]) {
				continue
			}
			pending[*event.ToolCallID.Value] = event.Seq
		case "tool.result":
			if event.ToolCallID.Value == nil || payload["tool"] != "ask_user_question" ||
				!approvalAnswer(payload["answers"]) {
				continue
			}
			pendingSeq, exists := pending[*event.ToolCallID.Value]
			if !exists || pendingSeq >= event.Seq {
				return nil, fmt.Errorf(
					"%w: event %d approval has no matching pending event",
					ErrSourceChanged, event.Seq,
				)
			}
			var generationID int64
			for _, artifact := range generationArtifacts {
				if artifact.seq < pendingSeq {
					generationID = artifact.id
				}
			}
			if generationID == 0 {
				return nil, fmt.Errorf(
					"%w: event %d approval has no preceding generation",
					ErrSourceChanged, event.Seq,
				)
			}
			approved[generationID] = approvalFact{
				Seq: event.Seq, ToolCallID: *event.ToolCallID.Value,
			}
		}
	}
	return approved, nil
}

func readGenerations(
	ctx context.Context,
	tx *sql.Tx,
	projectID int64,
	approved map[int64]approvalFact,
) ([]GenerationSnapshot, error) {
	ids := make([]int64, 0, len(approved))
	for id := range approved {
		ids = append(ids, id)
	}
	ids, _ = sortedPositiveUnique(ids)
	result := make([]GenerationSnapshot, 0, len(ids))
	for _, id := range ids {
		var item GenerationSnapshot
		var raw []byte
		var storedHash, storedVersion sql.NullString
		var retryFrom sql.NullInt64
		var retryReason sql.NullString
		var success bool
		err := tx.QueryRowContext(ctx, `
			SELECT id, project_id, generated_case_json, dsl_sha256,
			       dsl_canonical_version, success, retry_from_generation_id,
			       retry_reason_code
			FROM dsl_generation_runs
			WHERE id = $1`,
			id,
		).Scan(
			&item.ID, &item.ProjectID, &raw, &storedHash, &storedVersion,
			&success, &retryFrom, &retryReason,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: approved generation %d", ErrBrokenLink, id)
		}
		if err != nil {
			return nil, fmt.Errorf("read generation %d: %w", id, err)
		}
		if item.ProjectID != projectID || !success ||
			!storedHash.Valid || !storedVersion.Valid ||
			storedVersion.String != dsl.CanonicalVersion {
			return nil, fmt.Errorf("%w: approved generation %d", ErrBrokenLink, id)
		}
		canonical, _, err := dsl.ValidateCase(raw)
		if err != nil || dsl.SHA256(canonical) != storedHash.String {
			return nil, fmt.Errorf("%w: generation %d canonical DSL", ErrSourceChanged, id)
		}
		item.DSLCanonical = canonical
		item.DSLSHA256 = storedHash.String
		item.CanonicalVersion = storedVersion.String
		item.ApprovedBySeq = approved[id].Seq
		item.ApprovalToolCall = approved[id].ToolCallID
		item.RetryFromID = nullableInt64Slot(retryFrom, "generation_is_not_a_retry")
		item.RetryReasonCode = nullableStringSlot(
			retryReason, "generation_has_no_retry_reason",
		)
		item.Ref, err = sourceRef(
			SourceGeneration, strconv.FormatInt(id, 10),
			Unavailable[int64]("generation_has_no_global_sequence"),
			Available(item.CanonicalVersion),
			struct {
				ID               int64           `json:"id"`
				ProjectID        int64           `json:"project_id"`
				ApprovedBySeq    int64           `json:"approved_by_seq"`
				ApprovalToolCall string          `json:"approval_tool_call_id"`
				DSLCanonical     json.RawMessage `json:"dsl_canonical"`
				DSLSHA256        string          `json:"dsl_sha256"`
				CanonicalVersion string          `json:"canonical_version"`
				RetryFromID      Slot[int64]     `json:"retry_from_generation_id"`
				RetryReasonCode  Slot[string]    `json:"retry_reason_code"`
			}{
				item.ID, item.ProjectID, item.ApprovedBySeq, item.ApprovalToolCall,
				item.DSLCanonical, item.DSLSHA256, item.CanonicalVersion,
				item.RetryFromID, item.RetryReasonCode,
			},
		)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func readBatches(
	ctx context.Context,
	tx *sql.Tx,
	projectID int64,
	agentRunID string,
	generations []GenerationSnapshot,
	events []AgentEventSnapshot,
) ([]BatchSnapshot, error) {
	generationByID := make(map[int64]GenerationSnapshot, len(generations))
	for _, generation := range generations {
		generationByID[generation.ID] = generation
	}
	batchIDs, err := executionBatchArtifactIDs(events)
	if err != nil {
		return nil, err
	}
	result := make([]BatchSnapshot, 0)
	for _, batchID := range batchIDs {
		var item BatchSnapshot
		var idempotencyKey sql.NullString
		err := tx.QueryRowContext(ctx, `
			SELECT id, project_id, status, idempotency_key
			FROM execution_batches
			WHERE id = $1`,
			batchID,
		).Scan(
			&item.ID, &item.ProjectID, &item.Status, &idempotencyKey,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: execution batch %d", ErrBrokenLink, batchID)
		}
		if err != nil {
			return nil, fmt.Errorf("read source batch %d: %w", batchID, err)
		}
		if item.ProjectID != projectID || !idempotencyKey.Valid {
			return nil, fmt.Errorf("%w: batch %d idempotency key", ErrBrokenLink, item.ID)
		}
		prefix := "agent:" + agentRunID + ":generation:"
		suffix, matches := strings.CutPrefix(idempotencyKey.String, prefix)
		generationID, parseErr := strconv.ParseInt(suffix, 10, 64)
		if !matches || parseErr != nil || generationID <= 0 {
			return nil, fmt.Errorf("%w: batch %d generation key", ErrBrokenLink, item.ID)
		}
		generation, approved := generationByID[generationID]
		if !approved {
			return nil, fmt.Errorf(
				"%w: batch %d uses unapproved generation %d",
				ErrBrokenLink, item.ID, generationID,
			)
		}
		item.GenerationID = generationID
		item.Jobs, err = readJobs(ctx, tx, item.ID, generation.DSLSHA256)
		if err != nil {
			return nil, err
		}
		item.Ref, err = sourceRef(
			SourceBatch, strconv.FormatInt(item.ID, 10),
			Unavailable[int64]("batch_has_no_global_sequence"),
			Unavailable[string]("batch_has_no_schema_version"),
			struct {
				ID           int64  `json:"id"`
				ProjectID    int64  `json:"project_id"`
				GenerationID int64  `json:"generation_id"`
				Status       string `json:"status"`
			}{item.ID, item.ProjectID, item.GenerationID, item.Status},
		)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func executionBatchArtifactIDs(events []AgentEventSnapshot) ([]int64, error) {
	ids := make([]int64, 0)
	seen := make(map[int64]bool)
	for _, event := range events {
		if event.Type != "artifact.published" {
			continue
		}
		var payload struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return nil, fmt.Errorf("%w: event %d artifact", ErrSourceChanged, event.Seq)
		}
		if payload.Type != "execution_batch" {
			continue
		}
		id, err := strconv.ParseInt(payload.ID, 10, 64)
		if err != nil || id <= 0 || seen[id] {
			return nil, fmt.Errorf(
				"%w: event %d execution batch artifact",
				ErrSourceChanged, event.Seq,
			)
		}
		seen[id] = true
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, nil
}

func validateSourceLinks(snapshot SourceSnapshot, links sourceIdentityLinks) error {
	generations := make(map[int64]GenerationSnapshot, len(snapshot.Generations))
	batches := make(map[int64]BatchSnapshot, len(snapshot.Batches))
	executions := make(map[int64]ExecutionSnapshot)
	for _, generation := range snapshot.Generations {
		generations[generation.ID] = generation
	}
	for _, batch := range snapshot.Batches {
		batches[batch.ID] = batch
		for _, job := range batch.Jobs {
			for _, execution := range job.Executions {
				executions[execution.ID] = execution
			}
		}
	}
	if links.generationID.Valid {
		generation, exists := generations[links.generationID.Int64]
		if !exists {
			return fmt.Errorf(
				"%w: linked generation %d is absent from approved source facts",
				ErrBrokenLink, links.generationID.Int64,
			)
		}
		if !links.dslSHA256.Valid || links.dslSHA256.String != generation.DSLSHA256 {
			return fmt.Errorf("%w: linked generation SHA", ErrBrokenLink)
		}
	}
	if links.batchID.Valid {
		batch, exists := batches[links.batchID.Int64]
		if !exists {
			return fmt.Errorf(
				"%w: linked batch %d is absent from source artifacts",
				ErrBrokenLink, links.batchID.Int64,
			)
		}
		if !links.generationID.Valid || batch.GenerationID != links.generationID.Int64 {
			return fmt.Errorf("%w: linked batch generation", ErrBrokenLink)
		}
	}
	if links.executionID.Valid {
		execution, exists := executions[links.executionID.Int64]
		if !exists {
			return fmt.Errorf(
				"%w: linked execution %d is absent from source facts",
				ErrBrokenLink, links.executionID.Int64,
			)
		}
		if !links.dslSHA256.Valid || execution.DSLSHA256 != links.dslSHA256.String {
			return fmt.Errorf("%w: linked execution SHA", ErrBrokenLink)
		}
	}
	return nil
}

func readJobs(
	ctx context.Context,
	tx *sql.Tx,
	batchID int64,
	generationSHA string,
) ([]JobSnapshot, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, order_index, status, attempt_count, max_attempts,
		       dsl_sha256, dsl_canonical_version
		FROM execution_jobs
		WHERE batch_id = $1
		ORDER BY order_index, id`,
		batchID,
	)
	if err != nil {
		return nil, fmt.Errorf("read batch %d jobs: %w", batchID, err)
	}
	result := make([]JobSnapshot, 0)
	var expectedOrder int64
	for rows.Next() {
		var item JobSnapshot
		var hash, version sql.NullString
		if err := rows.Scan(
			&item.ID, &item.OrderIndex, &item.Status, &item.AttemptCount,
			&item.MaxAttempts, &hash, &version,
		); err != nil {
			return nil, fmt.Errorf("scan execution job: %w", err)
		}
		if item.OrderIndex != expectedOrder || !hash.Valid ||
			hash.String != generationSHA || !version.Valid ||
			version.String != dsl.CanonicalVersion ||
			item.AttemptCount < 0 || item.AttemptCount > item.MaxAttempts {
			return nil, fmt.Errorf("%w: execution job %d", ErrSourceChanged, item.ID)
		}
		expectedOrder++
		item.DSLSHA256 = hash.String
		item.CanonicalVersion = version.String
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate execution jobs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close execution jobs: %w", err)
	}
	for index := range result {
		item := &result[index]
		item.Executions, err = readExecutions(ctx, tx, item.ID, generationSHA)
		if err != nil {
			return nil, err
		}
		item.Ref, err = sourceRef(
			SourceJob, strconv.FormatInt(item.ID, 10),
			Available(item.OrderIndex),
			Available(item.CanonicalVersion),
			struct {
				ID           int64  `json:"id"`
				OrderIndex   int64  `json:"order_index"`
				Status       string `json:"status"`
				AttemptCount int64  `json:"attempt_count"`
				MaxAttempts  int64  `json:"max_attempts"`
				DSLSHA256    string `json:"dsl_sha256"`
			}{
				item.ID, item.OrderIndex, item.Status, item.AttemptCount,
				item.MaxAttempts, item.DSLSHA256,
			},
		)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func readExecutions(
	ctx context.Context,
	tx *sql.Tx,
	jobID int64,
	generationSHA string,
) ([]ExecutionSnapshot, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, attempt_number, status, dsl_sha256,
		       report_schema_version, report, failure_signal_json
		FROM test_case_runs
		WHERE job_id = $1
		ORDER BY attempt_number, id`,
		jobID,
	)
	if err != nil {
		return nil, fmt.Errorf("read job %d executions: %w", jobID, err)
	}
	defer rows.Close()
	result := make([]ExecutionSnapshot, 0)
	var previousAttempt int64
	for rows.Next() {
		var item ExecutionSnapshot
		var hash, reportVersion sql.NullString
		var report, failure []byte
		if err := rows.Scan(
			&item.ID, &item.Attempt, &item.Status, &hash,
			&reportVersion, &report, &failure,
		); err != nil {
			return nil, fmt.Errorf("scan execution: %w", err)
		}
		if item.Attempt < 1 || item.Attempt <= previousAttempt ||
			!hash.Valid || hash.String != generationSHA {
			return nil, fmt.Errorf("%w: execution %d lineage", ErrSourceChanged, item.ID)
		}
		previousAttempt = item.Attempt
		item.DSLSHA256 = hash.String
		item.ReportSchemaVersion = reportVersion.String
		item.FailureSignal, err = rawObjectSlot(
			failure, maxSourceReportBytes, "execution_has_no_failure_signal",
		)
		if err != nil {
			return nil, fmt.Errorf("execution %d failure signal: %w", item.ID, err)
		}
		if len(report) == 0 {
			if isTerminalExecutionStatus(item.Status) {
				return nil, fmt.Errorf(
					"%w: terminal execution %d has no report",
					ErrSourceChanged, item.ID,
				)
			}
			item.Report = Unavailable[json.RawMessage]("execution_report_not_available")
			item.ReportRef = Unavailable[SourceRef]("execution_report_not_available")
		} else {
			if len(report) > maxSourceReportBytes {
				return nil, fmt.Errorf("%w: execution %d report too large", ErrInvalid, item.ID)
			}
			if item.ReportSchemaVersion != "execution.report.v2" {
				return nil, fmt.Errorf(
					"%w: execution %d report %q",
					ErrUnsupportedSchema, item.ID, item.ReportSchemaVersion,
				)
			}
			canonical, err := validateExecutionReport(report)
			if err != nil {
				return nil, fmt.Errorf("execution %d report: %w", item.ID, err)
			}
			item.Report = Available(json.RawMessage(canonical))
			reportRef, err := sourceRef(
				SourceReport, strconv.FormatInt(item.ID, 10),
				Available(item.Attempt), Available(item.ReportSchemaVersion),
				json.RawMessage(canonical),
			)
			if err != nil {
				return nil, err
			}
			item.ReportRef = Available(reportRef)
		}
		item.Ref, err = sourceRef(
			SourceExecution, strconv.FormatInt(item.ID, 10),
			Available(item.Attempt),
			Available(item.ReportSchemaVersion),
			struct {
				ID                  int64  `json:"id"`
				Attempt             int64  `json:"attempt"`
				Status              string `json:"status"`
				DSLSHA256           string `json:"dsl_sha256"`
				ReportSchemaVersion string `json:"report_schema_version"`
			}{
				item.ID, item.Attempt, item.Status, item.DSLSHA256,
				item.ReportSchemaVersion,
			},
		)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate executions: %w", err)
	}
	return result, nil
}

func validateAgentEventSchema(event AgentEventSnapshot) error {
	var payload struct {
		SchemaVersion  string          `json:"schema_version"`
		Content        json.RawMessage `json:"content"`
		ContentSHA256  string          `json:"content_sha256"`
		ContentBytes   int             `json:"content_bytes"`
		Attempt        int64           `json:"attempt"`
		LogicalCallID  string          `json:"logical_call_id"`
		ToolCallIDs    []string        `json:"tool_call_ids"`
		ToolCallStatus string          `json:"tool_call_status"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("%w: event %d payload", ErrInvalid, event.Seq)
	}
	switch event.Type {
	case "research.llm_call":
		if payload.SchemaVersion != "research.llm_call.v1" ||
			payload.Attempt < 1 || strings.TrimSpace(payload.LogicalCallID) == "" ||
			(payload.ToolCallStatus != "available" &&
				payload.ToolCallStatus != "unavailable") {
			return fmt.Errorf(
				"%w: event %d research.llm_call",
				ErrUnsupportedSchema, event.Seq,
			)
		}
		if event.ToolCallID.Value != nil &&
			!slicesContains(payload.ToolCallIDs, *event.ToolCallID.Value) {
			return fmt.Errorf(
				"%w: event %d tool_call_id mismatch",
				ErrSourceChanged, event.Seq,
			)
		}
	case "tool.result":
		if payload.SchemaVersion == "" {
			return nil
		}
		if payload.SchemaVersion != "agent.tool_result.v1" {
			return fmt.Errorf(
				"%w: event %d tool.result",
				ErrUnsupportedSchema, event.Seq,
			)
		}
		canonical, err := CanonicalJSON(payload.Content)
		if err != nil || payload.ContentBytes <= 0 ||
			!sha256Pattern.MatchString(payload.ContentSHA256) {
			return fmt.Errorf(
				"%w: event %d tool result content metadata",
				ErrSourceChanged, event.Seq,
			)
		}
		sum := sha256.Sum256(canonical)
		if len(canonical) == payload.ContentBytes &&
			hex.EncodeToString(sum[:]) == payload.ContentSHA256 {
			return nil
		}
		// Metadata describes the pre-envelope tool bytes. The source hash binds
		// both that metadata and content normalized by the outer event JSON.
	case "tool.args.delta":
		var object map[string]json.RawMessage
		if err := json.Unmarshal(event.Payload, &object); err != nil {
			return err
		}
		if raw, exists := object["arguments"]; exists {
			if _, err := canonicalToolArguments(raw); err != nil {
				return fmt.Errorf(
					"%w: event %d tool arguments",
					ErrSourceChanged, event.Seq,
				)
			}
		}
	}
	return nil
}

func canonicalToolArguments(raw json.RawMessage) (json.RawMessage, error) {
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err == nil {
		raw = json.RawMessage(encoded)
	}
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(canonical, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%w: tool arguments must be an object", ErrInvalid)
	}
	return canonical, nil
}

func validateExecutionReport(raw []byte) ([]byte, error) {
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		return nil, err
	}
	var report struct {
		Status string `json:"status"`
		Steps  []struct {
			StepIndex int64  `json:"step_index"`
			Action    string `json:"action"`
			Status    string `json:"status"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(canonical, &report); err != nil ||
		strings.TrimSpace(report.Status) == "" {
		return nil, fmt.Errorf("%w: execution report envelope", ErrInvalid)
	}
	for index, step := range report.Steps {
		if step.StepIndex != int64(index) || step.Action == "" || step.Status == "" {
			return nil, fmt.Errorf("%w: execution report step %d", ErrInvalid, index)
		}
	}
	return canonical, nil
}

func sourceRef(
	kind SourceKind,
	id string,
	sequence Slot[int64],
	schemaVersion Slot[string],
	content any,
) (SourceRef, error) {
	hash, err := CanonicalSHA256(content)
	if err != nil {
		return SourceRef{}, err
	}
	ref := SourceRef{
		Kind: kind, ID: id, Sequence: sequence,
		ContentSHA256: hash, SchemaVersion: schemaVersion,
	}
	return ref, ref.NormalizeAndValidate()
}

func sourceSnapshotHash(snapshot SourceSnapshot) (string, error) {
	snapshot.SourceSHA256 = ""
	return CanonicalSHA256(snapshot)
}

func buildSourceCursor(snapshot SourceSnapshot) SourceCursor {
	cursor := SourceCursor{
		SchemaVersion: EventSchemaVersion,
		AgentRunID:    snapshot.AgentRunID,
	}
	if len(snapshot.Events) > 0 {
		cursor.AgentEventSeq = snapshot.Events[len(snapshot.Events)-1].Seq
	}
	for _, generation := range snapshot.Generations {
		cursor.ApprovedGenerationIDs = append(
			cursor.ApprovedGenerationIDs, generation.ID,
		)
	}
	for _, batch := range snapshot.Batches {
		cursor.BatchIDs = append(cursor.BatchIDs, batch.ID)
		for _, job := range batch.Jobs {
			for _, execution := range job.Executions {
				cursor.ExecutionIDs = append(cursor.ExecutionIDs, execution.ID)
			}
		}
	}
	return cursor
}

func eventSchemaSlot(event AgentEventSnapshot) Slot[string] {
	var payload struct {
		SchemaVersion string `json:"schema_version"`
	}
	_ = json.Unmarshal(event.Payload, &payload)
	if payload.SchemaVersion == "" {
		return Unavailable[string]("source_event_has_no_schema_version")
	}
	return Available(payload.SchemaVersion)
}

func nullableStringSlot(value sql.NullString, reason string) Slot[string] {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return Unavailable[string](reason)
	}
	return Available(value.String)
}

func nullableInt64Slot(value sql.NullInt64, reason string) Slot[int64] {
	if !value.Valid {
		return NotApplicable[int64](reason)
	}
	return Available(value.Int64)
}

func rawObjectSlot(
	raw []byte,
	limit int,
	reason string,
) (Slot[json.RawMessage], error) {
	if len(raw) == 0 {
		return Unavailable[json.RawMessage](reason), nil
	}
	if len(raw) > limit {
		return Slot[json.RawMessage]{}, fmt.Errorf("%w: JSON too large", ErrInvalid)
	}
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		return Slot[json.RawMessage]{}, err
	}
	var object map[string]any
	if json.Unmarshal(canonical, &object) != nil || object == nil {
		return Slot[json.RawMessage]{}, fmt.Errorf("%w: JSON object", ErrInvalid)
	}
	return Available(json.RawMessage(canonical)), nil
}

func containsApprovalQuestion(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		question, ok := item.(map[string]any)
		if ok && question["id"] == "approve_dsl" {
			return true
		}
	}
	return false
}

func approvalAnswer(value any) bool {
	answers, ok := value.(map[string]any)
	if !ok {
		return false
	}
	approved, ok := answers["approve_dsl"].(bool)
	return ok && approved
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return ""
	}
}

func slicesContains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func isTerminalExecutionStatus(status string) bool {
	switch status {
	case "passed", "failed", "needs_intervention", "cancelled":
		return true
	default:
		return false
	}
}
