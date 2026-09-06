package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresAgentEventReturnsNormalizedPayload(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
	if len(events) != 5 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].ToolCallID != "tool-1" ||
		events[0].Payload["tool_call_status"] != string(agentservice.ToolCallAvailable) {
		t.Fatalf("single event = %#v", events[0])
	}
	if events[1].ToolCallID != "" ||
		events[1].Payload["tool_call_status"] != string(agentservice.ToolCallAvailable) ||
		len(events[1].Payload["tool_call_ids"].([]any)) != 2 {
		t.Fatalf("multiple event = %#v", events[1])
	}
	if events[2].Payload["tool_call_unavailable_reason"] !=
		string(agentservice.ToolCallUnavailableModelReturnedFinalText) {
		t.Fatalf("no-tool event = %#v", events[2])
	}
	if events[3].Payload["tool_call_unavailable_reason"] !=
		string(agentservice.ToolCallUnavailableAttemptFailedNoResponse) {
		t.Fatalf("failed event = %#v", events[3])
	}
	if _, exists := events[4].Payload["tool_call_status"]; exists {
		t.Fatalf("legacy event was rewritten: %#v", events[4])
	}
}

func TestPostgresToolResultPreservesCompleteContentAndDigest(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
