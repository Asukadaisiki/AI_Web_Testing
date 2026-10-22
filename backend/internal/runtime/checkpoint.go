package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/model"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/operation"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
)

// StepKind identifies the durable work committed at one checkpoint.
type StepKind string

const (
	StepModel     StepKind = "model"
	StepOperation StepKind = "operation"
)

// CompletionStatus is a terminal run state.
type CompletionStatus string

const (
	CompletionSucceeded CompletionStatus = "succeeded"
	CompletionFailed    CompletionStatus = "failed"
	CompletionCancelled CompletionStatus = "cancelled"
)

// OperationCall is a model-selected call that must be resumed before another model turn.
type OperationCall struct {
	ToolCallID  string          `json:"tool_call_id"`
	Name        string          `json:"name"`
	Arguments   json.RawMessage `json:"arguments"`
	OperationID string          `json:"operation_id"`
}

// Checkpoint is the complete durable state needed to resume a run.
type Checkpoint struct {
	RunID            string
	Command          event.RunCommand
	NextCheckpoint   int64
	ModelCalls       int
	Messages         []model.Message
	PendingOperation *OperationCall
	CancelRequested  bool
	Completed        bool
}

// StepResult is atomically persisted by a CheckpointStore before any live append.
type StepResult struct {
	RunID           string
	Checkpoint      int64
	Kind            StepKind
	LogicalSpanID   string
	AttemptID       string
	ModelRequest    model.Request
	Model           model.Response
	OperationCall   *OperationCall
	OperationResult operation.Result
}

// Completion records a durable terminal outcome.
type Completion struct {
	RunID          string
	Status         CompletionStatus
	Assistant      string
	Error          string
	NextCheckpoint int64
}

// CheckpointStore persists checkpoints, trace evidence, durable events, and terminal state.
type CheckpointStore interface {
	Load(context.Context, string) (Checkpoint, error)
	StartAttempt(context.Context, StepResult) error
	CommitStep(context.Context, StepResult) error
	CompleteRun(context.Context, Completion) error
}

// LogicalSpanID derives a stable logical span identity from run, checkpoint, and kind.
func LogicalSpanID(runID string, checkpoint int64, kind StepKind) string {
	return stableID("span", runID, fmt.Sprintf("%d", checkpoint), string(kind))
}

// RunSpanID derives the deterministic root span for a run.
func RunSpanID(runID string) string {
	return stableID("span", runID, "run")
}

// AttemptID derives a stable attempt identity from its logical span and attempt number.
func AttemptID(logicalSpanID string, attempt int) string {
	return stableID("attempt", logicalSpanID, fmt.Sprintf("%d", attempt))
}

// OperationID derives the stable persisted effect key for one operation checkpoint.
func OperationID(runID string, checkpoint int64) string {
	return stableID("operation", runID, fmt.Sprintf("%d", checkpoint))
}

func stableID(prefix string, values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(value))
	}
	return prefix + "_" + hex.EncodeToString(hash.Sum(nil)[:16])
}

func initialMessages(checkpoint Checkpoint) []model.Message {
	messages := make([]model.Message, 0, len(checkpoint.Command.Messages)+len(checkpoint.Messages))
	for _, message := range checkpoint.Command.Messages {
		role := model.Role(message.Role)
		if role == "" {
			role = model.RoleUser
		}
		messages = append(messages, model.Message{Role: role, Content: message.Content})
	}
	messages = append(messages, checkpoint.Messages...)
	return messages
}

var (
	// ErrModelCallBudget means the durable model-call limit ended the run.
	ErrModelCallBudget = errors.New("model call budget exhausted")
	// ErrCancelled means authoritative durable cancellation ended the run.
	ErrCancelled = errors.New("run cancelled")
)

func stepEventData(step StepResult) json.RawMessage {
	value, _ := json.Marshal(map[string]any{
		"checkpoint": step.Checkpoint,
		"kind":       step.Kind,
		"span_id":    step.LogicalSpanID,
	})
	return value
}

func usageFor(response model.Response) usage.Usage {
	return response.Usage.Normalize()
}
