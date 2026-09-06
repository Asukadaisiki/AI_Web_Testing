package research

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"testing"
)

func TestProjectorProjectsAgentEventsExecutionRetriesAndTerminal(t *testing.T) {
	snapshot := projectorFixture(t)

	transitions, manifest, err := NewProjector().Project(snapshot)
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	if manifest.SourceCursor.AgentEventSeq != 15 ||
		!reflect.DeepEqual(manifest.SourceCursor.ApprovedGenerationIDs, []int64{31}) ||
		!reflect.DeepEqual(manifest.SourceCursor.BatchIDs, []int64{41}) ||
		!reflect.DeepEqual(manifest.SourceCursor.ExecutionIDs, []int64{61, 62}) ||
		manifest.SourceSHA256 != snapshot.SourceSHA256 ||
		manifest.TransitionCount != int64(len(transitions)) {
		t.Fatalf("manifest = %#v", manifest)
	}
	if len(transitions) != 7 {
		t.Fatalf("transition count = %d, want 7", len(transitions))
	}
	for index := range transitions {
		if transitions[index].Ordinal != int64(index) {
			t.Fatalf("transition[%d].Ordinal = %d", index, transitions[index].Ordinal)
		}
		if err := transitions[index].NormalizeAndValidate(); err != nil {
			t.Fatalf("transition[%d] invalid: %v", index, err)
		}
	}

	byKey := transitionPayloadsByKey(t, transitions)
	toolA := byKey["tool:tool-a"]
	toolB := byKey["tool:tool-b"]
	if toolA.Decision.Value == nil || toolB.Decision.Value == nil {
		t.Fatal("shared model decision was not projected onto both tool calls")
	}
	if !reflect.DeepEqual(toolA.Decision.Value.ToolCallIDs, []string{"tool-a", "tool-b"}) ||
		!reflect.DeepEqual(toolB.Decision.Value.ToolCallIDs, []string{"tool-a", "tool-b"}) {
		t.Fatalf(
			"decision tool_call_ids = %#v / %#v",
			toolA.Decision.Value.ToolCallIDs,
			toolB.Decision.Value.ToolCallIDs,
		)
	}
	var decisionData struct {
		Attempts []struct {
			Attempt int64  `json:"attempt"`
			Status  string `json:"status"`
		} `json:"attempts"`
	}
	if err := json.Unmarshal(toolA.Decision.Value.Data, &decisionData); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decisionData.Attempts, []struct {
		Attempt int64  `json:"attempt"`
		Status  string `json:"status"`
	}{{1, "failed"}, {2, "succeeded"}}) {
		t.Fatalf("decision attempts = %#v", decisionData.Attempts)
	}
	if toolA.Cost.Status != SlotAvailable ||
		toolB.Cost.Status != SlotNotApplicable ||
		toolB.Cost.Reason != "shared_model_cost_owned_by_tool_call:tool-a" {
		t.Fatalf("shared model costs = %#v / %#v", toolA.Cost, toolB.Cost)
	}
	if toolA.Action.Value == nil ||
		toolA.Action.Value.CausationID.Value == nil ||
		*toolA.Action.Value.CausationID.Value != toolA.Decision.Value.ID ||
		toolA.Execution.Value == nil ||
		toolA.Execution.Value.CausationID.Value == nil ||
		*toolA.Execution.Value.CausationID.Value != toolA.Action.Value.ID {
		t.Fatalf("tool-a causation chain = %#v", toolA)
	}

	var pendingData struct {
		Pending      bool   `json:"pending"`
		Resumed      bool   `json:"resumed"`
		CheckpointID string `json:"checkpoint_id"`
	}
	if toolB.Action.Value == nil ||
		json.Unmarshal(toolB.Action.Value.Data, &pendingData) != nil ||
		!pendingData.Pending || !pendingData.Resumed ||
		pendingData.CheckpointID != "checkpoint-1" {
		t.Fatalf("pending/resume action = %#v, data = %#v", toolB.Action, pendingData)
	}

	unknown := byKey["unknown:13"]
	if unknown.Unknown.Value == nil ||
		unknown.Unknown.Value.Kind != EventKindUnknown ||
		!reflect.DeepEqual(unknown.Unknown.Value.ToolCallIDs, []string{"tool-future"}) {
		t.Fatalf("unknown transition = %#v", unknown)
	}

	failedStep := byKey["execution:61:step:0"]
	if failedStep.Failure.Value == nil ||
		failedStep.Execution.Value == nil ||
		failedStep.Execution.Value.Attempt.Value == nil ||
		*failedStep.Execution.Value.Attempt.Value != 1 {
		t.Fatalf("failed execution step = %#v", failedStep)
	}
	retriedStep := byKey["execution:62:step:0"]
	if retriedStep.Recovery.Value == nil ||
		retriedStep.Recovery.Value.Attempt.Value == nil ||
		*retriedStep.Recovery.Value.Attempt.Value != 2 {
		t.Fatalf("retried execution step = %#v", retriedStep)
	}

	terminal := byKey["terminal:agent-run-1"]
	if !terminal.Done || terminal.Verification.Value == nil ||
		terminal.Reward.Status != SlotUnavailable ||
		terminal.Reward.Reason != "independent_oracle_not_persisted" {
		t.Fatalf("terminal transition = %#v", terminal)
	}
	for key, payload := range byKey {
		if payload.Reward.Status != SlotUnavailable ||
			payload.Reward.Reason != "independent_oracle_not_persisted" {
			t.Fatalf("%s inferred an oracle reward: %#v", key, payload.Reward)
		}
	}
}

