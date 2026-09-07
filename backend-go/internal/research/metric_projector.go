package research

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	MetricProjectorVersion = "research.metric_projector.v1"

	MetricReasonOracleUnavailable       = "independent_oracle_unavailable"
	MetricReasonNoExecutions            = "no_eligible_executions"
	MetricReasonExecutionFactMissing    = "execution_fact_missing"
	MetricReasonNoReports               = "no_execution_reports"
	MetricReasonReportFactMissing       = "execution_report_fact_missing"
	MetricReasonNoGrounding             = "no_eligible_grounding_steps"
	MetricReasonGroundingFactMissing    = "grounding_fact_missing"
	MetricReasonNoActions               = "no_eligible_actions"
	MetricReasonActionFactMissing       = "invalid_action_classification_fact_missing"
	MetricReasonNoVerifications         = "no_eligible_verifications"
	MetricReasonVerificationFactMissing = "verification_fact_missing"
	MetricReasonNoRecoveries            = "no_eligible_recovery_attempts"
	MetricReasonRecoveryFactMissing     = "recovery_fact_missing"
	MetricReasonLLMUsageMissing         = "llm_usage_fact_missing"
	MetricReasonLatencyFactMissing      = "latency_fact_missing"
	MetricReasonVisionFactMissing       = "vision_fact_missing"
)

type MetricProjector struct{}

func NewMetricProjector() *MetricProjector {
	return &MetricProjector{}
}

func (p *MetricProjector) Project(source MetricSource) (RunMetrics, error) {
	if err := source.NormalizeAndValidate(); err != nil {
		return RunMetrics{}, err
	}
	metrics := RunMetrics{
		SchemaVersion:       MetricVersion,
		ProjectorVersion:    MetricProjectorVersion,
		SourceSHA256:        source.SourceSHA256,
		TaskSuccess:         projectTaskSuccess(source.Oracle),
		ExecutionSuccess:    projectExecutionSuccess(source),
		VerificationSuccess: projectVerificationSuccess(source),
		GroundingAccuracy:   projectGroundingAccuracy(source),
		InvalidActionRate:   projectInvalidActionRate(source),
		RecoveryRate:        projectRecoveryRate(source),
		Steps:               projectSteps(source),
		Retries:             projectRetries(source),
		LLMCalls:            projectLLMCalls(source.Snapshot),
	}
	metrics.InputTokens, metrics.OutputTokens, metrics.TotalTokens =
		projectTokens(source.Snapshot)
	metrics.LatencyMS = projectLatency(source)
	metrics.VisionCalls = projectVisionCalls(source)
	hash, err := runMetricsHash(metrics)
	if err != nil {
		return RunMetrics{}, err
	}
	metrics.MetricsSHA256 = hash
	if err := metrics.Validate(); err != nil {
		return RunMetrics{}, err
	}
	return metrics, nil
}

func runMetricsHash(metrics RunMetrics) (string, error) {
	metrics.MetricsSHA256 = ""
	return CanonicalSHA256(metrics)
}

func projectTaskSuccess(oracle Slot[OracleDecision]) NullableValue[bool] {
	if oracle.Status != SlotAvailable || oracle.Value == nil {
		return unavailableMetric[bool](MetricReasonOracleUnavailable)
	}
	return availableMetric(oracle.Value.Passed)
}

func projectExecutionSuccess(source MetricSource) NullableValue[bool] {
	execution := finalMetricExecution(source)
	if execution == nil {
		return unavailableMetric[bool](MetricReasonNoExecutions)
	}
	if !isTerminalExecutionStatus(execution.Status) {
		return unavailableMetric[bool](MetricReasonExecutionFactMissing)
	}
	return availableMetric(execution.Status == "passed")
}

func projectVerificationSuccess(source MetricSource) NullableValue[bool] {
	if finalMetricExecution(source) == nil {
		return unavailableMetric[bool](MetricReasonNoVerifications)
	}
	steps, reason := finalMetricSteps(source)
	if reason != "" {
		return unavailableMetric[bool](MetricReasonVerificationFactMissing)
	}
	eligible := int64(0)
	success := true
	for _, step := range steps {
		conditions, exists, valid := metricArray(step, "condition_results")
		if !exists || !valid {
			return unavailableMetric[bool](MetricReasonVerificationFactMissing)
		}
		for _, rawCondition := range conditions {
			condition, ok := rawCondition.(map[string]any)
			if !ok {
				return unavailableMetric[bool](MetricReasonVerificationFactMissing)
			}
			status, ok := metricString(condition, "status")
			if !ok {
				return unavailableMetric[bool](MetricReasonVerificationFactMissing)
			}
			switch status {
			case "passed":
			case "failed", "error":
				success = false
			default:
				return unavailableMetric[bool](MetricReasonVerificationFactMissing)
			}
			eligible++
		}
	}
	if eligible == 0 {
		return unavailableMetric[bool](MetricReasonNoVerifications)
	}
	return availableMetric(success)
}

