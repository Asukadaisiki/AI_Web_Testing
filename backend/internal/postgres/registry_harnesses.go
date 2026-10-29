package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/registry"
	"github.com/jackc/pgx/v5"
)

func validateHarnessManifest(v registry.HarnessVersion) error {
	if strings.TrimSpace(v.Name) == "" || len(v.Name) > 128 || len(v.SystemPrompt) > 1<<20 {
		return errors.New("invalid Harness name or prompt")
	}
	if len(v.RuntimeIDs) == 0 {
		return errors.New("Harness requires compatible runtime_ids")
	}
	for _, ids := range [][]string{v.SkillVersionIDs, v.MCPBindingIDs, v.RuntimeIDs} {
		seen := map[string]bool{}
		for _, id := range ids {
			if id == "" || seen[id] {
				return errors.New("Harness references must be unique")
			}
			seen[id] = true
		}
	}
	for _, id := range v.RuntimeIDs {
		if !execution.KnownRuntime(id) {
			return errors.New("unknown compatible Runtime")
		}
	}
	return nil
}

func (s *Store) PublishProjectHarness(ctx context.Context, project, id string, v registry.HarnessVersion, actor string) (registry.HarnessVersion, error) {
	if err := validateHarnessManifest(v); err != nil {
		return v, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return v, err
	}
	defer tx.Rollback(ctx)
	if err = lockProject(ctx, tx, project); err != nil {
		return v, err
	}
	if id == "" {
		id, err = newID("ph")
		if err != nil {
			return v, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO project_harnesses(id,project_id,name,created_by) VALUES($1,$2,$3,$4)`, id, project, v.Name, actor)
	} else {
		var existing string
		err = tx.QueryRow(ctx, `SELECT id FROM project_harnesses WHERE id=$1 AND project_id=$2 AND revoked_at IS NULL FOR UPDATE`, id, project).Scan(&existing)
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrNotFound
		}
	}
	if err != nil {
		return v, err
	}
	v.ID, err = newID("hv")
	if err != nil {
		return v, err
	}
	v.HarnessID, v.ProjectID, v.CreatedBy = id, project, actor
	if err = tx.QueryRow(ctx, `SELECT COALESCE(max(version),0)+1 FROM harness_versions WHERE harness_id=$1`, id).Scan(&v.Version); err != nil {
		return v, err
	}
	snapshot := execution.HarnessSnapshot{ID: id, Name: v.Name, Revision: v.Version, SystemPrompt: v.SystemPrompt, Skills: []execution.Skill{}, MCP: map[string]execution.MCPServer{}}
	for _, skillID := range v.SkillVersionIDs {
		skill, err := scanSkill(tx.QueryRow(ctx, skillSelect+`WHERE v.id=$1 AND s.project_id=$2 FOR SHARE OF v`, skillID, project))
		if err != nil {
			return v, err
		}
		if skill.Status != "published" || skill.RevokedAt != nil {
			return v, ErrConflict
		}
		resource := execution.Skill{Name: skill.Name}
		for _, file := range skill.Files {
			if file.Path == "SKILL.md" {
				resource.Content = file.Content
			} else {
				resource.Files = append(resource.Files, execution.SkillFile{Path: file.Path, Content: file.Content})
			}
		}
		// Include immutable resource provenance without coupling Registry to SDK types.
		resourceRaw, _ := json.Marshal(resource)
		var metadata map[string]any
		_ = json.Unmarshal(resourceRaw, &metadata)
		metadata["version_id"], metadata["content_digest"] = skill.ID, skill.ContentDigest
		resourceRaw, _ = json.Marshal(metadata)
		_ = json.Unmarshal(resourceRaw, &resource)
		snapshot.Skills = append(snapshot.Skills, resource)
	}
	for _, bindingID := range v.MCPBindingIDs {
		binding, err := scanBinding(tx.QueryRow(ctx, bindingSelect+`WHERE id=$1 AND project_id=$2 FOR SHARE`, bindingID, project))
		if err != nil {
			return v, err
		}
		if !binding.Enabled {
			return v, ErrConflict
		}
		mcp, err := scanMCP(tx.QueryRow(ctx, mcpSelect+`WHERE v.id=$1 FOR SHARE OF v`, binding.MCPVersionID))
		if err != nil {
			return v, err
		}
		if err = registry.ValidateBinding(project, mcp, binding.Config, binding.SecretRefs); err != nil {
			return v, ErrConflict
		}
		if _, ok := snapshot.MCP[mcp.Name]; ok {
			return v, errors.New("Harness MCP names must be unique")
		}
		raw, _ := json.Marshal(map[string]any{"capability": mcp.Capability, "config": binding.Config, "secret_refs": binding.SecretRefs, "version_id": mcp.ID, "content_digest": mcp.ContentDigest})
		var server execution.MCPServer
		if err = json.Unmarshal(raw, &server); err != nil {
			return v, err
		}
		snapshot.MCP[mcp.Name] = server
	}
	if err = snapshot.Validate(); err != nil {
		return v, err
	}
	manifest, _ := json.Marshal(v)
	content, _ := json.Marshal(snapshot)
	v.ContentDigest = registry.Digest(struct {
		Manifest json.RawMessage
		Snapshot json.RawMessage
	}{manifest, content})
	var snap map[string]any
	_ = json.Unmarshal(content, &snap)
	snap["project_id"], snap["version_id"], snap["content_digest"], snap["runtime_ids"] = project, v.ID, v.ContentDigest, v.RuntimeIDs
	content, _ = json.Marshal(snap)
	manifest, _ = json.Marshal(v)
	_, err = tx.Exec(ctx, `INSERT INTO harness_versions(id,harness_id,version,manifest,snapshot,content_digest,created_by) VALUES($1,$2,$3,$4,$5,$6,$7)`, v.ID, id, v.Version, manifest, content, v.ContentDigest, actor)
	if err != nil {
		return v, err
	}
	if err = registryAudit(ctx, tx, project, actor, "harness.publish", v.ID); err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}

func (s *Store) ListProjectHarnesses(ctx context.Context, project string) ([]registry.HarnessVersion, error) {
	rows, err := s.pool.Query(ctx, `SELECT v.manifest,v.created_at,v.revoked_at FROM harness_versions v JOIN project_harnesses h ON h.id=v.harness_id WHERE h.project_id=$1 ORDER BY h.name,v.version DESC`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []registry.HarnessVersion{}
	for rows.Next() {
		var v registry.HarnessVersion
		var raw []byte
		if err = rows.Scan(&raw, &v.CreatedAt, &v.RevokedAt); err != nil {
			return nil, err
		}
		when, revoked := v.CreatedAt, v.RevokedAt
		if err = json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		v.CreatedAt, v.RevokedAt = when, revoked
		result = append(result, v)
	}
	return result, rows.Err()
}

func (s *Store) ResolveProjectHarness(ctx context.Context, project, id, runtimeID string) (execution.HarnessSnapshot, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return execution.HarnessSnapshot{}, err
	}
	defer tx.Rollback(ctx)
	h, err := resolveProjectHarnessTx(ctx, tx, project, id, runtimeID)
	if err != nil {
		return h, err
	}
	return h, tx.Commit(ctx)
}

func resolveProjectHarnessTx(ctx context.Context, tx pgx.Tx, project, id, runtimeID string) (execution.HarnessSnapshot, error) {
	var raw, manifest []byte
	var h execution.HarnessSnapshot
	err := tx.QueryRow(ctx, `SELECT v.snapshot,v.manifest FROM harness_versions v JOIN project_harnesses h ON h.id=v.harness_id
		WHERE h.project_id=$1 AND (v.id=$2 OR h.id=$2) AND h.revoked_at IS NULL AND v.revoked_at IS NULL
		ORDER BY v.version DESC LIMIT 1 FOR SHARE OF v,h`, project, id).Scan(&raw, &manifest)
	if errors.Is(err, pgx.ErrNoRows) {
		return h, ErrNotFound
	}
	if err != nil {
		return h, err
	}
	var v registry.HarnessVersion
	if err = json.Unmarshal(manifest, &v); err != nil {
		return h, err
	}
	compatible := false
	for _, candidate := range v.RuntimeIDs {
		compatible = compatible || candidate == runtimeID
	}
	if !compatible {
		return h, ErrConflict
	}
	for _, skillID := range v.SkillVersionIDs {
		var usable bool
		err = tx.QueryRow(ctx, `SELECT status='published' AND revoked_at IS NULL FROM skill_versions WHERE id=$1 FOR SHARE`, skillID).Scan(&usable)
		if err != nil || !usable {
			return h, ErrConflict
		}
	}
	for _, bindingID := range v.MCPBindingIDs {
		var usable bool
		err = tx.QueryRow(ctx, `SELECT b.enabled AND v.revoked_at IS NULL FROM project_mcp_bindings b JOIN mcp_versions v ON v.id=b.mcp_version_id WHERE b.id=$1 AND b.project_id=$2 FOR SHARE OF b,v`, bindingID, project).Scan(&usable)
		if err != nil || !usable {
			return h, ErrConflict
		}
	}
	// Snapshot pins the original executable version, including after binding edits.
	var pinned struct {
		MCP map[string]struct {
			VersionID string `json:"version_id"`
		} `json:"mcp"`
	}
	_ = json.Unmarshal(raw, &pinned)
	for _, mcp := range pinned.MCP {
		var usable bool
		err = tx.QueryRow(ctx, `SELECT revoked_at IS NULL FROM mcp_versions WHERE id=$1 FOR SHARE`, mcp.VersionID).Scan(&usable)
		if err != nil || !usable {
			return h, ErrConflict
		}
	}
	if err = json.Unmarshal(raw, &h); err != nil {
		return h, err
	}
	return h, h.Validate()
}

func (s *Store) RevokeProjectHarness(ctx context.Context, project, id, actor string) error {
	return s.revokeRegistry(ctx, project, id, actor, "harness")
}
