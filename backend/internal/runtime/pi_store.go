package runtime

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

// HarnessAttemptRef is the durable identity of one harness attempt returned to
// the runtime by StartRun.
type HarnessAttemptRef struct {
	ID                string
	RunID             string
	AttemptNumber     int
	ProviderAttemptID string
	Fence             postgres.HarnessFence
	WorkerURL         string
	// CancelRequested is true when the run had a durable cancel request at the
	// time the attempt was claimed. deriveOutcome forces RunCancelled in that
	// case regardless of the harness verdict.
	CancelRequested bool
}

// PiRunOutcome describes the terminal verdict derived from a harness run.
type PiRunOutcome struct {
	Status    domain.RunStatus
	Error     string
	Assistant string
	Uncertain bool
}

// PiTraceStore persists a Pi harness run's trace evidence and terminal state.
type PiTraceStore interface {
	StartRun(ctx context.Context, command event.RunCommand) (HarnessAttemptRef, error)
	AppendHarnessEvent(ctx context.Context, ref HarnessAttemptRef, ev piharness.Event) error
	CompleteHarnessRun(ctx context.Context, ref HarnessAttemptRef, outcome PiRunOutcome) (domain.RunStatus, error)
	CancellationRequested(ctx context.Context, runID string) (bool, error)
}

type PiLeaseStore interface {
	MarkRunStarted(context.Context, HarnessAttemptRef) error
	RenewRunLease(context.Context, HarnessAttemptRef, time.Duration) error
	ReleaseRunLease(context.Context, HarnessAttemptRef, bool) error
	AbandonUnstartedRun(context.Context, HarnessAttemptRef) error
}

// PostgresPiTraceStore adapts the PostgreSQL harness persistence API to the
// runtime PiTraceStore interface. It derives EvalHub logical span and attempt
// identities from the run id, harness attempt id, source sequence, and event
// kind. Pi provider identifiers are stored as evidence, never as primary keys.
type PostgresPiTraceStore struct {
	Store           *postgres.Store
	RuntimeWorkerID string
	LeaseTTL        time.Duration
}

func newProviderAttemptID(runID string) string {
	// A retry of the same Run must not share routing, cancellation or SDK
	// resource identity with an earlier session.
	return stableID("pi-attempt", runID, rand.Text())
}

