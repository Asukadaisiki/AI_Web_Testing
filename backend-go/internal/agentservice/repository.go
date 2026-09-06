package agentservice

import (
	"context"
	"errors"
	"time"
)

var (
	ErrRunNotFound          = errors.New("agent run not found")
	ErrRunAccessDenied      = errors.New("agent run access denied")
	ErrRunCancelled         = errors.New("agent run is cancelled")
	ErrRunNotWaitingForUser = errors.New("agent run is not waiting for user input")
	ErrToolCallMismatch     = errors.New("tool call does not match the pending call")
)

type Repository interface {
	CreateRun(ctx context.Context, run AgentRun) error
	GetRun(ctx context.Context, runID string) (AgentRun, error)
	SaveRun(ctx context.Context, run AgentRun) error
	CancelRun(
		ctx context.Context,
		runID string,
		updatedAt time.Time,
		event Event,
	) (AgentRun, Event, bool, error)
	AppendEvent(ctx context.Context, event Event) (Event, error)
	ListEvents(ctx context.Context, runID string, afterSeq int64) ([]Event, error)
}