func projectGroundingAccuracy(source MetricSource) NullableValue[float64] {
	steps, reason := finalMetricSteps(source)
	if reason != "" {
		return unavailableMetric[float64](MetricReasonReportFactMissing)
	}
	eligible := int64(0)
	for _, step := range steps {
		raw, exists := step["locator_trace"]
		if !exists || raw == nil {
			continue
		}
		if _, ok := raw.(map[string]any); !ok {
			return unavailableMetric[float64](MetricReasonGroundingFactMissing)
		}
		eligible++
	}
	if eligible == 0 {
		return unavailableMetric[float64](MetricReasonNoGrounding)
	}
	return unavailableMetric[float64](MetricReasonGroundingFactMissing)
}

func projectInvalidActionRate(source MetricSource) NullableValue[float64] {
	steps, reason := finalMetricSteps(source)
	if reason != "" {
		return unavailableMetric[float64](MetricReasonReportFactMissing)
	}
	if len(steps) == 0 {
		return unavailableMetric[float64](MetricReasonNoActions)
	}
	return unavailableMetric[float64](MetricReasonActionFactMissing)
}

func projectRecoveryRate(source MetricSource) NullableValue[float64] {
	outcomes := make(map[string]bool)
	for _, transition := range source.Transitions {
		var payload TransitionPayloadV1
		if json.Unmarshal(transition.PayloadJSON, &payload) != nil ||
			payload.Recovery.Status != SlotAvailable ||
			payload.Recovery.Value == nil {
			continue
		}
		recovery := payload.Recovery.Value
		executionID, bound := boundRecoveryExecutionID(*recovery, source.FinalRunLinks)
		if !bound {
			continue
		}
		var data map[string]any
		if decodeJSONObject(recovery.Data, &data) != nil {
			return unavailableMetric[float64](MetricReasonRecoveryFactMissing)
		}
		attempt, attemptOK := metricInt64(data, "attempt")
		previous, previousOK := metricInt64(data, "previous_attempt")
		outcome, outcomeOK := metricString(data, "outcome")
		if !attemptOK || !previousOK || attempt != previous+1 ||
			recovery.Attempt.Value == nil || *recovery.Attempt.Value != attempt ||
			!outcomeOK {
			return unavailableMetric[float64](MetricReasonRecoveryFactMissing)
		}
		var recovered bool
		switch outcome {
		case "recovered":
			recovered = true
		case "failed":
		default:
			return unavailableMetric[float64](MetricReasonRecoveryFactMissing)
		}
		key := fmt.Sprintf("%d:%d", executionID, attempt)
		if existing, exists := outcomes[key]; exists && existing != recovered {
			return unavailableMetric[float64](MetricReasonRecoveryFactMissing)
		}
		outcomes[key] = recovered
	}
	if len(outcomes) == 0 {
		return unavailableMetric[float64](MetricReasonNoRecoveries)
	}
	recovered := int64(0)
	for _, outcome := range outcomes {
		if outcome {
			recovered++
		}
	}
	return availableMetric(float64(recovered) / float64(len(outcomes)))
}

func projectSteps(source MetricSource) NullableValue[int64] {
	steps, reason := finalMetricSteps(source)
	if reason == MetricReasonNoReports {
		return unavailableMetric[int64](MetricReasonNoReports)
	}
	if reason != "" {
		return unavailableMetric[int64](MetricReasonReportFactMissing)
	}
	return availableMetric(int64(len(steps)))
}

func projectRetries(source MetricSource) NullableValue[int64] {
	jobs := metricJobs(source)
	if len(jobs) == 0 {
		return unavailableMetric[int64](MetricReasonNoExecutions)
	}
	retries := llmPhysicalRetries(source.Snapshot)
	for _, job := range jobs {
		if job.AttemptCount > 1 {
			retries += job.AttemptCount - 1
		}
	}
	return availableMetric(retries)
}

