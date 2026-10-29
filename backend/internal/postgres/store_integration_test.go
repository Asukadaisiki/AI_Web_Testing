package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryPostgresDSNRequiresExplicitIsolatedDatabase(t *testing.T) {
	for _, dsn := range []string{
		"",
		"postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace?sslmode=disable",
		"postgres://agenttrace:agenttrace@127.0.0.1:5433/postgres?sslmode=disable",
	} {
		if _, err := repositoryTestDatabaseName(dsn); err == nil {
			t.Fatalf("repositoryTestDatabaseName(%q) accepted a non-isolated target", dsn)
		}
	}

	database, err := repositoryTestDatabaseName(
		"postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace_test_repository?sslmode=disable",
	)
	if err != nil {
		t.Fatalf("repositoryTestDatabaseName: %v", err)
	}
	if database != "agenttrace_test_repository" {
		t.Fatalf("database = %q", database)
	}
}

func TestCreateMessageAndRunReplaysMatchingIdempotencyRequest(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	params := CreateMessageParams{
		ConversationID: conversationID,
		Content:        "Log in and verify the cart",

		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	}

	firstMessage, firstRun, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, params))
	if err != nil {
		t.Fatalf("first create message and run: %v", err)
	}
	secondMessage, secondRun, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, params))
	if err != nil {
		t.Fatalf("replay create message and run: %v", err)
	}

	if firstRun.ID != secondRun.ID || firstMessage.ID != secondMessage.ID {
		t.Fatal("idempotent request returned different identities")
	}
	assertRowCount(t, ctx, pool, "messages", 1)
	assertRowCount(t, ctx, pool, "runs", 1)
	assertRowCount(t, ctx, pool, "outbox_events", 1)
}

func TestCreateMessageSetsRunLineage(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)

	message, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "verify the cart",

		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "message-lineage",
	}))
	if err != nil {
		t.Fatalf("create message and run: %v", err)
	}

	if run.SourceMessageID != message.ID || run.RetryOfRunID != "" {
		t.Fatalf("run lineage = %#v", run)
	}
}

func TestCreateRerunRejectsNonFailedSourceStatuses(t *testing.T) {
	statuses := []domain.RunStatus{
		domain.RunQueued,
		domain.RunRunning,
		domain.RunWaitingInput,
		domain.RunWaitingApproval,
		domain.RunCancelling,
		domain.RunSucceeded,
		domain.RunCancelled,
	}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			ctx, pool, store := newRepositoryTestStore(t)
			conversationID := createConversationFixture(t, ctx, pool)
			_, source := createFailedRerunSource(t, ctx, store, conversationID, "rerun-status-"+string(status))
			if _, err := pool.Exec(ctx, `UPDATE runs SET status = $2 WHERE id = $1`, source.ID, status); err != nil {
				t.Fatalf("set source status %q: %v", status, err)
			}

			_, err := store.CreateRerun(ctx, CreateRerunParams{
				RunID:          source.ID,
				IdempotencyKey: "rerun-rejected",
			})
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("CreateRerun status %q error = %v, want ErrConflict", status, err)
			}
			assertRowCount(t, ctx, pool, "runs", 1)
			assertRowCount(t, ctx, pool, "outbox_events", 1)
		})
	}
}

