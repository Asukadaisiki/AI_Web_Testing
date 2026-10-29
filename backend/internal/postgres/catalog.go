package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/jackc/pgx/v5"
)

// Harness records are independent of Runs: deletion cannot invalidate a snapshot.
func (s *Store) ListHarnesses(ctx context.Context) ([]execution.HarnessSnapshot, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("postgres store is not configured")
	}
	rows, err := s.pool.Query(ctx, `SELECT snapshot FROM harnesses ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("list Harnesses: %w", err)
	}
	defer rows.Close()
	harnesses := []execution.HarnessSnapshot{}
	for rows.Next() {
		h, err := scanHarness(rows)
		if err != nil {
			return nil, err
		}
		harnesses = append(harnesses, h)
	}
	return harnesses, rows.Err()
}

func (s *Store) GetHarness(ctx context.Context, id string) (execution.HarnessSnapshot, error) {
	if s == nil || s.pool == nil {
		return execution.HarnessSnapshot{}, errors.New("postgres store is not configured")
	}
	return scanHarness(s.pool.QueryRow(ctx, `SELECT snapshot FROM harnesses WHERE id = $1`, id))
}

func (s *Store) CreateHarness(ctx context.Context, h execution.HarnessSnapshot) (execution.HarnessSnapshot, error) {
	if s == nil || s.pool == nil {
		return execution.HarnessSnapshot{}, errors.New("postgres store is not configured")
	}
	if err := h.Validate(); err != nil {
		return execution.HarnessSnapshot{}, err
	}
	id, err := newID("har")
	if err != nil {
		return execution.HarnessSnapshot{}, err
	}
	h = h.Clone()
	h.ID, h.Revision = id, 1
	raw, err := json.Marshal(h)
	if err != nil {
		return execution.HarnessSnapshot{}, err
	}
	return scanHarness(s.pool.QueryRow(ctx, `
		INSERT INTO harnesses (id, name, revision, snapshot)
		VALUES ($1, $2, $3, $4::jsonb) RETURNING snapshot`, h.ID, h.Name, h.Revision, raw))
}

func (s *Store) UpdateHarness(ctx context.Context, id string, h execution.HarnessSnapshot) (execution.HarnessSnapshot, error) {
	if s == nil || s.pool == nil {
		return execution.HarnessSnapshot{}, errors.New("postgres store is not configured")
	}
	h = h.Clone()
	h.ID = id
	if err := h.Validate(); err != nil {
		return execution.HarnessSnapshot{}, err
	}
	raw, err := json.Marshal(h)
	if err != nil {
		return execution.HarnessSnapshot{}, err
	}
	return scanHarness(s.pool.QueryRow(ctx, `
		UPDATE harnesses SET name = $2, revision = revision + 1,
			snapshot = jsonb_set($3::jsonb, '{revision}', to_jsonb(revision + 1))
		WHERE id = $1 RETURNING snapshot`, id, h.Name, raw))
}

func (s *Store) DeleteHarness(ctx context.Context, id string) (execution.HarnessSnapshot, error) {
	if s == nil || s.pool == nil {
		return execution.HarnessSnapshot{}, errors.New("postgres store is not configured")
	}
	return scanHarness(s.pool.QueryRow(ctx, `DELETE FROM harnesses WHERE id = $1 RETURNING snapshot`, id))
}

// SeedBuiltinHarness inserts once; restarts never overwrite user edits or resurrect deletion.
func (s *Store) SeedBuiltinHarness(ctx context.Context, h execution.HarnessSnapshot) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	if err := (&execution.Spec{RuntimeID: "pi", Harness: h}).Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(h.Clone())
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		INSERT INTO harness_seeds (id) VALUES ($1) ON CONFLICT DO NOTHING`, h.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO harnesses (id, name, revision, snapshot)
			VALUES ($1, $2, $3, $4::jsonb) ON CONFLICT DO NOTHING`, h.ID, h.Name, h.Revision, raw); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func scanHarness(row rowScanner) (execution.HarnessSnapshot, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return execution.HarnessSnapshot{}, ErrNotFound
		}
		return execution.HarnessSnapshot{}, fmt.Errorf("read Harness snapshot: %w", err)
	}
	var h execution.HarnessSnapshot
	if err := json.Unmarshal(raw, &h); err != nil {
		return execution.HarnessSnapshot{}, fmt.Errorf("decode Harness snapshot: %w", err)
	}
	return h, nil
}
