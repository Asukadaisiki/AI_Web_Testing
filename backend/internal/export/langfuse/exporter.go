// Package langfuse mirrors completed AgentTrace runs to Langfuse.
package langfuse

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	inboxconsumer "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	RunEventsTopic = "agenttrace.run.events.v1"
	DLQTopic       = "agenttrace.run.dlq.v1"

	runEventSchema    = "agenttrace.event.v1"
	completionEvent   = "runtime.completed"
	defaultMaxAttempt = 3
)

// ErrInvalidCanonicalTrace identifies a trace graph that cannot be mirrored
// without changing its canonical topology.
var ErrInvalidCanonicalTrace = errors.New("invalid canonical trace")

type permanentDeliveryError struct {
	cause error
}

func (e permanentDeliveryError) Error() string {
	return e.cause.Error()
}

func (e permanentDeliveryError) Unwrap() error {
	return e.cause
}

func (e permanentDeliveryError) Permanent() bool {
	return true
}

// Repository supplies the canonical data mirrored by the exporter.
type Repository interface {
	GetProject(context.Context, string) (postgres.Project, error)
	GetRun(context.Context, string) (domain.Run, error)
	GetConversation(context.Context, string) (postgres.Conversation, []domain.Message, error)
	ListTrace(context.Context, string) ([]domain.Span, error)
}

// Inbox commits successful deliveries by event ID.
type Inbox interface {
	Handle(context.Context, eventbus.Message, func(context.Context) error) error
}

// DeliveryGate serializes an exporter delivery across process instances.
type DeliveryGate interface {
	Lock(context.Context, string) (func() error, error)
}

// RetryStore owns durable failure counts and one-time DLQ handoff.
type RetryStore interface {
	DLQEventID(string) string
	Increment(context.Context, string, []byte) (RetryIncrement, error)
	Clear(context.Context, string) error
	HandoffDLQ(context.Context, string, DLQHandoff) error
	HasDLQHandoff(context.Context, string, string) (bool, error)
}

// RetryIncrement describes the durable state after one failed delivery.
type RetryIncrement struct {
	Attempts     int
	DLQHandedOff bool
}

// DLQHandoff is one idempotent transfer to the shared outbox topic.
type DLQHandoff struct {
	EventID string
	Topic   string
	Key     string
	Payload []byte
}

// Client accepts transport-independent Langfuse data.
type Client interface {
	Ingest(context.Context, Batch) error
}

// Batch is one completed AgentTrace run prepared for Langfuse ingestion.
type Batch struct {
	SessionID    string        `json:"session_id"`
	Trace        Trace         `json:"trace"`
	Observations []Observation `json:"observations"`
}

// Trace is the Langfuse trace projection for one AgentTrace run.
type Trace struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Name      string         `json:"name"`
	Status    string         `json:"status"`
	Input     any            `json:"input,omitempty"`
	Output    any            `json:"output,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	StartTime time.Time      `json:"start_time"`
	EndTime   *time.Time     `json:"end_time,omitempty"`
}

// Observation is one ordered Langfuse observation projection.
type Observation struct {
	ID                  string         `json:"id"`
	TraceID             string         `json:"trace_id"`
	ParentObservationID string         `json:"parent_observation_id,omitempty"`
	Type                string         `json:"type"`
	Name                string         `json:"name"`
	Status              string         `json:"status"`
	Input               any            `json:"input,omitempty"`
	Output              any            `json:"output,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
	StartTime           time.Time      `json:"start_time"`
	EndTime             *time.Time     `json:"end_time,omitempty"`
}

// Exporter validates completion events and mirrors canonical PostgreSQL data.
type Exporter struct {
	Repository   Repository
	Inbox        Inbox
	RetryStore   RetryStore
	DeliveryGate DeliveryGate
	Client       Client
	SecretFields []string
	MaxAttempts  int
}