func TestCreateRerunReplaysMatchingIdempotencyRequest(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, source := createFailedRerunSource(t, ctx, store, conversationID, "rerun-idempotency-source")
	params := CreateRerunParams{RunID: source.ID, IdempotencyKey: "rerun-replay"}

	first, err := store.CreateRerun(ctx, params)
	if err != nil {
		t.Fatalf("first CreateRerun: %v", err)
	}
	second, err := store.CreateRerun(ctx, params)
	if err != nil {
		t.Fatalf("replay CreateRerun: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("rerun replay IDs = %q, %q", first.ID, second.ID)
	}
	var outboxCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM outbox_events
		WHERE payload->>'aggregate_id' = $1 AND topic = $2`, first.ID, runCommandsTopic).Scan(&outboxCount); err != nil {
		t.Fatalf("count rerun outbox records: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("rerun outbox records = %d, want 1", outboxCount)
	}
}

func TestCreateRerunSerializesConcurrentMatchingIdempotencyRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, source := createFailedRerunSource(t, ctx, store, conversationID, "rerun-concurrent-source")

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin source lock holder: %v", err)
	}
	defer holder.Rollback(context.Background())
	if _, err := holder.Exec(ctx, `SELECT id FROM runs WHERE id = $1 FOR UPDATE`, source.ID); err != nil {
		t.Fatalf("hold source lock: %v", err)
	}

	results := make(chan struct {
		run domain.Run
		err error
	}, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			run, err := store.CreateRerun(ctx, CreateRerunParams{
				RunID:          source.ID,
				IdempotencyKey: "rerun-concurrent",
			})
			results <- struct {
				run domain.Run
				err error
			}{run: run, err: err}
		}()
	}
	waitForDatabaseLock(t, ctx, pool)
	select {
	case result := <-results:
		t.Fatalf("CreateRerun completed while source lock was held: %#v", result)
	default:
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release source lock: %v", err)
	}

	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent CreateRerun requests did not finish")
	}
	close(results)

	var runs []domain.Run
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent CreateRerun: %v", result.err)
		}
		runs = append(runs, result.run)
	}
	if len(runs) != 2 || runs[0].ID == "" || runs[0].ID != runs[1].ID {
		t.Fatalf("concurrent reruns = %#v, want one shared identity", runs)
	}
}

func waitForDatabaseLock(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE datname = current_database()
				  AND pid <> pg_backend_pid()
				  AND wait_event_type = 'Lock'
				  AND COALESCE(wait_event, '') <> 'advisory'
			)`).Scan(&waiting); err != nil {
			t.Fatalf("observe database lock wait: %v", err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("operation did not reach a database lock wait: %v", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func waitForAdvisoryLockWaiter(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_locks
				WHERE locktype = 'advisory'
				  AND NOT granted
			)`).Scan(&waiting); err != nil {
			t.Fatalf("observe advisory lock wait: %v", err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("rerun did not reach the deterministic overlap point: %v", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestCreateRerunUsesImmediateFailedParent(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, source := createFailedRerunSource(t, ctx, store, conversationID, "rerun-parent-source")

	first, err := store.CreateRerun(ctx, CreateRerunParams{
		RunID:          source.ID,
		IdempotencyKey: "rerun-parent-first",
	})
	if err != nil {
		t.Fatalf("create first rerun: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET status = 'failed' WHERE id = $1`, first.ID); err != nil {
		t.Fatalf("make first rerun failed: %v", err)
	}
	second, err := store.CreateRerun(ctx, CreateRerunParams{
		RunID:          first.ID,
		IdempotencyKey: "rerun-parent-second",
	})
	if err != nil {
		t.Fatalf("create second rerun: %v", err)
	}
	if second.SourceMessageID != source.SourceMessageID || second.RetryOfRunID != first.ID {
		t.Fatalf("second rerun lineage = %#v", second)
	}
	if second.RetryOfRunID == source.ID {
		t.Fatalf("second rerun retried root source %q, want immediate parent %q", source.ID, first.ID)
	}
}

func TestCreateRerunValidatesRequiredParams(t *testing.T) {
	ctx, _, store := newRepositoryTestStore(t)
	for _, params := range []CreateRerunParams{
		{IdempotencyKey: "rerun-validation"},
		{RunID: "run_validation"},
	} {
		if _, err := store.CreateRerun(ctx, params); err == nil {
			t.Fatalf("CreateRerun(%#v) succeeded without required values", params)
		}
	}
}

func TestRunLineageMigrationBackfillsExistingRows(t *testing.T) {
	dsn := os.Getenv("AGENTTRACE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_TEST_POSTGRES_DSN is not configured")
	}
	if _, err := repositoryTestDatabaseName(dsn); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)
	resetDatabase(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		CREATE TABLE schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		t.Fatalf("create migration ledger: %v", err)
	}
	for _, migration := range []string{
		"001_initial.sql",
		"002_outbox_claim_token.sql",
		"003_consumer_retry_counts.sql",
	} {
		if err := applyMigration(ctx, pool, migration); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (id, name) VALUES ('prj_legacy', 'Legacy');
		INSERT INTO conversations (id, project_id) VALUES ('conv_legacy', 'prj_legacy');
		INSERT INTO runs (id, project_id, conversation_id, status, specification)
		VALUES ('run_legacy', 'prj_legacy', 'conv_legacy', 'failed', '{}');
		INSERT INTO messages (id, conversation_id, run_id, ordinal, role, content)
		VALUES ('msg_legacy', 'conv_legacy', 'run_legacy', 1, 'user', 'legacy request')`); err != nil {
		t.Fatalf("seed legacy run lineage: %v", err)
	}

	if err := applyMigration(ctx, pool, "004_run_lineage.sql"); err != nil {
		t.Fatalf("apply 004_run_lineage.sql: %v", err)
	}
	var sourceMessageID string
	if err := pool.QueryRow(ctx, `
		SELECT source_message_id
		FROM runs
		WHERE id = 'run_legacy'`).Scan(&sourceMessageID); err != nil {
		t.Fatalf("read backfilled source message: %v", err)
	}
	if sourceMessageID != "msg_legacy" {
		t.Fatalf("source_message_id = %q, want %q", sourceMessageID, "msg_legacy")
	}
}

func TestManagedFinishPersistsOneAssistantMessage(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "verify the cart",

		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "completion-message",
	}))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	completedAt := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	completion := event.Envelope[json.RawMessage]{
		SchemaVersion: "agenttrace.event.v1",
		EventID:       "evt-completion-message",
		EventType:     event.RunCompleted,
		AggregateID:   run.ID,
		CorrelationID: run.ID,
		OccurredAt:    completedAt,
		Payload:       json.RawMessage(`{"status":"succeeded"}`),
	}
	const assistant = "Report: cart passed."
	if err := finishFixtureRun(t, ctx, store, run.ID, domain.RunSucceeded, nil, assistant, completion); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if err := finishFixtureRun(t, ctx, store, run.ID, domain.RunSucceeded, nil, assistant, completion); err != nil {
		t.Fatalf("replay completion: %v", err)
	}

	var count int
	var content string
	if err := pool.QueryRow(ctx, `
		SELECT count(*), coalesce(max(content), '')
		FROM messages
		WHERE conversation_id = $1 AND role = 'assistant'`, conversationID).Scan(&count, &content); err != nil {
		t.Fatalf("load assistant message: %v", err)
	}
	if count != 1 || content != assistant {
		t.Fatalf("assistant messages = count %d content %q, want one %q", count, content, assistant)
	}
}

func TestCreateMessageAndRunRejectsMismatchedIdempotencyRequest(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	params := CreateMessageParams{
		ConversationID: conversationID,
		Content:        "first request",

		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	}
	if _, _, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, params)); err != nil {
		t.Fatalf("first create message and run: %v", err)
	}

	params.Content = "different request"
	if _, _, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, params)); !errors.Is(err, ErrConflict) {
		t.Fatalf("mismatched replay error = %v, want ErrConflict", err)
	}
	assertRowCount(t, ctx, pool, "messages", 1)
	assertRowCount(t, ctx, pool, "runs", 1)
}

func TestCreateMessageAndRunRollsBackWhenOutboxInsertFails(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		ALTER TABLE outbox_events
		ADD CONSTRAINT reject_test_outbox_write CHECK (false)`); err != nil {
		t.Fatalf("reject outbox writes: %v", err)
	}

	_, _, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "must roll back",

		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	}))
	if err == nil {
		t.Fatal("expected outbox failure")
	}
	assertRowCount(t, ctx, pool, "messages", 0)
	assertRowCount(t, ctx, pool, "runs", 0)
	assertRowCount(t, ctx, pool, "idempotency_keys", 0)
}

