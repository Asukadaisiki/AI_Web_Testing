package testpostgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/registry"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PrepareManagedConversation supplies durable admission facts for persistence
// tests. Real publication and lifecycle transactions have their own suites.
func PrepareManagedConversation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, conversation string) string {
	t.Helper()
	if _, err := DatabaseName(pool.Config().ConnString()); err != nil {
		t.Fatal(err)
	}
	var project string
	if err := pool.QueryRow(ctx, `SELECT project_id FROM conversations WHERE id=$1`, conversation).Scan(&project); err != nil {
		t.Fatal(err)
	}
	for _, query := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO project_bindings(agenttrace_project_id,langfuse_project_id) VALUES($1,'lf_fixture_'||$1) ON CONFLICT DO NOTHING`, []any{project}},
		{`INSERT INTO environments(id,project_id,conversation_id,state) VALUES('env_fixture_'||$2,$1,$2,'ready') ON CONFLICT(conversation_id) DO UPDATE SET state='ready'`, []any{project, conversation}},
		{`DELETE FROM outbox_events WHERE key IN (SELECT id FROM environments WHERE conversation_id=$1) AND topic='agenttrace.environment.commands.v1'`, []any{conversation}},
		{`DELETE FROM environment_commands WHERE conversation_id=$1`, []any{conversation}},
	} {
		if _, err := pool.Exec(ctx, query.sql, query.args...); err != nil {
			t.Fatal(err)
		}
	}
	harnessID, versionID := "ph_fixture_"+project, "hv_fixture_"+project
	v := registry.HarnessVersion{ID: versionID, HarnessID: harnessID, ProjectID: project, Name: "Fixture",
		Version: 1, RuntimeIDs: []string{"pi", "claude-code", "codex"}, CreatedBy: "fixture"}
	snapshot := execution.HarnessSnapshot{ID: harnessID, Name: v.Name, Revision: 1, Skills: []execution.Skill{}, MCP: map[string]execution.MCPServer{}}
	manifest, _ := json.Marshal(v)
	content, _ := json.Marshal(snapshot)
	v.ContentDigest = registry.Digest(struct{ Manifest, Snapshot json.RawMessage }{manifest, content})
	snapshot.ProjectID, snapshot.VersionID, snapshot.ContentDigest, snapshot.RuntimeIDs = project, versionID, v.ContentDigest, v.RuntimeIDs
	content, _ = json.Marshal(snapshot)
	manifest, _ = json.Marshal(v)
	if _, err := pool.Exec(ctx, `INSERT INTO project_harnesses(id,project_id,name,created_by) VALUES($1,$2,'Fixture','fixture') ON CONFLICT DO NOTHING`, harnessID, project); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO harness_versions(id,harness_id,version,manifest,snapshot,content_digest,created_by)
		VALUES($1,$2,1,$3,$4,$5,'fixture') ON CONFLICT DO NOTHING`, versionID, harnessID, manifest, content, v.ContentDigest); err != nil {
		t.Fatal(err)
	}
	return versionID
}
