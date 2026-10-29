package langfuse

import (
	"encoding/json"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

func TestQueryUsageExportsScopeWithoutAssigningTotalsToIndividualCalls(t *testing.T) {
	batch := Batch{Observations: []Observation{
		{Type: "span", Metadata: map[string]any{"agenttrace_logical_span_id": "root"}},
		{Type: "generation", Metadata: map[string]any{"agenttrace_logical_span_id": "assistant"}},
	}}
	records := []postgres.TraceUsageDetail{
		{ID: "main-usage", SpanID: "root", Provider: "anthropic-compatible", Model: "main-model",
			InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
			Details: json.RawMessage(`{"granularity":"sdk_query_model_cumulative","usage_scope":"sdk_query_pipeline","sdk_result_uuid":"query"}`)},
		{ID: "side-usage", SpanID: "root", Provider: "anthropic-compatible", Model: "sidechain-model",
			InputTokens: 7, OutputTokens: 3, TotalTokens: 10,
			Details: json.RawMessage(`{"granularity":"sdk_query_model_cumulative","usage_scope":"sdk_query_pipeline","sdk_result_uuid":"query"}`)},
	}
	projectUsage(&batch, records)
	summary, ok := batch.Trace.Metadata["agenttrace_sdk_query_usage"].([]any)
	if !ok || len(summary) != 2 {
		t.Fatalf("query totals are missing from trace metadata: %#v", batch.Trace.Metadata)
	}
	root, ok := batch.Observations[0].Metadata["agenttrace_sdk_query_usage"].([]any)
	if !ok || len(root) != 2 {
		t.Fatal("query totals are missing from their parent observation")
	}
	if batch.Observations[1].UsageDetails != nil || len(batch.Observations) != 2 {
		t.Fatal("query totals were fabricated as individual model-call usage")
	}
}
