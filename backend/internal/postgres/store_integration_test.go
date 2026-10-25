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
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
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
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	}

	firstMessage, firstRun, err := store.CreateMessageAndRun(ctx, params)
	if err != nil {
		t.Fatalf("first create message and run: %v", err)
	}
	secondMessage, secondRun, err := store.CreateMessageAndRun(ctx, params)
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

	message, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "verify the cart",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "message-lineage",
	})
	if err != nil {
		t.Fatalf("create message and run: %v", err)
	}

	if run.SourceMessageID != message.ID || run.RetryOfRunID != "" {
		t.Fatalf("run lineage = %#v", run)
	}
}

func TestCreateRerunQueuesImmutableFailedSpecification(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	const (
		content = "rerun the failed checkout task"
		agentID = "browser-eval"
	)
	model := event.ModelRef{Provider: "scripted", Name: "rerun-fixture"}
	operations := []event.Operation{{
		Name:        "browser_execute_case",
		Description: "execute the checkout fixture",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Callback: event.Callback{
			Kind: "http",
			URL:  "http://worker:8100/operations/browser_execute_case",
		},
	}}
	message, source, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        content,
		AgentID:        agentID,
		Model:          model,
		Operations:     operations,
		IdempotencyKey: "rerun-source",
	})
	if err != nil {
		t.Fatalf("create rerun source: %v", err)
	}

	var sourceSpecification runSpecification
	if err := json.Unmarshal(source.Specification, &sourceSpecification); err != nil {
		t.Fatalf("decode source specification: %v", err)
	}
	sourceSpecification.Command.CheckpointVersion = 9
	sourceSpecificationJSON, err := json.Marshal(sourceSpecification)
	if err != nil {
		t.Fatalf("marshal failed source specification: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE runs
		SET status = 'failed', checkpoint_version = 9, specification = $2::jsonb
		WHERE id = $1`, source.ID, sourceSpecificationJSON); err != nil {
		t.Fatalf("make source failed: %v", err)
	}
	source.Status = domain.RunFailed
	source.CheckpointVersion = 9
	source.Specification = sourceSpecificationJSON

	var messageCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE conversation_id = $1`, conversationID).Scan(&messageCount); err != nil {
		t.Fatalf("count source messages: %v", err)
	}

	rerun, err := store.CreateRerun(ctx, CreateRerunParams{
		RunID:          source.ID,
		IdempotencyKey: "rerun-1",
	})
	if err != nil {
		t.Fatalf("create rerun: %v", err)
	}
	if rerun.ID == source.ID || rerun.Status != domain.RunQueued {
		t.Fatalf("rerun = %#v", rerun)
	}
	if rerun.SourceMessageID != source.SourceMessageID ||
		rerun.RetryOfRunID != source.ID ||
		rerun.CheckpointVersion != 0 {
		t.Fatalf("rerun lineage/checkpoint = %#v", rerun)
	}

	var storedSpecification runSpecification
	if err := json.Unmarshal(rerun.Specification, &storedSpecification); err != nil {
		t.Fatalf("decode rerun specification: %v", err)
	}
	if storedSpecification.AgentID != agentID ||
		storedSpecification.Command.RunID != rerun.ID ||
		storedSpecification.Command.CheckpointVersion != 0 ||
		storedSpecification.Command.Model != model ||
		len(storedSpecification.Command.Messages) != 1 ||
		storedSpecification.Command.Messages[0].ID != message.ID ||
		storedSpecification.Command.Messages[0].Content != content ||
		len(storedSpecification.Command.Operations) != 1 ||
		storedSpecification.Command.Operations[0].Name != operations[0].Name ||
		storedSpecification.Command.Operations[0].Callback != operations[0].Callback {
		t.Fatalf("rerun specification = %#v", storedSpecification)
	}
	assertOperationInputSchema(t, storedSpecification.Command.Operations[0].InputSchema)

	var payload []byte
	if err := pool.QueryRow(ctx, `
		SELECT payload
		FROM outbox_events
		WHERE key = $1 AND topic = $2`,
		rerun.ID, runCommandsTopic,
	).Scan(&payload); err != nil {
		t.Fatalf("read rerun command outbox: %v", err)
	}
	var envelope event.Envelope[event.RunCommand]
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode rerun command envelope: %v", err)
	}
	if envelope.SchemaVersion != "agenttrace.run-command.v1" ||
		envelope.EventType != "run.requested" ||
		envelope.AggregateID != rerun.ID ||
		envelope.CorrelationID != rerun.ID ||
		envelope.CausationID != source.ID ||
		envelope.Payload.RunID != rerun.ID ||
		envelope.Payload.CheckpointVersion != 0 ||
		envelope.Payload.Model != model ||
		len(envelope.Payload.Messages) != 1 ||
		envelope.Payload.Messages[0].ID != message.ID ||
		envelope.Payload.Messages[0].Content != content ||
		len(envelope.Payload.Operations) != 1 ||
		envelope.Payload.Operations[0].Name != operations[0].Name ||
		envelope.Payload.Operations[0].Callback != operations[0].Callback {
		t.Fatalf("rerun command envelope = %#v", envelope)
	}
	assertOperationInputSchema(t, envelope.Payload.Operations[0].InputSchema)
	var actualMessageCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE conversation_id = $1`, conversationID).Scan(&actualMessageCount); err != nil {
		t.Fatalf("count messages after rerun: %v", err)
	}
	if actualMessageCount != messageCount {
		t.Fatalf("message count = %d, want %d", actualMessageCount, messageCount)
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
		WHERE key = $1 AND topic = $2`, first.ID, runCommandsTopic).Scan(&outboxCount); err != nil {
		t.Fatalf("count rerun outbox records: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("rerun outbox records = %d, want 1", outboxCount)
	}
}