func TestProjectorIsByteDeterministic(t *testing.T) {
	snapshot := projectorFixture(t)
	first, firstManifest, err := NewProjector().Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	second, secondManifest, err := NewProjector().Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, err := json.Marshal(struct {
		Manifest    ProjectionManifest `json:"manifest"`
		Transitions []Transition       `json:"transitions"`
	}{firstManifest, first})
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := json.Marshal(struct {
		Manifest    ProjectionManifest `json:"manifest"`
		Transitions []Transition       `json:"transitions"`
	}{secondManifest, second})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("repeated Project() differs:\nfirst:  %s\nsecond: %s", firstBytes, secondBytes)
	}
}

func TestProjectorRejectsCursorAndEventReferenceDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SourceSnapshot)
	}{
		{
			name: "cursor",
			mutate: func(snapshot *SourceSnapshot) {
				snapshot.Cursor.AgentEventSeq--
			},
		},
		{
			name: "sequence",
			mutate: func(snapshot *SourceSnapshot) {
				snapshot.Events[3].Seq = 99
			},
		},
		{
			name: "reference hash",
			mutate: func(snapshot *SourceSnapshot) {
				snapshot.Events[0].Payload = json.RawMessage(`{"input":"changed"}`)
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			snapshot := projectorFixture(t)
			testCase.mutate(&snapshot)
			var err error
			snapshot.SourceSHA256, err = sourceSnapshotHash(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := NewProjector().Project(snapshot); !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("Project() error = %v, want ErrSourceChanged", err)
			}
		})
	}
}

