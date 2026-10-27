package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
)

const finishCaseToolName = "mcp__browser_harness__finish_case"

// HarnessAttemptRef is the durable identity of one harness attempt returned to
// the runtime by StartRun.
type HarnessAttemptRef struct {
	ID                string
	RunID             string
	AttemptNumber     int
	ProviderAttemptID string
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
}

// PiTraceStore persists a Pi harness run's trace evidence and terminal state.
type PiTraceStore interface {
	StartRun(ctx context.Context, command event.RunCommand) (HarnessAttemptRef, error)
	AppendHarnessEvent(ctx context.Context, ref HarnessAttemptRef, ev piharness.Event) error
	CompleteHarnessRun(ctx context.Context, ref HarnessAttemptRef, outcome PiRunOutcome) error
}

// PostgresPiTraceStore adapts the PostgreSQL harness persistence API to the
// runtime PiTraceStore interface. It derives EvalHub logical span and attempt
// identities from the run id, harness attempt id, source sequence, and event
// kind. Pi provider identifiers are stored as evidence, never as primary keys.
type PostgresPiTraceStore struct {
	Store *postgres.Store
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
	providerAttemptID := stableID("pi-attempt", command.RunID)
	attempt, err := s.Store.BeginHarnessAttempt(ctx, command.RunID, providerAttemptID)
	if err != nil {
		return HarnessAttemptRef{}, err
	}
	ref := HarnessAttemptRef{
		ID:                attempt.ID,
		RunID:             attempt.RunID,
		AttemptNumber:     attempt.AttemptNumber,
		ProviderAttemptID: attempt.ProviderAttemptID,
		CancelRequested:   attempt.CancelRequested,
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
	// StartTraceAttempt's child span must carry status "started" for validation,
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
	if err := s.Store.StartTraceAttempt(ctx, started); err != nil {
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

// CompleteHarnessRun records the terminal run state for a harness attempt.
func (s PostgresPiTraceStore) CompleteHarnessRun(ctx context.Context, ref HarnessAttemptRef, outcome PiRunOutcome) error {
	if s.Store == nil {
		return errors.New("postgres pi trace store is not configured")
	}
	now := time.Now().UTC()
	eventID := stableID("event", ref.RunID, "harness-terminal", string(outcome.Status))
	var errorPayload json.RawMessage
	if outcome.Error != "" {
		errorPayload, _ = json.Marshal(map[string]string{"message": outcome.Error})
	}
	envelope := event.Envelope[json.RawMessage]{
		SchemaVersion: "agenttrace.event.v1",
		EventID:       eventID,
		EventType:     "harness.completed",
		AggregateID:   ref.RunID,
		CorrelationID: ref.RunID,
		OccurredAt:    now,
		Payload:       errorPayload,
	}
	return s.Store.FinishHarnessAttempt(ctx, postgres.HarnessFinish{
		RunID:     ref.RunID,
		AttemptID: ref.ID,
		Status:    outcome.Status,
		Error:     errorPayload,
		Assistant: outcome.Assistant,
		Event:     envelope,
	})
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
		SourceSequence: ev.Sequence,
		SourceType:     string(ev.Type),
		Event:          envelope,
	}

	switch ev.Type {
	case piharness.EventStarted:
		// No additional trace evidence; the run span already exists.
	case piharness.EventMessageCompleted:
		span, attempt := s.modelSpan(ref, ev)
		record.Span = &span
		record.Attempt = &attempt
	case piharness.EventUsage:
		modelSpanID, err := s.Store.LatestModelSpanID(ctx, ref.RunID)
		if err != nil {
			return record, err
		}
		if modelSpanID == "" {
			return record, errors.New("pi.usage has no preceding model span")
		}
		attemptID := AttemptID(modelSpanID, 1)
		var payload piharness.UsagePayload
		_ = json.Unmarshal(ev.Payload, &payload)
		record.Usage = &postgres.TraceUsage{
			ID:       stableID("usage", attemptID),
			Provider: "model",
			Usage:    usage.Call(int(payload.InputTokens), int(payload.OutputTokens), 0, 0),
		}
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
	case piharness.EventObservation:
		spanID, err := s.Store.ActiveToolSpanID(ctx, ref.RunID)
		if err != nil {
			return record, err
		}
		if spanID != "" {
			span := domain.Span{
				ID:           spanID,
				RunID:        ref.RunID,
				TraceID:      traceID(ref.RunID),
				SpanID:       spanID,
				ParentSpanID: RunSpanID(ref.RunID),
				Kind:         domain.SpanOperation,
				Name:         "observation",
				Status:       "started",
				StartedAt:    occurredAt,
				Output:       append(json.RawMessage(nil), ev.Payload...),
			}
			record.Span = &span
		}
	case piharness.EventCaseCompleted:
		finishAttempt, err := s.Store.LatestCompletedToolAttempt(
			ctx, ref.RunID, finishCaseToolName, ev.Sequence,
		)
		if err != nil {
			return record, err
		}
		if finishAttempt.SpanID == "" || finishAttempt.SpanAttemptID == "" {
			return record, errors.New(
				"browser.case.completed has no completed finish_case attempt",
			)
		}
		var payload piharness.CaseCompletedPayload
		_ = json.Unmarshal(ev.Payload, &payload)
		verdict := payload.Verdict
		if verdict == "" {
			verdict = "unknown"
		}
		operationID := finishAttempt.OperationID
		if operationID == "" {
			operationID = stableID("operation", ref.RunID, ref.ID, "case")
		}
		operationResult := postgres.TraceOperationResult{
			ID:          stableID("result", ref.ID, fmt.Sprintf("%d", ev.Sequence)),
			OperationID: operationID,
			Status:      verdict,
			Result:      append(json.RawMessage(nil), payload.DryRun...),
			Artifacts:   deriveBrowserStepArtifacts(payload.DryRun),
		}
		record.Operation = &operationResult
		record.Attempt = &postgres.TraceAttempt{
			ID:            finishAttempt.SpanAttemptID,
			AttemptNumber: 1,
		}
	case piharness.EventSettled, piharness.EventFailed:
		// Terminal evidence is recorded by CompleteHarnessRun.
	}
	return record, nil
}

func (s PostgresPiTraceStore) modelSpan(ref HarnessAttemptRef, ev piharness.Event) (domain.Span, postgres.TraceAttempt) {
	spanID := stableID("span", ref.RunID, ref.ID, fmt.Sprintf("%d", ev.Sequence), string(ev.Type))
	now := ev.TimestampTime()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var payload piharness.MessageCompletedPayload
	_ = json.Unmarshal(ev.Payload, &payload)
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
		EndedAt:      &now,
		Output:       append(json.RawMessage(nil), ev.Payload...),
	}
	attempt := postgres.TraceAttempt{
		ID:            AttemptID(spanID, 1),
		AttemptNumber: 1,
		Provider:      "model",
		Status:        "succeeded",
		Request:       json.RawMessage(`{}`),
		Response:      append(json.RawMessage(nil), ev.Payload...),
		StartedAt:     now,
		EndedAt:       &now,
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

func deriveBrowserStepArtifacts(dryRun json.RawMessage) json.RawMessage {
	var decoded struct {
		Steps []struct {
			Evidence struct {
				ScreenshotPath string `json:"screenshot_path"`
			} `json:"evidence"`
		} `json:"steps"`
	}
	_ = json.Unmarshal(dryRun, &decoded)

	paths := make([]string, 0, len(decoded.Steps))
	seen := make(map[string]struct{}, len(decoded.Steps))
	for _, step := range decoded.Steps {
		path := step.Evidence.ScreenshotPath
		if path == "" {
			continue
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	encoded, _ := json.Marshal(map[string][]string{"screenshot_paths": paths})
	return encoded
}

func traceID(runID string) string {
	return stableID("trace", runID)
}
