package stream

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
	liveredis "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live/redis"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testredis"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

func TestPostgreSQLReplaySurvivesRedisFlushDB(t *testing.T) {
	postgresDSN := os.Getenv("AGENTTRACE_TEST_POSTGRES_DSN")
	redisAddress := os.Getenv("AGENTTRACE_TEST_REDIS_ADDR")
	if postgresDSN == "" || redisAddress == "" {
		t.Skip("AGENTTRACE_TEST_POSTGRES_DSN and AGENTTRACE_TEST_REDIS_ADDR are required")
	}
	if _, err := testpostgres.DatabaseName(postgresDSN); err != nil {
		t.Fatal(err)
	}
	redisTarget, err := testredis.Target(redisAddress, os.Getenv("AGENTTRACE_TEST_REDIS_DB"))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, postgresDSN)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset postgres: %v", err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate postgres: %v", err)
	}
	insertRunEventsFixture(t, ctx, pool)

	rawRedis := goredis.NewClient(&goredis.Options{Addr: redisTarget.Address, DB: redisTarget.Database})
	t.Cleanup(func() { _ = rawRedis.Close() })
	if err := rawRedis.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	liveStore := liveredis.NewClient(rawRedis)
	if err := liveStore.Append(ctx, "run-1", live.LiveEvent{
		Sequence: 1, Type: "run.started", Data: json.RawMessage(`{"state":"running"}`),
	}); err != nil {
		t.Fatalf("append redis notice: %v", err)
	}
	if err := rawRedis.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}

	service := NewService(NewPostgreSQLReader(pool), liveStore, Config{ReadBlock: 10 * time.Millisecond})
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, streamErrs := service.Subscribe(streamCtx, "run-1", "0")

	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "1", Type: "run.started", Data: json.RawMessage(`{"state":"running"}`),
	})
	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "2", Type: "span.completed", Data: json.RawMessage(`{"span":"plan"}`),
	})
	assertNoStreamError(t, streamErrs)
}

func insertRunEventsFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (id, name) VALUES ('project-1', 'Stream test');
		INSERT INTO conversations (id, project_id, title) VALUES ('conversation-1', 'project-1', 'Stream test');
		INSERT INTO runs (id, project_id, conversation_id, status, specification)
		VALUES ('run-1', 'project-1', 'conversation-1', 'running', '{}'::jsonb);
		INSERT INTO run_events (id, run_id, seq, event_type, payload)
		VALUES
			('event-1', 'run-1', 1, 'run.started', '{"state":"running"}'::jsonb),
			('event-2', 'run-1', 2, 'span.completed', '{"span":"plan"}'::jsonb)`); err != nil {
		t.Fatalf("insert run event fixture: %v", err)
	}
}
