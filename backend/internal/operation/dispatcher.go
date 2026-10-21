// Package operation defines the runtime boundary for idempotent operations.
package operation

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Call is a model-selected operation invocation with stable tracing identities.
type Call struct {
	RunID       string          `json:"run_id"`
	SpanID      string          `json:"span_id"`
	OperationID string          `json:"operation_id"`
	ToolCallID  string          `json:"tool_call_id"`
	Name        string          `json:"name"`
	Arguments   json.RawMessage `json:"arguments"`
}

// Result is the durable result of an operation invocation.
type Result struct {
	Status    string          `json:"status"`
	Output    json.RawMessage `json:"output,omitempty"`
	Artifacts json.RawMessage `json:"artifacts,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
}

// Definition is a server-owned operation transport and validation policy.
type Definition struct {
	Name           string
	Description    string
	InputSchema    json.RawMessage
	CallbackURL    string
	Timeout        time.Duration
	MaxAttempts    int
	RetryBackoff   time.Duration
	MaxResultBytes int64
}

// HTTPConfig contains server-owned dependencies and callback allowlisting.
type HTTPConfig struct {
	Client               *http.Client
	Operations           []Definition
	ApprovedCallbackURLs []string
}

// Dispatcher executes an operation. Implementations must use OperationID as
// their idempotency key when crossing a process boundary.
type Dispatcher interface {
	Dispatch(context.Context, Call) (Result, error)
}
