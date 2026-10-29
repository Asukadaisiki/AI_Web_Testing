package v3

import (
	"context"
	"net/http"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/registry"
)

type registryTestRepository struct {
	*managementRepository
	RegistryRepository
	published bool
}

func (r *registryTestRepository) PublishMCP(_ context.Context, v registry.MCPVersion, _ string) (registry.MCPVersion, error) {
	r.published = true
	v.ID = "mv_test"
	return v, nil
}
func (r *registryTestRepository) PublishSkill(_ context.Context, project, id, _ string) (registry.SkillVersion, error) {
	r.published = true
	return registry.SkillVersion{ID: id, ProjectID: project, Status: "published"}, nil
}

func TestManagementRegistryRoutesFailClosedAndRejectUnsafeMCP(t *testing.T) {
	repo := &registryTestRepository{managementRepository: ownedManagementRepository()}
	handler := New(ServerConfig{Repository: repo, ManagementEnabled: true, BridgeToken: testBridgeToken}).Handler()
	for _, tc := range []struct {
		method, path, body, role string
		want                     int
	}{
		{"POST", "/api/v1/mcp-definitions", `{"name":"bad","command":"/bin/sh","args":["-c","echo"],"env":{}}`, "system_admin", 400},
		{"POST", "/api/v1/mcp-definitions", `{"name":"browser-harness","capability":"browser-harness","transport":"stdio"}`, "member", 403},
		{"POST", "/api/v1/projects/project_1/skill-versions/sv_1/publish", `{}`, "member", 403},
		{"POST", "/api/v1/projects/foreign/skill-versions/sv_1/publish", `{}`, "admin", 403},
		{"POST", "/api/v1/projects/project_1/skills/uploads", `{"name":"bad","files":[{"path":"../SKILL.md","content":"x"}]}`, "member", 400},
	} {
		got := managementRequest(handler, tc.method, tc.path, tc.body, tc.role)
		if got.Code != tc.want || repo.published {
			t.Fatalf("%s: %d %s", tc.path, got.Code, got.Body)
		}
	}
	got := managementRequest(handler, "POST", "/api/v1/projects/project_1/skill-versions/sv_1/publish", "{}", "project_admin")
	if got.Code != 200 || !repo.published {
		t.Fatalf("project admin publish: %d %s", got.Code, got.Body)
	}
	unconfigured := New(ServerConfig{Repository: repo, ManagementEnabled: true}).Handler()
	got = request(t, unconfigured, http.MethodGet, "/api/v1/projects/project_1/skills", "")
	if got.Code != 503 {
		t.Fatalf("unauthenticated management %d", got.Code)
	}
}

func TestManagementPassesSelectorToTransactionalSnapshotResolution(t *testing.T) {
	repo := ownedManagementRepository()
	handler := New(ServerConfig{Repository: repo, ManagementEnabled: true, BridgeToken: testBridgeToken, Runtimes: []execution.Runtime{
		{ID: "pi", Available: true, Model: execution.Model{Provider: "scripted", Name: "fixture"}},
	}}).Handler()
	got := managementRequest(handler, "POST", "/api/v1/conversations/conv_1/messages", `{"content":"run","runtime_id":"pi","harness_id":"hv_1","idempotency_key":"k"}`, "member")
	if got.Code != 202 {
		t.Fatalf("submit %d %s", got.Code, got.Body)
	}
	if repo.message.RuntimeID != "pi" || repo.message.HarnessVersionID != "hv_1" || repo.message.Execution != nil {
		t.Fatalf("wrong transactional selector %+v", repo.message)
	}
}
