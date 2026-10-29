package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/registry"
	"github.com/jackc/pgx/v5"
)

type ManagementSeedParams struct {
	LangfuseProjectID string
	ProjectID         string
	ConversationID    string
	ActorID           string
	Builtin           execution.HarnessSnapshot
}

// The result is safe to print: credentials, profile references and launch paths
// are never part of bootstrap output or persisted registry content.
type ManagementSeedResult struct {
	ProjectID         string   `json:"project_id"`
	LangfuseProjectID string   `json:"langfuse_project_id"`
	ConversationID    string   `json:"conversation_id"`
	EnvironmentID     string   `json:"environment_id"`
	MCPVersionID      string   `json:"mcp_version_id"`
	MCPBindingID      string   `json:"mcp_binding_id"`
	SkillVersionIDs   []string `json:"skill_version_ids"`
	HarnessID         string   `json:"harness_id"`
	HarnessVersionID  string   `json:"harness_version_id"`
	RuntimeIDs        []string `json:"runtime_ids"`
}

const managementSeedHarnessName = "Browser Harness (Builtin)"

// SeedManagement is a trusted deployment operation, never a public membership
// API. A session lock serializes seeds while existing repository calls commit
// separately. Partial bootstrap resumes by canonical names; revoked resources
// are not republished or resurrected. The pool must allow at least two sessions.
func (s *Store) SeedManagement(ctx context.Context, p ManagementSeedParams) (ManagementSeedResult, error) {
	var result ManagementSeedResult
	if err := s.requireManagement(); err != nil {
		return result, err
	}
	if strings.TrimSpace(p.LangfuseProjectID) == "" || strings.TrimSpace(p.ActorID) == "" {
		return result, errors.New("Langfuse project id and seed actor are required")
	}
	if err := p.Builtin.Validate(); err != nil {
		return result, err
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(24138, 1)`); err != nil {
		// An uncertain lock acquisition must never return a locked session.
		_ = conn.Conn().Close(context.Background())
		return result, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, `SELECT pg_advisory_unlock(24138, 1)`); err != nil {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	projectID := p.ProjectID
	if projectID == "" {
		err = s.pool.QueryRow(ctx, `SELECT agenttrace_project_id FROM project_bindings WHERE langfuse_project_id=$1`, p.LangfuseProjectID).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			project, createErr := s.CreateProject(ctx, CreateProjectParams{Name: "Langfuse browser workspace", LangfuseProjectID: p.LangfuseProjectID})
			projectID, err = project.ID, createErr
		}
		if err != nil {
			return result, err
		}
	}
	binding, err := s.BindProject(ctx, projectID, p.LangfuseProjectID)
	if err != nil {
		return result, err
	}
	if binding.Status != "active" {
		return result, ErrConflict
	}
	result.ProjectID, result.LangfuseProjectID = projectID, binding.LangfuseProjectID
	if err = s.seedManagementRegistry(ctx, p, &result); err != nil {
		return result, err
	}
	conversationID := p.ConversationID
	if conversationID == "" {
		err = s.pool.QueryRow(ctx, `SELECT split_part(id,'/',3) FROM harness_seeds WHERE split_part(id,'/',1)='management-conversation' AND split_part(id,'/',2)=$1 ORDER BY id LIMIT 1`, projectID).Scan(&conversationID)
		if errors.Is(err, pgx.ErrNoRows) {
			conversations, listErr := s.ListConversations(ctx, projectID)
			if listErr != nil {
				return result, listErr
			}
			for _, conversation := range conversations {
				if conversation.State == "active" {
					conversationID = conversation.ID
					break
				}
			}
			if conversationID == "" {
				conversation, createErr := s.CreateConversation(ctx, CreateConversationParams{ProjectID: projectID, Title: "Browser workspace", CreatedBy: p.ActorID})
				conversationID, err = conversation.ID, createErr
			} else {
				err = nil
			}
		}
		if err != nil {
			return result, err
		}
	}
	environment, err := s.seedConversationEnvironment(ctx, projectID, conversationID, p.ActorID)
	if err != nil {
		return result, err
	}
	result.ConversationID, result.EnvironmentID = conversationID, environment.ID
	_, err = s.pool.Exec(ctx, `INSERT INTO harness_seeds(id) VALUES($1) ON CONFLICT DO NOTHING`, "management-conversation/"+projectID+"/"+conversationID)
	return result, err
}

func (s *Store) seedConversationEnvironment(ctx context.Context, project, conversation, actor string) (Environment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Environment{}, err
	}
	defer tx.Rollback(ctx)
	owner, err := lockConversation(ctx, tx, conversation)
	if err != nil {
		return Environment{}, err
	}
	if owner != project {
		return Environment{}, ErrConflict
	}
	c, err := scanConversation(tx.QueryRow(ctx, `SELECT id, project_id, COALESCE(title,''), metadata, created_at, updated_at, state, deleted_at FROM conversations WHERE id=$1`, conversation))
	if err != nil {
		return Environment{}, err
	}
	if c.State == "deleted" || c.State == "deleting" {
		return Environment{}, ErrConflict
	}
	e, err := scanEnvironment(tx.QueryRow(ctx, `SELECT `+environmentColumns+` FROM environments WHERE conversation_id=$1 FOR UPDATE`, conversation))
	if errors.Is(err, ErrNotFound) {
		var safe bool
		err = tx.QueryRow(ctx, `SELECT
			NOT EXISTS(SELECT 1 FROM runs WHERE conversation_id=$1 AND status NOT IN ('succeeded','failed','cancelled'))
			AND NOT EXISTS(SELECT 1 FROM outbox_events WHERE published_at IS NULL AND
				(key=$1 OR key IN(SELECT id FROM runs WHERE conversation_id=$1)))
			AND NOT EXISTS(SELECT 1 FROM inbox_events WHERE processed_at IS NULL AND
				(payload->'payload'->>'conversation_id'=$1 OR payload->'payload'->>'run_id' IN(SELECT id FROM runs WHERE conversation_id=$1)))`, conversation).Scan(&safe)
		if err != nil {
			return e, err
		}
		if !safe {
			return e, ErrConflict
		}
		if err = createConversationEnvironment(ctx, tx, c, actor, s.defaultEnvironmentPolicy()); err != nil {
			return e, err
		}
		e, err = scanEnvironment(tx.QueryRow(ctx, `SELECT `+environmentColumns+` FROM environments WHERE conversation_id=$1`, conversation))
	}
	if err != nil {
		return e, err
	}
	return e, tx.Commit(ctx)
}

func (s *Store) seedManagementRegistry(ctx context.Context, p ManagementSeedParams, out *ManagementSeedResult) error {
	harnesses, err := s.ListProjectHarnesses(ctx, out.ProjectID)
	if err != nil {
		return err
	}
	// Once a project seed exists, Registry publication is explicit. Do not
	// follow newer MCP or Skill versions on repeated deployment bootstrap.
	for _, h := range harnesses {
		if h.Name != managementSeedHarnessName {
			continue
		}
		out.HarnessID, out.HarnessVersionID, out.SkillVersionIDs, out.RuntimeIDs = h.HarnessID, h.ID, h.SkillVersionIDs, h.RuntimeIDs
		if len(h.MCPBindingIDs) != 1 {
			return ErrConflict
		}
		out.MCPBindingID = h.MCPBindingIDs[0]
		var raw []byte
		if err := s.pool.QueryRow(ctx, `SELECT snapshot FROM harness_versions WHERE id=$1`, h.ID).Scan(&raw); err != nil {
			return err
		}
		var snapshot execution.HarnessSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return err
		}
		mcp, ok := snapshot.MCP["browser-harness"]
		if !ok || mcp.VersionID == "" {
			return ErrConflict
		}
		out.MCPVersionID = mcp.VersionID
		return nil
	}
	versions, err := s.ListMCPVersions(ctx)
	if err != nil {
		return err
	}
	var mcp registry.MCPVersion
	for _, v := range versions {
		if v.Name == "browser-harness" {
			mcp = v
			break
		}
	}
	if mcp.ID == "" {
		mcp, err = s.PublishMCP(ctx, registry.MCPVersion{
			Name: "browser-harness", Capability: execution.BuiltinHarnessID, Transport: "stdio",
			ConfigSchema: registry.Schema{Properties: map[string]registry.Field{}},
			SecretSchema: registry.Schema{Properties: map[string]registry.Field{}},
		}, p.ActorID)
	}
	if err != nil {
		return err
	}
	if err = registry.ValidateMCP(mcp); err != nil || mcp.RevokedAt != nil {
		return ErrConflict
	}
	out.MCPVersionID = mcp.ID
	bindings, err := s.ListMCPBindings(ctx, out.ProjectID)
	if err != nil {
		return err
	}
	var binding registry.Binding
	for _, b := range bindings {
		if b.MCPVersionID == mcp.ID {
			binding = b
			break
		}
	}
	if binding.ID == "" {
		binding, err = s.SaveMCPBinding(ctx, out.ProjectID, "", p.ActorID, registry.Binding{
			MCPVersionID: mcp.ID, Config: map[string]any{}, SecretRefs: map[string]string{}, Enabled: true,
		})
	}
	if err != nil {
		return err
	}
	if !binding.Enabled {
		return ErrConflict
	}
	out.MCPBindingID = binding.ID
	skills, err := s.ListSkills(ctx, out.ProjectID)
	if err != nil {
		return err
	}
	for _, resource := range p.Builtin.Skills {
		var v registry.SkillVersion
		for _, existing := range skills {
			if existing.Name == resource.Name {
				v = existing
				break
			}
		}
		if v.ID == "" {
			files := []registry.File{{Path: "SKILL.md", Content: resource.Content}}
			for _, file := range resource.Files {
				files = append(files, registry.File{Path: file.Path, Content: file.Content})
			}
			v, err = s.UploadSkill(ctx, out.ProjectID, p.ActorID, resource.Name, files)
		}
		if err != nil {
			return err
		}
		if v.Status != "published" {
			v, err = s.PublishSkill(ctx, out.ProjectID, v.ID, p.ActorID)
		}
		if err != nil {
			return err
		}
		out.SkillVersionIDs = append(out.SkillVersionIDs, v.ID)
	}
	out.RuntimeIDs = []string{"pi", "claude-code", "codex"}
	h, err := s.PublishProjectHarness(ctx, out.ProjectID, "", registry.HarnessVersion{
		Name: managementSeedHarnessName, SystemPrompt: p.Builtin.SystemPrompt,
		SkillVersionIDs: out.SkillVersionIDs, MCPBindingIDs: []string{binding.ID}, RuntimeIDs: out.RuntimeIDs,
	}, p.ActorID)
	out.HarnessID, out.HarnessVersionID = h.HarnessID, h.ID
	return err
}