func TestConsumerRetryCountPersistsAcrossStoreInstances(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	if got, err := store.IncrementConsumerRetry(ctx, "runtime", "evt-retry", []byte(`not-json`)); err != nil || got != 1 {
		t.Fatalf("first retry count = (%d, %v), want (1, nil)", got, err)
	}
	second := NewStore(pool)
	if got, err := second.IncrementConsumerRetry(ctx, "runtime", "evt-retry", []byte(`not-json`)); err != nil || got != 2 {
		t.Fatalf("second retry count = (%d, %v), want (2, nil)", got, err)
	}
	if err := second.ClearConsumerRetry(ctx, "runtime", "evt-retry"); err != nil {
		t.Fatalf("clear retry count: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM consumer_retry_counts`).Scan(&count); err != nil {
		t.Fatalf("count retry rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("retry rows = %d, want 0", count)
	}
}

func TestConsumerDLQHandoffMovesRetryStateToOneOutboxEvent(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	const (
		consumer = "runtime"
		retryID  = "evt-dlq-handoff"
		outboxID = "evt-dlq-outbox"
		topic    = "agenttrace.run.dlq.v1"
		key      = "run-dlq-handoff"
	)
	dlqPayload := []byte(`{"original":{"event_id":"evt-dlq-handoff"},"error":"permanent failure","attempt":2}`)
	if _, err := store.IncrementConsumerRetry(ctx, consumer, retryID, []byte(`{"event_id":"evt-dlq-handoff"}`)); err != nil {
		t.Fatalf("record first failure: %v", err)
	}
	if _, err := store.IncrementConsumerRetry(ctx, consumer, retryID, []byte(`{"event_id":"evt-dlq-handoff"}`)); err != nil {
		t.Fatalf("record final failure: %v", err)
	}

	if err := store.HandoffConsumerRetryToOutbox(ctx, consumer, retryID, outboxID, topic, key, dlqPayload); err != nil {
		t.Fatalf("handoff consumer retry to outbox: %v", err)
	}
	if err := store.HandoffConsumerRetryToOutbox(ctx, consumer, retryID, outboxID, topic, key, dlqPayload); err != nil {
		t.Fatalf("idempotent handoff consumer retry to outbox: %v", err)
	}
	mismatchedPayload := []byte(`{"original":{"event_id":"evt-dlq-handoff"},"error":"different failure","attempt":2}`)
	if err := store.HandoffConsumerRetryToOutbox(
		ctx, consumer, retryID, outboxID, topic, key, mismatchedPayload,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("mismatched DLQ handoff error = %v, want ErrConflict", err)
	}

	var retryRows, outboxRows int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM consumer_retry_counts
		WHERE consumer = $1 AND event_id = $2`, consumer, retryID).Scan(&retryRows); err != nil {
		t.Fatalf("count retry state: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE event_id = $1 AND topic = $2 AND key = $3 AND payload = $4::jsonb`,
		outboxID, topic, key, dlqPayload,
	).Scan(&outboxRows); err != nil {
		t.Fatalf("count DLQ outbox event: %v", err)
	}
	if retryRows != 0 || outboxRows != 1 {
		t.Fatalf("retry rows=%d outbox rows=%d, want 0 and 1", retryRows, outboxRows)
	}
}

func newRepositoryTestStore(t *testing.T) (context.Context, *pgxpool.Pool, *Store) {
	t.Helper()

	dsn := os.Getenv("AGENTTRACE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_TEST_POSTGRES_DSN is not configured")
	}
	if _, err := repositoryTestDatabaseName(dsn); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)
	resetDatabase(t, ctx, pool)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return ctx, pool, NewStore(pool)
}