// StartRun claims the run, allocates a harness attempt, and creates the root
// run span. If the run is already terminal it returns postgres.ErrRunAlreadyTerminal.
func (s PostgresPiTraceStore) StartRun(ctx context.Context, command event.RunCommand) (HarnessAttemptRef, error) {
	if s.Store == nil {
		return HarnessAttemptRef{}, errors.New("postgres pi trace store is not configured")
	}
	if strings.TrimSpace(command.RunID) == "" {
		return HarnessAttemptRef{}, errors.New("pi run command run id is required")
	}
	run, err := s.Store.GetRun(ctx, command.RunID)
	if err != nil {
		return HarnessAttemptRef{}, err
	}
	switch run.Status {
	case domain.RunSucceeded, domain.RunFailed, domain.RunCancelled:
		return HarnessAttemptRef{}, postgres.ErrRunAlreadyTerminal
	}
	if err := command.ValidateManagedExecution(); err != nil {
		return HarnessAttemptRef{}, err
	}
	var persisted struct {
		Command   event.RunCommand `json:"command"`
		Execution *execution.Spec  `json:"execution"`
	}
	if err := json.Unmarshal(run.Specification, &persisted); err != nil {
		return HarnessAttemptRef{}, errors.New("invalid persisted managed command")
	}
	if err := persisted.Command.ValidateManagedExecution(); err != nil {
		return HarnessAttemptRef{}, err
	}
	if !reflect.DeepEqual(persisted.Command, command) || !reflect.DeepEqual(persisted.Execution, command.Execution) ||
		run.ProjectID != command.ProjectID || run.ConversationID != command.ConversationID {
		return HarnessAttemptRef{}, postgres.ErrConflict
	}
	providerAttemptID := newProviderAttemptID(command.RunID)
	claim, err := s.Store.ClaimHarnessLease(ctx, postgres.LeaseClaim{
		RunID: command.RunID, ProjectID: command.ProjectID, EnvironmentID: command.EnvironmentID,
		ProfileGeneration: command.ProfileGeneration, RuntimeWorkerID: s.RuntimeWorkerID,
		ProviderAttemptID: providerAttemptID, RuntimeID: command.Execution.RuntimeID, TTL: s.LeaseTTL,
	})
	if err != nil {
		return HarnessAttemptRef{}, err
	}
	attempt, fence := claim.Attempt, claim.Fence
	ref := HarnessAttemptRef{
		ID:                attempt.ID,
		RunID:             attempt.RunID,
		AttemptNumber:     attempt.AttemptNumber,
		ProviderAttemptID: attempt.ProviderAttemptID,
		CancelRequested:   attempt.CancelRequested,
		Fence:             fence,
		WorkerURL:         claim.WorkerURL,
	}
	now := time.Now().UTC()
	rootSpan := domain.Span{
		ID:        RunSpanID(command.RunID),
		RunID:     command.RunID,
		TraceID:   traceID(command.RunID),
		SpanID:    RunSpanID(command.RunID),
		Kind:      domain.SpanRun,
		Name:      "run",
		Status:    "running",
		StartedAt: now,
	}
	// The initialized child span must carry status "started" for validation,
	// but the root span is inserted first (via Root) with status "running" and
	// the duplicate insert is a no-op, so the persisted root keeps "running".
	startedSpan := rootSpan
	startedSpan.Status = "started"
	started := postgres.TraceAttemptStart{
		ExpectedCheckpoint: command.CheckpointVersion,
		Root:               &rootSpan,
		Span:               startedSpan,
		Attempt: postgres.TraceAttempt{
			ID:            AttemptID(RunSpanID(command.RunID), 1),
			AttemptNumber: 1,
			Status:        "started",
			StartedAt:     now,
		},
	}
	err = s.Store.InitializeHarnessTrace(ctx, fence, started)
	if err != nil {
		return HarnessAttemptRef{}, fmt.Errorf("create pi root span: %w", err)
	}
	return ref, nil
}

// AppendHarnessEvent derives the trace evidence for one harness event and
// persists it atomically.
func (s PostgresPiTraceStore) AppendHarnessEvent(ctx context.Context, ref HarnessAttemptRef, ev piharness.Event) error {
	if s.Store == nil {
		return errors.New("postgres pi trace store is not configured")
	}
	record, err := s.deriveEvent(ctx, ref, ev)
	if err != nil {
		return err
	}
	return s.Store.AppendHarnessEvent(ctx, record)
}

// CompleteHarnessRun records and returns the authoritative terminal run state.
func (s PostgresPiTraceStore) CompleteHarnessRun(ctx context.Context, ref HarnessAttemptRef, outcome PiRunOutcome) (domain.RunStatus, error) {
	if s.Store == nil {
		return "", errors.New("postgres pi trace store is not configured")
	}
	now := time.Now().UTC()
	eventID := stableID("event", ref.RunID, "harness-terminal")
	var errorPayload json.RawMessage
	if outcome.Error != "" {
		errorPayload, _ = json.Marshal(map[string]string{"message": outcome.Error})
	}
	envelope := event.Envelope[json.RawMessage]{
		SchemaVersion: "agenttrace.event.v1",
		EventID:       eventID,
		EventType:     event.RunCompleted,
		AggregateID:   ref.RunID,
		CorrelationID: ref.RunID,
		OccurredAt:    now,
	}
	return s.Store.FinishHarnessAttempt(ctx, postgres.HarnessFinish{
		RunID:     ref.RunID,
		AttemptID: ref.ID,
		Fence:     ref.Fence,
		Status:    outcome.Status,
		Error:     errorPayload,
		Assistant: outcome.Assistant,
		Uncertain: outcome.Uncertain,
		Event:     envelope,
	})
}

