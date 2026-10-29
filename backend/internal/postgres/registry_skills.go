package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/registry"
	"github.com/jackc/pgx/v5"
)

const skillSelect = `SELECT v.id,v.skill_id,s.project_id,s.name,v.version,v.status,v.files,v.content_digest,v.created_by,v.created_at,v.revoked_at FROM skill_versions v JOIN skills s ON s.id=v.skill_id `

func scanSkill(row rowScanner) (registry.SkillVersion, error) {
	var v registry.SkillVersion
	var raw []byte
	err := row.Scan(&v.ID, &v.SkillID, &v.ProjectID, &v.Name, &v.Version, &v.Status, &raw, &v.ContentDigest, &v.CreatedBy, &v.CreatedAt, &v.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(raw, &v.Files)
	}
	return v, err
}

func registryAudit(ctx context.Context, tx pgx.Tx, project, actor, action, id string) error {
	if strings.TrimSpace(actor) == "" {
		return errors.New("registry actor is required")
	}
	_, err := tx.Exec(ctx, `INSERT INTO registry_audit(project_id,actor_id,action,resource_id) VALUES(NULLIF($1,''),$2,$3,$4)`, project, actor, action, id)
	return err
}

func (s *Store) UploadSkill(ctx context.Context, project, actor, name string, files []registry.File) (registry.SkillVersion, error) {
	v, err := registry.ValidateSkill(name, files)
	if err != nil {
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
	skillID, err := newID("skill")
	if err != nil {
		return v, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO skills(id,project_id,name,slug,created_by) VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(project_id,slug) DO UPDATE SET name=skills.name RETURNING id`, skillID, project, name, strings.ToLower(name), actor).Scan(&skillID)
	if err != nil {
		return v, err
	}
	v.ID, err = newID("sv")
	if err != nil {
		return v, err
	}
	raw, _ := json.Marshal(v.Files)
	_, err = tx.Exec(ctx, `INSERT INTO skill_versions(id,skill_id,version,files,content_digest,created_by)
		VALUES($1,$2,(SELECT COALESCE(max(version),0)+1 FROM skill_versions WHERE skill_id=$2),$3,$4,$5)`, v.ID, skillID, raw, v.ContentDigest, actor)
	if err != nil {
		return v, err
	}
	if err = registryAudit(ctx, tx, project, actor, "skill.upload", v.ID); err != nil {
		return v, err
	}
	v, err = scanSkill(tx.QueryRow(ctx, skillSelect+`WHERE v.id=$1`, v.ID))
	if err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}

func (s *Store) ListSkills(ctx context.Context, project string) ([]registry.SkillVersion, error) {
	rows, err := s.pool.Query(ctx, skillSelect+`WHERE s.project_id=$1 ORDER BY s.name,v.version DESC`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []registry.SkillVersion{}
	for rows.Next() {
		v, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (s *Store) PublishSkill(ctx context.Context, project, id, actor string) (registry.SkillVersion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return registry.SkillVersion{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockProject(ctx, tx, project); err != nil {
		return registry.SkillVersion{}, err
	}
	v, err := scanSkill(tx.QueryRow(ctx, skillSelect+`WHERE v.id=$1 AND s.project_id=$2 FOR UPDATE OF v`, id, project))
	if err != nil {
		return v, err
	}
	if v.Status == "revoked" {
		return v, ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE skill_versions SET status='published' WHERE id=$1`, id); err != nil {
		return v, err
	}
	if err = registryAudit(ctx, tx, project, actor, "skill.publish", id); err != nil {
		return v, err
	}
	v.Status = "published"
	return v, tx.Commit(ctx)
}

func (s *Store) RevokeSkill(ctx context.Context, project, id, actor string) error {
	return s.revokeRegistry(ctx, project, id, actor, "skill")
}

func (s *Store) revokeRegistry(ctx context.Context, project, id, actor, kind string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var query string
	switch kind {
	case "skill":
		query = `UPDATE skill_versions v SET status='revoked',revoked_at=COALESCE(v.revoked_at,now()) FROM skills s WHERE s.id=v.skill_id AND s.project_id=$1 AND v.id=$2`
	case "harness":
		query = `UPDATE harness_versions v SET revoked_at=COALESCE(v.revoked_at,now()) FROM project_harnesses h WHERE h.id=v.harness_id AND h.project_id=$1 AND v.id=$2`
	case "mcp":
		query = `UPDATE mcp_versions SET revoked_at=COALESCE(revoked_at,now()) WHERE $1='' AND id=$2`
	default:
		return errors.New("unknown registry resource")
	}
	tag, err := tx.Exec(ctx, query, project, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err = registryAudit(ctx, tx, project, actor, kind+".revoke", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
