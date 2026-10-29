package runtime

import (
	"encoding/json"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

func TestSDKQueryUsagePersistsMultipleModelsWithoutInventingModelCalls(t *testing.T) {
	ctx, _, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, managedProjectParams(postgres.CreateProjectParams{Name: "SDK query accounting"}))
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, postgres.CreateMessageParams{
		ConversationID: conversation.ID, Content: "Answer",
		Model: event.ModelRef{Provider: "anthropic-compatible", Name: "main-model"}, IdempotencyKey: t.Name(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	traceStore := PostgresPiTraceStore{Store: store, RuntimeWorkerID: "runtime"}
	command := commandFor(run.ID)
	command.ConversationID = conversation.ID
	ref, err := traceStore.StartRun(ctx, managedFixtureCommand(t, ctx, traceStore, command))
	if err != nil {
		t.Fatal(err)
	}
	message := harEvent(piharness.EventMessageCompleted, run.ID, ref.ProviderAttemptID, 1,
		`{"text":"Answer","provider":"anthropic-compatible","model":"main-model"}`)
	if err := traceStore.AppendHarnessEvent(ctx, ref, message); err != nil {
		t.Fatal(err)
	}
	for i, model := range []string{"main-model", "sidechain-model"} {
		raw, _ := json.Marshal(map[string]any{
			"provider": "anthropic-compatible", "model": model,
			"input_tokens": 10 + i, "output_tokens": 5 + i,
			"granularity": "sdk_query_model_cumulative", "usage_scope": "sdk_query_pipeline",
			"sdk_result_uuid": "query-result",
		})
		usage := harEvent(piharness.EventUsage, run.ID, ref.ProviderAttemptID, int64(i+2), string(raw))
		if err := traceStore.AppendHarnessEvent(ctx, ref, usage); err != nil {
			t.Fatalf("persist %s cumulative usage: %v", model, err)
		}
		// Delivery replay must retain the same durable identities.
		if err := traceStore.AppendHarnessEvent(ctx, ref, usage); err != nil {
			t.Fatalf("replay %s cumulative usage: %v", model, err)
		}
	}
	details, err := store.GetTraceDetails(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(details.Usage) != 2 || details.Usage[0].ID == details.Usage[1].ID {
		t.Fatalf("distinct SDK model usage lost: %#v", details.Usage)
	}
	for _, record := range details.Usage {
		if record.SpanID != RunSpanID(run.ID) {
			t.Fatalf("query usage attached to individual message: %#v", record)
		}
	}
	models := 0
	for _, span := range details.Spans {
		if span.Kind == "model" {
			models++
		}
	}
	if models != 1 {
		t.Fatalf("invented model spans for cumulative usage: %d", models)
	}
}
