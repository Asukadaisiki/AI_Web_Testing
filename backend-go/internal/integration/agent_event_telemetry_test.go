package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/testpg"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresAgentEventReturnsNormalizedPayload(t *testing.T) {
	db := testpg.Open(t)
	ctx := context.Background()
	service := agentservice.NewService(agentservice.NewPostgresRepository(db))
	run, err := service.StartRun(ctx, "telemetry-integration", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM agent_events WHERE run_id = $1`, run.ID)
		_, _ = db.ExecContext(ctx, `DELETE FROM agent_runs WHERE id = $1`, run.ID)
	})

	persisted, err := service.RecordEvent(ctx, run, agentservice.Event{
		Type: agentservice.EventResearchLLMCall,
		Payload: map[string]any{
			"schema_version": agentservice.ResearchLLMCallSchemaV1,
			"nested":         json.RawMessage(`{"count":1}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	nested, ok := persisted.Payload["nested"].(map[string]any)
	if !ok || nested["count"] != float64(1) {
		t.Fatalf("persisted payload was not DB-normalized: %#v", persisted.Payload)
	}
	replayed, err := service.ListEvents(ctx, run.ID, persisted.Seq-1)
	if err != nil || len(replayed) != 1 {
		t.Fatalf("replayed = %#v, error = %v", replayed, err)
	}
	persistedBytes, _ := json.Marshal(persisted)
	replayedBytes, _ := json.Marshal(replayed[0])
	if string(persistedBytes) != string(replayedBytes) {
		t.Fatalf("persisted != replayed:\n%s\n%s", persistedBytes, replayedBytes)
	}
}

