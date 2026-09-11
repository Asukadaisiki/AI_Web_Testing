package agentservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
)

func TestRecordModelTelemetryEmitsOneSafeEventPerAttempt(t *testing.T) {
	service := NewService(NewMemoryRepository())
	run, err := service.StartRun(context.Background(), "conversation-1", "private user prompt")
	if err != nil {
		t.Fatal(err)
	}
	rawProviderError := "provider-private-detail Authorization: Bearer sk-private?api_key=secret-" +
		strings.Repeat("x", 300)
	longRequestID := strings.Repeat("r", 300)
	one := int64(1)
	cacheHit := int64(7)
	cacheMiss := int64(4)
	err = service.RecordModelTelemetry(context.Background(), run, agent.TelemetryRecord{
		LogicalCallID: "logical-1",
		StepID:        "step-1",
		ToolCallIDs:   []string{"tool-1"},
		Telemetry: agent.ModelTelemetry{
			Provider:        "provider",
			RequestedModel:  "requested",
			ResolvedModel:   "resolved",
			FinishReason:    "tool_calls",
			ClientRequestID: "e2e_" + strings.Repeat("1", 32),
			EndpointScheme:  "https",
			EndpointHost:    "api.deepseek.com",
			CredentialFingerprint: "sha256:v1:" +
				strings.Repeat("d", 64),
			LocalResponseCache: "not_configured",
			Prompt: agent.PromptSpec{
				Version: agent.SystemPromptVersion, RequestSHA256: strings.Repeat("a", 64),
				PromptSHA256: strings.Repeat("b", 64), ToolsetSHA256: strings.Repeat("c", 64),
				RequestBudget: agent.RequestSerializationBudget{
					RequestBytes: 2048, MessageBytes: 1536, ToolDefinitionBytes: 512,
					ExplorationSummaryBytes: 1024, ExplorationSummaryCount: 2,
				},
			},
			Usage: agent.ModelUsage{
				Status: agent.UsageAvailable, InputTokens: &one,
				OutputTokens: &one, TotalTokens: &one,
				PromptCacheHitTokens:  &cacheHit,
				PromptCacheMissTokens: &cacheMiss,
			},
			Reasoning: &agent.ReasoningAudit{
				ThinkingMode:     "enabled",
				ReasoningEffort:  "max",
				ContentAvailable: true,
				ContentBytes:     32,
				ContentSHA256:    strings.Repeat("e", 64),
			},
			Attempts: []agent.ModelAttempt{
				{
					Attempt: 1, Status: "failed",
					ProviderHeaderRequestID:       "failed-header-id",
					ProviderHeaderRequestIDHeader: "x-request-id",
					ProviderRequestID:             "failed-header-id",
					Error: &agent.ModelError{
						Category: "http", Code: "http_429",
						Message: rawProviderError, Retryable: true,
					},
				},
				{
					Attempt: 2, Status: "succeeded",
					ProviderResponseID:            longRequestID,
					ProviderHeaderRequestID:       "successful-header-id",
					ProviderHeaderRequestIDHeader: "x-request-id",
					ProviderRequestID:             "legacy-wrong-value",
				},
			},
			TotalLatencyMS: 9,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := service.ListEvents(context.Background(), run.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	pipelineEvents := eventsByType(events, EventPipelineTrace)
	events = eventsByType(events, EventResearchLLMCall)
	if len(events) != 2 {
		t.Fatalf("events = %#v", events)
	}
	if len(pipelineEvents) != 2 {
		t.Fatalf("pipeline events = %#v", pipelineEvents)
	}
	firstCumulative := pipelineEvents[0].Payload["model_request"].(map[string]any)["cumulative"].(map[string]any)
	secondCumulative := pipelineEvents[1].Payload["model_request"].(map[string]any)["cumulative"].(map[string]any)
	if firstCumulative["logical_calls"] != float64(1) ||
		firstCumulative["physical_attempts"] != float64(1) ||
		firstCumulative["usage_unavailable_attempts"] != float64(1) ||
		secondCumulative["logical_calls"] != float64(1) ||
		secondCumulative["physical_attempts"] != float64(2) ||
		secondCumulative["total_tokens"] != float64(1) {
		t.Fatalf("pipeline cumulative usage = %#v / %#v", firstCumulative, secondCumulative)
	}
	allowed := map[string]bool{
		"schema_version": true, "logical_call_id": true, "provider": true,
		"requested_model": true, "resolved_model": true, "prompt_spec": true,
		"usage": true, "finish_reason": true, "attempt": true,
		"attempt_status": true, "attempt_started_at": true,
		"attempt_latency_ms": true, "total_latency_ms": true,
		"http_status": true, "client_request_id": true,
		"endpoint_scheme": true, "endpoint_host": true,
		"credential_fingerprint": true, "provider_response_id": true,
		"provider_header_request_id":        true,
		"provider_header_request_id_header": true,
		"provider_request_id":               true, "local_response_cache": true,
		"retry_count":      true,
		"tool_call_status": true, "tool_call_unavailable_reason": true,
		"tool_call_ids": true, "error": true, "reasoning": true,
	}
	for index, event := range events {
		if event.Type != EventResearchLLMCall || event.StepID != "step-1" {
			t.Fatalf("event = %#v", event)
		}
		if event.Payload["schema_version"] != ResearchLLMCallSchemaV1 ||
			event.Payload["attempt"].(float64) != float64(index+1) {
			t.Fatalf("payload = %#v", event.Payload)
		}
		encoded := fmt.Sprintf("%v", event.Payload)
		if strings.Contains(encoded, "provider-private-detail") {
			t.Fatalf("payload leaked raw provider error: %s", encoded)
		}
		for key := range event.Payload {
			if !allowed[key] {
				t.Fatalf("payload contains non-whitelisted field %q", key)
			}
		}
		for _, forbidden := range []string{
			"provider-private-detail", "Authorization", "Bearer", "sk-private",
			"api_key", "messages", "headers", "cookie", "raw_response",
			"reasoning_content", "private chain of thought",
		} {
			if strings.Contains(encoded, forbidden) {
				t.Fatalf("payload contains forbidden field %q: %s", forbidden, encoded)
			}
		}
	}
	if _, exists := events[0].Payload["error"].(map[string]any)["message"]; exists {
		t.Fatalf("payload retained raw error message: %#v", events[0].Payload["error"])
	}
	if events[0].Payload["usage"].(map[string]any)["status"] != string(agent.UsageUnavailable) {
		t.Fatalf("failed usage = %#v", events[0].Payload["usage"])
	}
	requestBudget := events[1].Payload["prompt_spec"].(map[string]any)["request_budget"].(map[string]any)
	if requestBudget["request_bytes"] != float64(2048) ||
		requestBudget["exploration_summary_count"] != float64(2) {
		t.Fatalf("request budget = %#v", requestBudget)
	}
	if events[0].ToolCallID != "" ||
		events[0].Payload["tool_call_status"] != string(ToolCallUnavailable) ||
		events[0].Payload["tool_call_unavailable_reason"] != string(ToolCallUnavailableAttemptFailedNoResponse) {
		t.Fatalf("failed attempt association = %#v", events[0])
	}
	if _, exists := events[0].Payload["tool_call_ids"]; exists {
		t.Fatalf("failed attempt retained tool call ids: %#v", events[0].Payload)
	}
	if events[1].ToolCallID != "tool-1" ||
		events[1].Payload["tool_call_status"] != string(ToolCallAvailable) {
		t.Fatalf("successful attempt association = %#v", events[1])
	}
	singleToolCallIDs := events[1].Payload["tool_call_ids"].([]any)
	if len(singleToolCallIDs) != 1 || singleToolCallIDs[0] != "tool-1" {
		t.Fatalf("single tool_call_ids = %#v", singleToolCallIDs)
	}
	if _, exists := events[1].Payload["tool_call_unavailable_reason"]; exists {
		t.Fatalf("available attempt has unavailable reason: %#v", events[1].Payload)
	}
	if len(events[1].Payload["provider_request_id"].(string)) != 128 {
		t.Fatalf("provider request id was not bounded")
	}
	if events[1].Payload["provider_response_id"] != events[1].Payload["provider_request_id"] ||
		events[1].Payload["provider_header_request_id"] != "successful-header-id" ||
		events[1].Payload["provider_header_request_id_header"] != "x-request-id" ||
		events[1].Payload["client_request_id"] != "e2e_"+strings.Repeat("1", 32) ||
		events[1].Payload["endpoint_scheme"] != "https" ||
		events[1].Payload["endpoint_host"] != "api.deepseek.com" ||
		events[1].Payload["local_response_cache"] != "not_configured" {
		t.Fatalf("provider evidence = %#v", events[1].Payload)
	}
	usage := events[1].Payload["usage"].(map[string]any)
	if usage["prompt_cache_hit_tokens"] != float64(cacheHit) ||
		usage["prompt_cache_miss_tokens"] != float64(cacheMiss) {
		t.Fatalf("cache usage = %#v", usage)
	}
	reasoning := events[1].Payload["reasoning"].(map[string]any)
	if reasoning["thinking_mode"] != "enabled" ||
		reasoning["reasoning_effort"] != "max" ||
		reasoning["content_available"] != true ||
		reasoning["content_bytes"] != float64(32) ||
		reasoning["content_sha256"] != strings.Repeat("e", 64) {
		t.Fatalf("reasoning audit = %#v", reasoning)
	}
}

func TestRecordModelTelemetryPersistsMultipleAndUnavailableToolCalls(t *testing.T) {
	service := NewService(NewMemoryRepository())
	run, err := service.StartRun(context.Background(), "conversation-1", "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		logicalCallID string
		toolCallIDs   []string
	}{
		{logicalCallID: "multiple", toolCallIDs: []string{"tool-1", "tool-2"}},
		{logicalCallID: "none"},
	} {
		err = service.RecordModelTelemetry(context.Background(), run, agent.TelemetryRecord{
			LogicalCallID: testCase.logicalCallID,
			StepID:        "step-" + testCase.logicalCallID,
			ToolCallIDs:   testCase.toolCallIDs,
			Telemetry: agent.ModelTelemetry{
				Provider:       "provider",
				RequestedModel: "model",
				FinishReason:   "stop",
				Usage:          agent.ModelUsage{Status: agent.UsageUnavailable},
				Attempts:       []agent.ModelAttempt{{Attempt: 1, Status: "succeeded"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	events, err := service.ListEvents(context.Background(), run.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	events = eventsByType(events, EventResearchLLMCall)
	if len(events) != 2 {
		t.Fatalf("events = %#v", events)
	}
	multiple := events[0]
	if multiple.ToolCallID != "" ||
		multiple.Payload["tool_call_status"] != string(ToolCallAvailable) {
		t.Fatalf("multiple association = %#v", multiple)
	}
	toolCallIDs := multiple.Payload["tool_call_ids"].([]any)
	if len(toolCallIDs) != 2 || toolCallIDs[0] != "tool-1" || toolCallIDs[1] != "tool-2" {
		t.Fatalf("multiple tool_call_ids = %#v", toolCallIDs)
	}
	none := events[1]
	if none.ToolCallID != "" ||
		none.Payload["tool_call_status"] != string(ToolCallUnavailable) ||
		none.Payload["tool_call_unavailable_reason"] != string(ToolCallUnavailableModelReturnedFinalText) {
		t.Fatalf("no-tool association = %#v", none)
	}
	if _, exists := none.Payload["tool_call_ids"]; exists {
		t.Fatalf("no-tool event retained tool call ids: %#v", none.Payload)
	}
}

func eventsByType(events []Event, eventType EventType) []Event {
	result := make([]Event, 0, len(events))
	for _, event := range events {
		if event.Type == eventType {
			result = append(result, event)
		}
	}
	return result
}

func TestAskUserQuestionPauseAndResume(t *testing.T) {
	repository := NewMemoryRepository()
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	nextID := 0
	service := NewServiceWithDependencies(
		repository,
		NewEventBroker(),
		func() time.Time { return now },
		func(prefix string) string {
			nextID++
			return fmt.Sprintf("%s-%d", prefix, nextID)
		},
	)

	run, err := service.StartRun(context.Background(), "conversation-1", "测试登录")
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	run, pending, err := service.RequestUserInput(
		context.Background(),
		run.ID,
		AskUserRequest{
			Questions: []Question{{
				ID:       "login_mode",
				Prompt:   "选择登录方式",
				Type:     QuestionSingleSelect,
				Required: true,
				Options: []QuestionOption{
					{Value: "account", Label: "账号密码"},
					{Value: "cookie", Label: "登录态"},
				},
			}},
		},
	)
	if err != nil {
		t.Fatalf("RequestUserInput() error = %v", err)
	}
	if run.Status != RunStatusWaitingUser {
		t.Fatalf("status = %q, want %q", run.Status, RunStatusWaitingUser)
	}
	if run.PendingToolCallID == nil || *run.PendingToolCallID != pending.ToolCallID {
		t.Fatal("pending tool call was not persisted")
	}
	if pending.Type != EventToolPending || pending.CheckpointID == "" {
		t.Fatalf("pending event = %#v", pending)
	}

	run, err = service.ResumeToolCall(
		context.Background(),
		run.ID,
		pending.ToolCallID,
		ResumeToolCallRequest{
			Answers:  map[string]any{"login_mode": "account"},
			NextStep: "continue",
		},
	)
	if err != nil {
		t.Fatalf("ResumeToolCall() error = %v", err)
	}
	if run.Status != RunStatusRunning || run.PendingToolCallID != nil {
		t.Fatalf("resumed run = %#v", run)
	}

	events, err := service.ListEvents(context.Background(), run.ID, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	wantTypes := []EventType{
		EventRunStarted,
		EventToolStarted,
		EventToolArgsDelta,
		EventToolPending,
		EventToolResult,
		EventToolFinished,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("len(events) = %d, want %d", len(events), len(wantTypes))
	}
	for index, event := range events {
		if event.Seq != int64(index+1) {
			t.Fatalf("events[%d].Seq = %d, want %d", index, event.Seq, index+1)
		}
		if event.Type != wantTypes[index] {
			t.Fatalf("events[%d].Type = %q, want %q", index, event.Type, wantTypes[index])
		}
	}
}

func TestResumeToolCallRejectsWrongCall(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	run, err := service.StartRun(context.Background(), "conversation-1", "测试登录")
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	_, _, err = service.RequestUserInput(
		context.Background(),
		run.ID,
		AskUserRequest{Questions: []Question{{
			ID:       "confirm",
			Prompt:   "继续吗？",
			Type:     QuestionConfirm,
			Required: true,
		}}},
	)
	if err != nil {
		t.Fatalf("RequestUserInput() error = %v", err)
	}

	_, err = service.ResumeToolCall(
		context.Background(),
		run.ID,
		"wrong-tool-call",
		ResumeToolCallRequest{Answers: map[string]any{"confirm": true}},
	)
	if !errors.Is(err, ErrToolCallMismatch) {
		t.Fatalf("ResumeToolCall() error = %v, want ErrToolCallMismatch", err)
	}
}

func TestQuestionValidation(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	run, err := service.StartRun(context.Background(), "conversation-1", "测试登录")
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	_, _, err = service.RequestUserInput(context.Background(), run.ID, AskUserRequest{})
	if err == nil {
		t.Fatal("RequestUserInput() error = nil, want validation error")
	}
}

func TestCancelRunIsIdempotentAndProtectsCancelledState(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	stale, err := service.StartRun(context.Background(), "conversation-1", "测试登录")
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	cancelled, err := service.CancelRun(context.Background(), stale.ID, "driver timeout")
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if cancelled.Status != RunStatusCancelled {
		t.Fatalf("status = %q, want %q", cancelled.Status, RunStatusCancelled)
	}

	replayed, err := service.CancelRun(context.Background(), stale.ID, "duplicate request")
	if err != nil {
		t.Fatalf("second CancelRun() error = %v", err)
	}
	if replayed.Status != RunStatusCancelled {
		t.Fatalf("replayed status = %q, want cancelled", replayed.Status)
	}
	if err := service.SaveRun(context.Background(), stale); !errors.Is(err, ErrRunCancelled) {
		t.Fatalf("stale SaveRun() error = %v, want ErrRunCancelled", err)
	}
	completed, err := service.CompleteRun(context.Background(), stale)
	if err != nil {
		t.Fatalf("CompleteRun() after cancel error = %v", err)
	}
	if completed.Status != RunStatusCancelled {
		t.Fatalf("completed status = %q, want cancelled", completed.Status)
	}

	events, err := service.ListEvents(context.Background(), stale.ID, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 2 || events[1].Type != EventRunCancelled {
		t.Fatalf("events = %#v", events)
	}
	if events[1].Payload["reason"] != "driver timeout" {
		t.Fatalf("cancel reason = %#v", events[1].Payload["reason"])
	}
}

func TestCancelWaitingRunClearsPendingAndCompletedRunWins(t *testing.T) {
	service := NewService(NewMemoryRepository())
	waiting, err := service.StartRun(context.Background(), "conversation-1", "测试登录")
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	waiting, _, err = service.RequestUserInput(
		context.Background(),
		waiting.ID,
		AskUserRequest{Questions: []Question{{
			ID: "confirm", Prompt: "继续吗？", Type: QuestionConfirm, Required: true,
		}}},
	)
	if err != nil {
		t.Fatalf("RequestUserInput() error = %v", err)
	}
	cancelled, err := service.CancelRun(context.Background(), waiting.ID, "stale checkpoint")
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if cancelled.PendingToolCallID != nil || cancelled.PendingStepID != nil {
		t.Fatalf("cancelled pending fields = %#v", cancelled)
	}

	running, err := service.StartRun(context.Background(), "conversation-2", "完成")
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	completed, err := service.CompleteRun(context.Background(), running)
	if err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
	afterCancel, err := service.CancelRun(context.Background(), running.ID, "too late")
	if err != nil {
		t.Fatalf("CancelRun() completed error = %v", err)
	}
	if afterCancel.Status != RunStatusCompleted || completed.Status != RunStatusCompleted {
		t.Fatalf("completed run changed: %#v", afterCancel)
	}
}
