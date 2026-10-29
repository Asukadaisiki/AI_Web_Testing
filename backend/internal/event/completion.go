package event

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
)

const RunCompleted = "run.completed"

// RunCompletion is the application completion contract, independent of the
// executor. Producers must use the effective terminal status under the run lock.
type RunCompletion struct {
	RunID  string           `json:"run_id"`
	Status domain.RunStatus `json:"status"`
	Error  json.RawMessage  `json:"error,omitempty"`
}

func IsRunCompletion(kind string) bool {
	return kind == RunCompleted || kind == "runtime.completed" || kind == "harness.completed"
}

// DecodeRunCompletion accepts legacy events only to drain already queued v1
// deliveries. Remove these aliases after their outbox and consumer backlog drain.
// New producers must emit RunCompleted with an explicit run_id.
func DecodeRunCompletion(kind, aggregateID string, raw json.RawMessage) (RunCompletion, error) {
	var completion RunCompletion
	if !IsRunCompletion(kind) {
		return completion, errors.New("unsupported run completion type")
	}
	if err := json.Unmarshal(raw, &completion); err != nil {
		return completion, errors.New("decode run completion payload")
	}
	if kind == "runtime.completed" && completion.RunID == "" {
		var legacy struct {
			RunID string
		}
		if err := json.Unmarshal(raw, &legacy); err != nil {
			return completion, errors.New("decode legacy run completion payload")
		}
		completion.RunID = legacy.RunID
	}
	if kind == "harness.completed" && completion.RunID == "" {
		completion.RunID = aggregateID
	}
	if strings.TrimSpace(aggregateID) == "" || completion.RunID != aggregateID {
		return completion, errors.New("run completion payload does not match aggregate")
	}
	switch completion.Status {
	case domain.RunSucceeded, domain.RunFailed, domain.RunCancelled:
	default:
		return completion, errors.New("run completion status is not terminal")
	}
	return completion, nil
}
