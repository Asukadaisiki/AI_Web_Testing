package runtime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

var fixturePools sync.Map

func rememberFixturePool(t *testing.T, s *postgres.Store, pool *pgxpool.Pool) {
	fixturePools.Store(s, pool)
	t.Cleanup(func() { fixturePools.Delete(s) })
}

func managedProjectParams(p postgres.CreateProjectParams) postgres.CreateProjectParams {
	if p.LangfuseProjectID == "" {
		p.LangfuseProjectID = "lf_fixture_" + p.Name
	}
	return p
}

func managedMessageParams(t *testing.T, ctx context.Context, s *postgres.Store, p postgres.CreateMessageParams) postgres.CreateMessageParams {
	t.Helper()
	raw, ok := fixturePools.Load(s)
	if !ok {
		t.Fatal("missing isolated fixture pool")
	}
	pool := raw.(*pgxpool.Pool)
	p.RuntimeID = "pi"
	p.HarnessVersionID = testpostgres.PrepareManagedConversation(t, ctx, pool, p.ConversationID)
	for _, worker := range []postgres.WorkerRegistration{
		{ID: "runtime", Role: "runtime", Capacity: 8},
		{ID: "browser", Role: "browser", Capacity: 8, Endpoint: "http://assigned:8100"},
	} {
		if _, err := s.RegisterWorker(ctx, worker); err != nil {
			t.Fatal(err)
		}
	}
	env, err := s.GetConversationEnvironment(ctx, p.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BindEnvironmentWorker(ctx, env.ID, "browser"); err != nil {
		t.Fatal(err)
	}
	return p
}

func managedFixtureCommand(t *testing.T, ctx context.Context, s PostgresPiTraceStore, command event.RunCommand) event.RunCommand {
	t.Helper()
	run, err := s.Store.GetRun(ctx, command.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Command event.RunCommand `json:"command"`
	}
	if err := json.Unmarshal(run.Specification, &spec); err != nil {
		t.Fatal(err)
	}
	return spec.Command
}
