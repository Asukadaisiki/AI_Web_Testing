package groundingplan

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/browsercontract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
)

func TestServiceAppendsImmutableRevisionsThroughGrounding(t *testing.T) {
	ctx := context.Background()
	repository := newRecordingRepository()
	service := NewService(repository)
	taskPlan := testTaskPlan("task-plan-1", "run-1", 1, "click")

	initial, err := service.EnsureForTaskPlan(ctx, taskPlan)
	if err != nil {
		t.Fatal(err)
	}
	assertPlanState(t, initial, 1, StatusActive, StepPending)
	snapshots := []string{mustJSON(t, initial)}
	hashes := []string{initial.ContentSHA256}

	ref := testCandidateRef("candidate-submit")
	queried, err := service.RecordObservationQuery(
		ctx,
		taskPlan.RunID,
		QueryRecord{
			PlanStepID:     taskPlan.Steps[0].ID,
			SourceEventSeq: 17,
			ObservationID:  ref.ObservationID,
			Action:         taskPlan.Steps[0].Action,
			Query:          "submit",
			Role:           "button",
			Limit:          10,
		},
		[]CandidateOption{{
			CandidateRef: ref,
			ElementRef:   "state-1:7",
			Role:         "button",
			Name:         "Submit",
			Locator: browsercontract.LocatorSpec{
				Kind: "role", Role: "button",
				Name: stringPointer("Submit"), Exact: true,
			},
			Provenance:    "a11y_exact",
			ObservedCount: 1,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertPlanState(t, queried, 2, StatusActive, StepCandidatesAvailable)
	if len(queried.Steps[0].ObservationQueries) != 1 ||
		len(queried.Steps[0].ObservationRefs) != 1 ||
		len(queried.Steps[0].CandidateRefs) != 1 {
		t.Fatalf("query state = %#v", queried.Steps[0])
	}
	snapshots = append(snapshots, mustJSON(t, queried))
	hashes = append(hashes, queried.ContentSHA256)

	selected, err := service.RecordCandidateSelection(
		ctx,
		taskPlan.RunID,
		taskPlan.Steps[0].ID,
		ref,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertPlanState(t, selected, 3, StatusActive, StepCandidateSelected)
	if selected.Steps[0].SelectedCandidateRef == nil ||
		*selected.Steps[0].SelectedCandidateRef != ref {
		t.Fatalf("selected candidate = %#v", selected.Steps[0].SelectedCandidateRef)
	}
	snapshots = append(snapshots, mustJSON(t, selected))
	hashes = append(hashes, selected.ContentSHA256)

	probing, err := service.RecordProbeResult(
		ctx,
		taskPlan.RunID,
		taskPlan.Steps[0].ID,
		nil,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	assertPlanState(t, probing, 4, StatusActive, StepProbing)
	snapshots = append(snapshots, mustJSON(t, probing))
	hashes = append(hashes, probing.ContentSHA256)

	evidence := testResolvedTarget(taskPlan.Steps[0], ref)
	grounded, err := service.RecordProbeResult(
		ctx,
		taskPlan.RunID,
		taskPlan.Steps[0].ID,
		&evidence,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	assertPlanState(t, grounded, 5, StatusReady, StepGrounded)
	if grounded.CurrentPlanStepID != "" ||
		grounded.Steps[0].ResolvedTarget == nil {
		t.Fatalf("ready plan = %#v", grounded)
	}
	snapshots = append(snapshots, mustJSON(t, grounded))
	hashes = append(hashes, grounded.ContentSHA256)

	if len(repository.history[taskPlan.ID]) != 5 {
		t.Fatalf("revision rows = %d, want 5", len(repository.history[taskPlan.ID]))
	}
	for index, revision := range repository.history[taskPlan.ID] {
		if revision.Revision != index+1 {
			t.Fatalf("row %d revision = %d", index, revision.Revision)
		}
		if got := mustJSON(t, revision); got != snapshots[index] {
			t.Fatalf(
				"revision %d changed\n got: %s\nwant: %s",
				index+1,
				got,
				snapshots[index],
			)
		}
		if revision.ContentSHA256 != hashes[index] {
			t.Fatalf(
				"revision %d hash changed: got %s want %s",
				index+1,
				revision.ContentSHA256,
				hashes[index],
			)
		}
		if len(revision.ContentSHA256) != 64 ||
			revision.ContentSHA256 != strings.ToLower(revision.ContentSHA256) {
			t.Fatalf("row %d hash = %q", index, revision.ContentSHA256)
		}
	}
}

func TestEnsureForTaskPlanSupersedesPreviousTaskPlan(t *testing.T) {
	ctx := context.Background()
	repository := newRecordingRepository()
	service := NewService(repository)
	firstTaskPlan := testTaskPlan("task-plan-old", "run-revised", 1, "click")
	first, err := service.EnsureForTaskPlan(ctx, firstTaskPlan)
	if err != nil {
		t.Fatal(err)
	}

	secondTaskPlan := testTaskPlan("task-plan-new", "run-revised", 2, "input")
	second, err := service.EnsureForTaskPlan(ctx, secondTaskPlan)
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 1 || second.Status != StatusActive ||
		second.TaskPlanBinding() != secondTaskPlan.Binding() {
		t.Fatalf("new grounding plan = %#v", second)
	}
	oldCurrent, err := repository.GetCurrent(ctx, firstTaskPlan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if oldCurrent.Status != StatusSuperseded ||
		oldCurrent.Revision != first.Revision+1 {
		t.Fatalf("old grounding plan = %#v", oldCurrent)
	}
	if len(repository.history[firstTaskPlan.ID]) != 2 ||
		len(repository.history[secondTaskPlan.ID]) != 1 {
		t.Fatalf("histories = %#v", repository.history)
	}
}

func TestEnsureForTaskPlanDoesNotSupersedePreviousWhenReplacementFails(
	t *testing.T,
) {
	ctx := context.Background()
	repository := newRecordingRepository()
	service := NewService(repository)
	firstTaskPlan := testTaskPlan("task-plan-old", "run-atomic", 1, "click")
	first, err := service.EnsureForTaskPlan(ctx, firstTaskPlan)
	if err != nil {
		t.Fatal(err)
	}

	injected := errors.New("injected replacement failure")
	repository.createErr = injected
	repository.replaceErr = injected
	secondTaskPlan := testTaskPlan("task-plan-new", "run-atomic", 2, "input")
	if _, err := service.EnsureForTaskPlan(ctx, secondTaskPlan); !errors.Is(err, injected) {
		t.Fatalf("EnsureForTaskPlan() error = %v, want %v", err, injected)
	}

	oldCurrent, err := repository.GetCurrent(ctx, firstTaskPlan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if oldCurrent.ID != first.ID ||
		oldCurrent.Revision != first.Revision ||
		oldCurrent.Status != StatusActive {
		t.Fatalf("old grounding plan changed after failed replacement: %#v", oldCurrent)
	}
	if len(repository.history[firstTaskPlan.ID]) != 1 ||
		len(repository.history[secondTaskPlan.ID]) != 0 {
		t.Fatalf("histories after failed replacement = %#v", repository.history)
	}
}

func TestServiceRejectsOutOfOrderAndMismatchedMutations(t *testing.T) {
	ctx := context.Background()
	service := NewService(newRecordingRepository())
	taskPlan := testTaskPlan("task-plan-invalid", "run-invalid", 1, "click")
	if _, err := service.EnsureForTaskPlan(ctx, taskPlan); err != nil {
		t.Fatal(err)
	}
	ref := testCandidateRef("candidate-invalid")

	if _, err := service.RecordCandidateSelection(
		ctx,
		taskPlan.RunID,
		taskPlan.Steps[0].ID,
		ref,
	); err == nil {
		t.Fatal("candidate selection before query succeeded")
	}
	if _, err := service.RecordObservationQuery(
		ctx,
		taskPlan.RunID,
		QueryRecord{
			PlanStepID: taskPlan.Steps[0].ID, SourceEventSeq: 17,
			ObservationID: "obs-1", Action: taskPlan.Steps[0].Action,
			Query: "submit",
		},
		[]CandidateOption{{
			CandidateRef: ref, ElementRef: "state-1:7",
			Locator: browsercontract.LocatorSpec{
				Kind: "role", Role: "button",
				Name: stringPointer("Submit"), Exact: true,
			},
			Provenance: "a11y_exact", ObservedCount: 1,
		}},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordCandidateSelection(
		ctx,
		taskPlan.RunID,
		taskPlan.Steps[0].ID,
		ref,
	); err != nil {
		t.Fatal(err)
	}
	evidence := testResolvedTarget(taskPlan.Steps[0], ref)
	evidence.Action = "input"
	if _, err := service.RecordProbeResult(
		ctx,
		taskPlan.RunID,
		taskPlan.Steps[0].ID,
		&evidence,
		"",
	); err == nil {
		t.Fatal("mismatched resolved target action succeeded")
	}
}

func TestRecordProbeResultRejectsInvalidEvidenceWithoutAppendingRevision(
	t *testing.T,
) {
	ctx := context.Background()
	repository := newRecordingRepository()
	service := NewService(repository)
	taskPlan := testTaskPlan("task-plan-invalid-evidence", "run-invalid-evidence", 1, "click")
	if _, err := service.EnsureForTaskPlan(ctx, taskPlan); err != nil {
		t.Fatal(err)
	}
	ref := testCandidateRef("candidate-invalid-evidence")
	if _, err := service.RecordObservationQuery(
		ctx,
		taskPlan.RunID,
		QueryRecord{
			PlanStepID: taskPlan.Steps[0].ID, SourceEventSeq: ref.SourceEventSeq,
			ObservationID: ref.ObservationID, Action: taskPlan.Steps[0].Action,
			Query: "submit",
		},
		[]CandidateOption{{
			CandidateRef: ref, ElementRef: "state-1:7",
			Locator: browsercontract.LocatorSpec{
				Kind: "role", Role: "button",
				Name: stringPointer("Submit"), Exact: true,
			},
			Provenance: "a11y_exact", ObservedCount: 1,
		}},
	); err != nil {
		t.Fatal(err)
	}
	selected, err := service.RecordCandidateSelection(
		ctx,
		taskPlan.RunID,
		taskPlan.Steps[0].ID,
		ref,
	)
	if err != nil {
		t.Fatal(err)
	}
	revisionCount := len(repository.history[taskPlan.ID])

	evidence := testResolvedTarget(taskPlan.Steps[0], ref)
	evidence.Action = "input"
	if _, err := service.RecordProbeResult(
		ctx,
		taskPlan.RunID,
		taskPlan.Steps[0].ID,
		&evidence,
		"",
	); err == nil {
		t.Fatal("invalid resolved target evidence succeeded")
	}

	if got := len(repository.history[taskPlan.ID]); got != revisionCount {
		t.Fatalf("revision rows = %d, want %d", got, revisionCount)
	}
	current, err := repository.GetCurrent(ctx, taskPlan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != selected.ID ||
		current.Revision != selected.Revision ||
		current.Steps[0].Status != StepCandidateSelected {
		t.Fatalf("current plan changed after invalid evidence: %#v", current)
	}
}

func TestStartCandidateProbeAdvancesSelectionAndProbingAtomically(t *testing.T) {
	ctx := context.Background()
	repository := newRecordingRepository()
	service := NewService(repository)
	taskPlan := testTaskPlan("task-plan-start-probe", "run-start-probe", 1, "click")
	if _, err := service.EnsureForTaskPlan(ctx, taskPlan); err != nil {
		t.Fatal(err)
	}
	ref := testCandidateRef("candidate-start-probe")
	if _, err := service.RecordObservationQuery(
		ctx,
		taskPlan.RunID,
		QueryRecord{
			PlanStepID: taskPlan.Steps[0].ID, SourceEventSeq: ref.SourceEventSeq,
			ObservationID: ref.ObservationID, Action: taskPlan.Steps[0].Action,
			Query: "submit",
		},
		[]CandidateOption{{
			CandidateRef: ref, ElementRef: "state-1:7",
			Locator: browsercontract.LocatorSpec{
				Kind: "role", Role: "button",
				Name: stringPointer("Submit"), Exact: true,
			},
			Provenance: "a11y_exact", ObservedCount: 1,
		}},
	); err != nil {
		t.Fatal(err)
	}
	revisionCount := len(repository.history[taskPlan.ID])

	probing, err := service.StartCandidateProbe(
		ctx,
		taskPlan.RunID,
		taskPlan.Steps[0].ID,
		ref,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(repository.history[taskPlan.ID]); got != revisionCount+1 {
		t.Fatalf("revision rows = %d, want %d", got, revisionCount+1)
	}
	if probing.Steps[0].Status != StepProbing ||
		probing.Steps[0].SelectedCandidateRef == nil ||
		*probing.Steps[0].SelectedCandidateRef != ref ||
		len(probing.Steps[0].ProbeAttempts) != 1 ||
		probing.Steps[0].ProbeAttempts[0].Status != StepProbing {
		t.Fatalf("probing plan = %#v", probing)
	}
}

func assertPlanState(
	t *testing.T,
	plan Plan,
	revision int,
	status Status,
	stepStatus StepStatus,
) {
	t.Helper()
	if plan.SchemaVersion != SchemaVersion ||
		plan.Revision != revision ||
		plan.Status != status ||
		len(plan.Steps) != 1 ||
		plan.Steps[0].Status != stepStatus {
		t.Fatalf("plan state = %#v", plan)
	}
}

func testTaskPlan(id, runID string, version int, action string) taskplan.Plan {
	return taskplan.Plan{
		ID: id, SchemaVersion: taskplan.SchemaVersion, RunID: runID,
		Version: version, PlanSHA256: strings.Repeat(string(rune('a'+version-1)), 64),
		Steps: []taskplan.Step{{
			ID: "submit", Position: 0, Intent: "Submit form",
			Action: action, Target: "Submit",
			Status: taskplan.StepPending,
		}},
	}
}

func testCandidateRef(candidateID string) browsercontract.CandidateRef {
	return browsercontract.CandidateRef{
		SchemaVersion:  browsercontract.CandidateRefVersion,
		SourceEventSeq: 17,
		ProbeID:        "probe-1",
		ObservationID:  "obs-1",
		CandidateID:    candidateID,
	}
}

func testResolvedTarget(
	step taskplan.Step,
	ref browsercontract.CandidateRef,
) browsercontract.ResolvedTargetEvidence {
	return browsercontract.ResolvedTargetEvidence{
		SchemaVersion: browsercontract.ResolvedTargetVersion,
		ProbeID:       ref.ProbeID, PlanStepID: step.ID,
		StepIndex: 0, ActionIndex: 0, Action: step.Action,
		ObservationID: ref.ObservationID, PageStateID: "state-1",
		PageStateSHA256: strings.Repeat("c", 64),
		ElementRef:      "state-1:7", CandidateID: ref.CandidateID,
		SourceCandidate: &ref,
		Locator: browsercontract.LocatorSpec{
			Kind: "role", Role: "button",
			Name: stringPointer("Submit"), Exact: true,
		},
		ContextPath: browsercontract.ContextPath{
			Frames: []string{}, ShadowHosts: []string{},
		},
		Provenance: "a11y_exact", RuntimeMatchCount: 1,
		Visible: true, Enabled: true, Editable: step.Action == "input",
		Score: 0.95, ActionStatus: "succeeded",
	}
}

func stringPointer(value string) *string {
	return &value
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

type recordingRepository struct {
	history    map[string][]Plan
	createErr  error
	replaceErr error
}

func newRecordingRepository() *recordingRepository {
	return &recordingRepository{history: make(map[string][]Plan)}
}

func (r *recordingRepository) CreateInitial(
	_ context.Context,
	plan Plan,
) (Plan, error) {
	if r.createErr != nil {
		return Plan{}, r.createErr
	}
	r.history[plan.TaskPlanID] = append(
		r.history[plan.TaskPlanID],
		cloneTestPlan(plan),
	)
	return cloneTestPlan(plan), nil
}

func (r *recordingRepository) AppendRevision(
	_ context.Context,
	plan Plan,
) (Plan, error) {
	r.history[plan.TaskPlanID] = append(
		r.history[plan.TaskPlanID],
		cloneTestPlan(plan),
	)
	return cloneTestPlan(plan), nil
}

func (r *recordingRepository) GetCurrent(
	_ context.Context,
	key string,
) (Plan, error) {
	var current *Plan
	for taskPlanID, revisions := range r.history {
		if len(revisions) == 0 {
			continue
		}
		candidate := revisions[len(revisions)-1]
		if taskPlanID != key && candidate.RunID != key {
			continue
		}
		if current == nil ||
			candidate.TaskPlanVersion > current.TaskPlanVersion ||
			(candidate.TaskPlanVersion == current.TaskPlanVersion &&
				candidate.Revision > current.Revision) {
			copy := cloneTestPlan(candidate)
			current = &copy
		}
	}
	if current == nil {
		return Plan{}, ErrNotFound
	}
	return *current, nil
}

func (r *recordingRepository) SupersedeForTaskPlan(
	_ context.Context,
	taskPlanID string,
	at time.Time,
) error {
	revisions := r.history[taskPlanID]
	if len(revisions) == 0 {
		return nil
	}
	current := revisions[len(revisions)-1]
	if current.Status == StatusSuperseded {
		return nil
	}
	current.ID += "-superseded"
	current.Revision++
	current.Status = StatusSuperseded
	current.UpdatedAt = at
	hash, err := contentHash(current)
	if err != nil {
		return err
	}
	current.ContentSHA256 = hash
	r.history[taskPlanID] = append(revisions, cloneTestPlan(current))
	return nil
}

func (r *recordingRepository) ReplaceForTaskPlan(
	ctx context.Context,
	plan Plan,
) (Plan, error) {
	if r.replaceErr != nil {
		return Plan{}, r.replaceErr
	}
	if current, err := r.GetCurrent(ctx, plan.TaskPlanID); err == nil {
		return current, nil
	}
	previous, err := r.GetCurrent(ctx, plan.RunID)
	if err == nil && previous.TaskPlanID != plan.TaskPlanID {
		if err := r.SupersedeForTaskPlan(
			ctx,
			previous.TaskPlanID,
			plan.UpdatedAt,
		); err != nil {
			return Plan{}, err
		}
	}
	return r.CreateInitial(ctx, plan)
}

func cloneTestPlan(plan Plan) Plan {
	raw, _ := json.Marshal(plan)
	var cloned Plan
	_ = json.Unmarshal(raw, &cloned)
	return cloned
}

var _ Repository = (*recordingRepository)(nil)
