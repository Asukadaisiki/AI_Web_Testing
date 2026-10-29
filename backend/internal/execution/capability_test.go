package execution

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMCPCapabilityWireContract(t *testing.T) {
	raw := `{"capability":"reviewed-tools","config":{"endpoint":"https://service.test","options":{"limit":3},"enabled":true},"secret_refs":{"access":"secret://project-1/access"},"version_id":"version-1","content_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	var server MCPServer
	if err := json.Unmarshal([]byte(raw), &server); err != nil {
		t.Fatal(err)
	}
	h := fixtureHarness()
	h.MCP = map[string]MCPServer{"review": server}
	if err := h.Validate(); err != nil {
		t.Fatalf("reviewed descriptor rejected: %v", err)
	}
	copy := h.Clone()
	encoded, err := json.Marshal(copy.MCP["review"])
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	_ = json.Unmarshal(encoded, &got)
	_ = json.Unmarshal([]byte(raw), &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("capability lost on snapshot clone: %s", encoded)
	}
}

func TestSnapshotOptionalMetadataAndRuntimeCompatibility(t *testing.T) {
	raw := `{"runtime_id":"pi","harness":{"id":"generic","name":"Generic","revision":1,"system_prompt":"","skills":[{"name":"notes","content":"notes","version_id":"skill-1","content_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],"mcp":{},"project_id":"project-1","version_id":"harness-1","content_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","runtime_ids":["pi","codex"]}}`
	var spec Spec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		t.Fatal(err)
	}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	copy := spec.Clone()
	encoded, _ := json.Marshal(copy)
	for _, key := range []string{`"project_id":"project-1"`, `"version_id":"harness-1"`, `"version_id":"skill-1"`, `"runtime_ids":["pi","codex"]`} {
		if !strings.Contains(string(encoded), key) {
			t.Fatalf("lost metadata %s", key)
		}
	}
	for _, bad := range []string{
		strings.Replace(raw, `"project-1"`, `"../project"`, 1),
		strings.Replace(raw, `"harness-1"`, `"../version"`, 1),
		strings.Replace(raw, `"skill-1"`, `"../version"`, 1),
		strings.Replace(raw, `"runtime_id":"pi"`, `"runtime_id":"claude-code"`, 1),
		strings.Replace(raw, `["pi","codex"]`, `["pi","pi"]`, 1),
		strings.Replace(raw, `["pi","codex"]`, `["unknown"]`, 1),
		strings.Replace(raw, `["pi","codex"]`, `[]`, 1),
		strings.Replace(raw, `"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`, `"not-a-digest"`, 1),
	} {
		var value Spec
		err := json.Unmarshal([]byte(bad), &value)
		if err == nil {
			err = value.Validate()
		}
		if err == nil {
			t.Fatal("invalid metadata or incompatible runtime accepted")
		}
	}
}

func TestMCPRejectsPublicUnsafeAndUnscopedFields(t *testing.T) {
	for _, raw := range []string{
		`{"command":"node","args":["server.js"]}`,
		`{"capability":"browser-harness","config":{},"secret_refs":{},"command":"node"}`,
		`{"capability":"browser-harness","config":{},"secret_refs":{},"args":[]}`,
		`{"capability":"browser-harness","config":{},"secret_refs":{},"env":{}}`,
		`{"capability":"browser-harness","config":{},"secret_refs":{},"host_only_tools":[]}`,
		`{"capability":"reviewed","config":{"access_token":"plaintext"},"secret_refs":{}}`,
		`{"capability":"reviewed","config":{"nested":{"value":"env:HOST_TOKEN"}},"secret_refs":{}}`,
		`{"capability":"reviewed","config":{},"secret_refs":{"access":"env:HOST_TOKEN"}}`,
		`{"capability":"reviewed","config":{},"secret_refs":{"access":"plaintext"}}`,
		`{"capability":"reviewed","config":{},"secret_refs":{"access":"secret://project-1/../access"}}`,
		`{"capability":"browser-harness","config":{"worker_url":"https://evil.test"},"secret_refs":{}}`,
		`{"capability":"browser-harness","config":{},"secret_refs":{"access":"secret://project-1/access"}}`,
		`{"capability":"reviewed","config":null,"secret_refs":{}}`,
		`{"capability":"reviewed","config":{},"secret_refs":null}`,
		`{"capability":"reviewed","config":{},"secret_refs":{},"version_id":""}`,
	} {
		t.Run(raw, func(t *testing.T) {
			var server MCPServer
			err := json.Unmarshal([]byte(raw), &server)
			if err == nil {
				h := fixtureHarness()
				h.MCP = map[string]MCPServer{"review": server}
				err = h.Validate()
			}
			if err == nil {
				t.Fatal("unsafe MCP snapshot accepted")
			}
			if strings.Contains(err.Error(), "plaintext") || strings.Contains(err.Error(), "HOST_TOKEN") {
				t.Fatal("validation leaked input")
			}
		})
	}
}
