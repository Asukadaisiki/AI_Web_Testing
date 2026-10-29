package piharness

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestGenericRequestOmitsBrowserConfiguration(t *testing.T) {
	raw, err := json.Marshal(Request{RunID: "run", AttemptID: "attempt", Goal: "answer",
		Model: ModelRequest{Provider: "scripted", Name: "fixture", ReasoningEffort: "off"}})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"worker_url", "artifact_scope"} {
		if _, present := fields[key]; present {
			t.Fatalf("generic request includes %s: %s", key, raw)
		}
	}
}

func TestModelObservationProtocol(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    EventType
		payload string
		valid   bool
	}{
		{"actual input and timing", EventMessageCompleted, `{"text":"done","started_at":"2026-10-28T00:00:00Z","ended_at":"2026-10-28T00:00:01Z","input":{"messages":[{"role":"user","content":"answer"}]}}`, true},
		{"reverse timing", EventMessageCompleted, `{"text":"done","started_at":"2026-10-28T00:00:02Z","ended_at":"2026-10-28T00:00:01Z"}`, false},
		{"partial timing", EventMessageCompleted, `{"text":"done","started_at":"2026-10-28T00:00:00Z"}`, false},
		{"invalid input", EventMessageCompleted, `{"text":"done","input":null}`, false},
		{"cache usage", EventUsage, `{"input_tokens":10,"output_tokens":5,"cache_read_tokens":3,"cache_write_tokens":2,"total_tokens":20}`, true},
		{"negative cache", EventUsage, `{"input_tokens":10,"output_tokens":5,"cache_read_tokens":-1}`, false},
		{"missing verdict", EventSettled, `{"assistant_text":"done"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(Event{Type: tc.kind, RunID: "run", AttemptID: "attempt",
				Sequence: 1, Timestamp: "2026-10-28T00:00:01Z", Payload: json.RawMessage(tc.payload)})
			_, err := NewDecoder("run", "attempt").Decode(raw)
			if tc.valid && err != nil {
				t.Fatal(err)
			}
			if !tc.valid && !errors.Is(err, ErrInvalidPayload) {
				t.Fatalf("expected invalid payload, got %v", err)
			}
		})
	}
}
