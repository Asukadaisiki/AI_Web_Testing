package piharness

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestRequestRoundTrip(t *testing.T) {
	req := Request{
		ProtocolVersion: ProtocolVersion,
		RunID:           "run_1",
		AttemptID:       "attempt_1",
		Goal:            "verify the cart",
		Model: ModelRequest{
			Provider:        "scripted",
			Name:            "browser-fixture",
			ReasoningEffort: "high",
		},
		WorkerURL:     "http://worker:8100",
		ArtifactScope: "run_1",
	}

	line, err := req.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var got Request
	if err := got.UnmarshalJSON(line); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if got != req {
		t.Fatalf("request round trip mismatch:\n got: %+v\nwant: %+v", got, req)
	}
}

func TestRequestRejectsUnsupportedProtocolVersion(t *testing.T) {
	req := Request{
		ProtocolVersion: "pi-browser-harness.v2",
		RunID:           "run_1",
		AttemptID:       "attempt_1",
		Goal:            "verify the cart",
		Model: ModelRequest{
			Provider:        "scripted",
			Name:            "browser-fixture",
			ReasoningEffort: "high",
		},
		WorkerURL:     "http://worker:8100",
		ArtifactScope: "run_1",
	}

	line, err := req.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var got Request
	if err := got.UnmarshalJSON(line); err == nil {
		t.Fatalf("expected error for unsupported protocol version, got nil")
	}
}

func TestDecoderAcceptsValidTerminalStream(t *testing.T) {
	dec := NewDecoder("run_1", "attempt_1")

	lines := []string{
		`{"type":"harness.started","run_id":"run_1","attempt_id":"attempt_1","sequence":1,"timestamp":"2026-10-26T12:34:56.789Z","payload":{}}`,
		`{"type":"pi.message.completed","run_id":"run_1","attempt_id":"attempt_1","sequence":2,"timestamp":"2026-10-26T12:34:57.000Z","payload":{"text":"cart verified"}}`,
		`{"type":"harness.settled","run_id":"run_1","attempt_id":"attempt_1","sequence":3,"timestamp":"2026-10-26T12:34:58.000Z","payload":{"assistant_text":"cart verified"}}`,
	}

	for i, line := range lines {
		ev, err := dec.Decode([]byte(line))
		if err != nil {
			t.Fatalf("line %d: unexpected decode error: %v", i, err)
		}
		if ev.Sequence != int64(i+1) {
			t.Fatalf("line %d: sequence = %d, want %d", i, ev.Sequence, i+1)
		}
	}

	if !dec.TerminalSeen() {
		t.Fatalf("expected terminal to be seen")
	}
}

func TestDecoderRejectsRunIDMismatch(t *testing.T) {
	dec := NewDecoder("run_1", "attempt_1")
	line := `{"type":"harness.started","run_id":"run_other","attempt_id":"attempt_1","sequence":1,"timestamp":"2026-10-26T12:34:56.789Z","payload":{}}`

	_, err := dec.Decode([]byte(line))
	if !errors.Is(err, ErrRunIDMismatch) {
		t.Fatalf("expected ErrRunIDMismatch, got %v", err)
	}
}

func TestDecoderRejectsAttemptIDMismatch(t *testing.T) {
	dec := NewDecoder("run_1", "attempt_1")
	line := `{"type":"harness.started","run_id":"run_1","attempt_id":"attempt_other","sequence":1,"timestamp":"2026-10-26T12:34:56.789Z","payload":{}}`

	_, err := dec.Decode([]byte(line))
	if !errors.Is(err, ErrAttemptIDMismatch) {
		t.Fatalf("expected ErrAttemptIDMismatch, got %v", err)
	}
}

func TestDecoderRejectsSequenceNotStartingAtOne(t *testing.T) {
	dec := NewDecoder("run_1", "attempt_1")
	line := `{"type":"harness.started","run_id":"run_1","attempt_id":"attempt_1","sequence":2,"timestamp":"2026-10-26T12:34:56.789Z","payload":{}}`

	_, err := dec.Decode([]byte(line))
	if !errors.Is(err, ErrInvalidSequence) {
		t.Fatalf("expected ErrInvalidSequence, got %v", err)
	}
}

func TestDecoderRejectsDuplicateSequence(t *testing.T) {
	dec := NewDecoder("run_1", "attempt_1")
	first := `{"type":"harness.started","run_id":"run_1","attempt_id":"attempt_1","sequence":1,"timestamp":"2026-10-26T12:34:56.789Z","payload":{}}`
	if _, err := dec.Decode([]byte(first)); err != nil {
		t.Fatalf("first decode: %v", err)
	}
	second := `{"type":"pi.message.completed","run_id":"run_1","attempt_id":"attempt_1","sequence":1,"timestamp":"2026-10-26T12:34:57.000Z","payload":{"text":"x"}}`

	_, err := dec.Decode([]byte(second))
	if !errors.Is(err, ErrInvalidSequence) {
		t.Fatalf("expected ErrInvalidSequence for duplicate, got %v", err)
	}
}

