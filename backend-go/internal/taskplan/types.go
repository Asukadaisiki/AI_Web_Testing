package taskplan

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const SchemaVersion = "agent.task_plan.v1"

var (
	ErrNotFound     = errors.New("task plan not found")
	ErrAccessDenied = errors.New("task plan access denied")
)

type Status string

const (
	StatusGrounding          Status = "grounding"
	StatusReadyForGeneration Status = "ready_for_generation"
	StatusAwaitingApproval   Status = "awaiting_approval"
	StatusApproved           Status = "approved"
	StatusExecuting          Status = "executing"
	StatusCompleted          Status = "completed"
	StatusFailed             Status = "failed"
	StatusBlocked            Status = "blocked"
	StatusSuperseded         Status = "superseded"
)

type StepStatus string

const (
	StepPending  StepStatus = "pending"
	StepGrounded StepStatus = "grounded"
	StepFailed   StepStatus = "failed"
	StepBlocked  StepStatus = "blocked"
)

type SideEffect string

const (
	SideEffectNone         SideEffect = "none"
	SideEffectBrowserState SideEffect = "browser_state"
	SideEffectExternal     SideEffect = "external_state"
	SideEffectUnknown      SideEffect = "unknown"
)

type Binding struct {
	PlanID  string `json:"plan_id"`
	Version int    `json:"version"`
	SHA256  string `json:"sha256"`
}

type EvidenceRef struct {
	Tool          string `json:"tool"`
	EventSeq      int64  `json:"event_seq"`
	ContentSHA256 string `json:"content_sha256"`
}

type Step struct {
	ID                   string        `json:"id"`
	Position             int           `json:"position"`
	Intent               string        `json:"intent"`
	Action               string        `json:"action"`
	Target               string        `json:"target,omitempty"`
	Value                string        `json:"value,omitempty"`
	Trigger              string        `json:"trigger,omitempty"`
	ContextKey           string        `json:"context_key,omitempty"`
	TimeoutMS            int           `json:"timeout_ms,omitempty"`
	ExpectedOccurrences  int           `json:"expected_occurrences"`
	Idempotency          string        `json:"idempotency"`
	SideEffect           SideEffect    `json:"side_effect"`
	Preconditions        []string      `json:"preconditions"`
	CompletionConditions []string      `json:"completion_conditions"`
	Status               StepStatus    `json:"status"`
	GroundingAttempts    int           `json:"grounding_attempts"`
	Evidence             []EvidenceRef `json:"evidence"`
}

type Plan struct {
	ID                string     `json:"id"`
	SchemaVersion     string     `json:"schema_version"`
	RunID             string     `json:"run_id"`
	ActorUserID       int64      `json:"actor_user_id"`
	ProjectID         int64      `json:"project_id"`
	Version           int        `json:"version"`
	Goal              string     `json:"goal"`
	Status            Status     `json:"status"`
	PlanSHA256        string     `json:"plan_sha256"`
	MaxSideEffect     SideEffect `json:"max_side_effect"`
	ForbiddenActions  []string   `json:"forbidden_actions"`
	Steps             []Step     `json:"steps"`
	BoundGenerationID *int64     `json:"bound_generation_id,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (p Plan) Binding() Binding {
	return Binding{PlanID: p.ID, Version: p.Version, SHA256: p.PlanSHA256}
}

type Definition struct {
	Goal             string           `json:"goal"`
	MaxSideEffect    SideEffect       `json:"max_side_effect"`
	ForbiddenActions []string         `json:"forbidden_actions"`
	Steps            []StepDefinition `json:"steps"`
}

type StepDefinition struct {
	ID                   string     `json:"id"`
	Intent               string     `json:"intent"`
	Action               string     `json:"action"`
	Target               string     `json:"target,omitempty"`
	Value                string     `json:"value,omitempty"`
	Trigger              string     `json:"trigger,omitempty"`
	ContextKey           string     `json:"context_key,omitempty"`
	TimeoutMS            int        `json:"timeout_ms,omitempty"`
	ExpectedOccurrences  int        `json:"expected_occurrences"`
	Idempotency          string     `json:"idempotency"`
	SideEffect           SideEffect `json:"side_effect"`
	Preconditions        []string   `json:"preconditions"`
	CompletionConditions []string   `json:"completion_conditions"`
}

type CreateRequest struct {
	RunID       string
	ActorUserID int64
	ProjectID   int64
	Definition  Definition
}

type Repository interface {
	CreateVersion(ctx context.Context, plan Plan) (Plan, error)
	GetCurrent(ctx context.Context, runID string) (Plan, error)
	Save(ctx context.Context, plan Plan) error
}

func clonePlan(plan Plan) Plan {
	raw, _ := json.Marshal(plan)
	var cloned Plan
	_ = json.Unmarshal(raw, &cloned)
	return cloned
}
