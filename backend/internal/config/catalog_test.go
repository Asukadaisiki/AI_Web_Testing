package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
)

func TestRuntimeCatalogHasPublicModelsAndLocalReadinessOnly(t *testing.T) {
	t.Setenv("AGENTTRACE_CLAUDE_MODEL", "glm-5.3-flash")
	t.Setenv("AGENTTRACE_CODEX_MODEL", "glm-5.3-flash")
	t.Setenv("AGENTTRACE_CLAUDE_API_KEY", "private-claude-key")
	t.Setenv("AGENTTRACE_CODEX_API_KEY", "private-codex-key")
	t.Setenv("AGENTTRACE_CLAUDE_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("AGENTTRACE_CODEX_BASE_URL", "http://127.0.0.1:1/v1")
	cfg := Config{
		Model:   Model{Provider: "openai-compatible", Name: "glm-5.3-flash", ReasoningEffort: "high", APIKey: "private-pi-key"},
		Harness: Harness{Command: "go", Entry: "config.go", Directory: "."},
	}
	runtimes := cfg.RuntimeCatalog()
	if len(runtimes) != 3 {
		t.Fatal("runtime catalog missing entries")
	}
	for _, rt := range runtimes {
		if !execution.KnownRuntime(rt.ID) || !rt.Available || rt.Model.Name != "glm-5.3-flash" {
			t.Fatalf("unexpected runtime catalog entry: %#v", rt)
		}
	}
	if runtimes[1].Model.Provider != "anthropic-compatible" || runtimes[2].Model.Provider != "openai-responses" {
		t.Fatal("runtime model provider mismatch")
	}
	raw, _ := json.Marshal(runtimes)
	if strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "127.0.0.1") {
		t.Fatal("runtime catalog leaked environment-only configuration")
	}
	cfg.Harness.Command = "nonexistent-agenttrace-executable"
	for _, rt := range cfg.RuntimeCatalog() {
		if rt.Available || rt.UnavailableReason == "" {
			t.Fatal("missing executable did not affect readiness")
		}
	}
}

func TestBuiltinHarnessConfigDefaultsAndOverrides(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "postgres://unused")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "unused:9092")
	t.Setenv("AGENTTRACE_BUILTIN_HARNESS_DIR", "")
	t.Setenv("AGENTTRACE_BUILTIN_MCP_COMMAND", "")
	t.Setenv("AGENTTRACE_BUILTIN_MCP_ENTRY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BuiltinHarnessDirectory != "../pi-browser-harness/resources" || cfg.BuiltinMCPCommand != "node" || cfg.BuiltinMCPEntry != "../pi-browser-harness/dist/mcp/server.js" {
		t.Fatal("builtin resource defaults are not native")
	}
	t.Setenv("AGENTTRACE_BUILTIN_HARNESS_DIR", "/resources")
	t.Setenv("AGENTTRACE_BUILTIN_MCP_ENTRY", "/compiled/server.js")
	cfg, err = Load()
	if err != nil || cfg.BuiltinHarnessDirectory != "/resources" || cfg.BuiltinMCPEntry != "/compiled/server.js" {
		t.Fatal("builtin resource overrides ignored")
	}
}

func TestRuntimeCatalogDoesNotAdvertiseUnsupportedCredentialFallbacks(t *testing.T) {
	cfg := Config{
		Model:   Model{Provider: "scripted"},
		Harness: Harness{Command: "go", Entry: "config.go", Directory: "."},
	}
	t.Setenv("AGENTTRACE_CLAUDE_API_KEY", "")
	t.Setenv("AGENTTRACE_CODEX_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "unsupported-fallback")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "unsupported-fallback")
	t.Setenv("OPENAI_API_KEY", "unsupported-fallback")
	t.Setenv("AGENTTRACE_CODEX_BASE_URL", "https://provider.example/v1")
	runtimes := cfg.RuntimeCatalog()
	if runtimes[1].Available || runtimes[2].Available {
		t.Fatal("catalog advertised credentials ignored by SDK adapters")
	}
	t.Setenv("AGENTTRACE_CODEX_API_KEY", "private-key")
	t.Setenv("AGENTTRACE_CODEX_BASE_URL", "")
	if cfg.RuntimeCatalog()[2].Available {
		t.Fatal("Codex advertised without required provider base URL")
	}
	t.Setenv("AGENTTRACE_CODEX_BASE_URL", "not-a-url")
	if cfg.RuntimeCatalog()[2].Available {
		t.Fatal("Codex advertised invalid provider base URL")
	}
}