func TestDecoderRejectsOutOfOrderSequence(t *testing.T) {
	dec := NewDecoder("run_1", "attempt_1")
	first := `{"type":"harness.started","run_id":"run_1","attempt_id":"attempt_1","sequence":1,"timestamp":"2026-10-26T12:34:56.789Z","payload":{}}`
	if _, err := dec.Decode([]byte(first)); err != nil {
		t.Fatalf("first decode: %v", err)
	}
	second := `{"type":"pi.message.completed","run_id":"run_1","attempt_id":"attempt_1","sequence":5,"timestamp":"2026-10-26T12:34:57.000Z","payload":{"text":"x"}}`

	_, err := dec.Decode([]byte(second))
	if !errors.Is(err, ErrInvalidSequence) {
		t.Fatalf("expected ErrInvalidSequence for out-of-order, got %v", err)
	}
}

func TestDecoderRejectsInvalidTimestamp(t *testing.T) {
	cases := []struct {
		name string
		ts   string
	}{
		{"not rfc3339", "2026/10/26 12:34:56"},
		{"non-utc offset", "2026-10-26T12:34:56.789+08:00"},
		{"empty", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := NewDecoder("run_1", "attempt_1")
			line := `{"type":"harness.started","run_id":"run_1","attempt_id":"attempt_1","sequence":1,"timestamp":"` + tc.ts + `","payload":{}}`
			_, err := dec.Decode([]byte(line))
			if !errors.Is(err, ErrInvalidTimestamp) {
				t.Fatalf("expected ErrInvalidTimestamp, got %v", err)
			}
		})
	}
}

func TestDecoderAcceptsUTCTimestampVariants(t *testing.T) {
	cases := []string{
		"2026-10-26T12:34:56.789Z",
		"2026-10-26T12:34:56Z",
		"2026-10-26T12:34:56.789012345Z",
		"2026-10-26T12:34:56+00:00",
	}

	for _, ts := range cases {
		t.Run(ts, func(t *testing.T) {
			dec := NewDecoder("run_1", "attempt_1")
			line := `{"type":"harness.started","run_id":"run_1","attempt_id":"attempt_1","sequence":1,"timestamp":"` + ts + `","payload":{}}`
			ev, err := dec.Decode([]byte(line))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev.TimestampTime().Location() != time.UTC {
				t.Fatalf("expected UTC location, got %v", ev.TimestampTime().Location())
			}
		})
	}
}

func TestDecoderRejectsUnknownEventType(t *testing.T) {
	dec := NewDecoder("run_1", "attempt_1")
	line := `{"type":"harness.mystery","run_id":"run_1","attempt_id":"attempt_1","sequence":1,"timestamp":"2026-10-26T12:34:56.789Z","payload":{}}`

	_, err := dec.Decode([]byte(line))
	if !errors.Is(err, ErrUnknownEventType) {
		t.Fatalf("expected ErrUnknownEventType, got %v", err)
	}
}

func TestDecoderRejectsSecondTerminalRecord(t *testing.T) {
	dec := NewDecoder("run_1", "attempt_1")
	first := `{"type":"harness.started","run_id":"run_1","attempt_id":"attempt_1","sequence":1,"timestamp":"2026-10-26T12:34:56.789Z","payload":{}}`
	if _, err := dec.Decode([]byte(first)); err != nil {
		t.Fatalf("first decode: %v", err)
	}
	terminal := `{"type":"harness.settled","run_id":"run_1","attempt_id":"attempt_1","sequence":2,"timestamp":"2026-10-26T12:34:57.000Z","payload":{"assistant_text":"done"}}`
	if _, err := dec.Decode([]byte(terminal)); err != nil {
		t.Fatalf("terminal decode: %v", err)
	}

	secondTerminal := `{"type":"harness.failed","run_id":"run_1","attempt_id":"attempt_1","sequence":3,"timestamp":"2026-10-26T12:34:58.000Z","payload":{"error":"late"}}`
	_, err := dec.Decode([]byte(secondTerminal))
	if !errors.Is(err, ErrSecondTerminal) {
		t.Fatalf("expected ErrSecondTerminal, got %v", err)
	}
}

func TestDecoderRejectsMalformedJSON(t *testing.T) {
	dec := NewDecoder("run_1", "attempt_1")
	_, err := dec.Decode([]byte(`not-json`))
	if !errors.Is(err, ErrMalformedJSONL) {
		t.Fatalf("expected ErrMalformedJSONL, got %v", err)
	}
}