func TestPostgresResearchLLMCallToolAssociationsAndLegacyReplay(t *testing.T) {
	db := testpg.Open(t)
	ctx := context.Background()
	service := agentservice.NewService(agentservice.NewPostgresRepository(db))
	run, err := service.StartRun(ctx, "telemetry-association-integration", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM agent_events WHERE run_id = $1`, run.ID)
		_, _ = db.ExecContext(ctx, `DELETE FROM agent_runs WHERE id = $1`, run.ID)
	})

	records := []agent.TelemetryRecord{
		{
			LogicalCallID: "single",
			StepID:        "step-single",
			ToolCallIDs:   []string{"tool-1"},
			Telemetry: agent.ModelTelemetry{
				Provider: "provider", RequestedModel: "model",
				Attempts: []agent.ModelAttempt{{Attempt: 1, Status: "succeeded"}},
			},
		},
		{
			LogicalCallID: "multiple",
			StepID:        "step-multiple",
			ToolCallIDs:   []string{"tool-2", "tool-3"},
			Telemetry: agent.ModelTelemetry{
				Provider: "provider", RequestedModel: "model",
				Attempts: []agent.ModelAttempt{{Attempt: 1, Status: "succeeded"}},
			},
		},
		{
			LogicalCallID: "none",
			StepID:        "step-none",
			Telemetry: agent.ModelTelemetry{
				Provider: "provider", RequestedModel: "model",
				Attempts: []agent.ModelAttempt{{Attempt: 1, Status: "succeeded"}},
			},
		},
		{
			LogicalCallID: "failed",
			StepID:        "step-failed",
			Telemetry: agent.ModelTelemetry{
				Provider: "provider", RequestedModel: "model",
				Attempts: []agent.ModelAttempt{{
					Attempt: 1, Status: "failed",
					Error: &agent.ModelError{Category: "timeout", Retryable: true},
				}},
			},
		},
	}
	for index := range records {
		record := &records[index]
		record.Telemetry.ClientRequestID = "e2e_" +
			strings.Repeat(string(rune('a'+index)), 32)
		record.Telemetry.EndpointScheme = "https"
		record.Telemetry.EndpointHost = "api.deepseek.com"
		record.Telemetry.CredentialFingerprint = "sha256:v1:" +
			strings.Repeat(string(rune('a'+index)), 64)
		record.Telemetry.LocalResponseCache = "not_configured"
		for attemptIndex := range record.Telemetry.Attempts {
			attempt := &record.Telemetry.Attempts[attemptIndex]
			attempt.ProviderHeaderRequestID = "header-request-id"
			attempt.ProviderHeaderRequestIDHeader = "x-request-id"
			attempt.ProviderRequestID = "header-request-id"
			if attempt.Status == "succeeded" {
				attempt.ProviderResponseID = "provider-response-id"
				attempt.ProviderRequestID = "provider-response-id"
			}
		}
	}
	for _, record := range records {
		if err := service.RecordModelTelemetry(ctx, run, record); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.RecordEvent(ctx, run, agentservice.Event{
		Type:   agentservice.EventResearchLLMCall,
		StepID: "legacy-step",
		Payload: map[string]any{
			"schema_version":  agentservice.ResearchLLMCallSchemaV1,
			"logical_call_id": "legacy",
		},
	}); err != nil {
		t.Fatal(err)
	}

	events, err := service.ListEvents(ctx, run.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	// BUG-192: RecordModelTelemetry now also persists agent.pipeline.trace
	// per attempt, so filter by event type before asserting counts and
	// field-level content instead of indexing the raw event stream.
	traceEvents := 0
	llmEvents := make([]agentservice.Event, 0, len(events))
	for _, event := range events {
		switch event.Type {
		case agentservice.EventPipelineTrace:
			traceEvents++
		case agentservice.EventResearchLLMCall:
			llmEvents = append(llmEvents, event)
		}
	}
	if traceEvents != 4 {
		t.Fatalf("pipeline trace events = %d, want 4", traceEvents)
	}
	if len(llmEvents) != 5 {
		t.Fatalf("llm_call events = %d, want 5", len(llmEvents))
	}
	if llmEvents[0].ToolCallID != "tool-1" ||
		llmEvents[0].Payload["tool_call_status"] != string(agentservice.ToolCallAvailable) ||
		llmEvents[0].Payload["client_request_id"] != records[0].Telemetry.ClientRequestID ||
		llmEvents[0].Payload["endpoint_scheme"] != "https" ||
		llmEvents[0].Payload["endpoint_host"] != "api.deepseek.com" ||
		llmEvents[0].Payload["provider_response_id"] != "provider-response-id" ||
		llmEvents[0].Payload["provider_header_request_id"] != "header-request-id" ||
		llmEvents[0].Payload["provider_header_request_id_header"] != "x-request-id" ||
		llmEvents[0].Payload["provider_request_id"] != "provider-response-id" ||
		llmEvents[0].Payload["local_response_cache"] != "not_configured" {
		t.Fatalf("single event = %#v", llmEvents[0])
	}
	if llmEvents[1].ToolCallID != "" ||
		llmEvents[1].Payload["tool_call_status"] != string(agentservice.ToolCallAvailable) ||
		len(llmEvents[1].Payload["tool_call_ids"].([]any)) != 2 {
		t.Fatalf("multiple event = %#v", llmEvents[1])
	}
	if llmEvents[2].Payload["tool_call_unavailable_reason"] !=
		string(agentservice.ToolCallUnavailableModelReturnedFinalText) {
		t.Fatalf("no-tool event = %#v", llmEvents[2])
	}
	if llmEvents[3].Payload["tool_call_unavailable_reason"] !=
		string(agentservice.ToolCallUnavailableAttemptFailedNoResponse) {
		t.Fatalf("failed event = %#v", llmEvents[3])
	}
	if _, exists := llmEvents[4].Payload["tool_call_status"]; exists {
		t.Fatalf("legacy event was rewritten: %#v", llmEvents[4])
	}
}

func TestPostgresToolResultPreservesCompleteContentAndDigest(t *testing.T) {
	db := testpg.Open(t)
	ctx := context.Background()
	service := agentservice.NewService(agentservice.NewPostgresRepository(db))
	run, err := service.StartRun(ctx, "tool-result-integration", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM agent_events WHERE run_id = $1`, run.ID)
		_, _ = db.ExecContext(ctx, `DELETE FROM agent_runs WHERE id = $1`, run.ID)
	})

	raw := json.RawMessage(`{
		"url":"https://example.com",
		"a11y_nodes":[
			{"node_id":"e1","role":"button","name":"Keep complete"},
			{"node_id":"e2","role":"generic","name":"Do not truncate in event"}
		]
	}`)
	typedPayload, err := agent.NewToolResultEventPayload("explore_page", raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(typedPayload)
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	persisted, err := service.RecordEvent(ctx, run, agentservice.Event{
		Type: agentservice.EventToolResult, StepID: "step-tool",
		ToolCallID: "call-tool", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.ListEvents(ctx, run.ID, persisted.Seq-1)
	if err != nil || len(replayed) != 1 {
		t.Fatalf("replayed = %#v, error = %v", replayed, err)
	}
	event := replayed[0]
	if event.Payload["schema_version"] != agent.ToolResultSchemaV1 ||
		event.Payload["content_sha256"] != typedPayload.ContentSHA256 ||
		event.Payload["content_bytes"] != float64(len(raw)) {
		t.Fatalf("payload metadata = %#v", event.Payload)
	}
	content := event.Payload["content"].(map[string]any)
	nodes := content["a11y_nodes"].([]any)
	if len(nodes) != 2 ||
		nodes[1].(map[string]any)["name"] != "Do not truncate in event" {
		t.Fatalf("persisted content = %#v", content)
	}
}
