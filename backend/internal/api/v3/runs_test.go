package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/registry"
)

const privateRunSpecification = `{
	"agent_id":"private-agent",
	"providers":{"api_key":"private-provider-key"},
	"config":{"unknown":"private-root-config"},
	"command":{
		"model":{"provider":"openai-compatible","name":"frozen-model","reasoning_effort":"high",
			"api_key":"private-model-key","providers":{"url":"private-model-provider"}},
		"environment_id":"env_1","profile_generation":7,
		"worker_url":"http://private-worker.internal","artifact_scope":"private-artifact-scope",
		"fencing_token":123,"profile_ref":"private-profile",
		"messages":[{"content":"private-message"}],
		"operations":[{"callback":{"url":"private-callback"}}],
		"env":{"TOKEN":"private-command-token"}
	},
	"execution":{
		"runtime_id":"codex",
		"providers":{"api_key":"private-execution-key"},
		"harness":{
			"id":"ph_1","name":"Checkout","revision":3,"project_id":"project_1",
			"version_id":"hv_3","content_digest":"sha256:harness",
			"runtime_ids":["pi","codex"],
			"system_prompt":"private-system-prompt",
			"config":{"safe_looking_name":"private-harness-config"},
			"skills":[{"name":"checkout","version_id":"sv_2","content_digest":"sha256:skill",
				"content":"private-skill-content",
				"files":[{"path":"private-skill-path","content":"private-skill-file"}],
				"env":{"KEY":"private-skill-key"}}],
			"mcp":{"browser":{
				"capability":"browser-harness","version_id":"mv_4","content_digest":"sha256:mcp",
				"secret_refs":{"auth":"private-secret-ref"},
				"command":"/private/bin/runner","args":["private-command-argument"],
				"env":{"TOKEN":"private-mcp-token"},
				"providers":{"api_key":"private-mcp-provider"},
				"config":{"endpoint":"private-endpoint","label":"private-arbitrary-config",
					"nested":{"password":"private-config-password"}}
			}}
		}
	}
}`

const expectedPublicHarness = `{
	"harness_id":"ph_1","harness_version_id":"hv_3","revision":3,
	"name":"Checkout","project_id":"project_1","content_digest":"sha256:harness",
	"runtime_ids":["pi","codex"],
	"skill_version_ids":["sv_2"],"mcp_binding_ids":[],
	"runtime_id":"codex","environment_id":"env_1",
	"skills":[{"name":"checkout","version_id":"sv_2","content_digest":"sha256:skill"}],
	"mcp":{"browser":{"capability":"browser-harness","version_id":"mv_4","content_digest":"sha256:mcp"}}
}`

func frozenPublicRunFixture() domain.Run {
	when := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	return domain.Run{
		ID: "run_1", ProjectID: "project_1", ConversationID: "conv_1",
		Status: domain.RunSucceeded, CreatedAt: when, UpdatedAt: when,
		Specification: json.RawMessage(privateRunSpecification),
	}
}

func publicRunFromResponse(t *testing.T, handler http.Handler, path string) map[string]json.RawMessage {
	t.Helper()
	response := managementRequest(handler, http.MethodGet, path, "", "member")
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, response.Code, response.Body)
	}
	for _, forbidden := range []string{
		"private-", "/private/", `"specification"`, `"command"`, `"secret_refs"`,
		`"config"`, `"env"`, `"providers"`, `"api_key"`, `"system_prompt"`,
		`"content"`, `"files"`, `"worker_url"`, `"fencing_token"`,
	} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("GET %s leaked %q: %s", path, forbidden, response.Body)
		}
	}
	var body map[string]json.RawMessage
	decodeJSON(t, response, &body)
	if runs, ok := body["runs"]; ok {
		var history []map[string]json.RawMessage
		if err := json.Unmarshal(runs, &history); err != nil || len(history) != 1 {
			t.Fatalf("GET %s history: %s (%v)", path, runs, err)
		}
		return history[0]
	}
	return body
}

func assertRunJSONField(t *testing.T, body map[string]json.RawMessage, field, expected string) {
	t.Helper()
	var got, want any
	raw, ok := body[field]
	if !ok {
		t.Errorf("public Run omitted %s", field)
		return
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %s, want %s", field, raw, expected)
	}
}

