package langfuse

import (
	"encoding/json"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

func runInput(run domain.Run) any {
	var snapshot struct {
		Command event.RunCommand `json:"command"`
	}
	if json.Unmarshal(run.Specification, &snapshot) != nil {
		return nil
	}
	// Trace input is the immutable Run goal. Per-call model inputs also capture
	// any continuation responses; later Conversation messages cannot rewrite it.
	for i := len(snapshot.Command.Messages) - 1; i >= 0; i-- {
		if snapshot.Command.Messages[i].Role == "user" {
			return map[string]any{"goal": snapshot.Command.Messages[i].Content}
		}
	}
	return nil
}

func projectUsage(batch *Batch, records []postgres.TraceUsageDetail) {
	queryUsage := []any{}
	queryBySpan := map[string][]any{}
	for _, record := range records {
		var scope struct {
			Granularity string `json:"granularity"`
			UsageScope  string `json:"usage_scope"`
		}
		if json.Unmarshal(record.Details, &scope) != nil ||
			scope.Granularity != "sdk_query_model_cumulative" || scope.UsageScope != "sdk_query_pipeline" {
			continue
		}
		summary := map[string]any{
			"id": record.ID, "provider": record.Provider, "model": record.Model,
			"input_tokens": record.InputTokens, "output_tokens": record.OutputTokens,
			"total_tokens": record.TotalTokens, "details": decodeJSON(record.Details),
		}
		queryUsage = append(queryUsage, summary)
		queryBySpan[record.SpanID] = append(queryBySpan[record.SpanID], summary)
	}
	if len(queryUsage) > 0 {
		if batch.Trace.Metadata == nil {
			batch.Trace.Metadata = map[string]any{}
		}
		batch.Trace.Metadata["agenttrace_sdk_query_usage"] = queryUsage
	}
	for i := range batch.Observations {
		observation := &batch.Observations[i]
		spanID, _ := observation.Metadata["agenttrace_logical_span_id"].(string)
		if summaries := queryBySpan[spanID]; len(summaries) > 0 {
			observation.Metadata["agenttrace_sdk_query_usage"] = summaries
		}
		if observation.Type != "generation" {
			continue
		}
		var input, output, total, cost int64
		found, knownCost := false, true
		for _, record := range records {
			if record.SpanID != spanID {
				continue
			}
			var scope struct {
				Granularity string `json:"granularity"`
			}
			if json.Unmarshal(record.Details, &scope) == nil && scope.Granularity == "sdk_query_model_cumulative" {
				continue
			}
			found = true
			input += record.InputTokens
			output += record.OutputTokens
			total += record.TotalTokens
			cost += record.CostMicros
			var details struct {
				CostMicros *int64 `json:"cost_micros"`
			}
			// Old positive costs are known; old default zero costs are not.
			if json.Unmarshal(record.Details, &details) != nil ||
				(details.CostMicros == nil && record.CostMicros == 0) {
				knownCost = false
			}
			if record.Model != "" {
				observation.Model = record.Model
			}
		}
		if found {
			observation.UsageDetails = map[string]any{"input": input, "output": output, "total": total}
			if knownCost {
				observation.CostDetails = map[string]any{"total": float64(cost) / 1e6}
			}
		}
	}
}
