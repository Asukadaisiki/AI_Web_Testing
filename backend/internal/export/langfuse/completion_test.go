package langfuse

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type usageRepository struct {
	*memoryRepository
	details postgres.TraceDetails
}

func (r usageRepository) GetTraceDetails(context.Context, string) (postgres.TraceDetails, error) {
	return r.details, nil
}

func TestGenerationExportsCanonicalUsageAndOnlyKnownCost(t *testing.T) {
	for _, test := range []struct {
		details  string
		cost     int64
		wantCost bool
	}{
		{`{"input_tokens":10,"output_tokens":5}`, 0, false},
		{`{"input_tokens":10,"output_tokens":5,"cost_micros":0}`, 0, true},
		{`{"input_tokens":10,"output_tokens":5,"cost_micros":1250}`, 1250, true},
	} {
		repository := usageRepository{memoryRepository: canonicalRepository(), details: postgres.TraceDetails{
			Usage: []postgres.TraceUsageDetail{{
				SpanID: "logical-1", InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
				CostMicros: test.cost, Details: json.RawMessage(test.details), Model: "reported-model",
			}},
		}}
		client := &recordingClient{}
		exporter := newTestExporter(repository.memoryRepository, client)
		exporter.Repository = repository
		if err := exporter.Handle(context.Background(), completionMessage(t, "usage")); err != nil {
			t.Fatal(err)
		}
		raw := serializedIngestionRequest(t, ingestionRequest{Batch: []ingestionEvent{observationEvent(client.batches[0].Observations[0])}})
		var decoded struct {
			Batch []struct {
				Body map[string]any `json:"body"`
			} `json:"batch"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		body := decoded.Batch[0].Body
		if !reflect.DeepEqual(body["usageDetails"], map[string]any{"input": float64(10), "output": float64(5), "total": float64(15)}) {
			t.Fatalf("usage wire body = %s", raw)
		}
		cost, exists := body["costDetails"]
		if exists != test.wantCost {
			t.Fatalf("cost presence = %v, body = %s", exists, raw)
		}
		if exists && !reflect.DeepEqual(cost, map[string]any{"total": float64(test.cost) / 1e6}) {
			t.Fatalf("cost wire value = %#v", cost)
		}
		if body["model"] != "reported-model" {
			t.Fatalf("model = %#v", body["model"])
		}
		if client.batches[0].Observations[2].UsageDetails != nil || client.batches[0].Observations[2].CostDetails != nil {
			t.Fatal("fabricated usage on a generation without evidence")
		}
	}
}

func TestUnknownPiTimingIsAbsentFromIngestion(t *testing.T) {
	repository := canonicalRepository()
	repository.spans[0].Name = "message"
	repository.spans[0].Input = nil
	repository.spans[0].Output = json.RawMessage(`{"text":"answer"}`)
	client := &recordingClient{}
	if err := newTestExporter(repository, client).Handle(context.Background(), completionMessage(t, "unknown-timing")); err != nil {
		t.Fatal(err)
	}
	body := observationEvent(client.batches[0].Observations[0]).Body
	firstEvent := observationEvent(client.batches[0].Observations[0])
	time.Sleep(2 * time.Millisecond)
	secondEvent := observationEvent(client.batches[0].Observations[0])
	if !firstEvent.Timestamp.Equal(secondEvent.Timestamp) {
		t.Fatal("unknown timing changed ingestion timestamp across retries")
	}
	if firstEvent.Timestamp.IsZero() {
		t.Fatal("missing deterministic ingestion timestamp")
	}
	var fields map[string]any
	if err := json.Unmarshal(mustJSON(t, body), &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"startTime", "endTime", "input", "usageDetails", "costDetails"} {
		if _, exists := fields[key]; exists {
			t.Fatalf("unknown %s was fabricated: %#v", key, fields)
		}
	}
}

func TestApplicationCompletionExportsEveryTerminalOutcomeAndDeduplicates(t *testing.T) {
	for _, status := range []domain.RunStatus{domain.RunSucceeded, domain.RunFailed, domain.RunCancelled} {
		t.Run(string(status), func(t *testing.T) {
			repository := canonicalRepository()
			repository.run.Status = status
			client := &recordingClient{}
			exporter := newTestExporter(repository, client)
			message := completionMessage(t, "completion-"+string(status))
			var envelope event.Envelope[json.RawMessage]
			if err := json.Unmarshal(message.Value, &envelope); err != nil {
				t.Fatal(err)
			}
			envelope.EventType = "run.completed"
			envelope.Payload = mustJSON(t, map[string]any{"run_id": "run-1", "status": status})
			message.Value = mustJSON(t, envelope)
			for range 2 {
				if err := exporter.Handle(context.Background(), message); err != nil {
					t.Fatal(err)
				}
			}
			if len(client.batches) != 1 || client.batches[0].Trace.Status != string(status) {
				t.Fatalf("completion exported %d batches, want one %s", len(client.batches), status)
			}
		})
	}
}

func TestLegacyHarnessCompletionIsIgnored(t *testing.T) {
	client := &recordingClient{}
	exporter := newTestExporter(canonicalRepository(), client)
	message := completionMessage(t, "old-harness-completion")
	var envelope event.Envelope[json.RawMessage]
	if err := json.Unmarshal(message.Value, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.EventType = "harness.completed"
	envelope.Payload = json.RawMessage(`{"status":"succeeded"}`)
	message.Value = mustJSON(t, envelope)
	if err := exporter.Handle(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(client.batches) != 0 {
		t.Fatal("removed completion alias was exported")
	}
}

func TestTraceInputUsesRunSnapshotLastUserGoalNotConversation(t *testing.T) {
	repository := canonicalRepository()
	repository.run.Specification = json.RawMessage(`{"command":{"messages":[
		{"role":"user","content":"old goal"},
		{"role":"assistant","content":"old answer"},
		{"role":"user","content":"actual goal"}
	]}}`)
	repository.messages = append(repository.messages, domain.Message{Role: "user", Content: "future goal"})
	client := &recordingClient{}
	if err := newTestExporter(repository, client).Handle(context.Background(), completionMessage(t, "run-input")); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(client.batches[0].Trace.Input, map[string]any{"goal": "actual goal"}) {
		t.Fatalf("input = %#v, want actual run goal only", client.batches[0].Trace.Input)
	}
}

func TestMissingRunInputIsUnknown(t *testing.T) {
	repository := canonicalRepository()
	client := &recordingClient{}
	if err := newTestExporter(repository, client).Handle(context.Background(), completionMessage(t, "unknown-input")); err != nil {
		t.Fatal(err)
	}
	if client.batches[0].Trace.Input != nil {
		t.Fatalf("missing snapshot input fabricated from conversation: %#v", client.batches[0].Trace.Input)
	}
}
