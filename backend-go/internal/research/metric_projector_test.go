package research

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMetricProjectorProjectsMetricsIndependently(t *testing.T) {
	source := metricSourceFixture(t, nil, Available(metricOracleFixture(t, true)))
	metrics, err := NewMetricProjector().Project(source)
	if err != nil {
		t.Fatal(err)
	}

	assertBoolMetric(t, "task_success", metrics.TaskSuccess, true)
	assertBoolMetric(t, "execution_success", metrics.ExecutionSuccess, true)
	assertBoolMetric(t, "verification_success", metrics.VerificationSuccess, true)
	assertUnavailableMetric(
		t, metrics.GroundingAccuracy, MetricReasonGroundingFactMissing,
	)
	assertUnavailableMetric(
		t, metrics.InvalidActionRate, MetricReasonActionFactMissing,
	)
	assertUnavailableMetric(t, metrics.RecoveryRate, MetricReasonRecoveryFactMissing)
	assertIntMetric(t, "steps", metrics.Steps, 1)
	assertIntMetric(t, "retries", metrics.Retries, 2)
	assertIntMetric(t, "llm_calls", metrics.LLMCalls, 1)
	assertUnavailableMetric(t, metrics.InputTokens, MetricReasonLLMUsageMissing)
	assertUnavailableMetric(t, metrics.OutputTokens, MetricReasonLLMUsageMissing)
	assertUnavailableMetric(t, metrics.TotalTokens, MetricReasonLLMUsageMissing)
	assertIntMetric(t, "latency_ms", metrics.LatencyMS, 23)
	assertIntMetric(t, "vision_calls", metrics.VisionCalls, 1)
	if metrics.SchemaVersion != MetricVersion ||
		metrics.ProjectorVersion != MetricProjectorVersion ||
		metrics.SourceSHA256 != source.SourceSHA256 ||
		!sha256Pattern.MatchString(metrics.MetricsSHA256) {
		t.Fatalf("metrics provenance = %#v", metrics)
	}
}

func TestMetricProjectorKeepsSuccessSignalsIndependent(t *testing.T) {
	tests := []struct {
		name             string
		oraclePassed     bool
		mutate           func(*SourceSnapshot)
		taskSuccess      bool
		executionSuccess bool
		verifySuccess    bool
	}{
		{
			name:         "formal pass oracle fail",
			oraclePassed: false, taskSuccess: false,
			executionSuccess: true, verifySuccess: true,
		},
		{
			name:         "oracle pass formal fail",
			oraclePassed: true, taskSuccess: true,
			executionSuccess: false, verifySuccess: true,
			mutate: func(snapshot *SourceSnapshot) {
				execution := metricExecution(snapshot, 62)
				execution.Status = "failed"
				mutateMetricReportStatus(t, execution, "failed")
			},
		},
		{
			name:         "formal pass verification fail",
			oraclePassed: true, taskSuccess: true,
			executionSuccess: true, verifySuccess: false,
			mutate: func(snapshot *SourceSnapshot) {
				mutateMetricStep(t, snapshot, 62, func(step map[string]any) {
					conditions := step["condition_results"].([]any)
					conditions[0].(map[string]any)["status"] = "failed"
				})
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			source := metricSourceFixture(
				t, testCase.mutate,
				Available(metricOracleFixture(t, testCase.oraclePassed)),
			)
			metrics, err := NewMetricProjector().Project(source)
			if err != nil {
				t.Fatal(err)
			}
			assertBoolMetric(t, "task_success", metrics.TaskSuccess, testCase.taskSuccess)
			assertBoolMetric(
				t, "execution_success", metrics.ExecutionSuccess,
				testCase.executionSuccess,
			)
			assertBoolMetric(
				t, "verification_success", metrics.VerificationSuccess,
				testCase.verifySuccess,
			)
		})
	}
}