func projectorFixture(t *testing.T) SourceSnapshot {
	t.Helper()
	events := []AgentEventSnapshot{
		agentEventFixture(t, 1, "run.started", "", "", `{"input":"test"}`),
		agentEventFixture(t, 2, "research.llm_call", "", "", `{
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
		}`),
		agentEventFixture(t, 3, "research.llm_call", "", "", `{
			"schema_version":"research.llm_call.v1",
			"logical_call_id":"logical-1",
			"provider":"provider",
			"requested_model":"model",
			"resolved_model":"model-v1",
			"attempt":2,
			"attempt_status":"succeeded",
			"attempt_latency_ms":11,
			"tool_call_status":"available",
			"tool_call_ids":["tool-b","tool-a"],
			"usage":{"status":"available","input_tokens":10,"output_tokens":4,"total_tokens":14},
			"prompt_spec":{"version":"prompt.v1","prompt_sha256":"aaa","request_sha256":"bbb"}
		}`),
		agentEventFixture(t, 4, "tool.started", "tool-a", "", `{"tool":"explore_page"}`),
		agentEventFixture(t, 5, "tool.args.delta", "tool-a", "", `{"tool":"explore_page","arguments":"{\"url\":\"https://example.com\"}"}`),
		agentEventFixture(t, 6, "tool.result", "tool-a", "", toolResultPayload(t, "explore_page", `{"status":"ok","url":"https://example.com"}`)),
		agentEventFixture(t, 7, "tool.finished", "tool-a", "", `{"tool":"explore_page"}`),
		agentEventFixture(t, 8, "tool.started", "tool-b", "", `{"tool":"ask_user_question"}`),
		agentEventFixture(t, 9, "tool.args.delta", "tool-b", "", `{"tool":"ask_user_question","arguments":{"questions":[{"id":"approve_dsl"}]}}`),
		agentEventFixture(t, 10, "tool.pending", "tool-b", "checkpoint-1", `{"tool":"ask_user_question","questions":[{"id":"approve_dsl"}]}`),
		agentEventFixture(t, 11, "tool.result", "tool-b", "", `{"tool":"ask_user_question","answers":{"approve_dsl":true}}`),
		agentEventFixture(t, 12, "tool.finished", "tool-b", "", `{"tool":"ask_user_question"}`),
		agentEventFixture(t, 13, "future.event", "tool-future", "", `{"future":true}`),
		agentEventFixture(t, 14, "artifact.published", "tool-b", "", `{"type":"execution_batch","id":"41"}`),
		agentEventFixture(t, 15, "run.finished", "", "", `{}`),
	}

	generationRef := testSourceRef(
		t, SourceGeneration, "31",
		Unavailable[int64]("generation_has_no_global_sequence"),
	)
	batchRef := testSourceRef(
		t, SourceBatch, "41",
		Unavailable[int64]("batch_has_no_global_sequence"),
	)
	jobRef := testSourceRef(t, SourceJob, "51", Available(int64(0)))
	firstReport := json.RawMessage(`{
		"status":"failed",
		"steps":[
			{"step_index":0,"action":"click","status":"failed","duration_ms":9,"error_message":"not found","locator_trace":{"candidates":[]}},
			{"step_index":1,"action":"assert_text","status":"passed","duration_ms":3,"condition_results":[]}
		]
	}`)
	secondReport := json.RawMessage(`{
		"status":"passed",
		"steps":[
			{"step_index":0,"action":"click","status":"passed","duration_ms":5,"vlm_preverify_used":true,"locator_trace":{"candidates":[{"role":"button"}]}}
		]
	}`)
	firstExecution := executionFixture(
		t, 61, 1, "failed", firstReport,
	)
	firstExecution.FailureSignal = Available(json.RawMessage(`{
		"schema_version":"failure.signal.v1",
		"category":"locator",
		"stage":"action",
		"code":"not_found",
		"retryable":true,
		"step_index":0
	}`))
	secondExecution := executionFixture(
		t, 62, 2, "passed", secondReport,
	)

	snapshot := SourceSnapshot{
		SchemaVersion:  EventSchemaVersion,
		ResearchRunID:  "research-run-1",
		ProjectID:      7,
		AgentRunID:     "agent-run-1",
		AgentRunStatus: "completed",
		Events:         events,
		Generations: []GenerationSnapshot{{
			Ref: generationRef, ID: 31, ProjectID: 7, ApprovedBySeq: 11,
			ApprovalToolCall: "tool-b", DSLCanonical: json.RawMessage(`{"steps":[]}`),
			DSLSHA256: "dsl-hash", CanonicalVersion: "dsl.canonical.v1",
			RetryFromID:     NotApplicable[int64]("generation_is_not_a_retry"),
			RetryReasonCode: NotApplicable[string]("generation_has_no_retry_reason"),
		}},
		Batches: []BatchSnapshot{{
			Ref: batchRef, ID: 41, ProjectID: 7, GenerationID: 31, Status: "passed",
			Jobs: []JobSnapshot{{
				Ref: jobRef, ID: 51, OrderIndex: 0, Status: "passed",
				AttemptCount: 2, MaxAttempts: 2, DSLSHA256: "dsl-hash",
				CanonicalVersion: "dsl.canonical.v1",
				Executions:       []ExecutionSnapshot{firstExecution, secondExecution},
			}},
		}},
		Reward: Unavailable[json.RawMessage]("independent_oracle_not_persisted"),
		Cursor: SourceCursor{
			SchemaVersion: EventSchemaVersion, AgentRunID: "agent-run-1",
			AgentEventSeq: 15, ApprovedGenerationIDs: []int64{31},
			BatchIDs: []int64{41}, ExecutionIDs: []int64{61, 62},
		},
	}
	var err error
	snapshot.SourceSHA256, err = sourceSnapshotHash(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func agentEventFixture(
	t *testing.T,
	seq int64,
	eventType string,
	toolCallID string,
	checkpointID string,
	payload string,
) AgentEventSnapshot {
	t.Helper()
	event := AgentEventSnapshot{
		Seq: seq, Type: eventType, ConversationID: "conversation-1",
		StepID:       Available("step-1"),
		ToolCallID:   Unavailable[string]("agent_event_has_no_tool_call_id"),
		ParentID:     Unavailable[string]("agent_event_has_no_parent_id"),
		CheckpointID: Unavailable[string]("agent_event_has_no_checkpoint_id"),
	}
	if toolCallID != "" {
		event.ToolCallID = Available(toolCallID)
	}
	if checkpointID != "" {
		event.CheckpointID = Available(checkpointID)
	}
	canonical, err := CanonicalJSON([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	event.Payload = canonical
	event.Ref, err = sourceRef(
		SourceAgentEvent,
		"agent-run-1:"+jsonNumber(seq),
		Available(seq),
		eventSchemaSlot(event),
		struct {
			Seq            int64           `json:"seq"`
			Type           string          `json:"type"`
			ConversationID string          `json:"conversation_id"`
			StepID         Slot[string]    `json:"step_id"`
			ToolCallID     Slot[string]    `json:"tool_call_id"`
			ParentID       Slot[string]    `json:"parent_id"`
			CheckpointID   Slot[string]    `json:"checkpoint_id"`
			Payload        json.RawMessage `json:"payload"`
		}{
			event.Seq, event.Type, event.ConversationID, event.StepID,
			event.ToolCallID, event.ParentID, event.CheckpointID, event.Payload,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func executionFixture(
	t *testing.T,
	id int64,
	attempt int64,
	status string,
	report json.RawMessage,
) ExecutionSnapshot {
	t.Helper()
	reportRef := testSourceRef(t, SourceReport, jsonNumber(id), Available(attempt))
	executionRef := testSourceRef(t, SourceExecution, jsonNumber(id), Available(attempt))
	return ExecutionSnapshot{
		Ref: executionRef, ReportRef: Available(reportRef), ID: id, Attempt: attempt,
		Status: status, DSLSHA256: "dsl-hash",
		ReportSchemaVersion: "execution.report.v2", Report: Available(report),
		FailureSignal: Unavailable[json.RawMessage]("execution_has_no_failure_signal"),
	}
}

func transitionPayloadsByKey(
	t *testing.T,
	transitions []Transition,
) map[string]TransitionPayloadV1 {
	t.Helper()
	result := make(map[string]TransitionPayloadV1, len(transitions))
	for _, transition := range transitions {
		var payload TransitionPayloadV1
		if err := json.Unmarshal(transition.PayloadJSON, &payload); err != nil {
			t.Fatal(err)
		}
		result[transition.AppendKey] = payload
	}
	return result
}

func toolResultPayload(t *testing.T, tool, content string) string {
	t.Helper()
	canonical, err := CanonicalJSON([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	raw, err := json.Marshal(map[string]any{
		"schema_version": "agent.tool_result.v1",
		"tool":           tool,
		"content":        json.RawMessage(canonical),
		"content_sha256": hex.EncodeToString(sum[:]),
		"content_bytes":  len(canonical),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func jsonNumber(value int64) string {
	return strconv.FormatInt(value, 10)
}
