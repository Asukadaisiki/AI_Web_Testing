package langfuse

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

func TestExporterProjectFilterAcknowledgesForeignRunWithoutIngestion(t *testing.T) {
	repository := canonicalRepository()
	before := repository.run
	client := &recordingClient{}
	exporter := newTestExporter(repository, client)
	exporter.ExportProjectID = "other-project"
	message := completionMessage(t, "evt-foreign-project")
	if err := exporter.Handle(context.Background(), message); err != nil {
		t.Fatalf("filtered completion was not acknowledged: %v", err)
	}
	if len(client.batches) != 0 || repository.loads != 1 {
		t.Fatalf("foreign completion ingested=%d canonical loads=%d, want 0 and 1", len(client.batches), repository.loads)
	}
	if !exporter.Inbox.(*memoryInbox).seen["evt-foreign-project"] {
		t.Fatal("filtered completion did not commit inbox acknowledgement")
	}
	retries := exporter.RetryStore.(*memoryRetryStore)
	if len(retries.attempts) != 0 || retries.handoffCount != 0 {
		t.Fatal("filtered completion created retries or DLQ")
	}
	if !reflect.DeepEqual(repository.run, before) {
		t.Fatal("project filter changed canonical Run")
	}
	if err := exporter.Handle(context.Background(), message); err != nil || repository.loads != 1 {
		t.Fatal("filtered completion replay was not deduplicated")
	}
}

func TestExporterProjectFilterKeepsMatchingAndLegacyCompletion(t *testing.T) {
	for _, project := range []string{"", "project-1"} {
		t.Run(project, func(t *testing.T) {
			client := &recordingClient{}
			exporter := newTestExporter(canonicalRepository(), client)
			exporter.ExportProjectID = project
			if err := exporter.Handle(context.Background(), completionMessage(t, "evt-matching-project")); err != nil {
				t.Fatal(err)
			}
			if len(client.batches) != 1 {
				t.Fatalf("matching/legacy ingestion = %d, want 1", len(client.batches))
			}
		})
	}
}

func TestExporterLegacyMissingClientDoesNotReadCanonicalRun(t *testing.T) {
	repository := canonicalRepository()
	exporter := newTestExporter(repository, nil)
	if err := exporter.Handle(context.Background(), completionMessage(t, "evt-missing-client")); err == nil {
		t.Fatal("missing client accepted")
	}
	if repository.loads != 0 {
		t.Fatalf("legacy missing-client path loaded canonical data %d times, want 0", repository.loads)
	}
}

func TestExporterProjectFilterPrecedesCompletionAndClientValidation(t *testing.T) {
	repository := canonicalRepository()
	repository.run.Status = domain.RunRunning
	repository.run.FinishedAt = nil
	before := repository.run
	exporter := newTestExporter(repository, nil)
	exporter.ExportProjectID = "other-project"
	if err := exporter.Handle(context.Background(), completionMessage(t, "evt-foreign-incomplete")); err != nil {
		t.Fatalf("foreign completion retried before project filtering: %v", err)
	}
	if repository.loads != 1 || !reflect.DeepEqual(repository.run, before) {
		t.Fatal("foreign filter read downstream data or changed canonical facts")
	}
	if !exporter.Inbox.(*memoryInbox).seen["evt-foreign-incomplete"] {
		t.Fatal("foreign incomplete completion not acknowledged")
	}
}