func TestMetricProjectorReturnsNullWithStableReason(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SourceSnapshot)
		field  string
		reason string
	}{
		{
			name: "grounding has no eligible denominator",
			mutate: func(snapshot *SourceSnapshot) {
				mutateMetricStep(t, snapshot, 62, func(step map[string]any) {
					delete(step, "locator_trace")
				})
			},
			field: "grounding", reason: MetricReasonNoGrounding,
		},
		{
			name: "grounding fact missing",
			mutate: func(snapshot *SourceSnapshot) {
				mutateMetricStep(t, snapshot, 62, func(step map[string]any) {
					step["locator_trace"] = map[string]any{"candidates": []any{}}
				})
			},
			field: "grounding", reason: MetricReasonGroundingFactMissing,
		},
		{
			name: "action fact missing",
			mutate: func(snapshot *SourceSnapshot) {
				mutateMetricStep(t, snapshot, 62, func(step map[string]any) {
					step["action_outcome"] = map[string]any{
						"status": "failed", "side_effect_state": "not_committed",
					}
				})
			},
			field: "invalid_action", reason: MetricReasonActionFactMissing,
		},
		{
			name: "verification has no eligible denominator",
			mutate: func(snapshot *SourceSnapshot) {
				mutateMetricStep(t, snapshot, 62, func(step map[string]any) {
					step["condition_results"] = []any{}
				})
			},
			field: "verification", reason: MetricReasonNoVerifications,
		},
		{
			name: "latency fact missing",
			mutate: func(snapshot *SourceSnapshot) {
				mutateMetricStep(t, snapshot, 62, func(step map[string]any) {
					delete(step, "duration_ms")
				})
			},
			field: "latency", reason: MetricReasonLatencyFactMissing,
		},
		{
			name: "vision fact missing",
			mutate: func(snapshot *SourceSnapshot) {
				mutateMetricStep(t, snapshot, 62, func(step map[string]any) {
					delete(step, "vlm_preverify_used")
				})
			},
			field: "vision", reason: MetricReasonVisionFactMissing,
		},
		{
			name: "token usage missing",
			mutate: func(snapshot *SourceSnapshot) {
				snapshot.Events[1] = agentEventFixture(
					t, 2, "research.llm_call", "", "", `{
						"schema_version":"research.llm_call.v1",
						"logical_call_id":"logical-1",
						"provider":"provider",
						"requested_model":"model",
						"attempt":1,
						"attempt_status":"failed",
						"attempt_latency_ms":7,
						"tool_call_status":"unavailable",
						"usage":{"status":"unavailable"},
						"prompt_spec":{"version":"prompt.v1","prompt_sha256":"aaa","request_sha256":"bbb"}
					}`,
				)
			},
			field: "tokens", reason: MetricReasonLLMUsageMissing,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			source := metricSourceFixture(
				t, testCase.mutate, Available(metricOracleFixture(t, true)),
			)
			metrics, err := NewMetricProjector().Project(source)
			if err != nil {
				t.Fatal(err)
			}
			switch testCase.field {
			case "grounding":
				assertUnavailableMetric(t, metrics.GroundingAccuracy, testCase.reason)
			case "invalid_action":
				assertUnavailableMetric(t, metrics.InvalidActionRate, testCase.reason)
			case "verification":
				assertUnavailableMetric(t, metrics.VerificationSuccess, testCase.reason)
			case "latency":
				assertUnavailableMetric(t, metrics.LatencyMS, testCase.reason)
			case "vision":
				assertUnavailableMetric(t, metrics.VisionCalls, testCase.reason)
			case "tokens":
				assertUnavailableMetric(t, metrics.InputTokens, testCase.reason)
				assertUnavailableMetric(t, metrics.OutputTokens, testCase.reason)
				assertUnavailableMetric(t, metrics.TotalTokens, testCase.reason)
			}
			assertBoolMetric(t, "task_success", metrics.TaskSuccess, true)
		})
	}
}

