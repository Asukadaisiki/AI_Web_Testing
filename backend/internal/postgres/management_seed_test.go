package postgres

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/registry"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Never reset the caller's database or share the other agents' test DSN.
func seedTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("AGENTTRACE_SEED_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_SEED_TEST_POSTGRES_DSN is not configured")
	}
	if _, err := testpostgres.DatabaseName(dsn); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = "postgres"
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	id, err := newID("agenttrace_test_seed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{id}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg = cfg.Copy()
	cfg.ConnConfig.Database, cfg.MaxConns = id, 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{id}.Sanitize())
		admin.Close()
		if err != nil {
			t.Error(err)
		}
	})
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)
	store.SetManagementEnabled(true)
	if err := store.SetEnvironmentDefaultPolicy([]string{"127.0.0.1", "localhost"}); err != nil {
		t.Fatal(err)
	}
	return store
}

func seedBuiltin(t *testing.T) execution.HarnessSnapshot {
	t.Helper()
	builtin, err := execution.LoadBuiltinHarness(filepath.Join("..", "..", "..", "pi-browser-harness", "resources"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	return builtin
}

func TestManagementSeedRequiresManagementAndIdentity(t *testing.T) {
	for _, params := range []ManagementSeedParams{
		{}, {LangfuseProjectID: "lf"}, {ActorID: "seed"},
	} {
		if _, err := NewStore(nil).SeedManagement(context.Background(), params); err == nil {
			t.Fatal("unconfigured seed was accepted")
		}
	}
}

func TestManagementSeedRepeatedAndConcurrentPreservesVersions(t *testing.T) {
	s := seedTestStore(t)
	ctx := context.Background()
	p := ManagementSeedParams{LangfuseProjectID: "lf-seed", ActorID: "seed", Builtin: seedBuiltin(t)}
	first, err := s.SeedManagement(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	before := seedCounts(t, s)
	p.Builtin.SystemPrompt = "changed on disk; do not mutate the published seed"
	var group sync.WaitGroup
	for range 3 {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := s.SeedManagement(ctx, p)
			if err != nil {
				t.Error(err)
			} else if result.ProjectID != first.ProjectID || result.ConversationID != first.ConversationID || result.HarnessVersionID != first.HarnessVersionID {
				t.Error("seed recreated canonical identities")
			}
		}()
	}
	group.Wait()
	if after := seedCounts(t, s); string(before) != string(after) {
		t.Fatalf("repeated seed mutated ledger: %s -> %s", before, after)
	}
	for _, id := range []string{"pi", "claude-code", "codex"} {
		if _, err := s.ResolveProjectHarness(ctx, first.ProjectID, first.HarnessVersionID, id); err != nil {
			t.Fatal(err)
		}
	}
}

func seedCounts(t *testing.T, s *Store) []byte {
	t.Helper()
	var counts []byte
	err := s.pool.QueryRow(context.Background(), `SELECT json_build_array(
		(SELECT count(*) FROM projects), (SELECT count(*) FROM conversations),
		(SELECT count(*) FROM mcp_versions), (SELECT count(*) FROM skill_versions),
		(SELECT count(*) FROM harness_versions), (SELECT count(*) FROM registry_audit),
		(SELECT count(*) FROM environment_commands), (SELECT count(*) FROM outbox_events))`).Scan(&counts)
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

func TestManagementSeedConvertsLegacyWithoutLosingHistory(t *testing.T) {
	s := seedTestStore(t)
	ctx := context.Background()
	s.SetManagementEnabled(false)
	project, err := s.CreateProject(ctx, CreateProjectParams{Name: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := s.CreateConversation(ctx, CreateConversationParams{ProjectID: project.ID, Title: "original title"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `INSERT INTO messages(id,conversation_id,ordinal,role,content) VALUES('legacy-msg',$1,1,'user','keep history')`, conv.ID); err != nil {
		t.Fatal(err)
	}
	s.SetManagementEnabled(true)
	p := ManagementSeedParams{LangfuseProjectID: "lf-legacy", ProjectID: project.ID, ConversationID: conv.ID, ActorID: "seed", Builtin: seedBuiltin(t)}
	result, err := s.SeedManagement(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	original, messages, _, err := s.GetConversation(ctx, conv.ID)
	if err != nil || original.Title != conv.Title || len(messages) != 1 || messages[0].Content != "keep history" {
		t.Fatalf("lost history: %+v %+v %v", original, messages, err)
	}
	env, err := s.GetEnvironment(ctx, result.EnvironmentID)
	if err != nil || env.State != "provisioning" {
		t.Fatalf("missing provision: %+v %v", env, err)
	}
	var policy struct {
		AllowedDomains []string `json:"allowed_domains"`
	}
	if err = json.Unmarshal(env.Policy, &policy); err != nil || len(policy.AllowedDomains) != 2 || env.FencingToken < 1 {
		t.Fatalf("seed lost server policy/fence: %+v %s", env, env.Policy)
	}
	var command map[string]any
	var raw []byte
	if err = s.pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE key=$1`, env.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &command); err != nil {
		t.Fatal(err)
	}
	if command["event_type"] != "environment.provision_requested" {
		t.Fatal(command)
	}
	p.LangfuseProjectID = "different-lf"
	if _, err = s.SeedManagement(ctx, p); err == nil {
		t.Fatal("seed rebound an existing project")
	}
}

func TestManagementSeedSharesOnlyDeployedMCPAcrossProjects(t *testing.T) {
	s := seedTestStore(t)
	ctx := context.Background()
	p := ManagementSeedParams{LangfuseProjectID: "lf-one", ActorID: "seed", Builtin: seedBuiltin(t)}
	a, err := s.SeedManagement(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	p.LangfuseProjectID = "lf-two"
	b, err := s.SeedManagement(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if a.MCPVersionID != b.MCPVersionID || a.MCPBindingID == b.MCPBindingID ||
		a.HarnessVersionID == b.HarnessVersionID || a.EnvironmentID == b.EnvironmentID ||
		len(a.SkillVersionIDs) == 0 || a.SkillVersionIDs[0] == b.SkillVersionIDs[0] {
		t.Fatalf("incorrect project namespace: %+v %+v", a, b)
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM mcp_versions`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("builtin MCP published more than once: %d %v", n, err)
	}
}

func TestManagementSeedDoesNotFollowLaterRegistryPublication(t *testing.T) {
	s := seedTestStore(t)
	ctx := context.Background()
	p := ManagementSeedParams{LangfuseProjectID: "lf-pinned", ActorID: "seed", Builtin: seedBuiltin(t)}
	first, err := s.SeedManagement(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishMCP(ctx, registry.MCPVersion{Name: "browser-harness", Capability: "browser-harness", Transport: "stdio"}, "operator"); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM project_mcp_bindings`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	repeat, err := s.SeedManagement(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	var after int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM project_mcp_bindings`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if repeat.MCPVersionID != first.MCPVersionID || repeat.MCPBindingID != first.MCPBindingID || before != after {
		t.Fatalf("seed followed a new mutable selection: %+v -> %+v, bindings %d -> %d", first, repeat, before, after)
	}
}