// CancellationRequested reports whether the authoritative run state requires
// an active harness process to stop.
func (s PostgresPiTraceStore) CancellationRequested(ctx context.Context, runID string) (bool, error) {
	if s.Store == nil {
		return false, errors.New("postgres pi trace store is not configured")
	}
	return s.Store.CancellationRequested(ctx, runID)
}

func (s PostgresPiTraceStore) deriveEvent(ctx context.Context, ref HarnessAttemptRef, ev piharness.Event) (postgres.HarnessEventRecord, error) {
	occurredAt := ev.TimestampTime()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	eventID := stableID("event", ref.RunID, ref.ID, fmt.Sprintf("%d", ev.Sequence))
	envelope := event.Envelope[json.RawMessage]{
		SchemaVersion: "agenttrace.event.v1",
		EventID:       eventID,
		EventType:     string(ev.Type),
		AggregateID:   ref.RunID,
		CorrelationID: ref.RunID,
		OccurredAt:    occurredAt,
		Payload:       append(json.RawMessage(nil), ev.Payload...),
	}
	record := postgres.HarnessEventRecord{
		RunID:          ref.RunID,
		AttemptID:      ref.ID,
		Fence:          ref.Fence,
		SourceSequence: ev.Sequence,
		SourceType:     string(ev.Type),
		Event:          envelope,
	}

	switch ev.Type {
	case piharness.EventStarted:
		// No additional trace evidence; the run span already exists.
	case piharness.EventMessageCompleted:
		if _, err := decodeModelMessage(ev.Payload); err != nil {
			return record, err
		}
		span, attempt := s.modelSpan(ref, ev)
		record.Span = &span
		record.Attempt = &attempt
	case piharness.EventUsage:
		projected, err := modelUsage(ev.Payload, "")
		if err != nil {
			return record, err
		}
		if projected == nil {
			break
		}
		queryModelID, cumulative, err := sdkQueryUsageIdentity(ev.Payload)
		if err != nil {
			return record, err
		}
		if cumulative {
			// SDK pipeline totals include sidechains and cannot be attributed
			// to the latest individual assistant message.
			attemptID := AttemptID(RunSpanID(ref.RunID), 1)
			projected.ID = stableID("usage", ref.ID, queryModelID)
			record.Usage = projected
			record.Attempt = &postgres.TraceAttempt{ID: attemptID, AttemptNumber: 1}
			break
		}
		modelSpanID, err := s.Store.LatestHarnessModelSpanID(ctx, ref.RunID, ref.ID)
		if err != nil {
			return record, err
		}
		if modelSpanID == "" {
			return record, errors.New("pi.usage has no preceding model span")
		}
		attemptID := AttemptID(modelSpanID, 1)
		projected.ID = stableID("usage", attemptID)
		record.Usage = projected
		// Usage must attach to an attempt; build a minimal attempt reference so
		// the insert links to the existing model span attempt.
		record.Attempt = &postgres.TraceAttempt{ID: attemptID, AttemptNumber: 1}
	case piharness.EventToolStarted:
		span, attempt := s.toolSpan(ref, ev, "started")
		record.Span = &span
		record.Attempt = &attempt
	case piharness.EventToolCompleted:
		span, attempt := s.toolSpan(ref, ev, "completed")
		record.Span = &span
		record.Attempt = &attempt
	case piharness.EventSettled, piharness.EventFailed:
		// Terminal evidence is recorded by CompleteHarnessRun.
	}
	if err := (browserProjection{Store: s.Store}).project(ctx, ref, ev, &record); err != nil {
		return record, err
	}
	return record, nil
}

func (s PostgresPiTraceStore) MarkRunStarted(ctx context.Context, ref HarnessAttemptRef) error {
	return s.Store.MarkHarnessStarted(ctx, ref.Fence)
}

func (s PostgresPiTraceStore) RenewRunLease(ctx context.Context, ref HarnessAttemptRef, ttl time.Duration) error {
	return s.Store.RenewHarnessLease(ctx, ref.Fence, ttl)
}

func (s PostgresPiTraceStore) ReleaseRunLease(ctx context.Context, ref HarnessAttemptRef, acknowledged bool) error {
	return s.Store.ReleaseHarnessLease(ctx, ref.Fence, acknowledged)
}

