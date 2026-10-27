package domain

import (
	"encoding/json"
	"time"
)

// Message is one ordered entry in a conversation.
type Message struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversation_id"`
	RunID          string          `json:"run_id,omitempty"`
	Ordinal        int64           `json:"ordinal"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// Run is the durable, immutable request specification and its lifecycle state.
type Run struct {
	ID                string          `json:"id"`
	ProjectID         string          `json:"project_id"`
	ConversationID    string          `json:"conversation_id"`
	SourceMessageID   string          `json:"source_message_id,omitempty"`
	RetryOfRunID      string          `json:"retry_of_run_id,omitempty"`
	Status            RunStatus       `json:"status"`
	Specification     json.RawMessage `json:"specification"`
	CheckpointVersion int64           `json:"checkpoint_version"`
	CancelRequestedAt *time.Time      `json:"cancel_requested_at,omitempty"`
	StartedAt         *time.Time      `json:"started_at,omitempty"`
	FinishedAt        *time.Time      `json:"finished_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// Span is a durable trace-tree node.
type Span struct {
	ID             string          `json:"id"`
	RunID          string          `json:"run_id"`
	TraceID        string          `json:"trace_id"`
	SpanID         string          `json:"span_id"`
	ParentSpanID   string          `json:"parent_span_id,omitempty"`
	Kind           SpanKind        `json:"kind"`
	Name           string          `json:"name"`
	Status         string          `json:"status"`
	Input          json.RawMessage `json:"input,omitempty"`
	Output         json.RawMessage `json:"output,omitempty"`
	StartedAt      time.Time       `json:"started_at"`
	EndedAt        *time.Time      `json:"ended_at,omitempty"`
	SourceSequence int64           `json:"source_sequence,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}