func TestMetricSourceRejectsMutableOrIncompleteInputs(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*SourceSnapshot)
		oracle     func(*testing.T) Slot[OracleDecision]
		wantTarget error
	}{
		{
			name: "agent run is not completed",
			mutate: func(snapshot *SourceSnapshot) {
				snapshot.AgentRunStatus = "running"
			},
			oracle: func(t *testing.T) Slot[OracleDecision] {
				return Available(metricOracleFixture(t, true))
			},
			wantTarget: ErrSourceChanged,
		},
		{
			name: "final execution is not terminal",
			mutate: func(snapshot *SourceSnapshot) {
				metricExecution(snapshot, 62).Status = "running"
			},
			oracle: func(t *testing.T) Slot[OracleDecision] {
				return Available(metricOracleFixture(t, true))
			},
			wantTarget: ErrSourceChanged,
		},
		{
			name: "final report is unavailable",
			mutate: func(snapshot *SourceSnapshot) {
				execution := metricExecution(snapshot, 62)
				execution.Report = Unavailable[json.RawMessage](
					"execution_report_not_available",
				)
				execution.ReportRef = Unavailable[SourceRef](
					"execution_report_not_available",
				)
			},
			oracle: func(t *testing.T) Slot[OracleDecision] {
				return Available(metricOracleFixture(t, true))
			},
			wantTarget: ErrSourceChanged,
		},
		{
			name: "oracle is unavailable",
			oracle: func(_ *testing.T) Slot[OracleDecision] {
				return Unavailable[OracleDecision]("oracle_not_run")
			},
			wantTarget: ErrSourceChanged,
		},
		{
			name: "final links are incomplete",
			mutate: func(snapshot *SourceSnapshot) {
				snapshot.FinalRunLinks.ExecutionID = nil
			},
			oracle: func(t *testing.T) Slot[OracleDecision] {
				return Available(metricOracleFixture(t, true))
			},
			wantTarget: ErrBrokenLink,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			source := metricSourceFixture(
				t, nil, Available(metricOracleFixture(t, true)),
			)
			snapshot := source.Snapshot
			if testCase.mutate != nil {
				testCase.mutate(&snapshot)
			}
			snapshot.SourceSHA256 = ""
			var err error
			snapshot.SourceSHA256, err = sourceSnapshotHash(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = NewMetricSource(
				snapshot, source.Transitions, testCase.oracle(t),
			); !errors.Is(err, testCase.wantTarget) {
				t.Fatalf(
					"NewMetricSource() error = %v, want %v",
					err, testCase.wantTarget,
				)
			}
		})
	}
}

func TestMetricProjectorUsesOnlyBoundFinalExecution(t *testing.T) {
	source := metricSourceFixture(t, nil, Available(metricOracleFixture(t, true)))
	finalMetrics, err := NewMetricProjector().Project(source)
	if err != nil {
		t.Fatal(err)
	}
	assertBoolMetric(t, "final execution_success", finalMetrics.ExecutionSuccess, true)
	assertIntMetric(t, "final steps", finalMetrics.Steps, 1)

	oldSnapshot := source.Snapshot
	oldSnapshot.FinalRunLinks = metricRunLinks(
		oldSnapshot.AgentRunID, 31, 41, 61,
		*source.FinalRunLinks.DSLSHA256,
	)
	oldSource, err := NewMetricSource(
		oldSnapshot, source.Transitions, source.Oracle,
	)
	if err != nil {
		t.Fatal(err)
	}
	oldMetrics, err := NewMetricProjector().Project(oldSource)
	if err != nil {
		t.Fatal(err)
	}
	assertBoolMetric(t, "old execution_success", oldMetrics.ExecutionSuccess, false)
	assertIntMetric(t, "old steps", oldMetrics.Steps, 1)
	assertIntMetric(t, "old vision_calls", oldMetrics.VisionCalls, 0)
	if source.SourceSHA256 == oldSource.SourceSHA256 {
		t.Fatal("final execution link did not change metric source hash")
	}
}

func TestMetricProjectorIgnoresUnboundFailedBatch(t *testing.T) {
	source := metricSourceFixture(
		t,
		func(snapshot *SourceSnapshot) {
			addUnboundFailedBatch(t, snapshot)
		},
		Available(metricOracleFixture(t, true)),
	)
	metrics, err := NewMetricProjector().Project(source)
	if err != nil {
		t.Fatal(err)
	}
	assertBoolMetric(t, "execution_success", metrics.ExecutionSuccess, true)
	assertBoolMetric(t, "verification_success", metrics.VerificationSuccess, true)
	assertIntMetric(t, "steps", metrics.Steps, 1)
	assertIntMetric(t, "retries", metrics.Retries, 2)
	assertIntMetric(t, "vision_calls", metrics.VisionCalls, 1)
}

