// Package execution defines SDK-free, immutable runtime and Harness contracts.
package execution

import (
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"strings"
)

const BuiltinHarnessID = "browser-harness"

type Spec struct {
	RuntimeID string          `json:"runtime_id"`
	Harness   HarnessSnapshot `json:"harness"`
}

type HarnessSnapshot struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Revision      int                  `json:"revision"`
	SystemPrompt  string               `json:"system_prompt"`
	Skills        []Skill              `json:"skills"`
	MCP           map[string]MCPServer `json:"mcp"`
	ProjectID     string               `json:"project_id,omitempty"`
	VersionID     string               `json:"version_id,omitempty"`
	ContentDigest string               `json:"content_digest,omitempty"`
	RuntimeIDs    []string             `json:"runtime_ids,omitempty"`
}

type Skill struct {
	Name          string      `json:"name"`
	Content       string      `json:"content"`
	Files         []SkillFile `json:"files,omitempty"`
	VersionID     string      `json:"version_id,omitempty"`
	ContentDigest string      `json:"content_digest,omitempty"`
}

type SkillFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type MCPServer struct {
	Capability    string            `json:"capability"`
	Config        map[string]any    `json:"config"`
	SecretRefs    map[string]string `json:"secret_refs"`
	VersionID     string            `json:"version_id,omitempty"`
	ContentDigest string            `json:"content_digest,omitempty"`
}

type Model struct {
	Provider        string `json:"provider"`
	Name            string `json:"name"`
	ReasoningEffort string `json:"reasoning_effort"`
}

type Runtime struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Model             Model  `json:"model"`
	Available         bool   `json:"available"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

func KnownRuntime(id string) bool {
	return id == "pi" || id == "claude-code" || id == "codex"
}

func (s *Spec) Validate() error {
	if s == nil {
		return nil
	}
	if !KnownRuntime(s.RuntimeID) {
		return errors.New("unknown runtime_id")
	}
	if s.Harness.ID == "" || s.Harness.Revision < 1 || int64(s.Harness.Revision) > 9007199254740991 {
		return errors.New("Harness snapshot identity and revision are required")
	}
	if err := s.Harness.Validate(); err != nil {
		return err
	}
	if s.Harness.RuntimeIDs != nil {
		for _, id := range s.Harness.RuntimeIDs {
			if id == s.RuntimeID {
				return nil
			}
		}
		return errors.New("runtime is incompatible with Harness snapshot")
	}
	return nil
}

// Clone detaches all nested data before crossing an ownership boundary.
func (s *Spec) Clone() *Spec {
	if s == nil {
		return nil
	}
	copy := *s
	copy.Harness = s.Harness.Clone()
	return &copy
}

func (h HarnessSnapshot) Clone() HarnessSnapshot {
	raw, _ := json.Marshal(h)
	var copy HarnessSnapshot
	_ = json.Unmarshal(raw, &copy)
	if copy.Skills == nil {
		copy.Skills = []Skill{}
	}
	if copy.MCP == nil {
		copy.MCP = map[string]MCPServer{}
	}
	return copy
}

var resourceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var credentialName = regexp.MustCompile(`(?i)(TOKEN|KEY|SECRET|PASSWORD|AUTHORIZATION|COOKIE|CREDENTIAL)`)

func (h HarnessSnapshot) Validate() error {
	if strings.TrimSpace(h.Name) == "" {
		return errors.New("Harness name is required")
	}
	if h.ID != "" && !resourceName.MatchString(h.ID) {
		return errors.New("invalid Harness id")
	}
	if err := validateMetadata(h.ProjectID, h.VersionID, h.ContentDigest); err != nil {
		return err
	}
	if h.RuntimeIDs != nil {
		if len(h.RuntimeIDs) == 0 {
			return errors.New("Harness runtime_ids must not be empty")
		}
		runtimes := map[string]bool{}
		for _, id := range h.RuntimeIDs {
			if !KnownRuntime(id) || runtimes[id] {
				return errors.New("Harness runtime_ids must be unique known runtimes")
			}
			runtimes[id] = true
		}
	}
	names := map[string]bool{}
	for _, skill := range h.Skills {
		if err := validateMetadata("", skill.VersionID, skill.ContentDigest); err != nil {
			return err
		}
		key := strings.ToLower(skill.Name)
		if !resourceName.MatchString(skill.Name) || names[key] {
			return errors.New("skill names must be unique scoped names")
		}
		names[key] = true
		files := map[string]bool{"skill.md": true}
		for _, file := range skill.Files {
			key := strings.ToLower(file.Path)
			if !SafeSkillPath(file.Path) || files[key] {
				return errors.New("skill files must have unique scoped relative paths")
			}
			files[key] = true
		}
		for name := range files {
			for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
				if files[parent] {
					return errors.New("skill files must not collide with parent paths")
				}
			}
		}
	}
	servers := map[string]bool{}
	for name, server := range h.MCP {
		key, valid := normalizedMCPName(name)
		if !valid || servers[key] {
			return errors.New("MCP server names must be unique scoped names")
		}
		servers[key] = true
		if err := server.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func SafeSkillPath(value string) bool {
	for _, r := range value {
		if r < 32 {
			return false
		}
	}
	return value != "" && !strings.ContainsAny(value, "\\:\x00") && !path.IsAbs(value) &&
		path.Clean(value) == value && value != "." && value != ".." && !strings.HasPrefix(value, "../")
}

func normalizedMCPName(name string) (string, bool) {
	normalized := strings.NewReplacer(".", "_", "-", "_").Replace(name)
	if !resourceName.MatchString(name) || strings.Contains(normalized, "__") || reservedName(name) {
		return "", false
	}
	return strings.ToLower(normalized), true
}

func reservedName(name string) bool {
	return name == "__proto__" || name == "constructor" || name == "prototype"
}
