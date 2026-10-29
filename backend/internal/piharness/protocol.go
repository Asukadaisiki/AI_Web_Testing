// Package piharness mirrors the pi-browser-harness.v1 wire protocol in Go and
// provides a strict JSONL decoder plus a subprocess runner that drives the
// harness over stdin/stdout.
package piharness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
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
	ProtocolVersion   string          `json:"protocol_version"`
	RunID             string          `json:"run_id"`
	AttemptID         string          `json:"attempt_id"`
	Goal              string          `json:"goal"`
	Model             ModelRequest    `json:"model"`
	WorkerURL         string          `json:"worker_url,omitempty"`
	ArtifactScope     string          `json:"artifact_scope,omitempty"`
	Execution         *execution.Spec `json:"execution,omitempty"`
	ProjectID         string          `json:"project_id,omitempty"`
	EnvironmentID     string          `json:"environment_id,omitempty"`
	ProfileGeneration int64           `json:"profile_generation,omitempty"`
	FencingToken      int64           `json:"fencing_token,omitempty"`
}

// MarshalJSON serializes the request. It enforces the protocol version constant
// so callers cannot accidentally ship a different version.
func (r Request) MarshalJSON() ([]byte, error) {
	type alias Request
	if err := r.validateEnvironmentLease(); err != nil {
		return nil, err
	}
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
	if err := r.validateEnvironmentLease(); err != nil {
		return err
	}
	return nil
}

func (r Request) validateEnvironmentLease() error {
	if r.ProjectID == "" && r.EnvironmentID == "" && r.ProfileGeneration == 0 && r.FencingToken == 0 {
		return nil
	}
	if strings.TrimSpace(r.ProjectID) == "" || strings.TrimSpace(r.EnvironmentID) == "" ||
		r.ProfileGeneration < 1 || r.ProfileGeneration > 9007199254740991 || r.FencingToken < 1 || r.FencingToken > 9007199254740991 {
		return errors.New("piharness: complete managed environment lease identity is required")
	}
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
	Text     string `json:"text"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	// Input is the model transcript (messages plus optional systemPrompt/tools),
	// captured after Pi transformations. Timing is an optional UTC pair.
	Input     json.RawMessage `json:"input,omitempty"`
	StartedAt string          `json:"started_at,omitempty"`
	EndedAt   string          `json:"ended_at,omitempty"`
}

type UsagePayload struct {
	Provider         string `json:"provider,omitempty"`
	Model            string `json:"model,omitempty"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  *int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int64 `json:"cache_write_tokens,omitempty"`
	TotalTokens      *int64 `json:"total_tokens,omitempty"`
}

type ToolStartedPayload struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
}

type ToolCompletedPayload struct {
	ToolCallID       string          `json:"tool_call_id"`
	ToolName         string          `json:"tool_name"`
	IsError          bool            `json:"is_error"`
	StructuredResult json.RawMessage `json:"structured_result"`
}

type ObservationPayload struct {
	ToolCallID  string `json:"tool_call_id"`
	Observation any    `json:"observation"`
}

type CaseCompletedPayload struct {
	Verdict string          `json:"verdict"`
	Case    json.RawMessage `json:"case"`
	DryRun  json.RawMessage `json:"dry_run"`
}

