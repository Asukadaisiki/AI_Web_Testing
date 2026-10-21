// Package event defines versioned messages shared by durable storage and Kafka.
package event

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
)

const supportedSchemaMajor = 1

// Envelope carries a versioned domain event.
type Envelope[T any] struct {
	SchemaVersion string    `json:"schema_version"`
	EventID       string    `json:"event_id"`
	EventType     string    `json:"event_type"`
	AggregateID   string    `json:"aggregate_id"`
	OccurredAt    time.Time `json:"occurred_at"`
	CorrelationID string    `json:"correlation_id"`
	CausationID   string    `json:"causation_id,omitempty"`
	Payload       T         `json:"payload"`
}

// Validate rejects schemas whose major version this process cannot safely interpret.
func (e Envelope[T]) Validate() error {
	major, err := schemaMajor(e.SchemaVersion)
	if err != nil {
		return err
	}
	if major != supportedSchemaMajor {
		return fmt.Errorf("unsupported schema major version %d", major)
	}
	return nil
}

// UnmarshalJSON validates the schema major before accepting an event payload.
func (e *Envelope[T]) UnmarshalJSON(data []byte) error {
	type envelopeAlias Envelope[T]

	var decoded envelopeAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if err := Envelope[T](decoded).Validate(); err != nil {
		return err
	}
	*e = Envelope[T](decoded)
	return nil
}

func schemaMajor(version string) (int, error) {
	lastDot := strings.LastIndex(version, ".v")
	if lastDot < 1 || lastDot+2 == len(version) {
		return 0, fmt.Errorf("invalid schema version %q", version)
	}
	major, err := strconv.Atoi(version[lastDot+2:])
	if err != nil || major < 1 {
		return 0, fmt.Errorf("invalid schema version %q", version)
	}
	return major, nil
}

// ModelRef identifies a server-approved model.
type ModelRef struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

// Callback describes a server-owned operation callback.
type Callback struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// Operation is an immutable operation definition available to a run.
type Operation struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Callback    Callback        `json:"callback"`
}

// RunCommand is the immutable input accepted by the runtime.
type RunCommand struct {
	RunID             string           `json:"run_id"`
	ConversationID    string           `json:"conversation_id"`
	Model             ModelRef         `json:"model"`
	Messages          []domain.Message `json:"messages"`
	Operations        []Operation      `json:"operations"`
	CheckpointVersion int64            `json:"checkpoint_version"`
}
