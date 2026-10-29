package v3

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
)

func TestRuntimeSelectionUsesServerModelAndRegistrySelector(t *testing.T) {
	repository := ownedManagementRepository()
	runtimes := []execution.Runtime{
		{ID: "pi", Available: true, Model: execution.Model{Provider: "scripted", Name: "fixture"}},
		{ID: "claude-code", Available: true, Model: execution.Model{Provider: "anthropic-compatible", Name: "claude"}},
		{ID: "codex", Available: true, Model: execution.Model{Provider: "openai-responses", Name: "codex"}},
	}
	handler := New(ServerConfig{Repository: repository, BridgeToken: testBridgeToken, Runtimes: runtimes}).Handler()
	for _, rt := range runtimes {
		body, _ := json.Marshal(map[string]any{"content": "hello", "runtime_id": rt.ID, "harness_id": "project-version", "idempotency_key": rt.ID})
		got := managementRequest(handler, "POST", "/api/v1/conversations/conv_1/messages", string(body), "member")
		if got.Code != http.StatusAccepted || repository.message.RuntimeID != rt.ID ||
			repository.message.HarnessVersionID != "project-version" || repository.message.Model.Name != rt.Model.Name {
			t.Fatalf("server-owned selection: %d %s params=%+v", got.Code, got.Body, repository.message)
		}
	}
	for _, tc := range []struct{ body, code string }{
		{`{"runtime_id":"unknown","harness_id":"version"}`, "unknown_runtime"},
		{`{"runtime_id":"pi","harness_id":"version","model":{"provider":"scripted","name":"unapproved"}}`, "model_not_available"},
		{`{"harness_id":"version"}`, "invalid_request"},
		{`{"runtime_id":"pi"}`, "invalid_request"},
		{`{"runtime_id":"pi","harness_id":"version","agent_id":"pi"}`, "invalid_request"},
	} {
		var body map[string]any
		_ = json.Unmarshal([]byte(tc.body), &body)
		body["content"], body["idempotency_key"] = "hello", "reject"
		raw, _ := json.Marshal(body)
		got := managementRequest(handler, "POST", "/api/v1/conversations/conv_1/messages", string(raw), "member")
		assertErrorCode(t, got, http.StatusBadRequest, tc.code)
	}
	got := managementRequest(handler, "GET", "/api/v1/runtimes", "", "member")
	var list struct {
		Runtimes []execution.Runtime `json:"runtimes"`
	}
	decodeJSON(t, got, &list)
	if len(list.Runtimes) != 3 {
		t.Fatal("missing SDK catalog")
	}
	runtimes[2].Available = false
	handler = New(ServerConfig{Repository: repository, BridgeToken: testBridgeToken, Runtimes: runtimes}).Handler()
	got = managementRequest(handler, "POST", "/api/v1/conversations/conv_1/messages",
		`{"content":"hello","runtime_id":"codex","harness_id":"version","idempotency_key":"unavailable"}`, "member")
	assertErrorCode(t, got, http.StatusBadRequest, "runtime_unavailable")
}

func TestHistoricalRunExposesOnlyExecutionIdentity(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"execution": execution.Spec{RuntimeID: "codex", Harness: execution.HarnessSnapshot{ID: "historical", Revision: 2}}})
	public := toPublicRun(domain.Run{ID: "run", Specification: raw})
	if public.RuntimeID != "codex" || public.HarnessID != "historical" || public.HarnessRevision != 2 {
		t.Fatal("public run missing historical execution identity")
	}
}