func TestMetricProjectorDoesNotInferInvalidActionFromFailures(t *testing.T) {
	for _, category := range []string{"locator", "network", "action"} {
		t.Run(category, func(t *testing.T) {
			source := metricSourceFixture(
				t,
				func(snapshot *SourceSnapshot) {
					execution := metricExecution(snapshot, 62)
					execution.Status = "failed"
					mutateMetricReportStatus(t, execution, "failed")
					execution.FailureSignal = Available(json.RawMessage(
						`{"schema_version":"failure.signal.v1","category":"` +
							category + `"}`,
					))
					mutateMetricStep(t, snapshot, 62, func(step map[string]any) {
						step["status"] = "failed"
						step["action_outcome"] = map[string]any{
							"status": "failed", "side_effect_state": "not_committed",
						}
					})
				},
				Available(metricOracleFixture(t, true)),
			)
			metrics, err := NewMetricProjector().Project(source)
			if err != nil {
				t.Fatal(err)
			}
			assertUnavailableMetric(
				t, metrics.InvalidActionRate, MetricReasonActionFactMissing,
			)
		})
	}
}

func TestMetricProjectionIsDeterministicAndBoundToSources(t *testing.T) {
	source := metricSourceFixture(t, nil, Available(metricOracleFixture(t, true)))
	first, err := NewMetricProjector().Project(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewMetricProjector().Project(source)
	if err != nil {
		t.Fatal(err)
	}
	firstRaw, _ := json.Marshal(first)
	secondRaw, _ := json.Marshal(second)
	if !bytes.Equal(firstRaw, secondRaw) {
		t.Fatalf("metric projection differs:\n%s\n%s", firstRaw, secondRaw)
	}

	tamperedMetrics := first
	*tamperedMetrics.Steps.Value++
	if err := tamperedMetrics.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered metrics error = %v, want ErrInvalid", err)
	}

	tamperedTransitions := append([]Transition(nil), source.Transitions...)
	tamperedTransitions[0].AppendKey = "changed"
	if _, err := NewMetricSource(
		source.Snapshot, tamperedTransitions, source.Oracle,
	); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("tampered transition error = %v, want ErrSourceChanged", err)
	}

	failedOracleSource := metricSourceFixture(
		t, nil, Available(metricOracleFixture(t, false)),
	)
	failedOracleMetrics, err := NewMetricProjector().Project(failedOracleSource)
	if err != nil {
		t.Fatal(err)
	}
	if source.SourceSHA256 == failedOracleSource.SourceSHA256 ||
		first.MetricsSHA256 == failedOracleMetrics.MetricsSHA256 {
		t.Fatal("oracle decision did not change metric source and metrics hashes")
	}
}