func projectLLMCalls(snapshot SourceSnapshot) NullableValue[int64] {
	logicalCalls := make(map[string]struct{})
	for _, event := range snapshot.Events {
		if event.Type != "research.llm_call" {
			continue
		}
		var payload struct {
			LogicalCallID string `json:"logical_call_id"`
		}
		if json.Unmarshal(event.Payload, &payload) != nil ||
			strings.TrimSpace(payload.LogicalCallID) == "" {
			return unavailableMetric[int64](MetricReasonExecutionFactMissing)
		}
		logicalCalls[payload.LogicalCallID] = struct{}{}
	}
	return availableMetric(int64(len(logicalCalls)))
}

func projectTokens(
	snapshot SourceSnapshot,
) (NullableValue[int64], NullableValue[int64], NullableValue[int64]) {
	var input, output, total int64
	for _, event := range snapshot.Events {
		if event.Type != "research.llm_call" {
			continue
		}
		var payload map[string]any
		if decodeJSONObject(event.Payload, &payload) != nil {
			return unavailableTokenMetrics()
		}
		usage, ok := payload["usage"].(map[string]any)
		if !ok {
			return unavailableTokenMetrics()
		}
		status, ok := metricString(usage, "status")
		if !ok || status != "available" {
			return unavailableTokenMetrics()
		}
		attemptInput, inputOK := metricInt64(usage, "input_tokens")
		attemptOutput, outputOK := metricInt64(usage, "output_tokens")
		attemptTotal, totalOK := metricInt64(usage, "total_tokens")
		if !inputOK || !outputOK || !totalOK ||
			attemptInput < 0 || attemptOutput < 0 ||
			attemptTotal != attemptInput+attemptOutput {
			return unavailableTokenMetrics()
		}
		input += attemptInput
		output += attemptOutput
		total += attemptTotal
	}
	return availableMetric(input), availableMetric(output), availableMetric(total)
}

func projectLatency(source MetricSource) NullableValue[int64] {
	var total int64
	for _, event := range source.Snapshot.Events {
		if event.Type != "research.llm_call" {
			continue
		}
		var payload map[string]any
		if decodeJSONObject(event.Payload, &payload) != nil {
			return unavailableMetric[int64](MetricReasonLatencyFactMissing)
		}
		latency, ok := metricInt64(payload, "attempt_latency_ms")
		if !ok || latency < 0 {
			return unavailableMetric[int64](MetricReasonLatencyFactMissing)
		}
		total += latency
	}
	steps, reason := finalMetricSteps(source)
	if reason != "" && reason != MetricReasonNoReports {
		return unavailableMetric[int64](MetricReasonLatencyFactMissing)
	}
	for _, step := range steps {
		latency, ok := metricInt64(step, "duration_ms")
		if !ok || latency < 0 {
			return unavailableMetric[int64](MetricReasonLatencyFactMissing)
		}
		total += latency
	}
	return availableMetric(total)
}

func projectVisionCalls(source MetricSource) NullableValue[int64] {
	steps, reason := finalMetricSteps(source)
	if reason == MetricReasonNoReports {
		return unavailableMetric[int64](MetricReasonNoReports)
	}
	if reason != "" {
		return unavailableMetric[int64](MetricReasonVisionFactMissing)
	}
	var calls int64
	for _, step := range steps {
		used, ok := metricBool(step, "vlm_preverify_used")
		if !ok {
			return unavailableMetric[int64](MetricReasonVisionFactMissing)
		}
		if used {
			calls++
		}
	}
	return availableMetric(calls)
}

func metricJobs(source MetricSource) []JobSnapshot {
	batch := finalMetricBatch(source)
	if batch == nil {
		return nil
	}
	return batch.Jobs
}

func finalMetricBatch(source MetricSource) *BatchSnapshot {
	if source.FinalRunLinks.BatchID == nil {
		return nil
	}
	for index := range source.Snapshot.Batches {
		if source.Snapshot.Batches[index].ID == *source.FinalRunLinks.BatchID {
			return &source.Snapshot.Batches[index]
		}
	}
	return nil
}

func finalMetricExecution(source MetricSource) *ExecutionSnapshot {
	batch := finalMetricBatch(source)
	if batch == nil || source.FinalRunLinks.ExecutionID == nil {
		return nil
	}
	for jobIndex := range batch.Jobs {
		for executionIndex := range batch.Jobs[jobIndex].Executions {
			execution := &batch.Jobs[jobIndex].Executions[executionIndex]
			if execution.ID == *source.FinalRunLinks.ExecutionID {
				return execution
			}
		}
	}
	return nil
}