type SettledPayload struct {
	AssistantText string `json:"assistant_text"`
	// Verdict belongs to the selected Harness policy, not the transport or observer.
	Verdict string `json:"verdict"`
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
	ErrInvalidPayload    = errors.New("piharness: invalid event payload")
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
	if err := validateEventPayload(ev); err != nil {
		return nil, fmt.Errorf("%w for %s: %v", ErrInvalidPayload, ev.Type, err)
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

func validateEventPayload(ev Event) error {
	payload, err := objectPayload(ev.Payload)
	if err != nil {
		return fmt.Errorf("%w for %s: %v", ErrInvalidPayload, ev.Type, err)
	}

	if ev.Type == EventMessageCompleted || ev.Type == EventUsage {
		for _, field := range []string{"provider", "model"} {
			if _, present := payload[field]; present {
				if err := requireNonBlankString(payload, field); err != nil {
					return err
				}
			}
		}
	}

	switch ev.Type {
	case EventStarted:
		return validateExactKeys(payload, nil)
	case EventMessageCompleted:
		_, hasStart := payload["started_at"]
		_, hasEnd := payload["ended_at"]
		if hasStart != hasEnd {
			return errors.New("started_at and ended_at must be supplied together")
		}
		if hasStart {
			start, err := requiredTimestamp(payload, "started_at")
			if err != nil {
				return err
			}
			end, err := requiredTimestamp(payload, "ended_at")
			if err != nil {
				return err
			}
			if end.Before(start) {
				return errors.New("ended_at must not precede started_at")
			}
			_, startOffset := start.Zone()
			_, endOffset := end.Zone()
			if startOffset != 0 || endOffset != 0 {
				return errors.New("model timing must be UTC")
			}
		}
		if raw, present := payload["input"]; present {
			input, err := objectPayload(raw)
			if err != nil {
				return fmt.Errorf("input: %w", err)
			}
			messages, present := input["messages"]
			if !present || len(bytes.TrimSpace(messages)) == 0 || bytes.TrimSpace(messages)[0] != '[' {
				return errors.New("input.messages must be an array")
			}
			if _, present := input["systemPrompt"]; present {
				if err := requireString(input, "systemPrompt"); err != nil {
					return err
				}
			}
			if tools, present := input["tools"]; present {
				if len(bytes.TrimSpace(tools)) == 0 || bytes.TrimSpace(tools)[0] != '[' {
					return errors.New("input.tools must be an array")
				}
			}
		}
		return requireString(payload, "text")
	case EventUsage:
		if err := requireNonNegativeInt(payload, "input_tokens"); err != nil {
			return err
		}
		if err := requireNonNegativeInt(payload, "output_tokens"); err != nil {
			return err
		}
		for _, key := range []string{"cache_read_tokens", "cache_write_tokens", "total_tokens"} {
			if _, present := payload[key]; present {
				if err := requireNonNegativeInt(payload, key); err != nil {
					return err
				}
			}
		}
		return nil
	case EventToolStarted:
		if err := requireString(payload, "tool_call_id"); err != nil {
			return err
		}
		return requireString(payload, "tool_name")
	case EventToolCompleted:
		if err := requireString(payload, "tool_call_id"); err != nil {
			return err
		}
		if err := requireString(payload, "tool_name"); err != nil {
			return err
		}
		if err := requireBool(payload, "is_error"); err != nil {
			return err
		}
		if raw, ok := payload["structured_result"]; ok {
			if _, err := objectPayload(raw); err != nil {
				return fmt.Errorf("structured_result: %w", err)
			}
		}
		return nil
	case EventObservation:
		if err := validateExactKeys(payload, []string{"tool_call_id", "observation"}); err != nil {
			return err
		}
		return requireNonBlankString(payload, "tool_call_id")
	case EventCaseCompleted:
		return validateCaseCompletion(payload)
	case EventSettled:
		if raw, present := payload["verdict"]; present {
			var verdict string
			if err := json.Unmarshal(raw, &verdict); err != nil || (verdict != "succeeded" && verdict != "failed") {
				return errors.New("verdict must be succeeded or failed")
			}
		} else {
			return errors.New("verdict is required")
		}
		return requireString(payload, "assistant_text")
	case EventFailed:
		return requireString(payload, "error")
	default:
		return fmt.Errorf("unsupported event type %q", ev.Type)
	}
}

func validateCaseCompletion(payload map[string]json.RawMessage) error {
	if err := validateExactKeys(payload, []string{"verdict", "case", "dry_run"}); err != nil {
		return err
	}
	verdict, err := requiredString(payload, "verdict")
	if err != nil {
		return err
	}
	if verdict != "passed" && verdict != "failed" {
		return errors.New("verdict must be passed or failed")
	}
	caseValue, err := contract.Validate(payload["case"])
	if err != nil {
		return fmt.Errorf("case: %w", err)
	}
	dryRunStatus, err := validateDryRun(payload["dry_run"], caseValue)
	if err != nil {
		return fmt.Errorf("dry_run: %w", err)
	}
	if (verdict == "passed") != (dryRunStatus == "passed") {
		return errors.New("verdict must match dry_run.status")
	}
	return nil
}

func validateDryRun(raw json.RawMessage, browserCase contract.Case) (string, error) {
	value, err := objectPayload(raw)
	if err != nil {
		return "", err
	}
	if err := validateExactKeys(value, []string{
		"execution_id", "status", "started_at", "finished_at", "final_url", "steps", "error",
	}); err != nil {
		return "", err
	}
	executionID, err := requiredString(value, "execution_id")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(executionID) == "" {
		return "", errors.New("execution_id must be non-blank")
	}
	status, err := requiredString(value, "status")
	if err != nil {
		return "", err
	}
	if status != "passed" && status != "failed" && status != "error" {
		return "", errors.New("status must be passed, failed, or error")
	}
	startedAt, err := requiredTimestamp(value, "started_at")
	if err != nil {
		return "", err
	}
	finishedAt, err := requiredTimestamp(value, "finished_at")
	if err != nil {
		return "", err
	}
	if finishedAt.Before(startedAt) {
		return "", errors.New("finished_at must not precede started_at")
	}
	finalURL, err := requiredString(value, "final_url")
	if err != nil {
		return "", err
	}
	parsedURL, err := url.ParseRequestURI(finalURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", errors.New("final_url must be an absolute URL")
	}
	if err := requireStepError(value, "error"); err != nil {
		return "", err
	}

	var steps []json.RawMessage
	if err := json.Unmarshal(value["steps"], &steps); err != nil {
		return "", errors.New("steps must be an array")
	}
	if len(steps) != len(browserCase.Steps) {
		return "", errors.New("steps must cover the complete browser case")
	}
	for index, rawStep := range steps {
		if err := validateDryRunStep(rawStep, index, browserCase.Steps[index].Action); err != nil {
			return "", err
		}
	}
	return status, nil
}

func validateDryRunStep(raw json.RawMessage, index int, action contract.Action) error {
	step, err := objectPayload(raw)
	if err != nil {
		return err
	}
	if err := validateAllowedKeys(step, []string{
		"index", "action", "status", "started_at", "duration_ms", "url_before", "url_after",
		"conditions", "evidence", "error", "blocker", "hit_test", "recovery",
	}); err != nil {
		return err
	}
	stepIndex, err := requiredInt(step, "index")
	if err != nil {
		return err
	}
	if stepIndex != index {
		return fmt.Errorf("steps[%d].index must be %d", index, index)
	}
	stepAction, err := requiredString(step, "action")
	if err != nil {
		return err
	}
	if stepAction != string(action) {
		return fmt.Errorf("steps[%d].action must match browser case", index)
	}
	stepStatus, err := requiredString(step, "status")
	if err != nil {
		return err
	}
	if stepStatus != "passed" && stepStatus != "failed" {
		return fmt.Errorf("steps[%d].status must be passed or failed", index)
	}
	if _, err := requiredTimestamp(step, "started_at"); err != nil {
		return err
	}
	if err := requireNonNegativeInt(step, "duration_ms"); err != nil {
		return err
	}
	if _, err := requiredString(step, "url_before"); err != nil {
		return err
	}
	if _, err := requiredString(step, "url_after"); err != nil {
		return err
	}
	conditions, err := requiredArray(step, "conditions")
	if err != nil {
		return err
	}
	for index, condition := range conditions {
		if err := validateConditionResult(condition); err != nil {
			return fmt.Errorf("conditions[%d]: %w", index, err)
		}
	}
	if err := validateEvidence(step["evidence"]); err != nil {
		return err
	}
	return requireStepError(step, "error")
}

func validateEvidence(raw json.RawMessage) error {
	evidence, err := objectPayload(raw)
	if err != nil {
		return err
	}
	if err := validateExactKeys(evidence, []string{"screenshot_path", "console", "network"}); err != nil {
		return err
	}
	if err := requireNullableString(evidence, "screenshot_path"); err != nil {
		return err
	}
	console, err := requiredArray(evidence, "console")
	if err != nil {
		return err
	}
	for index, entry := range console {
		if err := validateConsoleEntry(entry); err != nil {
			return fmt.Errorf("console[%d]: %w", index, err)
		}
	}
	network, err := requiredArray(evidence, "network")
	if err != nil {
		return err
	}
	for index, entry := range network {
		if err := validateNetworkEntry(entry); err != nil {
			return fmt.Errorf("network[%d]: %w", index, err)
		}
	}
	return nil
}

func validateConditionResult(raw json.RawMessage) error {
	condition, err := objectPayload(raw)
	if err != nil {
		return err
	}
	phase, err := requiredString(condition, "phase")
	if err != nil {
		return err
	}
	if phase != "pre" && phase != "post" {
		return errors.New("phase must be pre or post")
	}
	if err := requireExecutionString(condition, "type"); err != nil {
		return err
	}
	if err := requireExecutionString(condition, "value"); err != nil {
		return err
	}
	if err := requireExecutionBool(condition, "satisfied"); err != nil {
		return err
	}
	return requireNullableString(condition, "detail")
}

func validateConsoleEntry(raw json.RawMessage) error {
	entry, err := objectPayload(raw)
	if err != nil {
		return err
	}
	if err := requireExecutionString(entry, "level"); err != nil {
		return err
	}
	return requireExecutionString(entry, "text")
}

func validateNetworkEntry(raw json.RawMessage) error {
	entry, err := objectPayload(raw)
	if err != nil {
		return err
	}
	if err := requireExecutionString(entry, "method"); err != nil {
		return err
	}
	if err := requireExecutionString(entry, "url"); err != nil {
		return err
	}
	status, ok := entry["status"]
	if !ok {
		return errors.New("status is required")
	}
	if bytes.Equal(bytes.TrimSpace(status), []byte("null")) {
		return nil
	}
	// Match TypeScript's Number.isInteger, including decimal and exponent JSON.
	var decoded float64
	if err := json.Unmarshal(status, &decoded); err != nil || math.Trunc(decoded) != decoded {
		return errors.New("status must be an integer or null")
	}
	return nil
}

// Pointer decoding rejects null rather than accepting a primitive zero value.
func requireExecutionString(value map[string]json.RawMessage, field string) error {
	raw, ok := value[field]
	if !ok {
		return fmt.Errorf("%s is required", field)
	}
	var decoded *string
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
		return fmt.Errorf("%s must be a string", field)
	}
	return nil
}

func requireExecutionBool(value map[string]json.RawMessage, field string) error {
	raw, ok := value[field]
	if !ok {
		return fmt.Errorf("%s is required", field)
	}
	var decoded *bool
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
		return fmt.Errorf("%s must be a boolean", field)
	}
	return nil
}