// Consume subscribes to the sole supported run event topic.
func (e *Exporter) Consume(ctx context.Context, bus eventbus.EventBus, group string) error {
	if bus == nil {
		return errors.New("Langfuse exporter event bus is required")
	}
	if strings.TrimSpace(group) == "" {
		return errors.New("Langfuse exporter consumer group is required")
	}
	return bus.Consume(ctx, RunEventsTopic, group, e.Handle)
}

// Handle processes a single run event without changing canonical product state.
func (e *Exporter) Handle(ctx context.Context, message eventbus.Message) (handleErr error) {
	eventID := fallbackEventID(message)
	if message.Topic != RunEventsTopic {
		return e.recordFailure(ctx, eventID, message, fmt.Errorf("unsupported run event topic %q", message.Topic))
	}

	envelope, err := decodeEvent(message.Value)
	if err != nil {
		return e.recordFailure(ctx, eventID, message, err)
	}
	eventID = envelope.EventID
	if envelope.EventType != completionEvent {
		return nil
	}
	if e.DeliveryGate != nil {
		release, err := e.DeliveryGate.Lock(ctx, eventID)
		if err != nil {
			return fmt.Errorf("lock Langfuse exporter delivery: %w", err)
		}
		defer func() {
			if err := release(); err != nil && handleErr == nil {
				handleErr = fmt.Errorf("release Langfuse exporter delivery lock: %w", err)
			}
		}()
	}
	if e.RetryStore != nil {
		outboxEventID := e.RetryStore.DLQEventID(eventID)
		handedOff, err := e.RetryStore.HasDLQHandoff(ctx, eventID, outboxEventID)
		if err != nil {
			return fmt.Errorf("check Langfuse export DLQ handoff: %w", err)
		}
		if handedOff {
			return nil
		}
	}
	if e.Inbox == nil {
		return e.recordFailure(ctx, eventID, message, errors.New("Langfuse exporter inbox is required"))
	}

	err = e.Inbox.Handle(ctx, message, func(exportCtx context.Context) error {
		return e.exportCompletion(exportCtx, envelope)
	})
	if err != nil {
		if !IsTransient(err) {
			return e.recordPermanentFailure(ctx, eventID, message, err)
		}
		return e.recordFailure(ctx, eventID, message, err)
	}
	if e.RetryStore != nil {
		if err := e.RetryStore.Clear(ctx, eventID); err != nil {
			return fmt.Errorf("clear Langfuse export retry: %w", err)
		}
	}
	return nil
}

func decodeEvent(raw []byte) (event.Envelope[json.RawMessage], error) {
	var envelope event.Envelope[json.RawMessage]
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return envelope, fmt.Errorf("decode run event: %w", err)
	}
	switch {
	case envelope.SchemaVersion != runEventSchema:
		return envelope, fmt.Errorf("unsupported run event schema %q", envelope.SchemaVersion)
	case strings.TrimSpace(envelope.EventID) == "":
		return envelope, errors.New("run event id is required")
	case strings.TrimSpace(envelope.EventType) == "":
		return envelope, errors.New("run event type is required")
	case strings.TrimSpace(envelope.AggregateID) == "":
		return envelope, errors.New("run event aggregate id is required")
	case envelope.OccurredAt.IsZero():
		return envelope, errors.New("run event occurrence time is required")
	case strings.TrimSpace(envelope.CorrelationID) == "":
		return envelope, errors.New("run event correlation id is required")
	}
	if envelope.EventType == completionEvent {
		var completion struct {
			RunID  string
			Status string
		}
		if err := json.Unmarshal(envelope.Payload, &completion); err != nil {
			return envelope, errors.New("decode run completion payload")
		}
		if completion.RunID != envelope.AggregateID {
			return envelope, errors.New("run completion payload does not match aggregate")
		}
		switch domain.RunStatus(completion.Status) {
		case domain.RunSucceeded, domain.RunFailed, domain.RunCancelled:
		default:
			return envelope, errors.New("run completion status is not terminal")
		}
	}
	return envelope, nil
}