func TestExporterRejectsWrongTopicAndSchema(t *testing.T) {
	repository := canonicalRepository()
	exporter := newTestExporter(repository, &recordingClient{})

	wrongTopic := completionMessage(t, "evt-wrong-topic")
	wrongTopic.Topic = "agenttrace.run.events.v2"
	if err := exporter.Handle(context.Background(), wrongTopic); err == nil {
		t.Fatal("accepted unsupported event topic")
	}

	wrongSchema := completionMessage(t, "evt-wrong-schema")
	var envelope map[string]any
	if err := json.Unmarshal(wrongSchema.Value, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["schema_version"] = "agenttrace.other-event.v1"
	wrongSchema.Value = mustJSON(t, envelope)
	if err := exporter.Handle(context.Background(), wrongSchema); err == nil {
		t.Fatal("accepted unsupported event schema")
	}
	if repository.loads != 0 {
		t.Fatalf("canonical repository loads = %d, want 0", repository.loads)
	}
}

func TestExporterIgnoresNonCompletionEventsWithAcceptedSchema(t *testing.T) {
	repository := canonicalRepository()
	client := &recordingClient{}
	exporter := newTestExporter(repository, client)
	message := completionMessage(t, "evt-step")
	var envelope event.Envelope[json.RawMessage]
	if err := json.Unmarshal(message.Value, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.EventType = "runtime.step.completed"
	message.Value = mustJSON(t, envelope)

	if err := exporter.Handle(context.Background(), message); err != nil {
		t.Fatalf("handle non-completion event: %v", err)
	}
	if repository.loads != 0 || len(client.batches) != 0 {
		t.Fatalf("non-completion event loaded=%d exported=%d", repository.loads, len(client.batches))
	}
}

func TestExporterRejectsMalformedCompletionPayloadBeforeCanonicalRead(t *testing.T) {
	repository := canonicalRepository()
	exporter := newTestExporter(repository, &recordingClient{})
	message := completionMessage(t, "evt-malformed-completion")
	var envelope event.Envelope[json.RawMessage]
	if err := json.Unmarshal(message.Value, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Payload = json.RawMessage(`{"Status":"succeeded"}`)
	message.Value = mustJSON(t, envelope)

	if err := exporter.Handle(context.Background(), message); err == nil {
		t.Fatal("accepted completion payload without a run ID")
	}
	if repository.loads != 0 {
		t.Fatalf("canonical repository loads = %d, want 0", repository.loads)
	}
}

func TestExporterRedactsConfiguredFieldsRecursively(t *testing.T) {
	repository := canonicalRepository()
	repository.spans[0].Input = json.RawMessage(`{
		"authorization":"top-secret",
		"nested":{"password":"nested-secret"},
		"items":[{"api_key":"array-secret"},{"safe":"visible"}]
	}`)
	repository.spans[0].Output = json.RawMessage(`[
		{"token":"output-secret"},
		{"nested":[{"password":"deep-secret"}]}
	]`)
	client := &recordingClient{}
	exporter := newTestExporter(repository, client)
	exporter.SecretFields = []string{"authorization", "password", "api_key", "token"}

	if err := exporter.Handle(context.Background(), completionMessage(t, "evt-redact")); err != nil {
		t.Fatalf("export completion: %v", err)
	}
	raw := string(mustJSON(t, client.batches[0]))
	for _, secret := range []string{
		"top-secret", "nested-secret", "array-secret", "output-secret", "deep-secret",
	} {
		if strings.Contains(raw, secret) {
			t.Fatalf("export payload leaked configured secret value %q: %s", secret, raw)
		}
	}
	if got := strings.Count(raw, "[REDACTED]"); got != 5 {
		t.Fatalf("redaction markers = %d, want 5: %s", got, raw)
	}
	if !strings.Contains(raw, `"safe":"visible"`) {
		t.Fatalf("redaction removed safe data: %s", raw)
	}
}

func TestExporterLoadsCanonicalCompletionAndPreservesOrderedSpans(t *testing.T) {
	repository := canonicalRepository()
	client := &recordingClient{}
	exporter := newTestExporter(repository, client)

	if err := exporter.Handle(context.Background(), completionMessage(t, "evt-ordered")); err != nil {
		t.Fatalf("export completion: %v", err)
	}
	if repository.loads != 4 {
		t.Fatalf("canonical repository loads = %d, want project, run, conversation, trace", repository.loads)
	}
	if len(client.batches) != 1 {
		t.Fatalf("export batches = %d, want 1", len(client.batches))
	}
	batch := client.batches[0]
	if batch.SessionID != "agenttrace-session-conv-1" {
		t.Fatalf("session ID = %q", batch.SessionID)
	}
	if batch.Trace.ID != "agenttrace-trace-run-1" {
		t.Fatalf("trace ID = %q", batch.Trace.ID)
	}
	if got := observationNames(batch.Observations); strings.Join(got, ",") != "first,second,third" {
		t.Fatalf("observation order = %v, want first,second,third", got)
	}
	if batch.Observations[1].ParentObservationID != "agenttrace-observation-span-1" {
		t.Fatalf("second parent = %q", batch.Observations[1].ParentObservationID)
	}
	if batch.Trace.Status != "succeeded" || batch.Trace.EndTime == nil {
		t.Fatalf("trace completion = status %q end %v", batch.Trace.Status, batch.Trace.EndTime)
	}
}

func TestExporterRejectsInvalidCanonicalSpanGraphBeforeHTTP(t *testing.T) {
	for _, test := range []struct {
		name    string
		corrupt func([]domain.Span)
	}{
		{
			name: "duplicate canonical ID",
			corrupt: func(spans []domain.Span) {
				spans[1].ID = spans[0].ID
			},
		},
		{
			name: "duplicate logical ID",
			corrupt: func(spans []domain.Span) {
				spans[1].SpanID = spans[0].SpanID
			},
		},
		{
			name: "missing parent",
			corrupt: func(spans []domain.Span) {
				spans[1].ParentSpanID = "logical-missing"
			},
		},
		{
			name: "self parent",
			corrupt: func(spans []domain.Span) {
				spans[1].ParentSpanID = spans[1].SpanID
			},
		},
		{
			name: "cycle",
			corrupt: func(spans []domain.Span) {
				spans[0].ParentSpanID = spans[1].SpanID
				spans[1].ParentSpanID = spans[0].SpanID
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusAccepted)
			}))
			defer server.Close()

			repository := canonicalRepository()
			test.corrupt(repository.spans)
			retries := newMemoryRetryStore()
			exporter := newTestExporter(
				repository,
				newHTTPTestClient(t, server.URL, server.Client()),
			)
			exporter.RetryStore = retries

			if err := exporter.Handle(
				context.Background(),
				completionMessage(t, "evt-invalid-span-"+test.name),
			); err != nil {
				t.Fatalf("invalid trace DLQ handoff: %v", err)
			}
			if requests.Load() != 0 {
				t.Fatalf("HTTP requests = %d, want 0", requests.Load())
			}
			if retries.handoffCount != 1 {
				t.Fatalf("DLQ handoffs = %d, want 1", retries.handoffCount)
			}
		})
	}
}