func metricSourceFixture(
	t *testing.T,
	mutate func(*SourceSnapshot),
	oracle Slot[OracleDecision],
) MetricSource {
	t.Helper()
	snapshot := projectorFixture(t)
	dslSHA256 := strings.Repeat("d", 64)
	snapshot.Generations[0].DSLSHA256 = dslSHA256
	snapshot.Batches[0].Jobs[0].DSLSHA256 = dslSHA256
	for index := range snapshot.Batches[0].Jobs[0].Executions {
		snapshot.Batches[0].Jobs[0].Executions[index].DSLSHA256 = dslSHA256
	}
	snapshot.FinalRunLinks = metricRunLinks(
		snapshot.AgentRunID, 31, 41, 62, dslSHA256,
	)
	snapshot.Events[1] = agentEventFixture(
		t, 2, "research.llm_call", "", "", `{
			"schema_version":"research.llm_call.v1",
			"logical_call_id":"logical-1",
			"provider":"provider",
			"requested_model":"model",
			"attempt":1,
			"attempt_status":"failed",
			"attempt_started_at":"1970-01-01T00:00:01Z",
			"attempt_latency_ms":7,
			"total_latency_ms":18,
			"retry_count":0,
			"tool_call_status":"unavailable",
			"tool_call_unavailable_reason":"model_attempt_failed_without_response",
			"usage":{"status":"unavailable"},
			"error":{"category":"http","code":"http_429","retryable":true},
			"prompt_spec":{"version":"prompt.v1","prompt_sha256":"aaa","request_sha256":"bbb"}
		}`,
	)
	snapshot.Events[2] = agentEventFixture(
		t, 3, "research.llm_call", "", "", `{
			"schema_version":"research.llm_call.v1",
			"logical_call_id":"logical-1",
			"provider":"provider",
			"requested_model":"model",
			"resolved_model":"model-v1",
			"attempt":2,
			"attempt_status":"succeeded",
			"attempt_started_at":"1970-01-01T00:00:02Z",
			"attempt_latency_ms":11,
			"total_latency_ms":18,
			"retry_count":1,
			"finish_reason":"tool_calls",
			"tool_call_status":"available",
			"tool_call_ids":["tool-b","tool-a"],
			"usage":{"status":"available","input_tokens":10,"output_tokens":4,"total_tokens":14},
			"prompt_spec":{"version":"prompt.v1","prompt_sha256":"aaa","request_sha256":"bbb"}
		}`,
	)
	setMetricExecutionReport(t, metricExecution(&snapshot, 61), json.RawMessage(`{
		"status":"failed",
		"steps":[{
			"step_index":0,
			"action":"click",
			"status":"failed",
			"duration_ms":9,
			"condition_results":[],
			"action_outcome":{"status":"failed","side_effect_state":"not_committed"},
			"locator_trace":{"candidates":[],"failure_reason":"no match"},
			"vlm_preverify_used":false
		}]
	}`))
	setMetricExecutionReport(t, metricExecution(&snapshot, 62), json.RawMessage(`{
		"status":"passed",
		"steps":[{
			"step_index":0,
			"action":"click",
			"status":"passed",
			"duration_ms":5,
			"condition_results":[{
				"phase":"postcondition",
				"index":0,
				"type":"text_visible",
				"status":"passed",
				"duration_ms":1
			}],
			"action_outcome":{"status":"succeeded","side_effect_state":"committed"},
			"locator_trace":{
				"match_strategy":"role",
				"candidates":[{"role":"button"}],
				"selected_candidate":{"role":"button"}
			},
			"vlm_preverify_used":true
		}]
	}`))
	if mutate != nil {
		mutate(&snapshot)
	}
	snapshot.Cursor = buildSourceCursor(snapshot)
	var err error
	snapshot.SourceSHA256, err = sourceSnapshotHash(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	transitions, _, err := NewProjector().Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewMetricSource(snapshot, transitions, oracle)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func metricOracleFixture(t *testing.T, passed bool) OracleDecision {
	t.Helper()
	source := testSourceRef(
		t, SourceOracle, "oracle-artifact-1",
		Unavailable[int64]("oracle_has_no_global_sequence"),
	)
	reason := "task_passed"
	if !passed {
		reason = "task_failed"
	}
	decision, err := NewOracleDecision(OracleDecision{
		ID: "oracle-1", Evaluator: "cart-oracle.v1",
		Passed: passed, ReasonCode: reason,
		DecisionFacts: []OracleDecisionFact{{
			Name: "cart_state", Passed: passed, Sources: []SourceRef{source},
		}},
		Sources: []SourceRef{source},
	})
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func metricExecution(snapshot *SourceSnapshot, id int64) *ExecutionSnapshot {
	for batchIndex := range snapshot.Batches {
		for jobIndex := range snapshot.Batches[batchIndex].Jobs {
			executions := snapshot.Batches[batchIndex].Jobs[jobIndex].Executions
			for executionIndex := range executions {
				if executions[executionIndex].ID == id {
					return &executions[executionIndex]
				}
			}
		}
	}
	panic("metric execution fixture not found")
}

func setMetricExecutionReport(
	t *testing.T,
	execution *ExecutionSnapshot,
	raw json.RawMessage,
) {
	t.Helper()
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	execution.Report = Available(json.RawMessage(canonical))
	ref, err := sourceRef(
		SourceReport, jsonNumber(execution.ID), Available(execution.Attempt),
		Available("execution.report.v2"), json.RawMessage(canonical),
	)
	if err != nil {
		t.Fatal(err)
	}
	execution.ReportRef = Available(ref)
}

func mutateMetricStep(
	t *testing.T,
	snapshot *SourceSnapshot,
	executionID int64,
	mutate func(map[string]any),
) {
	t.Helper()
	execution := metricExecution(snapshot, executionID)
	var report map[string]any
	if err := decodeJSONObject(*execution.Report.Value, &report); err != nil {
		t.Fatal(err)
	}
	steps := report["steps"].([]any)
	mutate(steps[0].(map[string]any))
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	setMetricExecutionReport(t, execution, raw)
}

func mutateMetricReportStatus(
	t *testing.T,
	execution *ExecutionSnapshot,
	status string,
) {
	t.Helper()
	var report map[string]any
	if err := decodeJSONObject(*execution.Report.Value, &report); err != nil {
		t.Fatal(err)
	}
	report["status"] = status
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	setMetricExecutionReport(t, execution, raw)
}

func assertBoolMetric(
	t *testing.T,
	name string,
	metric NullableValue[bool],
	want bool,
) {
	t.Helper()
	if metric.Value == nil || *metric.Value != want ||
		metric.UnavailableReason != nil {
		t.Fatalf("%s = %#v, want %v", name, metric, want)
	}
}

func assertIntMetric(
	t *testing.T,
	name string,
	metric NullableValue[int64],
	want int64,
) {
	t.Helper()
	if metric.Value == nil || *metric.Value != want ||
		metric.UnavailableReason != nil {
		t.Fatalf("%s = %#v, want %v", name, metric, want)
	}
}

func assertUnavailableMetric[T any](
	t *testing.T,
	metric NullableValue[T],
	wantReason string,
) {
	t.Helper()
	if metric.Value != nil || metric.UnavailableReason == nil ||
		*metric.UnavailableReason != wantReason {
		t.Fatalf("metric = %#v, want null reason %q", metric, wantReason)
	}
}

func metricRunLinks(
	agentRunID string,
	generationID int64,
	batchID int64,
	executionID int64,
	dslSHA256 string,
) RunLinks {
	return RunLinks{
		AgentRunID:   metricStringPointer(agentRunID),
		GenerationID: metricInt64Pointer(generationID),
		BatchID:      metricInt64Pointer(batchID),
		ExecutionID:  metricInt64Pointer(executionID),
		DSLSHA256:    metricStringPointer(dslSHA256),
	}
}

func metricStringPointer(value string) *string {
	return &value
}

func metricInt64Pointer(value int64) *int64 {
	return &value
}

func addUnboundFailedBatch(t *testing.T, snapshot *SourceSnapshot) {
	t.Helper()
	oldReport := json.RawMessage(`{
		"status":"failed",
		"steps":[{
			"step_index":0,
			"action":"click",
			"status":"failed",
			"duration_ms":100,
			"condition_results":[{"status":"failed"}],
			"action_outcome":{"status":"failed","side_effect_state":"not_committed"},
			"locator_trace":{"candidates":[],"failure_reason":"not found"},
			"vlm_preverify_used":true
		}]
	}`)
	first := executionFixture(t, 71, 1, "failed", oldReport)
	second := executionFixture(t, 72, 2, "failed", oldReport)
	dslSHA256 := *snapshot.FinalRunLinks.DSLSHA256
	first.DSLSHA256 = dslSHA256
	second.DSLSHA256 = dslSHA256
	oldBatch := BatchSnapshot{
		Ref: testSourceRef(
			t, SourceBatch, "40",
			Unavailable[int64]("batch_has_no_global_sequence"),
		),
		ID: 40, ProjectID: snapshot.ProjectID, GenerationID: 31, Status: "failed",
		Jobs: []JobSnapshot{{
			Ref: testSourceRef(t, SourceJob, "50", Available(int64(0))),
			ID:  50, OrderIndex: 0, Status: "failed",
			AttemptCount: 2, MaxAttempts: 2, DSLSHA256: dslSHA256,
			CanonicalVersion: "dsl.canonical.v1",
			Executions:       []ExecutionSnapshot{first, second},
		}},
	}
	events := append([]AgentEventSnapshot(nil), snapshot.Events[:13]...)
	events = append(
		events,
		agentEventFixture(
			t, 14, "artifact.published", "", "",
			`{"type":"execution_batch","id":"40"}`,
		),
		agentEventFixture(
			t, 15, "artifact.published", "", "",
			`{"type":"execution_batch","id":"41"}`,
		),
		agentEventFixture(t, 16, "run.finished", "", "", `{}`),
	)
	snapshot.Events = events
	snapshot.Batches = append([]BatchSnapshot{oldBatch}, snapshot.Batches...)
}

func TestMetricFixtureUsesExpectedStage4Projection(t *testing.T) {
	source := metricSourceFixture(t, nil, Available(metricOracleFixture(t, true)))
	projected, _, err := NewProjector().Project(source.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(
		transitionPayloadsByKey(t, projected),
		transitionPayloadsByKey(t, source.Transitions),
	) {
		t.Fatal("metric source did not preserve Stage 4 transitions")
	}
}