func (e *Exporter) exportCompletion(ctx context.Context, envelope event.Envelope[json.RawMessage]) error {
	if e.Repository == nil {
		return errors.New("Langfuse exporter repository is required")
	}
	if e.Client == nil {
		return errors.New("Langfuse exporter client is required")
	}

	run, err := e.Repository.GetRun(ctx, envelope.AggregateID)
	if err != nil {
		return fmt.Errorf("load canonical run: %w", err)
	}
	if run.ID != envelope.AggregateID || !terminalRun(run.Status) || run.FinishedAt == nil {
		return errors.New("canonical run completion is not visible")
	}
	project, err := e.Repository.GetProject(ctx, run.ProjectID)
	if err != nil {
		return fmt.Errorf("load canonical project: %w", err)
	}
	conversation, messages, err := e.Repository.GetConversation(ctx, run.ConversationID)
	if err != nil {
		return fmt.Errorf("load canonical conversation: %w", err)
	}
	spans, err := e.Repository.ListTrace(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("load canonical trace: %w", err)
	}
	if project.ID != run.ProjectID ||
		conversation.ID != run.ConversationID ||
		conversation.ProjectID != project.ID {
		return errors.New("canonical run relationships are inconsistent")
	}
	if err := validateSpanGraph(run.ID, spans); err != nil {
		return err
	}

	batch := buildBatch(project, conversation, messages, run, spans)
	redactor := newRedactor(e.SecretFields)
	batch.Trace.Input = redactor.apply(batch.Trace.Input)
	batch.Trace.Output = redactor.apply(batch.Trace.Output)
	batch.Trace.Metadata = redactedMap(redactor, batch.Trace.Metadata)
	for index := range batch.Observations {
		batch.Observations[index].Input = redactor.apply(batch.Observations[index].Input)
		batch.Observations[index].Output = redactor.apply(batch.Observations[index].Output)
		batch.Observations[index].Metadata = redactedMap(redactor, batch.Observations[index].Metadata)
	}
	if err := e.Client.Ingest(ctx, batch); err != nil {
		return fmt.Errorf("ingest Langfuse batch: %w", err)
	}
	return nil
}

func validateSpanGraph(runID string, spans []domain.Span) error {
	orderedSpans := append([]domain.Span(nil), spans...)
	sort.Slice(orderedSpans, func(i, j int) bool {
		if orderedSpans[i].SpanID != orderedSpans[j].SpanID {
			return orderedSpans[i].SpanID < orderedSpans[j].SpanID
		}
		return orderedSpans[i].ID < orderedSpans[j].ID
	})
	canonicalIDs := make(map[string]struct{}, len(spans))
	logicalSpans := make(map[string]domain.Span, len(spans))
	for _, span := range orderedSpans {
		switch {
		case strings.TrimSpace(span.ID) == "":
			return invalidCanonicalTrace("span canonical ID is required")
		case strings.TrimSpace(span.SpanID) == "":
			return invalidCanonicalTrace("span logical ID is required")
		case span.RunID != runID:
			return invalidCanonicalTrace("span %q belongs to run %q", span.ID, span.RunID)
		}
		if _, duplicate := canonicalIDs[span.ID]; duplicate {
			return invalidCanonicalTrace("duplicate canonical span ID %q", span.ID)
		}
		if _, duplicate := logicalSpans[span.SpanID]; duplicate {
			return invalidCanonicalTrace("duplicate logical span ID %q", span.SpanID)
		}
		canonicalIDs[span.ID] = struct{}{}
		logicalSpans[span.SpanID] = span
	}

	spanIDs := make([]string, 0, len(logicalSpans))
	for spanID := range logicalSpans {
		spanIDs = append(spanIDs, spanID)
	}
	sort.Strings(spanIDs)
	for _, spanID := range spanIDs {
		span := logicalSpans[spanID]
		if span.ParentSpanID == "" {
			continue
		}
		if span.ParentSpanID == span.SpanID {
			return invalidCanonicalTrace("span %q is its own parent", span.SpanID)
		}
		if _, exists := logicalSpans[span.ParentSpanID]; !exists {
			return invalidCanonicalTrace(
				"span %q references missing parent %q",
				span.SpanID,
				span.ParentSpanID,
			)
		}
	}

	const (
		visiting = 1
		visited  = 2
	)
	state := make(map[string]uint8, len(spans))
	var visit func(string) error
	visit = func(spanID string) error {
		switch state[spanID] {
		case visiting:
			return invalidCanonicalTrace("span parent cycle includes %q", spanID)
		case visited:
			return nil
		}
		state[spanID] = visiting
		if parentID := logicalSpans[spanID].ParentSpanID; parentID != "" {
			if err := visit(parentID); err != nil {
				return err
			}
		}
		state[spanID] = visited
		return nil
	}
	for _, spanID := range spanIDs {
		if err := visit(spanID); err != nil {
			return err
		}
	}
	return nil
}

