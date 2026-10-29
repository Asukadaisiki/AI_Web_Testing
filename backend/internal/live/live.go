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
)

// LiveEvent is a disposable wakeup. Sequence is retained for old stream records;
// readers must fetch authoritative sequences and payloads from PostgreSQL.
type LiveEvent struct {
	ID       string          `json:"-"`
	Sequence int64           `json:"sequence,omitempty"`
	Type     string          `json:"type"`
	Data     json.RawMessage `json:"data"`
}

// LiveStore holds capped, expiring run event wakeups.
type LiveStore interface {
	Append(ctx context.Context, runID string, event LiveEvent) error
	Read(ctx context.Context, runID, afterID string, block time.Duration) ([]LiveEvent, error)
}
