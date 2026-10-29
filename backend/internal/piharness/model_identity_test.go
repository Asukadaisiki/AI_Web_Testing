package piharness

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestModelIdentityProtocol(t *testing.T) {
	for _, kind := range []EventType{EventMessageCompleted, EventUsage} {
		for _, identity := range []struct {
			raw   string
			valid bool
		}{
			{`{}`, true},
			{`{"provider":"openai-compatible","model":"glm-4.7"}`, true},
			{`{"provider":null}`, false},
			{`{"model":null}`, false},
			{`{"provider":" "}`, false},
			{`{"model":123}`, false},
		} {
			t.Run(string(kind)+identity.raw, func(t *testing.T) {
				var payload map[string]json.RawMessage
				_ = json.Unmarshal([]byte(identity.raw), &payload)
				if kind == EventMessageCompleted {
					payload["text"] = json.RawMessage(`"answer"`)
				} else {
					payload["input_tokens"] = json.RawMessage(`10`)
					payload["output_tokens"] = json.RawMessage(`5`)
				}
				rawPayload, _ := json.Marshal(payload)
				raw, _ := json.Marshal(Event{Type: kind, RunID: "run", AttemptID: "attempt",
					Sequence: 1, Timestamp: "2026-10-28T00:00:00Z", Payload: rawPayload})
				decoded, err := NewDecoder("run", "attempt").Decode(raw)
				if !identity.valid {
					if !errors.Is(err, ErrInvalidPayload) {
						t.Fatalf("expected invalid identity, got %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var typed any = &MessageCompletedPayload{}
				if kind == EventUsage {
					typed = &UsagePayload{}
				}
				if err := json.Unmarshal(decoded.Payload, typed); err != nil {
					t.Fatal(err)
				}
				roundtrip, _ := json.Marshal(typed)
				var fields map[string]any
				_ = json.Unmarshal(roundtrip, &fields)
				if identity.raw != `{}` && (fields["provider"] != "openai-compatible" || fields["model"] != "glm-4.7") {
					t.Fatalf("typed protocol lost model identity: %s", roundtrip)
				}
			})
		}
	}
}