func TestPublicRunProjectionPrivacyAcrossEndpoints(t *testing.T) {
	repository := ownedManagementRepository()
	repository.run = frozenPublicRunFixture()
	repository.runs = []domain.Run{repository.run}
	handler := New(ServerConfig{
		Repository: repository, ManagementEnabled: true, BridgeToken: testBridgeToken,
	}).Handler()
	for _, path := range []string{
		"/api/v1/runs/run_1",
		"/api/v1/conversations/conv_1",
		"/api/v1/conversations/conv_1/runs",
	} {
		t.Run(path, func(t *testing.T) {
			body := publicRunFromResponse(t, handler, path)
			assertRunJSONField(t, body, "runtime_id", `"codex"`)
			assertRunJSONField(t, body, "harness_id", `"ph_1"`)
			assertRunJSONField(t, body, "harness_revision", `3`)
			assertRunJSONField(t, body, "model", `{"provider":"openai-compatible","name":"frozen-model","reasoning_effort":"high"}`)
			assertRunJSONField(t, body, "environment_id", `"env_1"`)
			assertRunJSONField(t, body, "profile_generation", `7`)
			assertRunJSONField(t, body, "harness_snapshot", expectedPublicHarness)
		})
	}
	if string(repository.run.Specification) != privateRunSpecification {
		t.Fatal("public projection mutated the stored specification")
	}
}

type publicRunRegistryRepository struct {
	*managementRepository
	RegistryRepository
	latest registry.HarnessVersion
}

func (r *publicRunRegistryRepository) PublishProjectHarness(_ context.Context, project, id string, v registry.HarnessVersion, actor string) (registry.HarnessVersion, error) {
	v.ID, v.HarnessID, v.ProjectID, v.CreatedBy = "hv_4", id, project, actor
	v.Version, v.ContentDigest = r.latest.Version+1, "sha256:new-harness"
	r.latest = v
	return v, nil
}

func (r *publicRunRegistryRepository) ListProjectHarnesses(context.Context, string) ([]registry.HarnessVersion, error) {
	return []registry.HarnessVersion{r.latest}, nil
}

func (r *publicRunRegistryRepository) ResolveProjectHarness(context.Context, string, string, string) (execution.HarnessSnapshot, error) {
	return execution.HarnessSnapshot{
		ID: r.latest.HarnessID, VersionID: r.latest.ID, Revision: r.latest.Version,
		Name: r.latest.Name, ContentDigest: r.latest.ContentDigest,
	}, nil
}

func TestPublicRunProjectionHistoryStaysFrozenAfterRegistryPublish(t *testing.T) {
	repository := &publicRunRegistryRepository{
		managementRepository: ownedManagementRepository(),
		latest: registry.HarnessVersion{
			ID: "hv_3", HarnessID: "ph_1", ProjectID: "project_1", Version: 3,
		},
	}
	repository.run = frozenPublicRunFixture()
	repository.runs = []domain.Run{repository.run}
	config := ServerConfig{
		Repository: repository, ManagementEnabled: true, BridgeToken: testBridgeToken,
	}
	handler := New(config).Handler()
	paths := []string{
		"/api/v1/runs/run_1", "/api/v1/conversations/conv_1", "/api/v1/conversations/conv_1/runs",
	}
	before := make([]map[string]json.RawMessage, len(paths))
	for i, path := range paths {
		before[i] = publicRunFromResponse(t, handler, path)
		assertRunJSONField(t, before[i], "harness_snapshot", expectedPublicHarness)
	}
	published := managementRequest(handler, http.MethodPost, "/api/v1/projects/project_1/harnesses/ph_1/versions",
		`{"name":"New checkout","system_prompt":"new prompt","runtime_ids":["pi"],"skill_version_ids":["sv_new"],"mcp_binding_ids":["binding_new"]}`, "project_admin")
	if published.Code != http.StatusCreated {
		t.Fatalf("publish new version: %d %s", published.Code, published.Body)
	}
	catalog := managementRequest(handler, http.MethodGet, "/api/v1/projects/project_1/harnesses", "", "member")
	var versions struct {
		Harnesses []registry.HarnessVersion `json:"harnesses"`
	}
	decodeJSON(t, catalog, &versions)
	if catalog.Code != http.StatusOK || len(versions.Harnesses) != 1 ||
		versions.Harnesses[0].ID != "hv_4" || versions.Harnesses[0].Version != 4 {
		t.Fatalf("new version not visible in registry: %d %s", catalog.Code, catalog.Body)
	}
	repository.environment.ProfileGeneration = 99
	config.Runtimes = []execution.Runtime{{
		ID: "codex", Model: execution.Model{Provider: "new-provider", Name: "new-model"},
	}}
	handler = New(config).Handler()
	for i, path := range paths {
		after := publicRunFromResponse(t, handler, path)
		if !reflect.DeepEqual(before[i], after) {
			t.Errorf("GET %s rewrote historical Run after registry/environment/model update", path)
		}
	}
}

