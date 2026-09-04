package agentcore

import (
	"context"
	"errors"
)

var (
	ErrRunNotFound          = errors.New("agent run not found")
	ErrRunNotWaitingForUser = errors.New("agent run is not waiting for user input")
	ErrToolCallMismatch     = errors.New("tool call does not match the pending call")
)

type Repository interface {
	CreateRun(ctx context.Context, run AgentRun) error
	GetRun(ctx context.Context, runID string) (AgentRun, error)
	SaveRun(ctx context.Context, run AgentRun) error
	AppendEvent(ctx context.Context, event Event) (Event, error)
	ListEvents(ctx context.Context, runID string, afterSeq int64) ([]Event, error)
}
