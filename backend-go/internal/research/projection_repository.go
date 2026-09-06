package research

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type ProjectionState struct {
	Cursor           Slot[SourceCursor] `json:"cursor"`
	SourceSHA256     Slot[string]       `json:"source_sha256"`
	ProjectionSHA256 string             `json:"projection_sha256"`
	TransitionCount  int64              `json:"transition_count"`
}

func (s *ProjectionState) NormalizeAndValidate() error {
	s.ProjectionSHA256 = strings.ToLower(strings.TrimSpace(s.ProjectionSHA256))
	if err := s.Cursor.Validate("projection_state.cursor"); err != nil {
		return err
	}
	if s.Cursor.Value != nil {
		if err := s.Cursor.Value.NormalizeAndValidate(); err != nil {
			return err
		}
	}
	if err := s.SourceSHA256.Validate("projection_state.source_sha256"); err != nil {
		return err
	}
	if s.SourceSHA256.Value != nil {
		sourceSHA256 := strings.ToLower(strings.TrimSpace(*s.SourceSHA256.Value))
		if !sha256Pattern.MatchString(sourceSHA256) {
			return fmt.Errorf("%w: projection state source hash", ErrInvalid)
		}
		s.SourceSHA256.Value = &sourceSHA256
	}
	if !sha256Pattern.MatchString(s.ProjectionSHA256) || s.TransitionCount < 0 {
		return fmt.Errorf("%w: projection state", ErrInvalid)
	}
	return nil
}

func EmptyProjectionState() ProjectionState {
	state := ProjectionState{
		Cursor:          Unavailable[SourceCursor]("projection_empty"),
		SourceSHA256:    Unavailable[string]("projection_empty"),
		TransitionCount: 0,
	}
	state.ProjectionSHA256, _ = projectionStateHash(nil, nil)
	return state
}

func (r *PostgresRepository) GetProjectionState(
	ctx context.Context,
	runID string,
) (ProjectionState, error) {
	if strings.TrimSpace(runID) == "" {
		return ProjectionState{}, fmt.Errorf("%w: research_run_id", ErrInvalid)
	}
	var exists bool
	if err := r.db.QueryRowContext(ctx,
		`SELECT true FROM research_runs WHERE id = $1`,
		runID,
	).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return ProjectionState{}, ErrNotFound
	} else if err != nil {
		return ProjectionState{}, fmt.Errorf("read research run projection: %w", err)
	}
	return readProjectionState(ctx, r.db, runID)
}