func TestSpanGraphCycleErrorUsesSortedLogicalID(t *testing.T) {
	spans := canonicalRepository().spans[:2]
	spans[0].ParentSpanID = spans[1].SpanID
	spans[1].ParentSpanID = spans[0].SpanID

	for attempt := 0; attempt < 32; attempt++ {
		err := validateSpanGraph("run-1", spans)
		if err == nil {
			t.Fatal("accepted cyclic span graph")
		}
		if got, want := err.Error(), `invalid canonical trace: span parent cycle includes "logical-1"`; got != want {
			t.Fatalf("cycle error = %q, want %q", got, want)
		}
	}
}

func TestPermanentFailureDLQPayloadDoesNotDependOnAttempts(t *testing.T) {
	message := completionMessage(t, "evt-permanent-payload")
	payloadForAttempts := func(attempts int) []byte {
		retries := newMemoryRetryStore()
		retries.attempts["evt-permanent-payload"] = attempts
		exporter := newTestExporter(canonicalRepository(), &recordingClient{})
		exporter.RetryStore = retries

		if err := exporter.recordPermanentFailure(
			context.Background(),
			"evt-permanent-payload",
			message,
			errors.New("permanent failure"),
		); err != nil {
			t.Fatalf("record permanent failure after %d attempts: %v", attempts, err)
		}
		return retries.handoffs["evt-permanent-payload"].Payload
	}

	first := payloadForAttempts(0)
	retried := payloadForAttempts(7)
	if string(first) != string(retried) {
		t.Fatalf("permanent DLQ payload changed with attempts:\nfirst: %s\nretried: %s", first, retried)
	}
	if strings.Contains(string(first), `"attempt"`) {
		t.Fatalf("permanent DLQ payload contains attempt state: %s", first)
	}
}

