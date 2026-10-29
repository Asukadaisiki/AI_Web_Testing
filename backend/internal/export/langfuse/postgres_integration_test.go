package langfuse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresHarnessCompletionExportsAllOutcomesWithRetryAndDeduplication(t *testing.T) {
	for _, status := range []domain.RunStatus{domain.RunSucceeded, domain.RunFailed, domain.RunCancelled} {
		t.Run(string(status), func(t *testing.T) {
			ctx, pool, store := newExporterPostgresStore(t)
			project, err := store.CreateProject(ctx, postgres.CreateProjectParams{Name: "completion"})
			if err != nil {
				t.Fatal(err)
			}
			conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{ProjectID: project.ID})
			if err != nil {
				t.Fatal(err)
			}
			_, run, err := store.CreateMessageAndRun(ctx, postgres.CreateMessageParams{
				ConversationID: conversation.ID, Content: "actual goal", AgentID: "browser-eval",
				Model: event.ModelRef{Provider: "pi", Name: "model"}, IdempotencyKey: t.Name(),
			})
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := store.BeginHarnessAttempt(ctx, run.ID, "provider-1")
			if err != nil {
				t.Fatal(err)
			}
			if status == domain.RunCancelled {
				if err := store.RequestCancel(ctx, run.ID); err != nil {
					t.Fatal(err)
				}
			}
			finish := postgres.HarnessFinish{
				RunID: run.ID, AttemptID: attempt.ID, Status: status,
				Event: event.Envelope[json.RawMessage]{
					SchemaVersion: "agenttrace.event.v1", EventID: "completion",
					EventType: event.RunCompleted, AggregateID: run.ID,
					CorrelationID: run.ID, OccurredAt: time.Now().UTC(),
				},
			}
			for range 2 {
				if _, err := store.FinishHarnessAttempt(ctx, finish); err != nil {
					t.Fatal(err)
				}
			}
			var raw []byte
			if err := pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE key = $1 AND payload->>'event_type' = 'run.completed'`, run.ID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			envelope, err := decodeEvent(raw)
			if err != nil {
				t.Fatal(err)
			}
			completion, err := event.DecodeRunCompletion(envelope.EventType, envelope.AggregateID, envelope.Payload)
			if err != nil || completion.Status != status {
				t.Fatalf("completion = %#v, %v", completion, err)
			}
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload ingestionRequest
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if requests.Add(1) == 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
				} else {
					w.WriteHeader(http.StatusAccepted)
				}
			}))
			defer server.Close()
			client := newHTTPTestClient(t, server.URL, server.Client())
			exporter := &Exporter{
				Repository: store, Client: client,
				Inbox:      PostgresInbox{Deduplicator: consumer.NewDeduplicator(pool, "completion-test")},
				RetryStore: PostgresRetryStore{Store: store, Consumer: "completion-test"},
			}
			message := eventbus.Message{Topic: RunEventsTopic, Key: run.ID, Value: raw}
			if err := exporter.Handle(ctx, message); err == nil {
				t.Fatal("transient HTTP failure was acknowledged")
			}
			for range 2 {
				if err := exporter.Handle(ctx, message); err != nil {
					t.Fatal(err)
				}
			}
			if requests.Load() != 2 {
				t.Fatalf("HTTP requests = %d, want failed attempt and one successful export", requests.Load())
			}
			unchanged, err := store.GetRun(ctx, run.ID)
			if err != nil || unchanged.Status != status {
				t.Fatalf("export altered canonical run: %#v, %v", unchanged, err)
			}
		})
	}
}

func TestPostgresExporterFailureHandsOffOnceWithoutCanonicalMutation(t *testing.T) {
	ctx, pool, store := newExporterPostgresStore(t)
	insertCompletedRunFixture(t, ctx, pool)

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := NewHTTPClient(HTTPConfig{
		BaseURL: server.URL, PublicKey: "public-test", SecretKey: "secret-test",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("create Langfuse client: %v", err)
	}
	exporter := &Exporter{
		Repository: store,
		Inbox: PostgresInbox{
			Deduplicator: consumer.NewDeduplicator(pool, "agenttrace-langfuse-exporter"),
		},
		RetryStore: PostgresRetryStore{
			Store: store, Consumer: "agenttrace-langfuse-exporter",
		},
		Client: client, MaxAttempts: 2,
	}
	message := completionMessage(t, "evt-postgres-export")

	if err := exporter.Handle(ctx, message); err == nil {
		t.Fatal("first provider failure returned nil")
	}
	assertExporterDatabaseState(t, ctx, pool, 0, 1, 0)
	if err := exporter.Handle(ctx, message); err != nil {
		t.Fatalf("DLQ handoff: %v", err)
	}
	assertExporterDatabaseState(t, ctx, pool, 0, 0, 1)
	if err := exporter.Handle(ctx, message); err != nil {
		t.Fatalf("replay after handoff: %v", err)
	}
	assertExporterDatabaseState(t, ctx, pool, 0, 0, 1)
	if requests.Load() != 2 {
		t.Fatalf("Langfuse requests = %d, want two attempts before handoff", requests.Load())
	}
}

func TestPostgresExporterPermanentHTTPFailureHandsOffImmediately(t *testing.T) {
	ctx, pool, store := newExporterPostgresStore(t)
	insertCompletedRunFixture(t, ctx, pool)

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := NewHTTPClient(HTTPConfig{
		BaseURL: server.URL, PublicKey: "public-test", SecretKey: "secret-test",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("create Langfuse client: %v", err)
	}
	exporter := &Exporter{
		Repository: store,
		Inbox: PostgresInbox{
			Deduplicator: consumer.NewDeduplicator(pool, "agenttrace-langfuse-exporter"),
		},
		RetryStore: PostgresRetryStore{
			Store: store, Consumer: "agenttrace-langfuse-exporter",
		},
		Client: client, MaxAttempts: 3,
	}
	message := completionMessage(t, "evt-postgres-export")

	if err := exporter.Handle(ctx, message); err != nil {
		t.Fatalf("permanent HTTP failure handoff: %v", err)
	}
	assertExporterDatabaseState(t, ctx, pool, 0, 0, 1)
	if err := exporter.Handle(ctx, message); err != nil {
		t.Fatalf("replay after permanent handoff: %v", err)
	}
	assertExporterDatabaseState(t, ctx, pool, 0, 0, 1)
	if requests.Load() != 1 {
		t.Fatalf("Langfuse requests = %d, want one before permanent handoff", requests.Load())
	}
}

func TestPostgresExporterDLQHandoffIsScopedByConsumerGroup(t *testing.T) {
	ctx, pool, store := newExporterPostgresStore(t)
	insertCompletedRunFixture(t, ctx, pool)

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := NewHTTPClient(HTTPConfig{
		BaseURL: server.URL, PublicKey: "public-test", SecretKey: "secret-test",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("create Langfuse client: %v", err)
	}

	message := completionMessage(t, "evt-postgres-export")
	for _, consumerName := range []string{"langfuse-group-a", "langfuse-group-b"} {
		exporter := &Exporter{
			Repository: store,
			Inbox: PostgresInbox{
				Deduplicator: consumer.NewDeduplicator(pool, consumerName),
			},
			RetryStore: PostgresRetryStore{
				Store: store, Consumer: consumerName,
			},
			Client: client, MaxAttempts: 3,
		}
		if err := exporter.Handle(ctx, message); err != nil {
			t.Fatalf("%s permanent failure handoff: %v", consumerName, err)
		}
	}

	var dlqRows int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM outbox_events
		WHERE topic = 'agenttrace.run.dlq.v1'
		  AND key = 'run-1'`).Scan(&dlqRows); err != nil {
		t.Fatalf("count group-scoped DLQ outbox events: %v", err)
	}
	if dlqRows != 2 {
		t.Fatalf("group-scoped DLQ outbox rows = %d, want 2", dlqRows)
	}
	if requests.Load() != 2 {
		t.Fatalf("Langfuse requests = %d, want one per consumer group", requests.Load())
	}
}

func TestPostgresExporterDeliveryGateSerializesTwoInstances(t *testing.T) {
	ctx, pool, store := newExporterPostgresStore(t)
	insertCompletedRunFixture(t, ctx, pool)

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := NewHTTPClient(HTTPConfig{
		BaseURL: server.URL, PublicKey: "public-test", SecretKey: "secret-test",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("create Langfuse client: %v", err)
	}

	releaseRetry := make(chan struct{})
	retries := blockingPostgresRetryStore{
		PostgresRetryStore: PostgresRetryStore{
			Store: store, Consumer: "agenttrace-langfuse-exporter",
		},
		entered: make(chan struct{}),
		release: releaseRetry,
	}
	newExporter := func() *Exporter {
		return &Exporter{
			Repository: store,
			Inbox: PostgresInbox{
				Deduplicator: consumer.NewDeduplicator(pool, "agenttrace-langfuse-exporter"),
			},
			RetryStore:   &retries,
			DeliveryGate: PostgresDeliveryGate{Pool: pool, Consumer: "agenttrace-langfuse-exporter"},
			Client:       client,
			MaxAttempts:  3,
		}
	}
	message := completionMessage(t, "evt-postgres-export")
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- newExporter().Handle(ctx, message)
	}()

	select {
	case <-retries.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first exporter did not reach durable permanent-failure handling")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- newExporter().Handle(ctx, message)
	}()
	close(releaseRetry)

	for name, done := range map[string]<-chan error{"first": firstDone, "second": secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s exporter handle: %v", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s exporter did not finish", name)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("Langfuse requests = %d, want one across two exporter instances", requests.Load())
	}
	assertExporterDatabaseState(t, ctx, pool, 0, 0, 1)
}

