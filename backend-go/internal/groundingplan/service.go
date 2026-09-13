package groundingplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/browsercontract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
)

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) EnsureForTaskPlan(
	ctx context.Context,
	source taskplan.Plan,
) (Plan, error) {
	if err := validateTaskPlan(source); err != nil {
		return Plan{}, err
	}
	current, err := s.repository.GetCurrent(ctx, source.ID)
	if err == nil {
		if err := validatePlan(current); err != nil {
			return Plan{}, fmt.Errorf("validate current grounding plan: %w", err)
		}
		if err := validateAgainstTaskPlan(current, source); err != nil {
			return Plan{}, err
		}
		return current, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Plan{}, err
	}

	now := s.now().UTC()
	steps := make([]Step, len(source.Steps))
	for index, sourceStep := range source.Steps {
		steps[index] = Step{
			PlanStepID:         sourceStep.ID,
			Position:           sourceStep.Position,
			Action:             sourceStep.Action,
			Status:             StepPending,
			ObservationQueries: []QueryRecord{},
			ObservationRefs:    []ObservationRef{},
			CandidateRefs:      []CandidateOption{},
			ProbeAttempts:      []ProbeAttempt{},
		}
	}
	plan := Plan{
		SchemaVersion:     SchemaVersion,
		RunID:             source.RunID,
		TaskPlanID:        source.ID,
		TaskPlanVersion:   source.Version,
		TaskPlanSHA256:    source.PlanSHA256,
		Revision:          1,
		Status:            StatusActive,
		CurrentPlanStepID: source.Steps[0].ID,
		Steps:             steps,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	plan.ID = revisionID(plan, now)
	plan.ContentSHA256, err = contentHash(plan)
	if err != nil {
		return Plan{}, err
	}
	if err := validateAgainstTaskPlan(plan, source); err != nil {
		return Plan{}, err
	}
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	return s.repository.ReplaceForTaskPlan(ctx, plan)
}

func (s *Service) RecordObservationQuery(
	ctx context.Context,
	runID string,
	query QueryRecord,
	candidates []CandidateOption,
) (Plan, error) {
	current, err := s.activePlan(ctx, runID)
	if err != nil {
		return Plan{}, err
	}
	index, err := currentStepIndex(current, query.PlanStepID)
	if err != nil {
		return Plan{}, err
	}
	step := current.Steps[index]
	if step.Status != StepPending &&
		step.Status != StepQuerying &&
		step.Status != StepFailed {
		return Plan{}, fmt.Errorf(
			"grounding step %q cannot record candidates from status %q",
			step.PlanStepID,
			step.Status,
		)
	}
	if query.SourceEventSeq < 1 ||
		strings.TrimSpace(query.Action) == "" ||
		query.Action != step.Action ||
		query.Limit < 0 {
		return Plan{}, errors.New("observation query does not match the grounding step")
	}
	if len(candidates) == 0 {
		return Plan{}, errors.New("observation query returned no candidates")
	}
	seen := make(map[browsercontract.CandidateRef]bool, len(candidates))
	refs := make([]ObservationRef, 0, len(candidates))
	refSeen := make(map[ObservationRef]bool, len(candidates))
	for _, candidate := range candidates {
		if err := validateCandidateOption(candidate); err != nil {
			return Plan{}, err
		}
		ref := candidate.CandidateRef
		if ref.SourceEventSeq != query.SourceEventSeq ||
			(query.ObservationID != "" &&
				ref.ObservationID != query.ObservationID) {
			return Plan{}, errors.New("candidate source does not match observation query")
		}
		if seen[ref] {
			return Plan{}, errors.New("observation query contains duplicate candidates")
		}
		seen[ref] = true
		observation := ObservationRef{
			SourceEventSeq: ref.SourceEventSeq,
			ProbeID:        ref.ProbeID,
			ObservationID:  ref.ObservationID,
		}
		if !refSeen[observation] {
			refSeen[observation] = true
			refs = append(refs, observation)
		}
	}

	next := clonePlan(current)
	next.Steps[index].ObservationQueries = append(
		next.Steps[index].ObservationQueries,
		query,
	)
	next.Steps[index].ObservationRefs = append(
		next.Steps[index].ObservationRefs,
		refs...,
	)
	next.Steps[index].CandidateRefs = append(
		next.Steps[index].CandidateRefs,
		candidates...,
	)
	next.Steps[index].SelectedCandidateRef = nil
	next.Steps[index].ResolvedTarget = nil
	next.Steps[index].LastError = ""
	next.Steps[index].Status = StepCandidatesAvailable
	next.Status = StatusActive
	return s.appendRevision(ctx, next)
}

func (s *Service) RecordCandidateSelection(
	ctx context.Context,
	runID string,
	planStepID string,
	candidate browsercontract.CandidateRef,
) (Plan, error) {
	current, err := s.activePlan(ctx, runID)
	if err != nil {
		return Plan{}, err
	}
	index, err := currentStepIndex(current, planStepID)
	if err != nil {
		return Plan{}, err
	}
	step := current.Steps[index]
	if step.Status != StepCandidatesAvailable {
		return Plan{}, fmt.Errorf(
			"grounding step %q cannot select a candidate from status %q",
			step.PlanStepID,
			step.Status,
		)
	}
	if err := candidate.Validate(); err != nil {
		return Plan{}, err
	}
	found := false
	for _, option := range step.CandidateRefs {
		if option.CandidateRef == candidate {
			found = true
			break
		}
	}
	if !found {
		return Plan{}, errors.New("selected candidate is not available for the grounding step")
	}
	next := clonePlan(current)
	selected := candidate
	next.Steps[index].SelectedCandidateRef = &selected
	next.Steps[index].Status = StepCandidateSelected
	next.Steps[index].LastError = ""
	return s.appendRevision(ctx, next)
}

func (s *Service) RecordProbeResult(
	ctx context.Context,
	runID string,
	planStepID string,
	evidence *browsercontract.ResolvedTargetEvidence,
	failure string,
) (Plan, error) {
	current, err := s.activePlan(ctx, runID)
	if err != nil {
		return Plan{}, err
	}
	index, err := currentStepIndex(current, planStepID)
	if err != nil {
		return Plan{}, err
	}
	if evidence != nil && strings.TrimSpace(failure) != "" {
		return Plan{}, errors.New("probe result cannot contain evidence and an error")
	}
	if evidence != nil {
		if err := validateResolvedTarget(current.Steps[index], *evidence); err != nil {
			return Plan{}, err
		}
	}
	if current.Steps[index].Status == StepCandidateSelected {
		next := clonePlan(current)
		next.Steps[index].Status = StepProbing
		next.Steps[index].ProbeAttempts = append(
			next.Steps[index].ProbeAttempts,
			ProbeAttempt{Status: StepProbing},
		)
		current, err = s.appendRevision(ctx, next)
		if err != nil {
			return Plan{}, err
		}
		if evidence == nil && strings.TrimSpace(failure) == "" {
			return current, nil
		}
		index, err = currentStepIndex(current, planStepID)
		if err != nil {
			return Plan{}, err
		}
	}
	if current.Steps[index].Status != StepProbing {
		return Plan{}, fmt.Errorf(
			"grounding step %q cannot record a probe from status %q",
			planStepID,
			current.Steps[index].Status,
		)
	}
	if evidence == nil && strings.TrimSpace(failure) == "" {
		return Plan{}, errors.New("probe is already in progress")
	}

	next := clonePlan(current)
	step := &next.Steps[index]
	attempt := &step.ProbeAttempts[len(step.ProbeAttempts)-1]
	if evidence == nil {
		attempt.Status = StepFailed
		attempt.Error = strings.TrimSpace(failure)
		step.Status = StepFailed
		step.LastError = attempt.Error
		next.Status = StatusFailed
		return s.appendRevision(ctx, next)
	}
	resolved := *evidence
	attempt.Status = StepGrounded
	attempt.ResolvedTarget = &resolved
	attempt.Error = ""
	step.Status = StepGrounded
	step.ResolvedTarget = &resolved
	step.LastError = ""
	if currentStepID(next.Steps) == "" {
		next.Status = StatusReady
		next.CurrentPlanStepID = ""
	} else {
		next.Status = StatusActive
	}
	return s.appendRevision(ctx, next)
}

func (s *Service) activePlan(ctx context.Context, runID string) (Plan, error) {
	if strings.TrimSpace(runID) == "" {
		return Plan{}, errors.New("grounding plan run_id is required")
	}
	plan, err := s.repository.GetCurrent(ctx, runID)
	if err != nil {
		return Plan{}, err
	}
	if err := validatePlan(plan); err != nil {
		return Plan{}, fmt.Errorf("validate current grounding plan: %w", err)
	}
	if plan.Status != StatusActive && plan.Status != StatusFailed {
		return Plan{}, fmt.Errorf(
			"grounding plan status %q does not allow mutation",
			plan.Status,
		)
	}
	return plan, nil
}

func (s *Service) appendRevision(
	ctx context.Context,
	plan Plan,
) (Plan, error) {
	plan.Revision++
	plan.UpdatedAt = s.now().UTC()
	plan.CurrentPlanStepID = currentStepID(plan.Steps)
	plan.ID = revisionID(plan, plan.UpdatedAt)
	hash, err := contentHash(plan)
	if err != nil {
		return Plan{}, err
	}
	plan.ContentSHA256 = hash
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	return s.repository.AppendRevision(ctx, plan)
}

func currentStepIndex(plan Plan, planStepID string) (int, error) {
	if planStepID == "" || plan.CurrentPlanStepID != planStepID {
		return -1, errors.New("mutation does not target the current grounding step")
	}
	for index := range plan.Steps {
		if plan.Steps[index].PlanStepID == planStepID {
			return index, nil
		}
	}
	return -1, errors.New("grounding plan current step is missing")
}

func validateCandidateOption(candidate CandidateOption) error {
	if err := candidate.CandidateRef.Validate(); err != nil {
		return fmt.Errorf("candidate option: %w", err)
	}
	if strings.TrimSpace(candidate.ElementRef) == "" ||
		strings.TrimSpace(candidate.Provenance) == "" ||
		candidate.ObservedCount != 1 {
		return errors.New("candidate option is incomplete")
	}
	if err := candidate.Locator.Validate(); err != nil {
		return fmt.Errorf("candidate option locator: %w", err)
	}
	return nil
}

func validateResolvedTarget(
	step Step,
	evidence browsercontract.ResolvedTargetEvidence,
) error {
	if err := evidence.Validate(); err != nil {
		return fmt.Errorf("resolved target evidence: %w", err)
	}
	if evidence.PlanStepID != step.PlanStepID ||
		evidence.Action != step.Action ||
		step.SelectedCandidateRef == nil ||
		evidence.SourceCandidate == nil ||
		*evidence.SourceCandidate != *step.SelectedCandidateRef ||
		evidence.CandidateID != step.SelectedCandidateRef.CandidateID {
		return errors.New("resolved target evidence does not match the selected candidate")
	}
	return nil
}

func revisionID(plan Plan, now time.Time) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s\x00%s\x00%d\x00%s\x00%s",
		plan.RunID,
		plan.TaskPlanID,
		plan.Revision,
		plan.Status,
		now.Format(time.RFC3339Nano),
	)))
	return "grounding_" + hex.EncodeToString(sum[:16])
}
