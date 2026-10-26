// Package piharness mirrors the pi-browser-harness.v1 wire protocol in Go and
// provides a strict JSONL decoder plus a subprocess runner that drives the
// harness over stdin/stdout.
package piharness

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ProtocolVersion is the exact protocol identifier this package speaks.
const ProtocolVersion = "pi-browser-harness.v1"

// ModelRequest identifies the model the harness should drive.
type ModelRequest struct {
	Provider        string `json:"provider"`
	Name            string `json:"name"`
	ReasoningEffort string `json:"reasoning_effort"`
}

// Request is the single JSON object written to the harness stdin.
type Request struct {
	ProtocolVersion string       `json:"protocol_version"`
	RunID           string       `json:"run_id"`
	AttemptID       string       `json:"attempt_id"`
	Goal            string       `json:"goal"`
	Model           ModelRequest `json:"model"`
	WorkerURL       string       `json:"worker_url"`
	ArtifactScope   string       `json:"artifact_scope"`
}

// MarshalJSON serializes the request. It enforces the protocol version constant
// so callers cannot accidentally ship a different version.
func (r Request) MarshalJSON() ([]byte, error) {
	type alias Request
	if r.ProtocolVersion == "" {
		r.ProtocolVersion = ProtocolVersion
	}
	return json.Marshal(alias(r))
}

// UnmarshalJSON validates the protocol version before accepting the request.
func (r *Request) UnmarshalJSON(data []byte) error {
	type alias Request
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("piharness: unsupported protocol version %q", decoded.ProtocolVersion)
	}
	*r = Request(decoded)
	return nil
}

// EventType enumerates the nine harness event discriminants.
type EventType string

const (
	EventStarted          EventType = "harness.started"
	EventMessageCompleted EventType = "pi.message.completed"
	EventUsage            EventType = "pi.usage"
	EventToolStarted      EventType = "pi.tool.started"
	EventToolCompleted    EventType = "pi.tool.completed"
	EventObservation      EventType = "browser.observation"
	EventCaseCompleted    EventType = "browser.case.completed"
	EventSettled          EventType = "harness.settled"
	EventFailed           EventType = "harness.failed"
)

var knownEventTypes = map[EventType]struct{}{
	EventStarted:          {},
	EventMessageCompleted: {},
	EventUsage:            {},
	EventToolStarted:      {},
	EventToolCompleted:    {},
	EventObservation:      {},
	EventCaseCompleted:    {},
	EventSettled:          {},
	EventFailed:           {},
}

func isTerminal(t EventType) bool {
	return t == EventSettled || t == EventFailed
}

// Event is a single JSONL record emitted by the harness. The Payload is kept as
// raw JSON so callers can decode it into the typed payload struct below.
type Event struct {
	Type      EventType       `json:"type"`
	RunID     string          `json:"run_id"`
	AttemptID string          `json:"attempt_id"`
	Sequence  int64           `json:"sequence"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// TimestampTime parses the event timestamp as a UTC time. It assumes the
// decoder already validated the value; a malformed timestamp returns the zero
// time.
func (e Event) TimestampTime() time.Time {
	t, err := time.Parse(time.RFC3339Nano, e.Timestamp)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// Typed payload structs. Each mirrors the corresponding TypeScript interface in
// run-protocol.ts. Extra keys are preserved by decoding into json.RawMessage on
// Event; these structs are convenience accessors for the documented fields.

type StartedPayload struct{}

type MessageCompletedPayload struct {
	Text string `json:"text"`
}

type UsagePayload struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type ToolStartedPayload struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
}

type ToolCompletedPayload struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	IsError    bool   `json:"is_error"`
}

type ObservationPayload struct {
	Observation any `json:"observation"`
}

type CaseCompletedPayload struct {
	Verdict string `json:"verdict"`
}

type SettledPayload struct {
	AssistantText string `json:"assistant_text"`
}

type FailedPayload struct {
	Error string `json:"error"`
}

// Sentinel protocol errors. Callers can match these with errors.Is.
var (
	ErrMalformedJSONL    = errors.New("piharness: malformed JSONL record")
	ErrUnknownEventType  = errors.New("piharness: unknown event type")
	ErrRunIDMismatch     = errors.New("piharness: run_id does not match request")
	ErrAttemptIDMismatch = errors.New("piharness: attempt_id does not match request")
	ErrInvalidSequence   = errors.New("piharness: invalid event sequence")
	ErrInvalidTimestamp  = errors.New("piharness: invalid or non-UTC timestamp")
	ErrSecondTerminal    = errors.New("piharness: second terminal record")
)

// Decoder validates a stream of JSONL harness events against the request
// identifiers and protocol invariants. It is not safe for concurrent use.
type Decoder struct {
	runID        string
	attemptID    string
	nextSequence int64
	terminalSeen bool
}

// NewDecoder creates a decoder that expects events for the given run and
// attempt. The first event must have sequence 1.
func NewDecoder(runID, attemptID string) *Decoder {
	return &Decoder{
		runID:        runID,
		attemptID:    attemptID,
		nextSequence: 1,
	}
}

// TerminalSeen reports whether a terminal event (harness.settled or
// harness.failed) has been decoded.
func (d *Decoder) TerminalSeen() bool {
	return d.terminalSeen
}

// Decode parses and validates one JSONL line.
func (d *Decoder) Decode(line []byte) (*Event, error) {
	var ev Event
	if err := json.Unmarshal(line, &ev); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedJSONL, err)
	}

	if _, ok := knownEventTypes[ev.Type]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownEventType, ev.Type)
	}

	if ev.RunID != d.runID {
		return nil, fmt.Errorf("%w: got %q want %q", ErrRunIDMismatch, ev.RunID, d.runID)
	}
	if ev.AttemptID != d.attemptID {
		return nil, fmt.Errorf("%w: got %q want %q", ErrAttemptIDMismatch, ev.AttemptID, d.attemptID)
	}

	if ev.Sequence != d.nextSequence {
		return nil, fmt.Errorf("%w: got %d want %d", ErrInvalidSequence, ev.Sequence, d.nextSequence)
	}

	t, err := time.Parse(time.RFC3339Nano, ev.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTimestamp, err)
	}
	if _, offset := t.Zone(); offset != 0 {
		return nil, fmt.Errorf("%w: timestamp is not UTC", ErrInvalidTimestamp)
	}

	if d.terminalSeen {
		return nil, ErrSecondTerminal
	}
	if isTerminal(ev.Type) {
		d.terminalSeen = true
	}

	d.nextSequence++
	return &ev, nil
}
