package execution

import (
	"testing"
	"time"
)

func TestElapsedMillisecondsNeverReturnsNegativeDuration(t *testing.T) {
	startedAt := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	if got := elapsedMilliseconds(startedAt, startedAt.Add(1500*time.Millisecond)); got != 1500 {
		t.Fatalf("elapsedMilliseconds() = %d, want 1500", got)
	}
	if got := elapsedMilliseconds(startedAt, startedAt.Add(-8*time.Hour)); got != 0 {
		t.Fatalf("elapsedMilliseconds() = %d, want 0 for reversed timestamps", got)
	}
}