func TestExporterDuplicateRetryUsesStableLangfuseIdentities(t *testing.T) {
	var (
		mu       sync.Mutex
		requests [][]byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/public/ingestion" {
			t.Errorf("ingestion path = %q", r.URL.Path)
		}
		publicKey, secretKey, ok := r.BasicAuth()
		if !ok || publicKey != "public-test" || secretKey != "secret-test" {
			t.Errorf("basic auth = (%q, %q, %v)", publicKey, secretKey, ok)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		mu.Lock()
		requests = append(requests, raw)
		attempt := len(requests)
		mu.Unlock()
		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPConfig{
		BaseURL:    server.URL,
		PublicKey:  "public-test",
		SecretKey:  "secret-test",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("create HTTP client: %v", err)
	}
	exporter := newTestExporter(canonicalRepository(), client)
	message := completionMessage(t, "evt-duplicate")

	if err := exporter.Handle(context.Background(), message); err == nil {
		t.Fatal("transient provider failure returned nil")
	}
	if err := exporter.Handle(context.Background(), message); err != nil {
		t.Fatalf("retry completion: %v", err)
	}
	if err := exporter.Handle(context.Background(), message); err != nil {
		t.Fatalf("deduplicated replay: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("HTTP requests = %d, want failed request and one retry", len(requests))
	}
	if string(requests[0]) != string(requests[1]) {
		t.Fatalf("duplicate ingestion identities changed:\nfirst: %s\nsecond: %s", requests[0], requests[1])
	}
	if strings.Contains(string(requests[0]), "secret-test") {
		t.Fatal("ingestion payload included Langfuse secret key")
	}
}

func TestExporterPermanentHTTPFailureHandsOffImmediatelyAndSuppressesReplay(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(status)
			}))
			defer server.Close()

			retries := newMemoryRetryStore()
			exporter := newTestExporter(
				canonicalRepository(),
				newHTTPTestClient(t, server.URL, server.Client()),
			)
			exporter.RetryStore = retries
			message := completionMessage(t, "evt-permanent-"+http.StatusText(status))

			if err := exporter.Handle(context.Background(), message); err != nil {
				t.Fatalf("permanent failure handoff: %v", err)
			}
			if retries.handoffCount != 1 {
				t.Fatalf("DLQ handoffs = %d, want 1", retries.handoffCount)
			}
			if requests.Load() != 1 {
				t.Fatalf("HTTP requests = %d, want 1", requests.Load())
			}

			if err := exporter.Handle(context.Background(), message); err != nil {
				t.Fatalf("replay after permanent handoff: %v", err)
			}
			if retries.handoffCount != 1 || requests.Load() != 1 {
				t.Fatalf(
					"replay state = %d handoffs, %d requests; want 1, 1",
					retries.handoffCount,
					requests.Load(),
				)
			}
		})
	}
}

func TestExporterTransientHTTPFailuresRemainRetryable(t *testing.T) {
	for _, status := range []int{
		http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusServiceUnavailable,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()

			retries := newMemoryRetryStore()
			exporter := newTestExporter(
				canonicalRepository(),
				newHTTPTestClient(t, server.URL, server.Client()),
			)
			exporter.RetryStore = retries
			eventID := "evt-transient-" + http.StatusText(status)

			if err := exporter.Handle(context.Background(), completionMessage(t, eventID)); err == nil {
				t.Fatal("transient HTTP failure returned nil")
			}
			if retries.handoffCount != 0 {
				t.Fatalf("DLQ handoffs = %d, want 0", retries.handoffCount)
			}
			if retries.attempts[eventID] != 1 {
				t.Fatalf("durable attempts = %d, want 1", retries.attempts[eventID])
			}
		})
	}
}

func TestExporterNetworkFailureRemainsRetryable(t *testing.T) {
	var requests atomic.Int64
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("connection reset")
	})}
	retries := newMemoryRetryStore()
	exporter := newTestExporter(
		canonicalRepository(),
		newHTTPTestClient(t, "https://langfuse.example", httpClient),
	)
	exporter.RetryStore = retries
	const eventID = "evt-network-failure"

	if err := exporter.Handle(context.Background(), completionMessage(t, eventID)); err == nil {
		t.Fatal("network failure returned nil")
	}
	if requests.Load() != 1 {
		t.Fatalf("HTTP requests = %d, want 1", requests.Load())
	}
	if retries.handoffCount != 0 || retries.attempts[eventID] != 1 {
		t.Fatalf(
			"network retry state = %d handoffs, %d attempts; want 0, 1",
			retries.handoffCount,
			retries.attempts[eventID],
		)
	}
}