func invalidCanonicalTrace(format string, values ...any) error {
	return permanentDeliveryError{
		cause: fmt.Errorf("%w: %s", ErrInvalidCanonicalTrace, fmt.Sprintf(format, values...)),
	}
}

func buildBatch(
	project postgres.Project,
	conversation postgres.Conversation,
	messages []domain.Message,
	run domain.Run,
	spans []domain.Span,
) Batch {
	sessionID := remoteID("session", conversation.ID)
	traceID := remoteID("trace", run.ID)
	name := strings.TrimSpace(conversation.Title)
	if name == "" {
		name = run.ID
	}
	inputMessages := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		inputMessages = append(inputMessages, map[string]any{
			"id":       message.ID,
			"ordinal":  message.Ordinal,
			"role":     message.Role,
			"content":  message.Content,
			"metadata": decodeJSON(message.Metadata),
		})
	}
	trace := Trace{
		ID:        traceID,
		SessionID: sessionID,
		Name:      name,
		Status:    string(run.Status),
		Input:     inputMessages,
		Metadata: map[string]any{
			"agenttrace_project_id":      project.ID,
			"agenttrace_project_name":    project.Name,
			"agenttrace_project":         decodeJSON(project.Metadata),
			"agenttrace_conversation_id": conversation.ID,
			"agenttrace_conversation":    decodeJSON(conversation.Metadata),
			"agenttrace_run_id":          run.ID,
		},
		StartTime: run.CreatedAt,
		EndTime:   run.FinishedAt,
	}
	if run.StartedAt != nil {
		trace.StartTime = *run.StartedAt
	}

	spanIDs := make(map[string]string, len(spans))
	for _, span := range spans {
		spanIDs[span.SpanID] = remoteID("observation", span.ID)
	}
	observations := make([]Observation, 0, len(spans))
	for _, span := range spans {
		observation := Observation{
			ID:        spanIDs[span.SpanID],
			TraceID:   traceID,
			Type:      observationType(span.Kind),
			Name:      span.Name,
			Status:    span.Status,
			Input:     decodeJSON(span.Input),
			Output:    decodeJSON(span.Output),
			StartTime: span.StartedAt,
			EndTime:   span.EndedAt,
			Metadata: map[string]any{
				"agenttrace_span_id":         span.ID,
				"agenttrace_logical_span_id": span.SpanID,
				"agenttrace_kind":            string(span.Kind),
			},
		}
		if span.ParentSpanID != "" {
			observation.ParentObservationID = spanIDs[span.ParentSpanID]
		}
		observations = append(observations, observation)
		if span.Output != nil {
			trace.Output = decodeJSON(span.Output)
		}
	}
	return Batch{SessionID: sessionID, Trace: trace, Observations: observations}
}

