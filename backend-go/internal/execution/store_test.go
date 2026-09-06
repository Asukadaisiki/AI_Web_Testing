package execution

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dsl"
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

func TestValidateDSLBindingsRejectsUnapprovedBytes(t *testing.T) {
	canonical := json.RawMessage(`{"name":"approved"}`)
	hash := "bb481c2b0d66e7282b3b7043ea41f886741a6c1dd279de4a2a9e630d3d38ceea"

	if err := validateDSLBindings(
		[]int64{7},
		map[int64]CanonicalDSLBinding{7: {
			CanonicalJSON: canonical, SHA256: hash, Version: dsl.CanonicalVersion,
		}},
	); err != nil {
		t.Fatalf("validateDSLBindings() error = %v", err)
	}
	if err := validateDSLBindings(
		[]int64{7},
		map[int64]CanonicalDSLBinding{7: {
			CanonicalJSON: canonical, SHA256: "0" + hash[1:], Version: dsl.CanonicalVersion,
		}},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("SHA mismatch error = %v, want ErrConflict", err)
	}
	if err := validateDSLBindings(
		[]int64{7},
		map[int64]CanonicalDSLBinding{8: {
			CanonicalJSON: canonical, SHA256: hash, Version: dsl.CanonicalVersion,
		}},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("unselected case error = %v, want ErrConflict", err)
	}
}