func TestPublicRunProjectionLegacyFallbackAndEmptyResources(t *testing.T) {
	for _, tc := range []struct {
		name, specification, runtime, harness string
		revision                              string
	}{
		{"command fallback", `{"command":{"execution":{"runtime_id":"pi","harness":{"id":"legacy","name":"Legacy","revision":1}}}}`, "pi", "legacy", "1"},
		{"top-level precedence", `{"execution":{"runtime_id":"codex","harness":{"id":"top","revision":2}},"command":{"execution":{"runtime_id":"pi","harness":{"id":"nested","revision":1}}}}`, "codex", "top", "2"},
		{"null top-level fallback", `{"execution":null,"command":{"execution":{"runtime_id":"pi","harness":{"id":"legacy","revision":1}}}}`, "pi", "legacy", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(toPublicRun(domain.Run{Specification: json.RawMessage(tc.specification)}))
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			assertRunJSONField(t, body, "runtime_id", `"`+tc.runtime+`"`)
			assertRunJSONField(t, body, "harness_id", `"`+tc.harness+`"`)
			assertRunJSONField(t, body, "harness_revision", tc.revision)
			var snapshot map[string]json.RawMessage
			if err := json.Unmarshal(body["harness_snapshot"], &snapshot); err != nil {
				t.Fatalf("missing legacy public snapshot: %v", err)
			}
			for _, field := range []string{"runtime_ids", "skills", "skill_version_ids", "mcp_binding_ids"} {
				assertRunJSONField(t, snapshot, field, `[]`)
			}
			assertRunJSONField(t, snapshot, "mcp", `{}`)
			for _, field := range []string{"model", "environment_id", "profile_generation"} {
				if _, present := body[field]; present {
					t.Errorf("legacy Run fabricated %s", field)
				}
			}
		})
	}
}

func TestPublicRunProjectionIgnoresPrivatePayloadTypesAndPreservesZeroGeneration(t *testing.T) {
	raw, err := json.Marshal(toPublicRun(domain.Run{Specification: json.RawMessage(`{
		"command":{"model":{"provider":"public-provider","name":"public-model","api_key":{"anything":"private-key"}},
			"environment_id":"env_1","profile_generation":0},
		"execution":{"runtime_id":"pi","harness":{"id":"legacy","revision":1,
			"system_prompt":{"anything":"private-prompt"},"skills":[{"name":"skill","content":123,"files":false}],
			"mcp":{"browser":{"capability":"browser-harness","config":"private-config","secret_refs":123}}}}
	}`)}))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	assertRunJSONField(t, body, "model", `{"provider":"public-provider","name":"public-model"}`)
	assertRunJSONField(t, body, "profile_generation", `0`)
	assertRunJSONField(t, body, "runtime_id", `"pi"`)
	if !strings.Contains(string(body["harness_snapshot"]), `"capability":"browser-harness"`) || strings.Contains(string(raw), "private-") {
		t.Fatalf("private payload types interfered with public projection: %s", raw)
	}
}

func TestPublicRunProjectionAbsentOrMalformedSpecificationKeepsLegacyRun(t *testing.T) {
	for _, specification := range []string{
		"", "null", "{}", "{",
		`{"execution":{"runtime_id":"pi","harness":{"id":"partial","revision":1}},"command":{"profile_generation":"bad"}}`,
	} {
		t.Run(specification, func(t *testing.T) {
			run := frozenPublicRunFixture()
			run.Specification = json.RawMessage(specification)
			raw, err := json.Marshal(toPublicRun(run))
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			assertRunJSONField(t, body, "id", `"run_1"`)
			assertRunJSONField(t, body, "status", `"succeeded"`)
			for _, field := range []string{
				"runtime_id", "harness_id", "harness_revision", "harness_snapshot", "model", "environment_id", "profile_generation",
			} {
				if _, present := body[field]; present {
					t.Errorf("absent or malformed specification exposed partial %s: %s", field, raw)
				}
			}
		})
	}
}