func TestCreateRerunSerializesConcurrentMatchingIdempotencyRequests(t *testing.T) {
	ctx, _, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, store.pool)
	_, source := createFailedRerunSource(t, ctx, store, conversationID, "rerun-concurrent-source")

	start := make(chan struct{})
	results := make(chan struct {
		run domain.Run
		err error
	}, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
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
	close(start)
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

func TestFinishRunPersistsOneAssistantMessage(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "verify the cart",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "completion-message",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	completedAt := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	completion := event.Envelope[json.RawMessage]{
		SchemaVersion: "agenttrace.event.v1",
		EventID:       "evt-completion-message",
		EventType:     "runtime.completed",
		AggregateID:   run.ID,
		CorrelationID: run.ID,
		OccurredAt:    completedAt,
		Payload:       json.RawMessage(`{"status":"succeeded"}`),
	}
	const assistant = "Report: cart passed."
	if err := store.FinishRun(ctx, run.ID, domain.RunSucceeded, nil, assistant, completion); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if err := store.FinishRun(ctx, run.ID, domain.RunSucceeded, nil, assistant, completion); err != nil {
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

func TestFinishRunSerializesAssistantOrdinalsWithinConversation(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION delay_assistant_message_insert() RETURNS trigger AS $$
		BEGIN
			PERFORM pg_sleep(0.2);
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER delay_assistant_message_insert
		BEFORE INSERT ON messages
		FOR EACH ROW
		WHEN (NEW.role = 'assistant')
		EXECUTE FUNCTION delay_assistant_message_insert();`); err != nil {
		t.Fatalf("install concurrent insert delay: %v", err)
	}
	runs := make([]domain.Run, 2)
	for index := range runs {
		_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
			ConversationID: conversationID,
			Content:        "concurrent terminal request",
			AgentID:        "browser-eval",
			Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
			IdempotencyKey: "concurrent-terminal-" + string(rune('a'+index)),
		})
		if err != nil {
			t.Fatalf("create run %d: %v", index, err)
		}
		runs[index] = run
	}

	start := make(chan struct{})
	errs := make(chan error, len(runs))
	var group sync.WaitGroup
	for index, run := range runs {
		group.Add(1)
		go func(index int, run domain.Run) {
			defer group.Done()
			<-start
			errs <- store.FinishRun(ctx, run.ID, domain.RunSucceeded, nil,
				"concurrent assistant response",
				event.Envelope[json.RawMessage]{
					SchemaVersion: "agenttrace.event.v1",
					EventID:       "evt-concurrent-terminal-" + string(rune('a'+index)),
					EventType:     "runtime.completed",
					AggregateID:   run.ID,
					CorrelationID: run.ID,
					OccurredAt:    time.Date(2026, 10, 21, 12, 0, index, 0, time.UTC),
					Payload:       json.RawMessage(`{"status":"succeeded"}`),
				})
		}(index, run)
	}
	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent FinishRun: %v", err)
		}
	}

	var terminalRuns, assistantMessages, uniqueOrdinals int
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM runs WHERE conversation_id = $1 AND status = 'succeeded'),
			(SELECT count(*) FROM messages WHERE conversation_id = $1 AND role = 'assistant'),
			(SELECT count(DISTINCT ordinal) FROM messages WHERE conversation_id = $1)`,
		conversationID,
	).Scan(&terminalRuns, &assistantMessages, &uniqueOrdinals); err != nil {
		t.Fatalf("read concurrent terminal persistence: %v", err)
	}
	if terminalRuns != 2 || assistantMessages != 2 || uniqueOrdinals != 4 {
		t.Fatalf("terminal runs=%d assistant messages=%d unique ordinals=%d, want 2, 2, 4",
			terminalRuns, assistantMessages, uniqueOrdinals)
	}
}

func TestCreateMessageAndRunRejectsMismatchedIdempotencyRequest(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	params := CreateMessageParams{
		ConversationID: conversationID,
		Content:        "first request",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	}
	if _, _, err := store.CreateMessageAndRun(ctx, params); err != nil {
		t.Fatalf("first create message and run: %v", err)
	}

	params.Content = "different request"
	if _, _, err := store.CreateMessageAndRun(ctx, params); !errors.Is(err, ErrConflict) {
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

	_, _, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "must roll back",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	})
	if err == nil {
		t.Fatal("expected outbox failure")
	}
	assertRowCount(t, ctx, pool, "messages", 0)
	assertRowCount(t, ctx, pool, "runs", 0)
	assertRowCount(t, ctx, pool, "idempotency_keys", 0)
}

func TestClaimRunClaimsOnlyExpectedCheckpoint(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "claim me",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	})
	if err != nil {
		t.Fatalf("create message and run: %v", err)
	}

	claimed, ok, err := store.ClaimRun(ctx, run.ID, 0)
	if err != nil {
		t.Fatalf("claim run: %v", err)
	}
	if !ok {
		t.Fatal("first matching checkpoint must claim the run")
	}
	if claimed.Status != domain.RunRunning || claimed.CheckpointVersion != 1 || claimed.StartedAt == nil {
		t.Fatalf("claimed run = %#v", claimed)
	}

	if _, ok, err := store.ClaimRun(ctx, run.ID, 0); err != nil || ok {
		t.Fatalf("stale checkpoint claim = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

func TestStartTraceAttemptDoesNotAdvanceCheckpointAndCompletes(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "start trace attempt",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "start-trace-attempt",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, ok, err := store.ClaimRun(ctx, run.ID, 0); err != nil || !ok {
		t.Fatalf("claim run = (ok=%v, err=%v)", ok, err)
	}

	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	started := TraceAttemptStart{
		ExpectedCheckpoint: 1,
		Root: &domain.Span{
			ID: "spn_root", RunID: run.ID, TraceID: "trc_started", SpanID: "run-root",
			Kind: domain.SpanRun, Name: "run", Status: "running", StartedAt: now,
		},
		Span: domain.Span{
			ID: "spn_started", RunID: run.ID, TraceID: "trc_started", SpanID: "model-1",
			ParentSpanID: "run-root", Kind: domain.SpanModel, Name: "model", Status: "started",
			Input: json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`), StartedAt: now,
		},
		Attempt: TraceAttempt{
			ID: "att_started", AttemptNumber: 1, Provider: "model", Status: "started",
			Request: json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`), StartedAt: now,
		},
	}
	if err := store.StartTraceAttempt(ctx, started); err != nil {
		t.Fatalf("start trace attempt: %v", err)
	}
	if err := store.StartTraceAttempt(ctx, started); err != nil {
		t.Fatalf("idempotent start trace attempt: %v", err)
	}

	var checkpoint int64
	if err := pool.QueryRow(ctx, `SELECT checkpoint_version FROM runs WHERE id = $1`, run.ID).Scan(&checkpoint); err != nil {
		t.Fatalf("read checkpoint after start: %v", err)
	}
	if checkpoint != 1 {
		t.Fatalf("checkpoint after start = %d, want 1", checkpoint)
	}
	assertRowCount(t, ctx, pool, "spans", 2)
	assertRowCount(t, ctx, pool, "span_attempts", 1)
	var rootCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM spans
		WHERE run_id = $1
		  AND span_id = 'run-root'
		  AND kind = 'run'
		  AND parent_span_id IS NULL`, run.ID).Scan(&rootCount); err != nil {
		t.Fatalf("count trace root: %v", err)
	}
	if rootCount != 1 {
		t.Fatalf("trace root count = %d, want 1", rootCount)
	}

	finished := now.Add(time.Second)
	step := TraceStep{
		ExpectedCheckpoint: 1,
		Span: domain.Span{
			ID: "spn_started", RunID: run.ID, TraceID: "trc_started", SpanID: "model-1",
			ParentSpanID: "run-root", Kind: domain.SpanModel, Name: "model", Status: "succeeded",
			Input:  json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`),
			Output: json.RawMessage(`{"content":"done"}`), StartedAt: finished, EndedAt: &finished,
		},
		Attempt: &TraceAttempt{
			ID: "att_started", AttemptNumber: 1, Provider: "model", Status: "succeeded",
			Request:  json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`),
			Response: json.RawMessage(`{"content":"done"}`), StartedAt: finished, EndedAt: &finished,
		},
		Event: event.Envelope[json.RawMessage]{
			SchemaVersion: "agenttrace.event.v1", EventID: "evt_started_complete",
			EventType: "runtime.step.completed", AggregateID: run.ID, CorrelationID: run.ID,
			OccurredAt: finished, Payload: json.RawMessage(`{"checkpoint":1}`),
		},
	}
	if err := store.AppendTraceStep(ctx, step); err != nil {
		t.Fatalf("complete started trace attempt: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT checkpoint_version FROM runs WHERE id = $1`, run.ID).Scan(&checkpoint); err != nil {
		t.Fatalf("read checkpoint after completion: %v", err)
	}
	if checkpoint != 2 {
		t.Fatalf("checkpoint after completion = %d, want 2", checkpoint)
	}
	var status string
	var endedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT status, ended_at FROM span_attempts WHERE id = 'att_started'`).Scan(&status, &endedAt); err != nil {
		t.Fatalf("load completed attempt: %v", err)
	}
	if status != "succeeded" || endedAt == nil {
		t.Fatalf("completed attempt = status %q ended_at %v, want succeeded with end time", status, endedAt)
	}
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

func TestAppendTraceStepCommitsTraceAndReplaysMatchingPayload(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "trace this",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	})
	if err != nil {
		t.Fatalf("create message and run: %v", err)
	}
	if _, ok, err := store.ClaimRun(ctx, run.ID, 0); err != nil || !ok {
		t.Fatalf("claim run = (ok=%v, err=%v)", ok, err)
	}

	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	step := TraceStep{
		ExpectedCheckpoint: 1,
		Span: domain.Span{
			ID:        "spn_test",
			RunID:     run.ID,
			TraceID:   "trc_test",
			SpanID:    "model-1",
			Kind:      domain.SpanModel,
			Name:      "Plan next action",
			Status:    "succeeded",
			Input:     json.RawMessage(`{"prompt":"trace this"}`),
			Output:    json.RawMessage(`{"content":"done"}`),
			StartedAt: now,
			EndedAt:   &now,
		},
		Attempt: &TraceAttempt{
			ID:            "att_test",
			AttemptNumber: 1,
			Provider:      "scripted",
			Status:        "succeeded",
			StartedAt:     now,
			EndedAt:       &now,
		},
		Usage: &TraceUsage{
			ID:       "use_test",
			Provider: "scripted",
			Model:    "browser-eval-fixture",
			Usage:    usage.Call(10, 5, 0, 0),
			Details:  json.RawMessage(`{"source":"test"}`),
		},
		Operation: &TraceOperationResult{
			ID:          "opr_test",
			OperationID: "op_test",
			Status:      "succeeded",
			Result:      json.RawMessage(`{"verified":true}`),
			Artifacts:   json.RawMessage(`[]`),
		},
		Event: event.Envelope[json.RawMessage]{
			SchemaVersion: "agenttrace.event.v1",
			EventID:       "evt_trace_test",
			EventType:     "span.completed",
			AggregateID:   run.ID,
			OccurredAt:    now,
			CorrelationID: run.ID,
			Payload:       json.RawMessage(`{"span_id":"model-1"}`),
		},
	}

	if err := store.AppendTraceStep(ctx, step); err != nil {
		t.Fatalf("append trace step: %v", err)
	}
	if err := store.AppendTraceStep(ctx, step); err != nil {
		t.Fatalf("replay trace step: %v", err)
	}
	assertRowCount(t, ctx, pool, "spans", 1)
	assertRowCount(t, ctx, pool, "span_attempts", 1)
	assertRowCount(t, ctx, pool, "usage_records", 1)
	assertRowCount(t, ctx, pool, "operation_results", 1)
	assertRowCount(t, ctx, pool, "run_events", 1)
	assertRowCount(t, ctx, pool, "outbox_events", 2)

	trace, err := store.GetTraceDetails(ctx, run.ID)
	if err != nil {
		t.Fatalf("get trace details: %v", err)
	}
	if len(trace.Spans) != 1 || trace.Spans[0].SpanID != "model-1" {
		t.Fatalf("trace spans = %#v", trace.Spans)
	}
	if len(trace.Attempts) != 1 ||
		trace.Attempts[0].SpanID != "model-1" ||
		trace.Attempts[0].ID != "att_test" {
		t.Fatalf("trace attempts = %#v", trace.Attempts)
	}
	if len(trace.Usage) != 1 ||
		trace.Usage[0].SpanID != "model-1" ||
		trace.Usage[0].TotalTokens != 15 {
		t.Fatalf("trace usage = %#v", trace.Usage)
	}
	if len(trace.Operations) != 1 ||
		trace.Operations[0].SpanID != "model-1" ||
		string(trace.Operations[0].Result) != `{"verified": true}` {
		t.Fatalf("trace operations = %#v", trace.Operations)
	}

	var checkpoint int64
	if err := pool.QueryRow(ctx, `SELECT checkpoint_version FROM runs WHERE id = $1`, run.ID).Scan(&checkpoint); err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if checkpoint != 2 {
		t.Fatalf("checkpoint = %d, want 2", checkpoint)
	}
}

func TestAppendTraceStepRollsBackWhenOutboxInsertFails(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "trace rollback",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	})
	if err != nil {
		t.Fatalf("create message and run: %v", err)
	}
	if _, ok, err := store.ClaimRun(ctx, run.ID, 0); err != nil || !ok {
		t.Fatalf("claim run = (ok=%v, err=%v)", ok, err)
	}
	if _, err := pool.Exec(ctx, `
		ALTER TABLE outbox_events
		ADD CONSTRAINT reject_test_trace_outbox_write CHECK (event_id <> 'evt_trace_test')`); err != nil {
		t.Fatalf("reject trace outbox write: %v", err)
	}

	if err := store.AppendTraceStep(ctx, traceStepFixture(run.ID)); err == nil {
		t.Fatal("expected trace outbox failure")
	}
	assertRowCount(t, ctx, pool, "spans", 0)
	assertRowCount(t, ctx, pool, "span_attempts", 0)
	assertRowCount(t, ctx, pool, "usage_records", 0)
	assertRowCount(t, ctx, pool, "operation_results", 0)
	assertRowCount(t, ctx, pool, "run_events", 0)

	var checkpoint int64
	if err := pool.QueryRow(ctx, `SELECT checkpoint_version FROM runs WHERE id = $1`, run.ID).Scan(&checkpoint); err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if checkpoint != 1 {
		t.Fatalf("checkpoint = %d, want 1 after rollback", checkpoint)
	}
}

func TestAppendTraceStepRejectsMismatchedReplayPayload(t *testing.T) {
	ctx, _, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, store.pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "trace this",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	})
	if err != nil {
		t.Fatalf("create message and run: %v", err)
	}
	if _, ok, err := store.ClaimRun(ctx, run.ID, 0); err != nil || !ok {
		t.Fatalf("claim run = (ok=%v, err=%v)", ok, err)
	}

	step := traceStepFixture(run.ID)
	if err := store.AppendTraceStep(ctx, step); err != nil {
		t.Fatalf("append trace step: %v", err)
	}
	step.Event.Payload = json.RawMessage(`{"span_id":"different"}`)
	if err := store.AppendTraceStep(ctx, step); !errors.Is(err, ErrConflict) {
		t.Fatalf("mismatched trace replay error = %v, want ErrConflict", err)
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

	message, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "failed rerun source",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		t.Fatalf("create failed rerun source: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE runs SET status = 'failed' WHERE id = $1`, run.ID); err != nil {
		t.Fatalf("mark rerun source failed: %v", err)
	}
	run.Status = domain.RunFailed
	return message, run
}

func assertOperationInputSchema(t *testing.T, raw json.RawMessage) {
	t.Helper()

	var schema struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode operation input schema: %v", err)
	}
	if schema.Type != "object" {
		t.Fatalf("operation input schema = %s, want type object", raw)
	}
}

func traceStepFixture(runID string) TraceStep {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	return TraceStep{
		ExpectedCheckpoint: 1,
		Span: domain.Span{
			ID:        "spn_test",
			RunID:     runID,
			TraceID:   "trc_test",
			SpanID:    "model-1",
			Kind:      domain.SpanModel,
			Name:      "Plan next action",
			Status:    "succeeded",
			StartedAt: now,
		},
		Event: event.Envelope[json.RawMessage]{
			SchemaVersion: "agenttrace.event.v1",
			EventID:       "evt_trace_test",
			EventType:     "span.completed",
			AggregateID:   runID,
			OccurredAt:    now,
			CorrelationID: runID,
			Payload:       json.RawMessage(`{"span_id":"model-1"}`),
		},
	}
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
