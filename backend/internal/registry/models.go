// Package registry validates project content and system-reviewed executable resources.
package registry

import "time"

type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type SkillVersion struct {
	ID            string     `json:"id"`
	SkillID       string     `json:"skill_id"`
	ProjectID     string     `json:"project_id"`
	Name          string     `json:"name"`
	Version       int        `json:"version"`
	Status        string     `json:"status"`
	Files         []File     `json:"files"`
	ContentDigest string     `json:"content_digest"`
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
}

type Field struct {
	Type string   `json:"type"`
	Enum []string `json:"enum,omitempty"`
}

// Schema deliberately supports a bounded subset; unsupported schema constructs fail closed.
type Schema struct {
	Properties map[string]Field `json:"properties"`
	Required   []string         `json:"required,omitempty"`
}

type MCPVersion struct {
	ID            string     `json:"id"`
	DefinitionID  string     `json:"mcp_definition_id"`
	Name          string     `json:"name"`
	Version       int        `json:"version"`
	Capability    string     `json:"capability"`
	Transport     string     `json:"transport"`
	ConfigSchema  Schema     `json:"config_schema"`
	SecretSchema  Schema     `json:"secret_schema"`
	DeclaredTools []string   `json:"declared_tools"`
	HostOnlyTools []string   `json:"host_only_tools"`
	ContentDigest string     `json:"content_digest"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
}

type Binding struct {
	ID           string            `json:"id"`
	ProjectID    string            `json:"project_id"`
	MCPVersionID string            `json:"mcp_version_id"`
	Config       map[string]any    `json:"config"`
	SecretRefs   map[string]string `json:"secret_refs"`
	Enabled      bool              `json:"enabled"`
	Revision     int               `json:"revision"`
	CreatedBy    string            `json:"created_by"`
}

type HarnessVersion struct {
	ID              string     `json:"id"`
	HarnessID       string     `json:"harness_id"`
	ProjectID       string     `json:"project_id"`
	Name            string     `json:"name"`
	Version         int        `json:"version"`
	SystemPrompt    string     `json:"system_prompt"`
	SkillVersionIDs []string   `json:"skill_version_ids"`
	MCPBindingIDs   []string   `json:"mcp_binding_ids"`
	RuntimeIDs      []string   `json:"runtime_ids"`
	ContentDigest   string     `json:"content_digest"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
}