func TestDecoderRejectsMalformedBrowserCaseCompletionEvidence(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{
			name:    "previously accepted passed verdict without objective evidence",
			payload: `{"verdict":"passed"}`,
		},
		{
			name:    "invalid verdict enum",
			payload: `{"verdict":"unknown","case":{},"dry_run":{}}`,
		},
		{
			name:    "unexpected payload field",
			payload: `{"verdict":"passed","case":{},"dry_run":{},"steps":[]}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := NewDecoder("run_1", "attempt_1")
			line := `{"type":"browser.case.completed","run_id":"run_1","attempt_id":"attempt_1","sequence":1,"timestamp":"2026-10-26T12:34:56.789Z","payload":` + tc.payload + `}`

			if _, err := dec.Decode([]byte(line)); err == nil {
				t.Fatal("malformed browser case completion evidence was accepted")
			}
		})
	}
}

func TestDecoderRejectsBrowserObservationWithoutValidToolCallID(t *testing.T) {
	for _, payload := range []string{
		`{"observation":{"url":"http://worker"}}`,
		`{"tool_call_id":" \t","observation":{"url":"http://worker"}}`,
	} {
		dec := NewDecoder("run_1", "attempt_1")
		line := `{"type":"browser.observation","run_id":"run_1","attempt_id":"attempt_1","sequence":1,"timestamp":"2026-10-26T12:34:56.789Z","payload":` + payload + `}`

		_, err := dec.Decode([]byte(line))
		if !errors.Is(err, ErrInvalidPayload) {
			t.Fatalf("decode observation payload %s = %v, want ErrInvalidPayload", payload, err)
		}
	}
}

func TestDecoderTypedPayloads(t *testing.T) {
	dec := NewDecoder("run_1", "attempt_1")

	cases := []struct {
		line string
		typ  EventType
	}{
		{`{"type":"harness.started","run_id":"run_1","attempt_id":"attempt_1","sequence":1,"timestamp":"2026-10-26T12:34:56.789Z","payload":{}}`, EventStarted},
		{`{"type":"pi.message.completed","run_id":"run_1","attempt_id":"attempt_1","sequence":2,"timestamp":"2026-10-26T12:34:57.000Z","payload":{"text":"hi"}}`, EventMessageCompleted},
		{`{"type":"pi.usage","run_id":"run_1","attempt_id":"attempt_1","sequence":3,"timestamp":"2026-10-26T12:34:58.000Z","payload":{"input_tokens":1,"output_tokens":2}}`, EventUsage},
		{`{"type":"pi.tool.started","run_id":"run_1","attempt_id":"attempt_1","sequence":4,"timestamp":"2026-10-26T12:34:59.000Z","payload":{"tool_call_id":"c1","tool_name":"navigate"}}`, EventToolStarted},
		{`{"type":"pi.tool.completed","run_id":"run_1","attempt_id":"attempt_1","sequence":5,"timestamp":"2026-10-26T12:35:00.000Z","payload":{"tool_call_id":"c1","tool_name":"navigate","is_error":false}}`, EventToolCompleted},
		{`{"type":"browser.observation","run_id":"run_1","attempt_id":"attempt_1","sequence":6,"timestamp":"2026-10-26T12:35:01.000Z","payload":{"tool_call_id":"c1","observation":{"url":"x"}}}`, EventObservation},
		{`{"type":"browser.case.completed","run_id":"run_1","attempt_id":"attempt_1","sequence":7,"timestamp":"2026-10-26T12:35:02.000Z","payload":` + validCaseCompletedPayload() + `}`, EventCaseCompleted},
	}

	for i, tc := range cases {
		ev, err := dec.Decode([]byte(tc.line))
		if err != nil {
			t.Fatalf("case %d (%s): %v", i, tc.typ, err)
		}
		if ev.Type != tc.typ {
			t.Fatalf("case %d: type = %s, want %s", i, ev.Type, tc.typ)
		}
	}
}

func validCaseCompletedPayload() string {
	return `{
		"verdict":"passed",
		"case":{
			"case_version":"loop.case.v1",
			"name":"open page",
			"goal":"open the page",
			"base_url":"https://example.test",
			"steps":[{
				"index":0,
				"action":"goto",
				"intent":"open the page",
				"value":"https://example.test",
				"submit":false,
				"preconditions":[],
				"postconditions":[{"type":"url_contains","value":"example.test","timeout_ms":1000}],
				"timeout_ms":1000
			}]
		},
		"dry_run":{
			"execution_id":"execution-1",
			"status":"passed",
			"started_at":"2026-10-26T12:35:00Z",
			"finished_at":"2026-10-26T12:35:01Z",
			"final_url":"https://example.test",
			"steps":[{
				"index":0,
				"action":"goto",
				"status":"passed",
				"started_at":"2026-10-26T12:35:00Z",
				"duration_ms":10,
				"url_before":"about:blank",
				"url_after":"https://example.test",
				"conditions":[],
				"evidence":{"screenshot_path":null,"console":[],"network":[]},
				"error":null
			}],
			"error":null
		}
	}`
}

func TestToolCompletedPayloadPreservesStructuredResult(t *testing.T) {
	var payload ToolCompletedPayload
	if err := json.Unmarshal([]byte(`{
		"tool_call_id":"c1",
		"tool_name":"mcp__browser_harness__assert_text",
		"is_error":false,
		"structured_result":{"ok":false,"error":"condition_unmet"}
	}`), &payload); err != nil {
		t.Fatalf("decode tool completion: %v", err)
	}
	if string(payload.StructuredResult) != `{"ok":false,"error":"condition_unmet"}` {
		t.Fatalf("structured result = %s", payload.StructuredResult)
	}
}
