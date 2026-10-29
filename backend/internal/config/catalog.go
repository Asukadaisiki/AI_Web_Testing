package config

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
)

const DefaultReasoningEffort = "high"

// RuntimeCatalog reports public model configuration and local readiness, not provider reachability.
func (c Config) RuntimeCatalog() []execution.Runtime {
	reasoning := c.Model.ReasoningEffort
	if reasoning == "" {
		reasoning = DefaultReasoningEffort
	}
	piModel := execution.Model{Provider: c.Model.Provider, Name: c.Model.Name, ReasoningEffort: reasoning}
	if piModel.Provider == "scripted" {
		piModel.Name = "browser-eval-fixture"
	}
	runtimes := []execution.Runtime{
		{ID: "pi", Name: "Pi", Model: piModel, Available: c.Model.APIKey != "" || c.Model.Provider == "scripted"},
		{ID: "claude-code", Name: "Claude Code", Model: execution.Model{
			Provider: "anthropic-compatible", Name: envOr("AGENTTRACE_CLAUDE_MODEL", "glm-5.3-flash"),
			ReasoningEffort: envOr("AGENTTRACE_CLAUDE_REASONING_EFFORT", reasoning),
		}, Available: environmentPresent("AGENTTRACE_CLAUDE_API_KEY") &&
			(!environmentPresent("AGENTTRACE_CLAUDE_BASE_URL") || validProviderURL(os.Getenv("AGENTTRACE_CLAUDE_BASE_URL")))},
		{ID: "codex", Name: "Codex", Model: execution.Model{
			Provider: "openai-responses", Name: envOr("AGENTTRACE_CODEX_MODEL", "glm-5.3-flash"),
			ReasoningEffort: envOr("AGENTTRACE_CODEX_REASONING_EFFORT", reasoning),
		}, Available: environmentPresent("AGENTTRACE_CODEX_API_KEY") && validProviderURL(os.Getenv("AGENTTRACE_CODEX_BASE_URL"))},
	}
	_, executableErr := exec.LookPath(c.Harness.Command)
	entry := c.Harness.Entry
	if !filepath.IsAbs(entry) {
		entry = filepath.Join(c.Harness.Directory, entry)
	}
	entryInfo, entryErr := os.Stat(entry)
	hostReady := executableErr == nil && entryErr == nil && entryInfo.Mode().IsRegular()
	for i := range runtimes {
		if !hostReady {
			runtimes[i].Available = false
			runtimes[i].UnavailableReason = "execution host executable or entry is unavailable"
		} else if !runtimes[i].Available {
			runtimes[i].UnavailableReason = "runtime credentials or provider configuration are incomplete"
		} else if runtimes[i].Model.Provider == "" || runtimes[i].Model.Name == "" {
			runtimes[i].Available = false
			runtimes[i].UnavailableReason = "runtime model is not configured"
		}
	}
	return runtimes
}

func environmentPresent(name string) bool {
	return strings.TrimSpace(os.Getenv(name)) != ""
}

func validProviderURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Host != "" && parsed.User == nil && (parsed.Scheme == "http" || parsed.Scheme == "https")
}