func observationType(kind domain.SpanKind) string {
	if kind == domain.SpanModel {
		return "generation"
	}
	return "span"
}

func terminalRun(status domain.RunStatus) bool {
	return status == domain.RunSucceeded || status == domain.RunFailed || status == domain.RunCancelled
}

func decodeJSON(raw json.RawMessage) any {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

type redactor struct {
	fields map[string]struct{}
}

func newRedactor(fields []string) redactor {
	result := redactor{fields: make(map[string]struct{}, len(fields))}
	for _, field := range fields {
		field = strings.ToLower(strings.TrimSpace(field))
		if field != "" {
			result.fields[field] = struct{}{}
		}
	}
	return result
}

func (r redactor) apply(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			if _, secret := r.fields[strings.ToLower(key)]; secret {
				result[key] = "[REDACTED]"
				continue
			}
			result[key] = r.apply(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = r.apply(item)
		}
		return result
	default:
		return value
	}
}

func redactedMap(redactor redactor, value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	redacted, _ := redactor.apply(value).(map[string]any)
	return redacted
}

func (e *Exporter) recordFailure(
	ctx context.Context,
	eventID string,
	message eventbus.Message,
	cause error,
) error {
	return e.recordFailureWithPolicy(ctx, eventID, message, cause, false)
}

func (e *Exporter) recordPermanentFailure(
	ctx context.Context,
	eventID string,
	message eventbus.Message,
	cause error,
) error {
	return e.recordFailureWithPolicy(ctx, eventID, message, cause, true)
}

func (e *Exporter) recordFailureWithPolicy(
	ctx context.Context,
	eventID string,
	message eventbus.Message,
	cause error,
	permanent bool,
) error {
	if e.RetryStore == nil {
		return cause
	}
	increment, err := e.RetryStore.Increment(ctx, eventID, message.Value)
	if err != nil {
		return fmt.Errorf("record Langfuse export failure: %w", err)
	}
	if increment.DLQHandedOff {
		return nil
	}
	if !permanent && increment.Attempts < e.maxAttempts() {
		return cause
	}
	original := any(json.RawMessage(message.Value))
	if !json.Valid(message.Value) {
		original = string(message.Value)
	}
	payload, err := json.Marshal(struct {
		Original any    `json:"original"`
		Error    string `json:"error"`
	}{
		Original: original,
		Error:    "Langfuse export failed",
	})
	if err != nil {
		return errors.New("marshal Langfuse export DLQ payload")
	}
	handoff := DLQHandoff{
		EventID: e.RetryStore.DLQEventID(eventID),
		Topic:   DLQTopic,
		Key:     message.Key,
		Payload: payload,
	}
	if err := e.RetryStore.HandoffDLQ(ctx, eventID, handoff); err != nil {
		return fmt.Errorf("handoff Langfuse export DLQ: %w", err)
	}
	return nil
}

func (e *Exporter) maxAttempts() int {
	if e.MaxAttempts < 1 {
		return defaultMaxAttempt
	}
	return e.MaxAttempts
}

func fallbackEventID(message eventbus.Message) string {
	var envelope struct {
		EventID string `json:"event_id"`
	}
	if json.Unmarshal(message.Value, &envelope) == nil && strings.TrimSpace(envelope.EventID) != "" {
		return strings.TrimSpace(envelope.EventID)
	}
	return stableID("langfuse-retry", message.Topic, message.Key, string(message.Value))
}

func remoteID(kind, sourceID string) string {
	return "agenttrace-" + kind + "-" + sourceID
}

func stableID(prefix string, values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(value))
	}
	return prefix + "_" + hex.EncodeToString(hash.Sum(nil)[:16])
}

// PostgresInbox adapts the shared PostgreSQL inbox to the exporter contract.
type PostgresInbox struct {
	Deduplicator *inboxconsumer.Deduplicator
}

