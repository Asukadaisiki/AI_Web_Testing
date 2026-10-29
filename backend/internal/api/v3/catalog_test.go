package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/agentprofile"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type catalogRepository struct {
	fakeRepository
	harness execution.HarnessSnapshot
}

func (r *catalogRepository) ListHarnesses(context.Context) ([]execution.HarnessSnapshot, error) {
	return []execution.HarnessSnapshot{r.harness}, nil
}
func (r *catalogRepository) GetHarness(_ context.Context, id string) (execution.HarnessSnapshot, error) {
	if id != r.harness.ID {
		return execution.HarnessSnapshot{}, postgres.ErrNotFound
	}
	return r.harness, nil
}
func (r *catalogRepository) CreateHarness(_ context.Context, h execution.HarnessSnapshot) (execution.HarnessSnapshot, error) {
	h.ID, h.Revision = "created", 1
	r.harness = h
	return h, nil
}
func (r *catalogRepository) UpdateHarness(_ context.Context, id string, h execution.HarnessSnapshot) (execution.HarnessSnapshot, error) {
	h.ID, h.Revision = id, r.harness.Revision+1
	r.harness = h
	return h, nil
}
func (r *catalogRepository) DeleteHarness(context.Context, string) (execution.HarnessSnapshot, error) {
	return r.harness, nil
}

func TestRuntimeSelectionUsesServerModelAndHarness(t *testing.T) {
	profile, _ := agentprofile.LoadBrowserEval()
	repository := &catalogRepository{harness: execution.HarnessSnapshot{
		ID: "generic", Name: "Generic", Revision: 2, SystemPrompt: "original",
		Skills: []execution.Skill{}, MCP: map[string]execution.MCPServer{},
	}}
	runtimes := []execution.Runtime{
		{ID: "pi", Name: "Pi", Available: true, Model: execution.Model{Provider: "openai-compatible", Name: "pi-model", ReasoningEffort: "high"}},
		{ID: "claude-code", Name: "Claude Code", Available: true, Model: execution.Model{Provider: "anthropic-compatible", Name: "glm-5.3-flash", ReasoningEffort: "high"}},
		{ID: "codex", Name: "Codex", Available: true, Model: execution.Model{Provider: "openai-responses", Name: "codex-model", ReasoningEffort: "high"}},
	}
	handler := New(ServerConfig{Repository: repository, Profile: profile, Runtimes: runtimes}).Handler()
	for _, rt := range runtimes {
		body, _ := json.Marshal(map[string]any{"content": "hello", "runtime_id": rt.ID, "harness_id": "generic", "agent_id": rt.ID, "model": rt.Model, "idempotency_key": rt.ID})
		response := request(t, handler, http.MethodPost, "/api/v1/conversations/conv_1/messages", string(body))
		if response.Code != http.StatusAccepted {
			t.Fatalf("selected %s status %d: %s", rt.ID, response.Code, response.Body.String())
		}
		if repository.message.Execution == nil || repository.message.Execution.RuntimeID != rt.ID || repository.message.Model.Name != rt.Model.Name {
			t.Fatal("runtime selection did not reach persistence")
		}
		repository.message.Execution.Harness.SystemPrompt = "client mutation"
		if repository.harness.SystemPrompt != "original" {
			t.Fatal("API handed repository-owned data across boundary")
		}
	}
	for _, agentID := range []string{"", "browser-eval", "codex"} {
		body, _ := json.Marshal(map[string]any{
			"content": "hello", "runtime_id": "codex", "harness_id": "generic",
			"agent_id": agentID, "idempotency_key": "server-owned-model",
		})
		response := request(t, handler, http.MethodPost, "/api/v1/conversations/conv_1/messages", string(body))
		if response.Code != http.StatusAccepted || repository.message.AgentID != "codex" || repository.message.Model.Name != "codex-model" {
			t.Fatalf("runtime must authoritatively select server-owned model: %d %s", response.Code, response.Body.String())
		}
	}
	for _, tc := range []struct{ body, code string }{
		{`{"runtime_id":"unknown","harness_id":"generic"}`, "unknown_runtime"},
		{`{"runtime_id":"pi","harness_id":"unknown"}`, "unknown_harness"},
		{`{"runtime_id":"pi","harness_id":"generic","model":{"provider":"openai-compatible","name":"unapproved"}}`, "model_not_available"},
		{`{"harness_id":"generic"}`, "invalid_request"},
	} {
		var body map[string]any
		_ = json.Unmarshal([]byte(tc.body), &body)
		body["content"], body["idempotency_key"] = "hello", "reject"
		raw, _ := json.Marshal(body)
		response := request(t, handler, http.MethodPost, "/api/v1/conversations/conv_1/messages", string(raw))
		assertErrorCode(t, response, http.StatusBadRequest, tc.code)
	}
	response := request(t, handler, http.MethodGet, "/api/v1/runtimes", "")
	var list struct {
		Runtimes []execution.Runtime `json:"runtimes"`
	}
	decodeJSON(t, response, &list)
	if len(list.Runtimes) != 3 {
		t.Fatal("missing runtime catalog")
	}
	response = request(t, handler, http.MethodGet, "/api/v1/harnesses", "")
	var harnessList struct {
		Harnesses []execution.HarnessSnapshot `json:"harnesses"`
	}
	decodeJSON(t, response, &harnessList)
	if len(harnessList.Harnesses) != 1 || harnessList.Harnesses[0].ID != "generic" {
		t.Fatal("missing Harness catalog")
	}
	runtimes[2].Available = false
	handler = New(ServerConfig{Repository: repository, Profile: profile, Runtimes: runtimes}).Handler()
	response = request(t, handler, http.MethodPost, "/api/v1/conversations/conv_1/messages", `{"content":"hello","runtime_id":"codex","harness_id":"generic","idempotency_key":"unavailable"}`)
	assertErrorCode(t, response, http.StatusBadRequest, "runtime_unavailable")
}

func TestHarnessCRUDReturnsSnapshotsAndRunExposesOnlyIdentity(t *testing.T) {
	repository := &catalogRepository{}
	handler := New(ServerConfig{Repository: repository}).Handler()
	body := `{"name":"Generic","system_prompt":"hello","skills":[],"mcp":{}}`
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/api/v1/harnesses", http.StatusCreated},
		{http.MethodPut, "/api/v1/harnesses/created", http.StatusOK},
		{http.MethodGet, "/api/v1/harnesses/created", http.StatusOK},
		{http.MethodDelete, "/api/v1/harnesses/created", http.StatusOK},
	} {
		response := request(t, handler, tc.method, tc.path, body)
		if response.Code != tc.status {
			t.Fatalf("%s status %d: %s", tc.method, response.Code, response.Body.String())
		}
		var h execution.HarnessSnapshot
		decodeJSON(t, response, &h)
		if h.ID != "created" {
			t.Fatal("CRUD did not return snapshot")
		}
	}
	raw, _ := json.Marshal(map[string]any{"execution": execution.Spec{RuntimeID: "codex", Harness: repository.harness}})
	public := toPublicRun(domain.Run{ID: "run", Specification: raw})
	if public.RuntimeID != "codex" || public.HarnessID != "created" || public.HarnessRevision != 2 {
		t.Fatal("public run missing immutable execution identity")
	}
}