func newExporterPostgresStore(t *testing.T) (context.Context, *pgxpool.Pool, *postgres.Store) {
	t.Helper()
	dsn := os.Getenv("AGENTTRACE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_TEST_POSTGRES_DSN is not configured")
	}
	if _, err := testpostgres.DatabaseName(dsn); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset PostgreSQL: %v", err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate PostgreSQL: %v", err)
	}
	return ctx, pool, postgres.NewStore(pool)
}

type blockingPostgresRetryStore struct {
	PostgresRetryStore
	once    sync.Once
	entered chan struct{}
	release <-chan struct{}
}

func (s *blockingPostgresRetryStore) Increment(
	ctx context.Context,
	eventID string,
	payload []byte,
) (RetryIncrement, error) {
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	return s.PostgresRetryStore.Increment(ctx, eventID, payload)
}

func insertCompletedRunFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	start := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Second)
	statements := []struct {
		query string
		args  []any
	}{
		{
			query: `INSERT INTO projects (id, name, metadata)
				VALUES ('project-1', 'Export project', '{"environment":"fixture"}')`,
		},
		{
			query: `INSERT INTO conversations (id, project_id, title, metadata)
				VALUES ('conv-1', 'project-1', 'Export conversation', '{}')`,
		},
		{
			query: `INSERT INTO runs (
					id, project_id, conversation_id, status, specification,
					started_at, finished_at, created_at, updated_at
				)
				VALUES ('run-1', 'project-1', 'conv-1', 'succeeded', '{}', $1, $2, $1, $2)`,
			args: []any{start, end},
		},
		{
			query: `INSERT INTO messages (
					id, conversation_id, run_id, ordinal, role, content, metadata, created_at
				)
				VALUES ('message-1', 'conv-1', 'run-1', 1, 'user', 'test checkout', '{}', $1)`,
			args: []any{start},
		},
		{
			query: `INSERT INTO spans (
					id, run_id, trace_id, span_id, kind, name, status,
					input, output, started_at, ended_at, created_at, updated_at
				)
				VALUES (
					'span-1', 'run-1', 'trace-1', 'logical-1', 'model', 'complete',
					'succeeded', '{"prompt":"checkout"}', '{"answer":"done"}',
					$1, $2, $1, $2
				)`,
			args: []any{start, end},
		},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("insert completed run fixture: %v", err)
		}
	}
}