func TestExporterTransientFailureDoesNotMutateCanonicalRun(t *testing.T) {
	repository := canonicalRepository()
	before := repository.run
	exporter := newTestExporter(repository, &recordingClient{err: errors.New("provider unavailable")})

	err := exporter.Handle(context.Background(), completionMessage(t, "evt-transient"))
	if err == nil {
		t.Fatal("transient provider failure returned nil")
	}
	if !reflect.DeepEqual(repository.run, before) {
		t.Fatalf("canonical run changed from %#v to %#v", before, repository.run)
	}
	if repository.run.Status != domain.RunSucceeded {
		t.Fatalf("canonical run status = %q, want succeeded", repository.run.Status)
	}
}

func TestExporterDurablyHandsOffOnceAndReplaySkipsExport(t *testing.T) {
	repository := canonicalRepository()
	client := &recordingClient{err: errors.New("provider unavailable")}
	retries := newMemoryRetryStore()
	exporter := newTestExporter(repository, client)
	exporter.RetryStore = retries
	exporter.MaxAttempts = 2
	message := completionMessage(t, "evt-dlq")

	if err := exporter.Handle(context.Background(), message); err == nil {
		t.Fatal("first provider failure returned nil")
	}
	if err := exporter.Handle(context.Background(), message); err != nil {
		t.Fatalf("durable DLQ handoff: %v", err)
	}
	callsAtHandoff := len(client.batches)
	if retries.handoffCount != 1 {
		t.Fatalf("DLQ handoffs = %d, want 1", retries.handoffCount)
	}
	handoff := retries.handoffs["evt-dlq"]
	if handoff.Topic != "agenttrace.run.dlq.v1" {
		t.Fatalf("DLQ topic = %q", handoff.Topic)
	}

	if err := exporter.Handle(context.Background(), message); err != nil {
		t.Fatalf("replay after DLQ handoff: %v", err)
	}
	if retries.handoffCount != 1 {
		t.Fatalf("DLQ handoffs after replay = %d, want 1", retries.handoffCount)
	}
	if len(client.batches) != callsAtHandoff {
		t.Fatalf("exports after handoff = %d, want %d", len(client.batches), callsAtHandoff)
	}
}

func newTestExporter(repository *memoryRepository, client Client) *Exporter {
	return &Exporter{
		Repository:  repository,
		Inbox:       newMemoryInbox(),
		RetryStore:  newMemoryRetryStore(),
		Client:      client,
		MaxAttempts: 3,
	}
}

func canonicalRepository() *memoryRepository {
	start := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Second)
	return &memoryRepository{
		project: postgres.Project{
			ID: "project-1", Name: "Checkout evaluation",
			Metadata: json.RawMessage(`{"environment":"fixture"}`),
		},
		conversation: postgres.Conversation{
			ID: "conv-1", ProjectID: "project-1", Title: "Evaluate checkout",
			Metadata: json.RawMessage(`{"suite":"smoke"}`),
		},
		messages: []domain.Message{
			{ID: "message-1", ConversationID: "conv-1", RunID: "run-1", Ordinal: 1, Role: "user", Content: "test checkout"},
		},
		run: domain.Run{
			ID: "run-1", ProjectID: "project-1", ConversationID: "conv-1",
			Status: domain.RunSucceeded, StartedAt: &start, FinishedAt: &end,
			CreatedAt: start, UpdatedAt: end,
		},
		spans: []domain.Span{
			{
				ID: "span-1", RunID: "run-1", TraceID: "trace-1", SpanID: "logical-1",
				Kind: domain.SpanModel, Name: "first", Status: "succeeded",
				Input: json.RawMessage(`{"prompt":"first"}`), Output: json.RawMessage(`{"answer":"one"}`),
				StartedAt: start, EndedAt: timePointer(start.Add(time.Second)),
			},
			{
				ID: "span-2", RunID: "run-1", TraceID: "trace-1", SpanID: "logical-2",
				ParentSpanID: "logical-1", Kind: domain.SpanOperation, Name: "second", Status: "succeeded",
				Input: json.RawMessage(`{"operation":"click"}`), Output: json.RawMessage(`{"clicked":true}`),
				StartedAt: start.Add(time.Second), EndedAt: timePointer(start.Add(2 * time.Second)),
			},
			{
				ID: "span-3", RunID: "run-1", TraceID: "trace-1", SpanID: "logical-3",
				Kind: domain.SpanModel, Name: "third", Status: "succeeded",
				Input: json.RawMessage(`{"prompt":"final"}`), Output: json.RawMessage(`{"answer":"done"}`),
				StartedAt: start.Add(2 * time.Second), EndedAt: &end,
			},
		},
	}
}

