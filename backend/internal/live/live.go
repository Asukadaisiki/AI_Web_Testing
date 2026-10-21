// Package live defines the disposable live-event store used to accelerate run streaming.
package live

import (
	"context"
	"encoding/json"
	"time"
)

const (
	// MaxStreamLength bounds the number of events retained for one run.
	MaxStreamLength int64 = 2000
	// StreamTTL limits how long a disposable run stream remains available.
	StreamTTL = 24 * time.Hour
	// LeaseTTL is the required ownership lease lifetime.
	LeaseTTL = 30 * time.Second
	// CancelTTL limits a disposable cancellation hint.
	CancelTTL = 24 * time.Hour
)

// LiveEvent is either a durable event notification identified by Sequence or an
// ephemeral token delta with Sequence zero.
type LiveEvent struct {
	ID       string          `json:"-"`
	Sequence int64           `json:"sequence,omitempty"`
	Type     string          `json:"type"`
	Data     json.RawMessage `json:"data"`
}

// LiveStore holds capped, expiring live updates and disposable run coordination hints.
type LiveStore interface {
	Append(ctx context.Context, runID string, event LiveEvent) error
	Read(ctx context.Context, runID, afterID string, block time.Duration) ([]LiveEvent, error)
	SetLease(ctx context.Context, runID, owner string, ttl time.Duration) error
	RequestCancel(ctx context.Context, runID string, ttl time.Duration) error
	IsCancelRequested(ctx context.Context, runID string) (bool, error)
}