func repositoryTestDatabaseName(dsn string) (string, error) {
	return testpostgres.DatabaseName(dsn)
}

func createConversationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()

	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, name) VALUES ('prj_fixture', 'Fixture')`); err != nil {
		t.Fatalf("insert project fixture: %v", err)
	}
	const conversationID = "conv_fixture"
	if _, err := pool.Exec(ctx, `
		INSERT INTO conversations (id, project_id, title)
		VALUES ($1, 'prj_fixture', 'Fixture conversation')`, conversationID); err != nil {
		t.Fatalf("insert conversation fixture: %v", err)
	}
	return conversationID
}

func createFailedRerunSource(
	t *testing.T,
	ctx context.Context,
	store *Store,
	conversationID, idempotencyKey string,
) (domain.Message, domain.Run) {
	t.Helper()

	message, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "failed rerun source",

		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: idempotencyKey,
	}))
	if err != nil {
		t.Fatalf("create failed rerun source: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE runs SET status = 'failed' WHERE id = $1`, run.ID); err != nil {
		t.Fatalf("mark rerun source failed: %v", err)
	}
	run.Status = domain.RunFailed
	return message, run
}

func assertRowCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, want int) {
	t.Helper()

	var got int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&got); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s rows = %d, want %d", table, got, want)
	}
}
