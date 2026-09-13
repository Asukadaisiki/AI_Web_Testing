package groundingplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/browsercontract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
)

const SchemaVersion = "grounding.plan.v1"

var (
	ErrNotFound = errors.New("grounding plan not found")
	ErrConflict = errors.New("grounding plan revision conflict")
)

type Status string

const (
	StatusActive     Status = "active"
	StatusReady      Status = "ready"
	StatusFailed     Status = "failed"
	StatusBlocked    Status = "blocked"
	StatusSuperseded Status = "superseded"
)

type StepStatus string

const (
	StepPending             StepStatus = "pending"
	StepQuerying            StepStatus = "querying"
	StepCandidatesAvailable StepStatus = "candidates_available"
	StepCandidateSelected   StepStatus = "candidate_selected"
	StepProbing             StepStatus = "probing"
	StepGrounded            StepStatus = "grounded"
	StepFailed              StepStatus = "failed"
	StepBlocked             StepStatus = "blocked"
)

type QueryRecord struct {
	PlanStepID     string `json:"plan_step_id"`
	SourceEventSeq int64  `json:"source_event_seq"`
	ObservationID  string `json:"observation_id,omitempty"`
	Action         string `json:"action"`
	Query          string `json:"query,omitempty"`
	Role           string `json:"role,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

type ObservationRef struct {
	SourceEventSeq int64  `json:"source_event_seq"`
	ProbeID        string `json:"probe_id,omitempty"`
	ObservationID  string `json:"observation_id,omitempty"`
}

type CandidateDOM struct {
	Tag   string            `json:"tag,omitempty"`
	Text  string            `json:"text,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

type CandidateOption struct {
	CandidateRef  browsercontract.CandidateRef `json:"candidate_ref"`
	ElementRef    string                       `json:"element_ref"`
	Role          string                       `json:"role,omitempty"`
	Name          string                       `json:"name,omitempty"`
	DOM           CandidateDOM                 `json:"dom,omitempty"`
	Locator       browsercontract.LocatorSpec  `json:"locator"`
	Provenance    string                       `json:"provenance"`
	ObservedCount int                          `json:"observed_count"`
}

type ProbeAttempt struct {
	Status         StepStatus                              `json:"status"`
	ResolvedTarget *browsercontract.ResolvedTargetEvidence `json:"resolved_target,omitempty"`
	Error          string                                  `json:"error,omitempty"`
}

type Step struct {
	PlanStepID           string                                  `json:"plan_step_id"`
	Position             int                                     `json:"position"`
	Action               string                                  `json:"action"`
	Status               StepStatus                              `json:"status"`
	ObservationQueries   []QueryRecord                           `json:"observation_queries"`
	ObservationRefs      []ObservationRef                        `json:"observation_refs"`
	CandidateRefs        []CandidateOption                       `json:"candidate_refs"`
	SelectedCandidateRef *browsercontract.CandidateRef           `json:"selected_candidate_ref,omitempty"`
	ProbeAttempts        []ProbeAttempt                          `json:"probe_attempts"`
	ResolvedTarget       *browsercontract.ResolvedTargetEvidence `json:"resolved_target,omitempty"`
	LastError            string                                  `json:"last_error,omitempty"`
}

type Plan struct {
	ID                string    `json:"id"`
	SchemaVersion     string    `json:"schema_version"`
	RunID             string    `json:"run_id"`
	TaskPlanID        string    `json:"task_plan_id"`
	TaskPlanVersion   int       `json:"task_plan_version"`
	TaskPlanSHA256    string    `json:"task_plan_sha256"`
	Revision          int       `json:"revision"`
	Status            Status    `json:"status"`
	CurrentPlanStepID string    `json:"current_plan_step_id,omitempty"`
	Steps             []Step    `json:"steps"`
	ContentSHA256     string    `json:"content_sha256"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (p Plan) TaskPlanBinding() taskplan.Binding {
	return taskplan.Binding{
		PlanID:  p.TaskPlanID,
		Version: p.TaskPlanVersion,
		SHA256:  p.TaskPlanSHA256,
	}
}

type Repository interface {
	CreateInitial(context.Context, Plan) (Plan, error)
	AppendRevision(context.Context, Plan) (Plan, error)
	GetCurrent(context.Context, string) (Plan, error)
	SupersedeForTaskPlan(context.Context, string, time.Time) error
}

func contentHash(plan Plan) (string, error) {
	canonical := struct {
		ID                string `json:"id"`
		SchemaVersion     string `json:"schema_version"`
		RunID             string `json:"run_id"`
		TaskPlanID        string `json:"task_plan_id"`
		TaskPlanVersion   int    `json:"task_plan_version"`
		TaskPlanSHA256    string `json:"task_plan_sha256"`
		Revision          int    `json:"revision"`
		Status            Status `json:"status"`
		CurrentPlanStepID string `json:"current_plan_step_id,omitempty"`
		Steps             []Step `json:"steps"`
	}{
		ID: plan.ID, SchemaVersion: plan.SchemaVersion, RunID: plan.RunID,
		TaskPlanID: plan.TaskPlanID, TaskPlanVersion: plan.TaskPlanVersion,
		TaskPlanSHA256: plan.TaskPlanSHA256, Revision: plan.Revision,
		Status: plan.Status, CurrentPlanStepID: plan.CurrentPlanStepID,
		Steps: plan.Steps,
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode grounding plan canonical JSON: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func validatePlan(plan Plan) error {
	if strings.TrimSpace(plan.ID) == "" ||
		plan.SchemaVersion != SchemaVersion ||
		strings.TrimSpace(plan.RunID) == "" ||
		strings.TrimSpace(plan.TaskPlanID) == "" ||
		plan.TaskPlanVersion < 1 ||
		!validSHA256(plan.TaskPlanSHA256) ||
		plan.Revision < 1 ||
		!validStatus(plan.Status) ||
		len(plan.Steps) == 0 ||
		plan.CreatedAt.IsZero() ||
		plan.UpdatedAt.IsZero() {
		return errors.New("grounding plan is incomplete")
	}
	seen := make(map[string]bool, len(plan.Steps))
	for index, step := range plan.Steps {
		if strings.TrimSpace(step.PlanStepID) == "" ||
			seen[step.PlanStepID] ||
			step.Position != index ||
			strings.TrimSpace(step.Action) == "" ||
			!validStepStatus(step.Status) {
			return fmt.Errorf("grounding step %d is invalid", index)
		}
		seen[step.PlanStepID] = true
	}
	expectedCurrent := currentStepID(plan.Steps)
	if plan.Status == StatusReady {
		if expectedCurrent != "" || plan.CurrentPlanStepID != "" {
			return errors.New("ready grounding plan contains an unfinished step")
		}
	} else if plan.Status != StatusSuperseded &&
		plan.CurrentPlanStepID != expectedCurrent {
		return errors.New("grounding plan current step does not match its steps")
	}
	hash, err := contentHash(plan)
	if err != nil {
		return err
	}
	if plan.ContentSHA256 != hash {
		return errors.New("grounding plan content hash does not match")
	}
	return nil
}

func validateTaskPlan(plan taskplan.Plan) error {
	binding := plan.Binding()
	if strings.TrimSpace(plan.ID) == "" ||
		strings.TrimSpace(plan.RunID) == "" ||
		binding.Version < 1 ||
		!validSHA256(binding.SHA256) ||
		len(plan.Steps) == 0 {
		return errors.New("task plan is incomplete for grounding")
	}
	seen := make(map[string]bool, len(plan.Steps))
	for index, step := range plan.Steps {
		if strings.TrimSpace(step.ID) == "" ||
			seen[step.ID] ||
			step.Position != index ||
			strings.TrimSpace(step.Action) == "" {
			return fmt.Errorf("task plan step %d is invalid for grounding", index)
		}
		seen[step.ID] = true
	}
	return nil
}

func validateAgainstTaskPlan(plan Plan, source taskplan.Plan) error {
	if plan.RunID != source.RunID ||
		plan.TaskPlanBinding() != source.Binding() ||
		len(plan.Steps) != len(source.Steps) {
		return errors.New("grounding plan does not match its task plan")
	}
	for index, step := range plan.Steps {
		sourceStep := source.Steps[index]
		if step.PlanStepID != sourceStep.ID ||
			step.Position != sourceStep.Position ||
			step.Action != sourceStep.Action {
			return errors.New("grounding plan steps do not match their task plan")
		}
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validStatus(status Status) bool {
	switch status {
	case StatusActive, StatusReady, StatusFailed, StatusBlocked, StatusSuperseded:
		return true
	default:
		return false
	}
}

func validStepStatus(status StepStatus) bool {
	switch status {
	case StepPending, StepQuerying, StepCandidatesAvailable,
		StepCandidateSelected, StepProbing, StepGrounded,
		StepFailed, StepBlocked:
		return true
	default:
		return false
	}
}

func currentStepID(steps []Step) string {
	for _, step := range steps {
		if step.Status != StepGrounded {
			return step.PlanStepID
		}
	}
	return ""
}

func clonePlan(plan Plan) Plan {
	raw, _ := json.Marshal(plan)
	var cloned Plan
	_ = json.Unmarshal(raw, &cloned)
	return cloned
}
