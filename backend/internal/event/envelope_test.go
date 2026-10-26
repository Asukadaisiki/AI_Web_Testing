package event

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
)

func TestEnvelopeRoundTripsRunCommand(t *testing.T) {
	input := Envelope[RunCommand]{
		SchemaVersion: "agenttrace.run-command.v1",
		EventID:       "evt_123",
		EventType:     "run.requested",
		AggregateID:   "run_123",
		OccurredAt:    time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
		CorrelationID: "run_123",
		CausationID:   "msg_123",
		Payload: RunCommand{
			RunID:          "run_123",
			ConversationID: "conv_123",
			Model:          ModelRef{Provider: "scripted", Name: "browser-eval-fixture", ReasoningEffort: "high"},
			Messages: []domain.Message{
				{Role: "user", Content: "Log in and verify the cart"},
			},
			Operations: []Operation{
				{
					Name:        "browser_execute_case",
					Description: "Execute a grounded browser evaluation case",
					InputSchema: json.RawMessage(`{"type":"object"}`),
					Callback:    Callback{Kind: "http", URL: "http://worker:8100/operations/browser_execute_case"},
				},
			},
			CheckpointVersion: 3,
			WorkerURL:         "http://worker:8100",
			ArtifactScope:     "run_123",
		},
	}

	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	var output Envelope[RunCommand]
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if output.SchemaVersion != "agenttrace.run-command.v1" {
		t.Fatalf("schema version = %q", output.SchemaVersion)
	}
	if output.Payload.RunID != "run_123" || output.Payload.ConversationID != "conv_123" {
		t.Fatalf("run command IDs = %#v", output.Payload)
	}
	if output.Payload.Model != (ModelRef{Provider: "scripted", Name: "browser-eval-fixture", ReasoningEffort: "high"}) {
		t.Fatalf("model = %#v", output.Payload.Model)
	}
	if len(output.Payload.Messages) != 1 || output.Payload.Messages[0].Content != "Log in and verify the cart" {
		t.Fatalf("messages = %#v", output.Payload.Messages)
	}
	if len(output.Payload.Operations) != 1 || output.Payload.Operations[0].Callback.URL != "http://worker:8100/operations/browser_execute_case" {
		t.Fatalf("operations = %#v", output.Payload.Operations)
	}
	if output.Payload.CheckpointVersion != 3 {
		t.Fatalf("checkpoint version = %d", output.Payload.CheckpointVersion)
	}
	if output.Payload.WorkerURL != "http://worker:8100" || output.Payload.ArtifactScope != "run_123" {
		t.Fatalf("worker url / artifact scope = %q / %q", output.Payload.WorkerURL, output.Payload.ArtifactScope)
	}
}

func TestEnvelopeRejectsUnknownMajorSchemaVersion(t *testing.T) {
	var envelope Envelope[RunCommand]
	err := json.Unmarshal([]byte(`{
		"schema_version":"agenttrace.run-command.v2",
		"event_id":"evt_123",
		"event_type":"run.requested",
		"aggregate_id":"run_123",
		"occurred_at":"2026-10-21T12:00:00Z",
		"correlation_id":"run_123",
		"payload":{}
	}`), &envelope)
	if err == nil {
		t.Fatal("expected unknown major schema version to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported schema major version 2") {
		t.Fatalf("error = %v", err)
	}
}
