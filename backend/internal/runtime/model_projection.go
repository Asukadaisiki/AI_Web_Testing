package runtime

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
)

// These optional fields are normalized by the TS model event adapter. Missing
// data is evidence of absence, not an empty request or a zero-duration call.
type modelMessageEvidence struct {
	Provider  string          `json:"provider,omitempty"`
	Model     string          `json:"model,omitempty"`
	Input     json.RawMessage `json:"input"`
	StartedAt *time.Time      `json:"started_at"`
	EndedAt   *time.Time      `json:"ended_at"`
}

func decodeModelMessage(raw json.RawMessage) (modelMessageEvidence, error) {
	var evidence modelMessageEvidence
	if err := json.Unmarshal(raw, &evidence); err != nil {
		return evidence, err
	}
	if err := validateModelIdentity(raw); err != nil {
		return evidence, err
	}
	if evidence.StartedAt != nil && evidence.EndedAt != nil &&
		evidence.EndedAt.Before(*evidence.StartedAt) {
		return evidence, errors.New("model ended_at precedes started_at")
	}
	return evidence, nil
}

func modelUsage(raw json.RawMessage, attemptID string) (*postgres.TraceUsage, error) {
	var evidence struct {
		InputTokens      *int   `json:"input_tokens"`
		OutputTokens     *int   `json:"output_tokens"`
		TotalTokens      *int   `json:"total_tokens"`
		CacheReadTokens  int    `json:"cache_read_tokens"`
		CacheWriteTokens int    `json:"cache_write_tokens"`
		ReasoningTokens  int    `json:"reasoning_tokens"`
		CachedTokens     int    `json:"cached_tokens"`
		CostMicros       *int64 `json:"cost_micros"`
		Provider         string `json:"provider"`
		Model            string `json:"model"`
	}
	if err := json.Unmarshal(raw, &evidence); err != nil {
		return nil, err
	}
	if err := validateModelIdentity(raw); err != nil {
		return nil, err
	}
	if evidence.InputTokens == nil && evidence.OutputTokens == nil {
		return nil, nil
	}
	if evidence.InputTokens == nil || evidence.OutputTokens == nil ||
		*evidence.InputTokens < 0 || *evidence.OutputTokens < 0 ||
		evidence.ReasoningTokens < 0 || evidence.CachedTokens < 0 ||
		evidence.CacheReadTokens < 0 || evidence.CacheWriteTokens < 0 ||
		(evidence.TotalTokens != nil && *evidence.TotalTokens < 0) ||
		(evidence.CostMicros != nil && *evidence.CostMicros < 0) {
		return nil, errors.New("model usage requires nonnegative reported token counts and cost")
	}
	value := usage.Call(*evidence.InputTokens, *evidence.OutputTokens, evidence.ReasoningTokens, evidence.CachedTokens)
	if evidence.TotalTokens != nil {
		value.TotalTokens = *evidence.TotalTokens
	}
	if evidence.CostMicros != nil {
		value.CostMicros = *evidence.CostMicros
	}
	provider := evidence.Provider
	if provider == "" {
		provider = "model"
	}
	return &postgres.TraceUsage{
		ID: stableID("usage", attemptID), Provider: provider, Model: evidence.Model,
		Usage: value, Details: append(json.RawMessage(nil), raw...),
	}, nil
}

func sdkQueryUsageIdentity(raw json.RawMessage) (string, bool, error) {
	var scope struct {
		Granularity string `json:"granularity"`
		UsageScope  string `json:"usage_scope"`
		ResultID    string `json:"sdk_result_uuid"`
		Provider    string `json:"provider"`
		Model       string `json:"model"`
	}
	if err := json.Unmarshal(raw, &scope); err != nil {
		return "", false, err
	}
	if scope.Granularity != "sdk_query_model_cumulative" {
		return "", false, nil
	}
	if scope.UsageScope != "sdk_query_pipeline" || strings.TrimSpace(scope.ResultID) == "" ||
		strings.TrimSpace(scope.Provider) == "" || strings.TrimSpace(scope.Model) == "" {
		return "", false, errors.New("SDK query usage requires scope, result and model identity")
	}
	return stableID("query-model", scope.ResultID, scope.Provider, scope.Model), true, nil
}

func validateModelIdentity(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, field := range []string{"provider", "model"} {
		if value, present := fields[field]; present {
			var decoded string
			if err := json.Unmarshal(value, &decoded); err != nil || strings.TrimSpace(decoded) == "" {
				return errors.New("model identity " + field + " must be a nonblank string")
			}
		}
	}
	return nil
}