func completionMessage(t *testing.T, eventID string) eventbus.Message {
	t.Helper()
	occurredAt := time.Date(2026, 10, 21, 12, 0, 3, 0, time.UTC)
	return eventbus.Message{
		Topic: "agenttrace.run.events.v1",
		Key:   "run-1",
		Value: mustJSON(t, event.Envelope[map[string]any]{
			SchemaVersion: "agenttrace.event.v1",
			EventID:       eventID,
			EventType:     event.RunCompleted,
			AggregateID:   "run-1",
			OccurredAt:    occurredAt,
			CorrelationID: "run-1",
			Payload: map[string]any{
				"run_id": "run-1", "status": "succeeded",
			},
		}),
	}
}

func observationNames(observations []Observation) []string {
	names := make([]string, len(observations))
	for index, observation := range observations {
		names[index] = observation.Name
	}
	return names
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return raw
}

type memoryRepository struct {
	project      postgres.Project
	conversation postgres.Conversation
	messages     []domain.Message
	run          domain.Run
	spans        []domain.Span
	loads        int
}

func (r *memoryRepository) GetProject(context.Context, string) (postgres.Project, error) {
	r.loads++
	return r.project, nil
}

func (r *memoryRepository) GetRun(context.Context, string) (domain.Run, error) {
	r.loads++
	return r.run, nil
}

func (r *memoryRepository) GetConversation(
	context.Context,
	string,
) (postgres.Conversation, []domain.Message, []domain.Run, error) {
	r.loads++
	return r.conversation, append([]domain.Message(nil), r.messages...), nil, nil
}

func (r *memoryRepository) ListTrace(context.Context, string) ([]domain.Span, error) {
	r.loads++
	return append([]domain.Span(nil), r.spans...), nil
}

type recordingClient struct {
	batches []Batch
	err     error
}

func (c *recordingClient) Ingest(_ context.Context, batch Batch) error {
	c.batches = append(c.batches, batch)
	return c.err
}

type memoryInbox struct {
	mu   sync.Mutex
	seen map[string]bool
}

func newMemoryInbox() *memoryInbox {
	return &memoryInbox{seen: make(map[string]bool)}
}

func (i *memoryInbox) Handle(ctx context.Context, message eventbus.Message, handle func(context.Context) error) error {
	var envelope event.Envelope[json.RawMessage]
	if err := json.Unmarshal(message.Value, &envelope); err != nil {
		return err
	}
	i.mu.Lock()
	if i.seen[envelope.EventID] {
		i.mu.Unlock()
		return nil
	}
	i.mu.Unlock()
	if err := handle(ctx); err != nil {
		return err
	}
	i.mu.Lock()
	i.seen[envelope.EventID] = true
	i.mu.Unlock()
	return nil
}

type memoryRetryStore struct {
	mu           sync.Mutex
	attempts     map[string]int
	handoffs     map[string]DLQHandoff
	handoffCount int
}

func newMemoryRetryStore() *memoryRetryStore {
	return &memoryRetryStore{
		attempts: make(map[string]int),
		handoffs: make(map[string]DLQHandoff),
	}
}

func (s *memoryRetryStore) DLQEventID(eventID string) string {
	return stableID("langfuse-dlq", eventID)
}

func (s *memoryRetryStore) Increment(_ context.Context, eventID string, _ []byte) (RetryIncrement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.handoffs[eventID]; exists {
		return RetryIncrement{DLQHandedOff: true}, nil
	}
	s.attempts[eventID]++
	return RetryIncrement{Attempts: s.attempts[eventID]}, nil
}

func (s *memoryRetryStore) Clear(_ context.Context, eventID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, eventID)
	return nil
}

func (s *memoryRetryStore) HandoffDLQ(_ context.Context, eventID string, handoff DLQHandoff) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.handoffs[eventID]; exists {
		return nil
	}
	handoff.Payload = append([]byte(nil), handoff.Payload...)
	s.handoffs[eventID] = handoff
	delete(s.attempts, eventID)
	s.handoffCount++
	return nil
}

func (s *memoryRetryStore) HasDLQHandoff(_ context.Context, eventID, outboxEventID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	handoff, exists := s.handoffs[eventID]
	return exists && handoff.EventID == outboxEventID, nil
}
