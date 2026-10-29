package piharness

import (
	"errors"
	"testing"
)

func TestDecoderValidatesOptionalHarnessVerdict(t *testing.T) {
	for _, verdict := range []string{`"succeeded"`, `"failed"`, `"passed"`, `null`, `false`} {
		t.Run(verdict, func(t *testing.T) {
			decoder := NewDecoder("run", "attempt")
			_, err := decoder.Decode([]byte(`{"type":"harness.settled","run_id":"run","attempt_id":"attempt","sequence":1,"timestamp":"2026-10-28T00:00:00Z","payload":{"assistant_text":"done","verdict":` + verdict + `}}`))
			valid := verdict == `"succeeded"` || verdict == `"failed"`
			if valid && err != nil {
				t.Fatalf("valid verdict rejected: %v", err)
			}
			if !valid && !errors.Is(err, ErrInvalidPayload) {
				t.Fatalf("invalid verdict accepted: %v", err)
			}
		})
	}
}