func requireStepError(value map[string]json.RawMessage, field string) error {
	raw, ok := value[field]
	if !ok {
		return fmt.Errorf("%s is required", field)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	stepError, err := objectPayload(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	if err := validateExactKeys(stepError, []string{"kind", "message"}); err != nil {
		return err
	}
	if err := requireString(stepError, "kind"); err != nil {
		return err
	}
	return requireString(stepError, "message")
}

func objectPayload(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("must be an object")
	}
	if value == nil {
		return nil, errors.New("must be an object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("must contain one JSON value")
	}
	return value, nil
}

func validateExactKeys(value map[string]json.RawMessage, expected []string) error {
	if len(value) != len(expected) {
		return errors.New("has missing or unexpected fields")
	}
	if err := validateAllowedKeys(value, expected); err != nil {
		return err
	}
	for _, key := range expected {
		if _, ok := value[key]; !ok {
			return fmt.Errorf("%s is required", key)
		}
	}
	return nil
}

func validateAllowedKeys(value map[string]json.RawMessage, allowed []string) error {
	for key := range value {
		if !containsKey(allowed, key) {
			return fmt.Errorf("unexpected field %q", key)
		}
	}
	return nil
}

func containsKey(keys []string, want string) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
}

func requiredString(value map[string]json.RawMessage, field string) (string, error) {
	raw, ok := value[field]
	if !ok {
		return "", fmt.Errorf("%s is required", field)
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("%s must be a string", field)
	}
	return decoded, nil
}

func requireString(value map[string]json.RawMessage, field string) error {
	_, err := requiredString(value, field)
	return err
}

func requireNonBlankString(value map[string]json.RawMessage, field string) error {
	decoded, err := requiredString(value, field)
	if err != nil {
		return err
	}
	if strings.TrimSpace(decoded) == "" {
		return fmt.Errorf("%s must be non-blank", field)
	}
	return nil
}

func requireBool(value map[string]json.RawMessage, field string) error {
	raw, ok := value[field]
	if !ok {
		return fmt.Errorf("%s is required", field)
	}
	var decoded bool
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("%s must be a boolean", field)
	}
	return nil
}

func requiredInt(value map[string]json.RawMessage, field string) (int, error) {
	raw, ok := value[field]
	if !ok {
		return 0, fmt.Errorf("%s is required", field)
	}
	var decoded int
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return 0, fmt.Errorf("%s must be an integer", field)
	}
	return decoded, nil
}

func requireNonNegativeInt(value map[string]json.RawMessage, field string) error {
	decoded, err := requiredInt(value, field)
	if err != nil {
		return err
	}
	if decoded < 0 {
		return fmt.Errorf("%s must be non-negative", field)
	}
	return nil
}

func requiredArray(value map[string]json.RawMessage, field string) ([]json.RawMessage, error) {
	raw, ok := value[field]
	if !ok {
		return nil, fmt.Errorf("%s is required", field)
	}
	var decoded []json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
		return nil, fmt.Errorf("%s must be an array", field)
	}
	return decoded, nil
}

func requireNullableString(value map[string]json.RawMessage, field string) error {
	raw, ok := value[field]
	if !ok {
		return fmt.Errorf("%s is required", field)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	return requireString(value, field)
}

func requiredTimestamp(value map[string]json.RawMessage, field string) (time.Time, error) {
	text, err := requiredString(value, field)
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339", field)
	}
	return parsed, nil
}