// Handle commits the event ID only after the export succeeds.
func (i PostgresInbox) Handle(
	ctx context.Context,
	message eventbus.Message,
	handle func(context.Context) error,
) error {
	if i.Deduplicator == nil {
		return errors.New("Langfuse exporter inbox is required")
	}
	return i.Deduplicator.Handle(
		ctx,
		message,
		func(ctx context.Context, _ inboxconsumer.Tx, _ eventbus.Message) error {
			return handle(ctx)
		},
	)
}

// PostgresDeliveryGate holds one session advisory lock across the complete
// exporter delivery. Its distinct namespace keeps the gate ordered before the
// retry store's transaction advisory lock.
type PostgresDeliveryGate struct {
	Pool     *pgxpool.Pool
	Consumer string
}

func (g PostgresDeliveryGate) Lock(ctx context.Context, eventID string) (func() error, error) {
	if g.Pool == nil {
		return nil, errors.New("Langfuse exporter delivery pool is required")
	}
	if strings.TrimSpace(g.Consumer) == "" || strings.TrimSpace(eventID) == "" {
		return nil, errors.New("Langfuse exporter delivery consumer and event id are required")
	}
	conn, err := g.Pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire Langfuse exporter delivery connection: %w", err)
	}
	key := "langfuse-delivery:" + g.Consumer + ":" + eventID
	if _, err := conn.Exec(ctx, `
		SELECT pg_advisory_lock(hashtextextended($1, 0::bigint))`, key); err != nil {
		conn.Release()
		return nil, fmt.Errorf("acquire Langfuse exporter delivery lock: %w", err)
	}

	released := false
	return func() error {
		if released {
			return nil
		}
		released = true
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var unlocked bool
		err := conn.QueryRow(releaseCtx, `
			SELECT pg_advisory_unlock(hashtextextended($1, 0::bigint))`, key).Scan(&unlocked)
		if err == nil && !unlocked {
			err = errors.New("Langfuse exporter delivery lock was not held")
		}
		if err != nil {
			_ = conn.Conn().Close(releaseCtx)
			conn.Release()
			return fmt.Errorf("release Langfuse exporter delivery lock: %w", err)
		}
		conn.Release()
		return nil
	}, nil
}

// PostgresRetryStore adapts the shared durable retry/outbox primitives.
type PostgresRetryStore struct {
	Store    *postgres.Store
	Consumer string
}

func (s PostgresRetryStore) DLQEventID(eventID string) string {
	return stableID("langfuse-dlq", s.Consumer, eventID)
}

func (s PostgresRetryStore) Increment(ctx context.Context, eventID string, payload []byte) (RetryIncrement, error) {
	increment, err := s.Store.IncrementConsumerRetryUnlessDLQHandoff(
		ctx,
		s.Consumer,
		eventID,
		s.DLQEventID(eventID),
		payload,
	)
	if err != nil {
		return RetryIncrement{}, err
	}
	return RetryIncrement{
		Attempts:     increment.Attempts,
		DLQHandedOff: increment.DLQHandedOff,
	}, nil
}

func (s PostgresRetryStore) Clear(ctx context.Context, eventID string) error {
	return s.Store.ClearConsumerRetry(ctx, s.Consumer, eventID)
}

func (s PostgresRetryStore) HandoffDLQ(ctx context.Context, eventID string, handoff DLQHandoff) error {
	return s.Store.HandoffConsumerRetryToOutbox(
		ctx,
		s.Consumer,
		eventID,
		handoff.EventID,
		handoff.Topic,
		handoff.Key,
		handoff.Payload,
	)
}

func (s PostgresRetryStore) HasDLQHandoff(
	ctx context.Context,
	_ string,
	outboxEventID string,
) (bool, error) {
	return s.Store.HasConsumerRetryDLQHandoff(ctx, outboxEventID)
}
