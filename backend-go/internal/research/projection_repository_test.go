package research_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/research"
)

func TestPostgresRepositoryDeleteAndReplaceProjectionIsByteStable(t *testing.T) {
	fixture := newPostgresFixture(t, false)
	repository := research.NewPostgresRepository(fixture.db)
	experiment := fixture.createExperiment(t, repository, "replace-byte-stable", 2)
	run := fixture.newRun(experiment.ID, "replace-byte-stable", 0)
	if _, createErr := repository.CreateRun(fixture.ctx, run); createErr != nil {
		t.Fatal(createErr)
	}

	empty, err := repository.GetProjectionState(fixture.ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstInput, manifest := projectionFixture(t, run.ID, "source-a", "one", "two")
	first, firstState, err := repository.ReplaceProjection(
		fixture.ctx, run.ID, empty, manifest, firstInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertContiguousOrdinals(t, first)

	if deleteErr := repository.DeleteTransitions(fixture.ctx, run.ID); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	afterDelete, err := repository.GetProjectionState(fixture.ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondInput, secondManifest := projectionFixture(t, run.ID, "source-a", "one", "two")
	second, secondState, err := repository.ReplaceProjection(
		fixture.ctx, run.ID, afterDelete, secondManifest, secondInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertContiguousOrdinals(t, second)
	if firstState.ProjectionSHA256 != secondState.ProjectionSHA256 {
		t.Fatalf(
			"projection hash changed after Delete+Replace: %s != %s",
			firstState.ProjectionSHA256,
			secondState.ProjectionSHA256,
		)
	}
	if !bytes.Equal(projectionContentBytes(t, first), projectionContentBytes(t, second)) {
		t.Fatal("projection content changed byte-for-byte after Delete+Replace")
	}
}

func TestPostgresRepositoryConcurrentReplaceProjectionCAS(t *testing.T) {
	fixture := newPostgresFixture(t, false)
	repository := research.NewPostgresRepository(fixture.db)
	experiment := fixture.createExperiment(t, repository, "replace-concurrent", 2)
	run := fixture.newRun(experiment.ID, "replace-concurrent", 0)
	if _, createErr := repository.CreateRun(fixture.ctx, run); createErr != nil {
		t.Fatal(createErr)
	}
	expected, err := repository.GetProjectionState(fixture.ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}

	type replacement struct {
		transitions []research.Transition
		manifest    research.ProjectionManifest
	}
	replacements := []replacement{{}, {}}
	replacements[0].transitions, replacements[0].manifest =
		projectionFixture(t, run.ID, "source-left", "left-0", "left-1")
	replacements[1].transitions, replacements[1].manifest =
		projectionFixture(t, run.ID, "source-right", "right-0", "right-1", "right-2")

	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	type replacementResult struct {
		transitions []research.Transition
		state       research.ProjectionState
	}
	results := make(chan replacementResult, len(replacements))
	failures := make(chan error, len(replacements))
	for _, candidate := range replacements {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			persisted, state, replaceErr := repository.ReplaceProjection(
				fixture.ctx,
				run.ID,
				expected,
				candidate.manifest,
				candidate.transitions,
			)
			if replaceErr != nil {
				failures <- replaceErr
				return
			}
			results <- replacementResult{transitions: persisted, state: state}
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)
	close(failures)

	var successful []replacementResult
	for result := range results {
		successful = append(successful, result)
		assertContiguousOrdinals(t, result.transitions)
	}
	var failed []error
	for err := range failures {
		failed = append(failed, err)
	}
	if len(successful) != 1 || len(failed) != 1 ||
		!errors.Is(failed[0], research.ErrConflict) {
		t.Fatalf("concurrent ReplaceProjection() successes=%#v failures=%v", successful, failed)
	}
	current, err := repository.GetProjectionState(fixture.ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ProjectionSHA256 != successful[0].state.ProjectionSHA256 {
		t.Fatalf("current projection = %#v, winner = %#v", current, successful[0].state)
	}
	persisted, err := repository.ListTransitions(
		fixture.ctx,
		research.TransitionFilter{ResearchRunID: run.ID},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertContiguousOrdinals(t, persisted)
}

func TestPostgresRepositoryReplaceProjectionRejectsSourceCursorDrift(t *testing.T) {
	fixture := newPostgresFixture(t, false)
	repository := research.NewPostgresRepository(fixture.db)
	experiment := fixture.createExperiment(t, repository, "replace-source-drift", 1)
	run := fixture.newRun(experiment.ID, "replace-source-drift", 0)
	if _, err := repository.CreateRun(fixture.ctx, run); err != nil {
		t.Fatal(err)
	}
	sourceMarker := "source-cursor-" + fixture.suffix
	agentRunID := "agent-" + sourceMarker
	if _, err := fixture.db.ExecContext(fixture.ctx, `
		INSERT INTO agent_runs (
			id, actor_user_id, conversation_id, project_id, status, input,
			transcript_json, last_event_seq
		) VALUES ($1, $2, $3, $4, 'completed', 'projection cursor fixture', '[]'::json, 2)`,
		agentRunID,
		fixture.userID,
		"conversation-"+fixture.suffix,
		fixture.projectID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdateRunLinks(
		fixture.ctx,
		run.ID,
		research.RunLinks{AgentRunID: &agentRunID},
		time.Time{},
	); err != nil {
		t.Fatal(err)
	}
	expected, err := repository.GetProjectionState(fixture.ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	transitions, manifest := projectionFixture(
		t, run.ID, sourceMarker, "stale-source",
	)
	if _, _, err := repository.ReplaceProjection(
		fixture.ctx,
		run.ID,
		expected,
		manifest,
		transitions,
	); !errors.Is(err, research.ErrSourceChanged) {
		t.Fatalf("stale source cursor error = %v, want ErrSourceChanged", err)
	}
}

func TestPostgresRepositoryReplaceProjectionRollsBackDeleteOnInsertFailure(t *testing.T) {
	fixture := newPostgresFixture(t, false)
	repository := research.NewPostgresRepository(fixture.db)
	experiment := fixture.createExperiment(t, repository, "replace-rollback", 2)
	run := fixture.newRun(experiment.ID, "replace-rollback", 0)
	if _, createErr := repository.CreateRun(fixture.ctx, run); createErr != nil {
		t.Fatal(createErr)
	}
	empty, err := repository.GetProjectionState(fixture.ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	initialInput, initialManifest := projectionFixture(
		t, run.ID, "source-initial", "initial-0", "initial-1",
	)
	initial, initialState, err := repository.ReplaceProjection(
		fixture.ctx, run.ID, empty, initialManifest, initialInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	before := projectionContentBytes(t, initial)

	duplicateInput, duplicateManifest := projectionFixture(
		t, run.ID, "source-invalid", "duplicate", "duplicate",
	)
	if _, _, replaceErr := repository.ReplaceProjection(
		fixture.ctx,
		run.ID,
		initialState,
		duplicateManifest,
		duplicateInput,
	); !errors.Is(replaceErr, research.ErrConflict) {
		t.Fatalf("duplicate append key error = %v, want ErrConflict", replaceErr)
	}

	current, err := repository.GetProjectionState(fixture.ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ProjectionSHA256 != initialState.ProjectionSHA256 {
		t.Fatalf("failed replacement changed projection state: %#v", current)
	}
	persisted, err := repository.ListTransitions(
		fixture.ctx,
		research.TransitionFilter{ResearchRunID: run.ID},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertContiguousOrdinals(t, persisted)
	if !bytes.Equal(before, projectionContentBytes(t, persisted)) {
		t.Fatal("failed replacement did not roll back the deleted projection")
	}
}

func projectionFixture(
	t *testing.T,
	runID string,
	sourceMarker string,
	appendKeys ...string,
) ([]research.Transition, research.ProjectionManifest) {
	t.Helper()
	sourceSHA, err := research.CanonicalSHA256(map[string]string{"source": sourceMarker})
	if err != nil {
		t.Fatal(err)
	}
	cursor := research.SourceCursor{
		SchemaVersion: research.EventSchemaVersion,
		AgentRunID:    "agent-" + sourceMarker,
		AgentEventSeq: int64(len(appendKeys)),
	}
	manifest, err := research.NewProjectionManifest(
		cursor,
		sourceSHA,
		int64(len(appendKeys)),
	)
	if err != nil {
		t.Fatal(err)
	}
	transitions := make([]research.Transition, 0, len(appendKeys))
	for index, appendKey := range appendKeys {
		payload := research.TransitionPayloadV1{
			SchemaVersion:    research.TransitionSchemaVersion,
			ProjectorVersion: research.ProjectorVersion,
			Unit: research.TransitionUnit{
				Type: "repository_fixture",
				ID:   fmt.Sprintf("%s:%d", sourceMarker, index),
			},
			State:        research.Unavailable[research.ResearchEvent]("state_source_unavailable"),
			Observation:  research.NotApplicable[research.ResearchEvent]("unit_has_no_observation"),
			Candidate:    research.NotApplicable[research.ResearchEvent]("unit_has_no_candidates"),
			Decision:     research.NotApplicable[research.ResearchEvent]("unit_has_no_decision"),
			Action:       research.NotApplicable[research.ResearchEvent]("unit_has_no_action"),
			Execution:    research.NotApplicable[research.ResearchEvent]("unit_has_no_execution"),
			Verification: research.NotApplicable[research.ResearchEvent]("unit_has_no_verification"),
			Failure:      research.NotApplicable[research.ResearchEvent]("unit_has_no_failure"),
			Recovery:     research.NotApplicable[research.ResearchEvent]("unit_has_no_recovery"),
			Reward:       research.Unavailable[research.ResearchEvent]("independent_oracle_not_persisted"),
			Unknown:      research.NotApplicable[research.ResearchEvent]("unit_has_no_unknown_event"),
			Cost:         research.NotApplicable[research.CostSummary]("unit_has_no_direct_cost"),
			Projection:   manifest,
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := research.TransitionContentSHA256(research.SchemaVersion, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		transitions = append(transitions, research.Transition{
			ResearchRunID: runID,
			Ordinal:       int64(index),
			AppendKey:     appendKey,
			ContentSHA256: hash,
			SchemaVersion: research.SchemaVersion,
			PayloadJSON:   raw,
			ArtifactRefs:  []research.ArtifactRef{},
		})
	}
	return transitions, manifest
}

func projectionContentBytes(t *testing.T, transitions []research.Transition) []byte {
	t.Helper()
	type content struct {
		Ordinal       int64                  `json:"ordinal"`
		AppendKey     string                 `json:"append_key"`
		ContentSHA256 string                 `json:"content_sha256"`
		SchemaVersion string                 `json:"schema_version"`
		PayloadJSON   json.RawMessage        `json:"transition"`
		ArtifactRefs  []research.ArtifactRef `json:"artifact_refs"`
	}
	items := make([]content, 0, len(transitions))
	for _, transition := range transitions {
		items = append(items, content{
			Ordinal: transition.Ordinal, AppendKey: transition.AppendKey,
			ContentSHA256: transition.ContentSHA256,
			SchemaVersion: transition.SchemaVersion,
			PayloadJSON:   transition.PayloadJSON,
			ArtifactRefs:  transition.ArtifactRefs,
		})
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertContiguousOrdinals(t *testing.T, transitions []research.Transition) {
	t.Helper()
	for index, transition := range transitions {
		if transition.Ordinal != int64(index) {
			t.Fatalf("transition[%d].Ordinal = %d", index, transition.Ordinal)
		}
	}
}
