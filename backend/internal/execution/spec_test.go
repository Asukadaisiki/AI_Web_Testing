package execution

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureHarness() HarnessSnapshot {
	return HarnessSnapshot{
		ID: "generic", Name: "Generic", Revision: 1, SystemPrompt: "Answer carefully",
		Skills: []Skill{{Name: "notes", Content: "instructions", Files: []SkillFile{{Path: "references/guide.md", Content: "guide"}}}},
		MCP:    map[string]MCPServer{"local": {Capability: "reviewed-tools", Config: map[string]any{"options": map[string]any{"limit": 3}}, SecretRefs: map[string]string{"access": "secret://project-1/access"}}},
	}
}

func TestSpecWireAndCloneIsolation(t *testing.T) {
	spec := &Spec{RuntimeID: "codex", Harness: fixtureHarness()}
	clone := spec.Clone()
	clone.Harness.Skills[0].Files[0].Content = "changed"
	server := clone.Harness.MCP["local"]
	server.Config["options"].(map[string]any)["limit"] = 9
	server.SecretRefs["access"] = "secret://project-1/changed"
	if spec.Harness.Skills[0].Files[0].Content != "guide" || spec.Harness.MCP["local"].Config["options"].(map[string]any)["limit"] != 3 || spec.Harness.MCP["local"].SecretRefs["access"] != "secret://project-1/access" {
		t.Fatal("clone mutated original snapshot")
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"runtime_id":"codex"`, `"system_prompt"`, `"skills"`, `"files"`, `"mcp"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("wire missing %s", key)
		}
	}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	spec.RuntimeID = "unknown"
	if err := spec.Validate(); err == nil {
		t.Fatal("unknown runtime accepted")
	}
	spec.RuntimeID = "codex"
	spec.Harness.Revision = 1 << 53
	if err := spec.Validate(); err == nil {
		t.Fatal("revision outside execution host safe integer range accepted")
	}
}

func TestHarnessRejectsUnsafePathsDuplicateNamesAndLiteralSecrets(t *testing.T) {
	for _, path := range []string{"../escape", "/absolute", "refs/../../escape", `refs\escape`, "a/../b", "./guide", "C:/file"} {
		h := fixtureHarness()
		h.Skills[0].Files[0].Path = path
		if err := h.Validate(); err == nil {
			t.Fatalf("accepted path %q", path)
		}
	}
	h := fixtureHarness()
	h.Skills = append(h.Skills, h.Skills[0])
	if err := h.Validate(); err == nil {
		t.Fatal("accepted duplicate skills")
	}
	h = fixtureHarness()
	server := h.MCP["local"]
	server.Config["API_KEY"] = "literal-credential"
	h.MCP["local"] = server
	if err := h.Validate(); err == nil {
		t.Fatal("accepted literal secret")
	}
}

func TestHarnessValidationMatchesExecutionHost(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*HarnessSnapshot)
	}{
		{"unscoped skill", func(h *HarnessSnapshot) { h.Skills[0].Name = "-notes" }},
		{"case skill alias", func(h *HarnessSnapshot) { h.Skills = append(h.Skills, Skill{Name: "NOTES"}) }},
		{"case file alias", func(h *HarnessSnapshot) {
			h.Skills[0].Files = append(h.Skills[0].Files, SkillFile{Path: "REFERENCES/GUIDE.MD"})
		}},
		{"implicit skill collision", func(h *HarnessSnapshot) { h.Skills[0].Files = []SkillFile{{Path: "skill.md"}} }},
		{"file parent collision", func(h *HarnessSnapshot) { h.Skills[0].Files = []SkillFile{{Path: "refs"}, {Path: "refs/guide.md"}} }},
		{"implicit file parent collision", func(h *HarnessSnapshot) { h.Skills[0].Files = []SkillFile{{Path: "SKILL.md/child"}} }},
		{"path control", func(h *HarnessSnapshot) { h.Skills[0].Files[0].Path = "refs/a\nb" }},
		{"path trailing slash", func(h *HarnessSnapshot) { h.Skills[0].Files[0].Path = "refs/" }},
		{"unscoped MCP", func(h *HarnessSnapshot) { h.MCP["-server"] = h.MCP["local"] }},
		{"dotted MCP alias", func(h *HarnessSnapshot) {
			h.MCP["a.b"] = h.MCP["local"]
			h.MCP["a_b"] = h.MCP["local"]
		}},
		{"MCP normalized alias", func(h *HarnessSnapshot) {
			h.MCP["a-b"] = h.MCP["local"]
			h.MCP["A_B"] = h.MCP["local"]
		}},
		{"MCP reserved name", func(h *HarnessSnapshot) { h.MCP["constructor"] = h.MCP["local"] }},
		{"MCP double underscore", func(h *HarnessSnapshot) { h.MCP["a__b"] = h.MCP["local"] }},
		{"config NUL", func(h *HarnessSnapshot) { h.MCP["local"].Config["value"] = "\x00" }},
		{"config reserved", func(h *HarnessSnapshot) { h.MCP["local"].Config["constructor"] = "value" }},
		{"embedded env reference", func(h *HarnessSnapshot) { h.MCP["local"].Config["value"] = "prefix env:TOKEN" }},
		{"literal authorization", func(h *HarnessSnapshot) { h.MCP["local"].Config["AUTHORIZATION"] = "literal" }},
		{"literal cookie", func(h *HarnessSnapshot) { h.MCP["local"].Config["COOKIE"] = "literal" }},
		{"literal credential", func(h *HarnessSnapshot) { h.MCP["local"].Config["CREDENTIAL"] = "literal" }},
		{"non JSON config", func(h *HarnessSnapshot) { h.MCP["local"].Config["value"] = make(chan int) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := fixtureHarness()
			tc.edit(&h)
			if err := h.Validate(); err == nil {
				t.Fatal("accepted resources rejected by execution host")
			}
		})
	}
	h := fixtureHarness()
	h.Skills[0].Name = "review.notes"
	h.MCP["my.server"] = h.MCP["local"]
	h.MCP["a-b"] = h.MCP["local"]
	h.MCP["empty"] = MCPServer{Capability: "reviewed-tools", Config: map[string]any{}, SecretRefs: map[string]string{}}
	if err := h.Validate(); err != nil {
		t.Fatalf("valid host configuration rejected: %v", err)
	}
}

func TestBuiltinHarnessReadsActualResourcesAndSupportingFiles(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "browser-case-authoring")
	if err := os.MkdirAll(filepath.Join(skillDir, "references"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(dir, "system-prompt.md"):            "actual prompt",
		filepath.Join(skillDir, "SKILL.md"):               "actual skill",
		filepath.Join(skillDir, "references", "guide.md"): "supporting content",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h, err := LoadBuiltinHarness(dir, "/untrusted/command", "/untrusted/server.js")
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != BuiltinHarnessID || h.SystemPrompt != "actual prompt" || h.Skills[0].Content != "actual skill" || h.Skills[0].Files[0].Content != "supporting content" {
		t.Fatalf("wrong builtin snapshot: %#v", h)
	}
	server := h.MCP["browser-harness"]
	if server.Capability != BuiltinHarnessID || len(server.Config) != 0 || len(server.SecretRefs) != 0 {
		t.Fatal("builtin MCP is not a capability-only snapshot")
	}
	raw, _ := json.Marshal(h)
	if strings.Contains(string(raw), "/untrusted/") || strings.Contains(string(raw), `"command"`) || strings.Contains(string(raw), `"env"`) {
		t.Fatal("builtin snapshot includes launch paths")
	}
}
