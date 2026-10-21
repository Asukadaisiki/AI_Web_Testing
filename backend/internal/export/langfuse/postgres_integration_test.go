package langfuse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

func newExporterPostgresStore(t *testing.T) (context.Context, *pgxpool.Pool, *postgres.Store) {
	t.Helper()
	dsn := os.Getenv("AGENTTRACE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_TEST_POSTGRES_DSN is not configured")
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