func assertExporterDatabaseState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	inboxRows int,
	retryRows int,
	dlqRows int,
) {
	t.Helper()
	var (
		status domain.RunStatus
		inbox  int
		retry  int
		dlq    int
	)
	if err := pool.QueryRow(ctx, `SELECT status FROM runs WHERE id = 'run-1'`).Scan(&status); err != nil {
		t.Fatalf("read canonical run status: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM inbox_events
		WHERE consumer = 'agenttrace-langfuse-exporter'
		  AND event_id = 'evt-postgres-export'`).Scan(&inbox); err != nil {
		t.Fatalf("count exporter inbox: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM consumer_retry_counts
		WHERE consumer = 'agenttrace-langfuse-exporter'
		  AND event_id = 'evt-postgres-export'`).Scan(&retry); err != nil {
		t.Fatalf("count exporter retry: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE topic = 'agenttrace.run.dlq.v1'
		  AND key = 'run-1'`).Scan(&dlq); err != nil {
		t.Fatalf("count exporter DLQ outbox: %v", err)
	}
	if status != domain.RunSucceeded {
		t.Fatalf("canonical run status = %q, want succeeded", status)
	}
	if inbox != inboxRows || retry != retryRows || dlq != dlqRows {
		t.Fatalf(
			"exporter database state = inbox %d retry %d DLQ %d, want %d %d %d",
			inbox, retry, dlq, inboxRows, retryRows, dlqRows,
		)
	}
	if dlqRows == 1 {
		var payload []byte
		if err := pool.QueryRow(ctx, `
			SELECT payload FROM outbox_events
			WHERE topic = 'agenttrace.run.dlq.v1'
			  AND key = 'run-1'`).Scan(&payload); err != nil {
			t.Fatalf("read exporter DLQ payload: %v", err)
		}
		if !json.Valid(payload) {
			t.Fatalf("DLQ payload is invalid JSON: %s", payload)
		}
	}
}