func finalMetricSteps(source MetricSource) ([]map[string]any, string) {
	execution := finalMetricExecution(source)
	if execution == nil || execution.Report.Status != SlotAvailable ||
		execution.Report.Value == nil {
		return nil, MetricReasonNoReports
	}
	return metricExecutionSteps(*execution)
}

func llmPhysicalRetries(snapshot SourceSnapshot) int64 {
	attempts := make(map[string]map[int64]struct{})
	for _, event := range snapshot.Events {
		if event.Type != "research.llm_call" {
			continue
		}
		var payload struct {
			LogicalCallID string `json:"logical_call_id"`
			Attempt       int64  `json:"attempt"`
		}
		if json.Unmarshal(event.Payload, &payload) != nil {
			continue
		}
		logicalCallID := strings.TrimSpace(payload.LogicalCallID)
		if logicalCallID == "" || payload.Attempt <= 1 {
			continue
		}
		if attempts[logicalCallID] == nil {
			attempts[logicalCallID] = make(map[int64]struct{})
		}
		attempts[logicalCallID][payload.Attempt] = struct{}{}
	}
	var retries int64
	for _, physicalAttempts := range attempts {
		retries += int64(len(physicalAttempts))
	}
	return retries
}

func boundRecoveryExecutionID(event ResearchEvent, links RunLinks) (int64, bool) {
	if links.BatchID == nil || links.ExecutionID == nil {
		return 0, false
	}
	batchID := strconv.FormatInt(*links.BatchID, 10)
	executionID := strconv.FormatInt(*links.ExecutionID, 10)
	hasBatch := false
	hasExecution := false
	for _, source := range event.Sources {
		switch {
		case source.Kind == SourceBatch && source.ID == batchID:
			hasBatch = true
		case source.Kind == SourceExecution && source.ID == executionID:
			hasExecution = true
		}
	}
	return *links.ExecutionID, hasBatch && hasExecution
}

func metricExecutionSteps(execution ExecutionSnapshot) ([]map[string]any, string) {
	if execution.Report.Status != SlotAvailable || execution.Report.Value == nil {
		return nil, MetricReasonReportFactMissing
	}
	var report map[string]any
	if decodeJSONObject(*execution.Report.Value, &report) != nil {
		return nil, MetricReasonReportFactMissing
	}
	rawSteps, exists, valid := metricArray(report, "steps")
	if !exists || !valid {
		return nil, MetricReasonReportFactMissing
	}
	steps := make([]map[string]any, 0, len(rawSteps))
	for _, rawStep := range rawSteps {
		step, ok := rawStep.(map[string]any)
		if !ok {
			return nil, MetricReasonReportFactMissing
		}
		steps = append(steps, step)
	}
	return steps, ""
}

func decodeJSONObject(raw json.RawMessage, target *map[string]any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil || *target == nil {
		return fmt.Errorf("%w: expected JSON object", ErrInvalid)
	}
	return nil
}

func metricArray(object map[string]any, key string) ([]any, bool, bool) {
	value, exists := object[key]
	if !exists {
		return nil, false, false
	}
	items, valid := value.([]any)
	return items, true, valid
}

func metricString(object map[string]any, key string) (string, bool) {
	value, exists := object[key]
	if !exists {
		return "", false
	}
	result, valid := value.(string)
	return result, valid
}

func metricBool(object map[string]any, key string) (bool, bool) {
	value, exists := object[key]
	if !exists {
		return false, false
	}
	result, valid := value.(bool)
	return result, valid
}

func metricInt64(object map[string]any, key string) (int64, bool) {
	value, exists := object[key]
	if !exists {
		return 0, false
	}
	switch typed := value.(type) {
	case json.Number:
		result, err := typed.Int64()
		return result, err == nil
	case float64:
		if math.Trunc(typed) != typed {
			return 0, false
		}
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}

func unavailableTokenMetrics() (
	NullableValue[int64],
	NullableValue[int64],
	NullableValue[int64],
) {
	return unavailableMetric[int64](MetricReasonLLMUsageMissing),
		unavailableMetric[int64](MetricReasonLLMUsageMissing),
		unavailableMetric[int64](MetricReasonLLMUsageMissing)
}

func availableMetric[T any](value T) NullableValue[T] {
	return NullableValue[T]{Value: &value}
}

func unavailableMetric[T any](reason string) NullableValue[T] {
	return NullableValue[T]{UnavailableReason: &reason}
}
