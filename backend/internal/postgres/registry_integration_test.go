package postgres

import (
	"errors"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/registry"
)

func TestRegistryImmutablePublicationAndProjectIsolation(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	p1, err := store.CreateProject(ctx, CreateProjectParams{Name: "one", LangfuseProjectID: "lf-one"})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := store.CreateProject(ctx, CreateProjectParams{Name: "two", LangfuseProjectID: "lf-two"})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := store.UploadSkill(ctx, p1.ID, "author", "login", []registry.File{{Path: "SKILL.md", Content: "Use this instruction."}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishSkill(ctx, p2.ID, draft.ID, "admin"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross project publish: %v", err)
	}
	published, err := store.PublishSkill(ctx, p1.ID, draft.ID, "admin")
	if err != nil || published.Status != "published" {
		t.Fatalf("publish: %v %#v", err, published)
	}
	if _, err := pool.Exec(ctx, `UPDATE skill_versions SET files='[]' WHERE id=$1`, draft.ID); err == nil {
		t.Fatal("published bytes mutable")
	}
	h, err := store.PublishProjectHarness(ctx, p1.ID, "", registry.HarnessVersion{Name: "Login", SystemPrompt: "prompt", SkillVersionIDs: []string{published.ID}, RuntimeIDs: []string{"pi", "claude-code", "codex"}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ResolveProjectHarness(ctx, p1.ID, h.ID, "pi")
	if err != nil || len(snapshot.Skills) != 1 || snapshot.Skills[0].Content != "Use this instruction." {
		t.Fatalf("resolve: %v %#v", err, snapshot)
	}
	if _, err := store.ResolveProjectHarness(ctx, p2.ID, h.ID, "pi"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross project resolve: %v", err)
	}
	if err := store.RevokeSkill(ctx, p1.ID, published.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveProjectHarness(ctx, p1.ID, h.ID, "pi"); !errors.Is(err, ErrConflict) {
		t.Fatalf("new execution of revoked resource allowed: %v", err)
	}
	if snapshot.Skills[0].Content != "Use this instruction." {
		t.Fatal("detached historical snapshot changed")
	}
}

func TestRegistryBrowserCapabilityMaterializesWithoutLaunchFields(t *testing.T) {
	ctx, _, store := newRepositoryTestStore(t)
	project, err := store.CreateProject(ctx, CreateProjectParams{Name: "browser", LangfuseProjectID: "lf-browser"})
	if err != nil {
		t.Fatal(err)
	}
	mcp, err := store.PublishMCP(ctx, registry.MCPVersion{Name: "browser-harness", Capability: "browser-harness", Transport: "stdio"}, "system")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := store.SaveMCPBinding(ctx, project.ID, "", "admin", registry.Binding{MCPVersionID: mcp.ID, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	h, err := store.PublishProjectHarness(ctx, project.ID, "", registry.HarnessVersion{Name: "Browser", MCPBindingIDs: []string{binding.ID}, RuntimeIDs: []string{"pi", "claude-code", "codex"}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, runtimeID := range []string{"pi", "claude-code", "codex"} {
		snapshot, err := store.ResolveProjectHarness(ctx, project.ID, h.ID, runtimeID)
		if err != nil || snapshot.MCP["browser-harness"].VersionID != mcp.ID {
			t.Fatalf("resolve %s: %v %+v", runtimeID, err, snapshot)
		}
	}
}