func (s PostgresPiTraceStore) AbandonUnstartedRun(ctx context.Context, ref HarnessAttemptRef) error {
	return s.Store.AbandonUnstartedHarnessLease(ctx, ref.Fence)
}

func (s PostgresPiTraceStore) modelSpan(ref HarnessAttemptRef, ev piharness.Event) (domain.Span, postgres.TraceAttempt) {
	spanID := stableID("span", ref.RunID, ref.ID, fmt.Sprintf("%d", ev.Sequence), string(ev.Type))
	now := ev.TimestampTime()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	payload, _ := decodeModelMessage(ev.Payload)
	if payload.StartedAt != nil && !payload.StartedAt.IsZero() {
		now = *payload.StartedAt
	}
	span := domain.Span{
		ID:           spanID,
		RunID:        ref.RunID,
		TraceID:      traceID(ref.RunID),
		SpanID:       spanID,
		ParentSpanID: RunSpanID(ref.RunID),
		Kind:         domain.SpanModel,
		Name:         "message",
		Status:       "succeeded",
		StartedAt:    now,
		EndedAt:      payload.EndedAt,
		Input:        append(json.RawMessage(nil), payload.Input...),
		Output:       append(json.RawMessage(nil), ev.Payload...),
	}
	attempt := postgres.TraceAttempt{
		ID:            AttemptID(spanID, 1),
		AttemptNumber: 1,
		Provider:      payload.Provider,
		Status:        "succeeded",
		Request:       append(json.RawMessage(nil), payload.Input...),
		Response:      append(json.RawMessage(nil), ev.Payload...),
		StartedAt:     now,
		EndedAt:       payload.EndedAt,
	}
	if attempt.Provider == "" {
		attempt.Provider = "model"
	}
	return span, attempt
}

func (s PostgresPiTraceStore) toolSpan(ref HarnessAttemptRef, ev piharness.Event, phase string) (domain.Span, postgres.TraceAttempt) {
	var toolCallID, toolName string
	var isError bool
	switch ev.Type {
	case piharness.EventToolStarted:
		var payload piharness.ToolStartedPayload
		_ = json.Unmarshal(ev.Payload, &payload)
		toolCallID = payload.ToolCallID
		toolName = payload.ToolName
	case piharness.EventToolCompleted:
		var payload piharness.ToolCompletedPayload
		_ = json.Unmarshal(ev.Payload, &payload)
		toolCallID = payload.ToolCallID
		toolName = payload.ToolName
		isError = payload.IsError
	}
	// The logical operation span is derived from the tool call id so started and
	// completed events share one span and attempt.
	spanID := stableID("span", ref.RunID, ref.ID, "tool", toolCallID)
	now := ev.TimestampTime()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	status := "started"
	if phase == "completed" {
		status = "succeeded"
		if isError {
			status = "failed"
		}
	}
	evidence, _ := json.Marshal(map[string]string{
		"tool_call_id": toolCallID,
		"tool_name":    toolName,
	})
	span := domain.Span{
		ID:           spanID,
		RunID:        ref.RunID,
		TraceID:      traceID(ref.RunID),
		SpanID:       spanID,
		ParentSpanID: RunSpanID(ref.RunID),
		Kind:         domain.SpanOperation,
		Name:         toolName,
		Status:       status,
		StartedAt:    now,
		Input:        evidence,
	}
	if phase == "completed" {
		span.EndedAt = &now
		span.Output = append(json.RawMessage(nil), ev.Payload...)
	}
	attempt := postgres.TraceAttempt{
		ID:            AttemptID(spanID, 1),
		AttemptNumber: 1,
		OperationID:   stableID("operation", ref.RunID, ref.ID, toolCallID),
		Status:        status,
		Request:       evidence,
		StartedAt:     now,
	}
	if phase == "completed" {
		attempt.EndedAt = &now
		attempt.Response = append(json.RawMessage(nil), ev.Payload...)
		if isError {
			attempt.Error = evidence
		}
	}
	return span, attempt
}

func traceID(runID string) string {
	return stableID("trace", runID)
}
