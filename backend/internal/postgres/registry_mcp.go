package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/registry"
	"github.com/jackc/pgx/v5"
)

const mcpSelect = `SELECT v.id,v.mcp_definition_id,d.name,v.version,v.descriptor,v.content_digest,v.created_by,v.created_at,v.revoked_at FROM mcp_versions v JOIN mcp_definitions d ON d.id=v.mcp_definition_id `

func scanMCP(row rowScanner) (registry.MCPVersion, error) {
	var v registry.MCPVersion
	var raw []byte
	var descriptor registry.MCPVersion
	err := row.Scan(&v.ID, &v.DefinitionID, &v.Name, &v.Version, &raw, &v.ContentDigest, &v.CreatedBy, &v.CreatedAt, &v.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(raw, &descriptor); err != nil {
		return v, err
	}
	v.Capability, v.Transport = descriptor.Capability, descriptor.Transport
	v.ConfigSchema, v.SecretSchema = descriptor.ConfigSchema, descriptor.SecretSchema
	v.DeclaredTools, v.HostOnlyTools = descriptor.DeclaredTools, descriptor.HostOnlyTools
	return v, nil
}

func (s *Store) PublishMCP(ctx context.Context, input registry.MCPVersion, actor string) (registry.MCPVersion, error) {
	if err := registry.ValidateMCP(input); err != nil {
		return input, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return input, err
	}
	defer tx.Rollback(ctx)
	defID, err := newID("mcp")
	if err != nil {
		return input, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO mcp_definitions(id,name) VALUES($1,$2) ON CONFLICT(name) DO UPDATE SET name=mcp_definitions.name RETURNING id`, defID, input.Name).Scan(&defID)
	if err != nil {
		return input, err
	}
	id, err := newID("mv")
	if err != nil {
		return input, err
	}
	input.ID, input.DefinitionID, input.Version, input.CreatedBy = "", "", 0, ""
	raw, _ := json.Marshal(input)
	digest := registry.Digest(input)
	_, err = tx.Exec(ctx, `INSERT INTO mcp_versions(id,mcp_definition_id,version,descriptor,content_digest,created_by)
		VALUES($1,$2,(SELECT COALESCE(max(version),0)+1 FROM mcp_versions WHERE mcp_definition_id=$2),$3,$4,$5)`, id, defID, raw, digest, actor)
	if err != nil {
		return input, err
	}
	if err = registryAudit(ctx, tx, "", actor, "mcp.publish", id); err != nil {
		return input, err
	}
	output, err := scanMCP(tx.QueryRow(ctx, mcpSelect+`WHERE v.id=$1`, id))
	if err != nil {
		return output, err
	}
	return output, tx.Commit(ctx)
}

func (s *Store) ListMCPVersions(ctx context.Context) ([]registry.MCPVersion, error) {
	rows, err := s.pool.Query(ctx, mcpSelect+`ORDER BY d.name,v.version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []registry.MCPVersion{}
	for rows.Next() {
		v, err := scanMCP(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (s *Store) RevokeMCP(ctx context.Context, id, actor string) error {
	return s.revokeRegistry(ctx, "", id, actor, "mcp")
}

const bindingSelect = `SELECT id,project_id,mcp_version_id,config,secret_refs,enabled,revision,created_by FROM project_mcp_bindings `

func scanBinding(row rowScanner) (registry.Binding, error) {
	var b registry.Binding
	var config, refs []byte
	err := row.Scan(&b.ID, &b.ProjectID, &b.MCPVersionID, &config, &refs, &b.Enabled, &b.Revision, &b.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	if err = json.Unmarshal(config, &b.Config); err != nil {
		return b, err
	}
	err = json.Unmarshal(refs, &b.SecretRefs)
	return b, err
}

func (s *Store) SaveMCPBinding(ctx context.Context, project, id, actor string, input registry.Binding) (registry.Binding, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return input, err
	}
	defer tx.Rollback(ctx)
	if err = lockProject(ctx, tx, project); err != nil {
		return input, err
	}
	version, err := scanMCP(tx.QueryRow(ctx, mcpSelect+`WHERE v.id=$1 FOR SHARE OF v`, input.MCPVersionID))
	if err != nil {
		return input, err
	}
	if err = registry.ValidateBinding(project, version, input.Config, input.SecretRefs); err != nil {
		return input, err
	}
	if input.Config == nil {
		input.Config = map[string]any{}
	}
	if input.SecretRefs == nil {
		input.SecretRefs = map[string]string{}
	}
	config, _ := json.Marshal(input.Config)
	refs, _ := json.Marshal(input.SecretRefs)
	if id == "" {
		id, err = newID("binding")
		if err != nil {
			return input, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO project_mcp_bindings(id,project_id,mcp_version_id,config,secret_refs,enabled,created_by) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, project, input.MCPVersionID, config, refs, input.Enabled, actor)
	} else {
		var tag interface{ RowsAffected() int64 }
		tag, err = tx.Exec(ctx, `UPDATE project_mcp_bindings SET mcp_version_id=$3,config=$4,secret_refs=$5,enabled=$6,revision=revision+1,updated_at=now() WHERE id=$1 AND project_id=$2`, id, project, input.MCPVersionID, config, refs, input.Enabled)
		if err == nil && tag.RowsAffected() == 0 {
			err = ErrNotFound
		}
	}
	if err != nil {
		return input, err
	}
	if err = registryAudit(ctx, tx, project, actor, "mcp.bind", id); err != nil {
		return input, err
	}
	output, err := scanBinding(tx.QueryRow(ctx, bindingSelect+`WHERE id=$1`, id))
	if err != nil {
		return output, err
	}
	return output, tx.Commit(ctx)
}

func (s *Store) ListMCPBindings(ctx context.Context, project string) ([]registry.Binding, error) {
	rows, err := s.pool.Query(ctx, bindingSelect+`WHERE project_id=$1 ORDER BY id`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []registry.Binding{}
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}