func (r *PostgresRepository) ReplaceProjection(
	ctx context.Context,
	runID string,
	expected ProjectionState,
	manifest ProjectionManifest,
	transitions []Transition,
) ([]Transition, ProjectionState, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, ProjectionState{}, fmt.Errorf("%w: research_run_id", ErrInvalid)
	}
	if err := expected.NormalizeAndValidate(); err != nil {
		return nil, ProjectionState{}, err
	}
	if err := manifest.NormalizeAndValidate(); err != nil {
		return nil, ProjectionState{}, err
	}
	if manifest.TransitionCount != int64(len(transitions)) || len(transitions) == 0 {
		return nil, ProjectionState{}, fmt.Errorf(
			"%w: projection transition count",
			ErrInvalid,
		)
	}
	for index := range transitions {
		if transitions[index].ResearchRunID == "" {
			transitions[index].ResearchRunID = runID
		}
		if transitions[index].ResearchRunID != runID {
			return nil, ProjectionState{}, fmt.Errorf(
				"%w: transition %d run mismatch",
				ErrInvalid, index,
			)
		}
		transitions[index].Ordinal = int64(index)
		if transitions[index].CreatedAt.IsZero() {
			transitions[index].CreatedAt = r.now().UTC()
		}
		var payload TransitionPayloadV1
		if err := json.Unmarshal(transitions[index].PayloadJSON, &payload); err != nil {
			return nil, ProjectionState{}, fmt.Errorf(
				"%w: transition %d payload: %v",
				ErrInvalid, index, err,
			)
		}
		if payload.Projection.ManifestSHA256 != manifest.ManifestSHA256 {
			return nil, ProjectionState{}, fmt.Errorf(
				"%w: transition %d projection manifest",
				ErrInvalid, index,
			)
		}
		if err := payload.Validate(); err != nil {
			return nil, ProjectionState{}, err
		}
		var err error
		transitions[index].ContentSHA256, err = TransitionContentSHA256(
			transitions[index].SchemaVersion,
			transitions[index].PayloadJSON,
			transitions[index].ArtifactRefs,
		)
		if err != nil {
			return nil, ProjectionState{}, err
		}
		if err := transitions[index].NormalizeAndValidate(); err != nil {
			return nil, ProjectionState{}, err
		}
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, ProjectionState{}, fmt.Errorf("begin projection replacement: %w", err)
	}
	defer tx.Rollback()
	var runSchemaVersion, runProjectorVersion string
	var agentRunID sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT schema_version, projector_version, agent_run_id
		 FROM research_runs
		 WHERE id = $1
		 FOR UPDATE`,
		runID,
	).Scan(
		&runSchemaVersion,
		&runProjectorVersion,
		&agentRunID,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ProjectionState{}, ErrNotFound
	} else if err != nil {
		return nil, ProjectionState{}, fmt.Errorf("lock research run projection: %w", err)
	}
	if runSchemaVersion != SchemaVersion ||
		runProjectorVersion != manifest.ProjectorVersion {
		return nil, ProjectionState{}, fmt.Errorf(
			"%w: projection run version",
			ErrUnsupportedSchema,
		)
	}
	if agentRunID.Valid {
		var lastEventSeq int64
		if err := tx.QueryRowContext(ctx,
			`SELECT last_event_seq FROM agent_runs WHERE id = $1 FOR SHARE`,
			agentRunID.String,
		).Scan(&lastEventSeq); err != nil {
			return nil, ProjectionState{}, fmt.Errorf(
				"lock projection source cursor: %w",
				err,
			)
		}
		if agentRunID.String != manifest.SourceCursor.AgentRunID ||
			lastEventSeq != manifest.SourceCursor.AgentEventSeq {
			return nil, ProjectionState{}, fmt.Errorf(
				"%w: projection source cursor changed",
				ErrSourceChanged,
			)
		}
	}
	current, err := readProjectionState(ctx, tx, runID)
	if err != nil {
		return nil, ProjectionState{}, err
	}
	if !sameProjectionState(current, expected) {
		return nil, ProjectionState{}, fmt.Errorf(
			"%w: projection cursor/hash compare-and-swap",
			ErrConflict,
		)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM research_transitions WHERE research_run_id = $1`,
		runID,
	); err != nil {
		return nil, ProjectionState{}, fmt.Errorf("delete old projection: %w", err)
	}
	persisted := make([]Transition, 0, len(transitions))
	for _, transition := range transitions {
		artifacts, err := json.Marshal(transition.ArtifactRefs)
		if err != nil {
			return nil, ProjectionState{}, fmt.Errorf("encode artifact references: %w", err)
		}
		item, err := scanTransition(tx.QueryRowContext(ctx, `
			INSERT INTO research_transitions (
				research_run_id, ordinal, append_key, content_sha256,
				schema_version, transition_json, artifact_refs_json, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING `+transitionColumns,
			runID, transition.Ordinal, transition.AppendKey,
			transition.ContentSHA256, transition.SchemaVersion,
			string(transition.PayloadJSON), string(artifacts), transition.CreatedAt,
		))
		if err != nil {
			return nil, ProjectionState{}, fmt.Errorf(
				"insert replacement transition %d: %w",
				transition.Ordinal, classifyPersistenceError(err),
			)
		}
		persisted = append(persisted, item)
	}
	next, err := projectionStateFromTransitions(persisted)
	if err != nil {
		return nil, ProjectionState{}, err
	}
	if err := tx.Commit(); err != nil {
		return nil, ProjectionState{}, fmt.Errorf("commit projection replacement: %w", err)
	}
	return persisted, next, nil
}

type projectionQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readProjectionState(
	ctx context.Context,
	query projectionQueryer,
	runID string,
) (ProjectionState, error) {
	rows, err := query.QueryContext(ctx, `
		SELECT `+transitionColumns+`
		FROM research_transitions
		WHERE research_run_id = $1
		ORDER BY ordinal`,
		runID,
	)
	if err != nil {
		return ProjectionState{}, fmt.Errorf("read projection state: %w", err)
	}
	defer rows.Close()
	transitions := make([]Transition, 0)
	for rows.Next() {
		item, err := scanTransition(rows)
		if err != nil {
			return ProjectionState{}, fmt.Errorf("scan projection state: %w", err)
		}
		transitions = append(transitions, item)
	}
	if err := rows.Err(); err != nil {
		return ProjectionState{}, fmt.Errorf("iterate projection state: %w", err)
	}
	return projectionStateFromTransitions(transitions)
}

func projectionStateFromTransitions(
	transitions []Transition,
) (ProjectionState, error) {
	if len(transitions) == 0 {
		return EmptyProjectionState(), nil
	}
	for index := range transitions {
		if transitions[index].Ordinal != int64(index) {
			return ProjectionState{}, fmt.Errorf(
				"%w: persisted projection ordinal gap",
				ErrSourceChanged,
			)
		}
	}
	var manifest *ProjectionManifest
	legacy := false
	for index, transition := range transitions {
		var envelope struct {
			Projection *ProjectionManifest `json:"projection"`
		}
		if err := json.Unmarshal(transition.PayloadJSON, &envelope); err != nil {
			return ProjectionState{}, fmt.Errorf("decode projection manifest: %w", err)
		}
		if envelope.Projection == nil {
			legacy = true
			continue
		}
		if err := envelope.Projection.NormalizeAndValidate(); err != nil {
			return ProjectionState{}, err
		}
		if manifest == nil {
			copy := *envelope.Projection
			manifest = &copy
		} else if manifest.ManifestSHA256 != envelope.Projection.ManifestSHA256 {
			return ProjectionState{}, fmt.Errorf(
				"%w: inconsistent projection manifest at ordinal %d",
				ErrSourceChanged, index,
			)
		}
	}
	state := ProjectionState{TransitionCount: int64(len(transitions))}
	switch {
	case legacy && manifest != nil:
		return ProjectionState{}, fmt.Errorf(
			"%w: mixed legacy and versioned projection",
			ErrSourceChanged,
		)
	case manifest == nil:
		state.Cursor = Unavailable[SourceCursor]("legacy_projection_has_no_cursor")
		state.SourceSHA256 = Unavailable[string]("legacy_projection_has_no_source_hash")
	default:
		if manifest.TransitionCount != int64(len(transitions)) {
			return ProjectionState{}, fmt.Errorf(
				"%w: projection manifest transition count",
				ErrSourceChanged,
			)
		}
		state.Cursor = Available(manifest.SourceCursor)
		state.SourceSHA256 = Available(manifest.SourceSHA256)
	}
	hash, err := projectionStateHash(manifest, transitions)
	if err != nil {
		return ProjectionState{}, err
	}
	state.ProjectionSHA256 = hash
	if err := state.NormalizeAndValidate(); err != nil {
		return ProjectionState{}, err
	}
	return state, nil
}

func projectionStateHash(
	manifest *ProjectionManifest,
	transitions []Transition,
) (string, error) {
	type item struct {
		Ordinal       int64  `json:"ordinal"`
		AppendKey     string `json:"append_key"`
		ContentSHA256 string `json:"content_sha256"`
	}
	items := make([]item, 0, len(transitions))
	for _, transition := range transitions {
		items = append(items, item{
			Ordinal: transition.Ordinal, AppendKey: transition.AppendKey,
			ContentSHA256: transition.ContentSHA256,
		})
	}
	return CanonicalSHA256(struct {
		Manifest    *ProjectionManifest `json:"manifest"`
		Transitions []item              `json:"transitions"`
	}{Manifest: manifest, Transitions: items})
}

func sameProjectionState(left, right ProjectionState) bool {
	leftRaw, leftErr := json.Marshal(left)
	rightRaw, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftRaw) == string(rightRaw)
}
