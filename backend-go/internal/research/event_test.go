package research

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestResearchEventEnvelopeIsCanonicalAndTamperEvident(t *testing.T) {
	firstSource := testSourceRef(t, SourceAgentEvent, "run-1:2", Available(int64(2)))
	secondSource := testSourceRef(t, SourceAgentEvent, "run-1:1", Available(int64(1)))
	event, err := NewResearchEvent(ResearchEvent{
		Kind:          EventKindDecision,
		ResearchRunID: "research-run-1",
		CorrelationID: Available("logical-call-1"),
		CausationID:   NotApplicable[string]("root_decision"),
		ToolCallIDs:   []string{"tool-b", "tool-a", "tool-a"},
		Attempt:       Available(int64(2)),
		StepIndex:     NotApplicable[int64]("model_decision"),
		Sources:       []SourceRef{firstSource, secondSource},
		Data:          json.RawMessage(`{"z":1,"a":"测试"}`),
	})
	if err != nil {
		t.Fatalf("NewResearchEvent() error = %v", err)
	}
	if event.SchemaVersion != EventSchemaVersion ||
		event.ID != "rev_"+event.ContentSHA256 ||
		len(event.ToolCallIDs) != 2 || event.ToolCallIDs[0] != "tool-a" ||
		event.Sources[0].ID != "run-1:1" ||
		string(event.Data) != `{"a":"测试","z":1}` {
		t.Fatalf("normalized event = %#v", event)
	}

	reordered, err := NewResearchEvent(ResearchEvent{
		Kind:          EventKindDecision,
		ResearchRunID: "research-run-1",
		CorrelationID: Available("logical-call-1"),
		CausationID:   NotApplicable[string]("root_decision"),
		ToolCallIDs:   []string{"tool-a", "tool-b"},
		Attempt:       Available(int64(2)),
		StepIndex:     NotApplicable[int64]("model_decision"),
		Sources:       []SourceRef{secondSource, firstSource},
		Data:          json.RawMessage(`{"a":"测试","z":1}`),
	})
	if err != nil || reordered.ContentSHA256 != event.ContentSHA256 {
		t.Fatalf("canonical identity changed: %#v, %v", reordered, err)
	}

	event.Data = json.RawMessage(`{"a":"changed","z":1}`)
	if err := event.NormalizeAndValidate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered event error = %v, want ErrInvalid", err)
	}
}

func TestResearchEventKindsAndUnavailableSlots(t *testing.T) {
	kinds := []EventKind{
		EventKindObservation,
		EventKindDecision,
		EventKindAction,
		EventKindExecution,
		EventKindVerification,
		EventKindFailure,
		EventKindRecovery,
		EventKindReward,
		EventKindUnknown,
	}
	for _, kind := range kinds {
		event, err := NewResearchEvent(ResearchEvent{
			Kind:          kind,
			ResearchRunID: "research-run-kinds",
			CorrelationID: Unavailable[string]("correlation_not_persisted"),
			CausationID:   NotApplicable[string]("root_event"),
			Attempt:       NotApplicable[int64]("no_attempt"),
			StepIndex:     NotApplicable[int64]("no_step"),
			Sources:       []SourceRef{},
			Data:          json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatalf("kind %s: %v", kind, err)
		}
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"value":null`) {
			t.Fatalf("kind %s encoded ambiguous null slot: %s", kind, raw)
		}
	}

	invalid := Unavailable[string]("")
	if err := invalid.Validate("missing"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty unavailable reason error = %v, want ErrInvalid", err)
	}
}

func TestResearchEventEncodesEmptyCollectionsAsArrays(t *testing.T) {
	event, err := NewResearchEvent(ResearchEvent{
		Kind:          EventKindObservation,
		ResearchRunID: "research-run-empty",
		CorrelationID: Unavailable[string]("correlation_not_persisted"),
		CausationID:   NotApplicable[string]("root_event"),
		Attempt:       NotApplicable[int64]("no_attempt"),
		StepIndex:     NotApplicable[int64]("no_step"),
		Data:          json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"tool_call_ids":[]`) ||
		!strings.Contains(string(raw), `"sources":[]`) {
		t.Fatalf("empty event collections are not arrays: %s", raw)
	}
}

func TestToolResultSchemaAcceptsRawMetadataForNormalizedContent(t *testing.T) {
	original := []byte("{\n  \"value\": \"<tag>\"\n}")
	sum := sha256.Sum256(original)
	var content any
	if err := json.Unmarshal(original, &content); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": "agent.tool_result.v1",
		"tool":           "explore_page",
		"content":        content,
		"content_sha256": hex.EncodeToString(sum[:]),
		"content_bytes":  len(original),
	})
	if err != nil {
		t.Fatal(err)
	}
	event := AgentEventSnapshot{
		Seq: 1, Type: "tool.result", Payload: payload,
	}
	if err := validateAgentEventSchema(event); err != nil {
		t.Fatalf("normalized tool result error = %v", err)
	}
	event.Payload = json.RawMessage(`{
		"schema_version":"agent.tool_result.v1",
		"tool":"explore_page",
		"content":{},
		"content_sha256":"invalid",
		"content_bytes":2
	}`)
	if err := validateAgentEventSchema(event); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("invalid metadata error = %v, want ErrSourceChanged", err)
	}
}

func TestCanonicalJSONRejectsInvalidUTF8AndTrailingValues(t *testing.T) {
	if _, err := CanonicalJSON([]byte{'"', 0xff, '"'}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid UTF-8 error = %v, want ErrInvalid", err)
	}
	if _, err := CanonicalJSON([]byte(`{} {}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("trailing JSON error = %v, want ErrInvalid", err)
	}
	canonical, err := CanonicalJSON([]byte("{\n \"b\": 2, \"a\": 1\n}"))
	if err != nil || string(canonical) != `{"a":1,"b":2}` {
		t.Fatalf("CanonicalJSON() = %s, %v", canonical, err)
	}
}

func testSourceRef(
	t *testing.T,
	kind SourceKind,
	id string,
	sequence Slot[int64],
) SourceRef {
	t.Helper()
	ref, err := sourceRef(
		kind,
		id,
		sequence,
		Unavailable[string]("fixture_has_no_schema_version"),
		map[string]any{"kind": kind, "id": id},
	)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
