package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/planner"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/store"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
)

func TestPlanningCallsContainOneCurrentStateInsteadOfHistory(t *testing.T) {
	const goal = "搜索 widget 并提交"
	h := newHarness(t, []ScriptedStep{
		{Tool: "open_page", Arguments: json.RawMessage(
			`{"url":"` + listURL + `","intent":"打开商品列表"}`)},
		{Tool: "input", Arguments: json.RawMessage(
			`{"hint":"Search","value":"widget","intent":"填写搜索词","expect_value":"widget"}`)},
		{Tool: "click", Arguments: json.RawMessage(
			`{"candidate_id":"act_search_submit","intent":"提交搜索","expect_url":"search=widget"}`)},
		{Tool: "finish_case", Arguments: json.RawMessage(`{"name":"搜索商品"}`)},
	})

	run := h.plan(t, goal)
	if run.Status != store.StatusAwaitingApproval {
		t.Fatalf("status = %q, want %q", run.Status, store.StatusAwaitingApproval)
	}

	calls := h.runtime.llm.(*ScriptedLLM).CallsSeen()
	if len(calls) != 4 {
		t.Fatalf("captured calls = %d, want 4", len(calls))
	}
	for callIndex, messages := range calls {
		if len(messages) != 4 {
			t.Fatalf("call %d messages = %d, want 4: %#v", callIndex, len(messages), messages)
		}
		wantRoles := []Role{RoleSystem, RoleUser, RoleUser, RoleUser}
		for index, message := range messages {
			if message.Role != wantRoles[index] {
				t.Fatalf("call %d message %d role = %q, want %q", callIndex, index, message.Role, wantRoles[index])
			}
			if message.Role == RoleAssistant || message.Role == RoleTool {
				t.Fatalf("call %d replays protocol history at message %d: %#v", callIndex, index, message)
			}
		}
		if messages[0].Content != SystemPrompt() {
			t.Fatalf("call %d system prompt changed", callIndex)
		}
		if messages[1].Content != GoalMessage(goal) {
			t.Fatalf("call %d goal = %q, want original goal", callIndex, messages[1].Content)
		}

		var committed struct {
			Steps []planner.StepView `json:"committed_steps"`
		}
		decodeContextMessage(t, messages[2].Content, "Committed planner steps:\n", &committed)
		if len(committed.Steps) != callIndex {
			t.Fatalf("call %d committed steps = %d, want %d", callIndex, len(committed.Steps), callIndex)
		}

		var current map[string]any
		decodeContextMessage(t, messages[3].Content, "Current planner state:\n", &current)
		if _, duplicated := current["committed_steps"]; duplicated {
			t.Fatalf("call %d duplicates committed steps in current state: %s", callIndex, messages[3].Content)
		}
		if callIndex == 0 {
			if _, exists := current["current_page"]; exists {
				t.Fatalf("initial call unexpectedly has a page: %s", messages[3].Content)
			}
			if _, exists := current["last_result"]; exists {
				t.Fatalf("initial call unexpectedly has a last result: %s", messages[3].Content)
			}
			continue
		}
		if _, exists := current["current_page"]; !exists {
			t.Fatalf("call %d has no current page: %s", callIndex, messages[3].Content)
		}
		lastResult, exists := current["last_result"].(map[string]any)
		if !exists {
			t.Fatalf("call %d has no compact last result: %s", callIndex, messages[3].Content)
		}
		if _, duplicated := lastResult["page"]; duplicated {
			t.Fatalf("call %d embeds an old observation in last_result: %s", callIndex, messages[3].Content)
		}
		if _, duplicated := lastResult["steps"]; duplicated {
			t.Fatalf("call %d embeds old steps in last_result: %s", callIndex, messages[3].Content)
		}
	}
}

func TestCommittedPrefixSerializationIsDeterministic(t *testing.T) {
	state := PlanningContext{
		Goal: "buy one widget",
		State: planner.StateSnapshot{
			Version: 1,
			Steps: []planner.StepView{{
				Index:          0,
				Action:         "goto",
				Intent:         "open catalog",
				Preconditions:  []string{},
				Postconditions: []string{"url_contains:/products"},
			}},
			Page: &planner.PageView{
				URL:   "https://shop.test/products",
				Title: "Products",
				ActionCandidates: []planner.ActionCandidateView{{
					CandidateID: "candidate-1",
					Kind:        "element_candidate",
					Action:      "click",
					TargetRef:   "product-1",
					Attributes:  map[string]string{"name": "widget", "data-testid": "product"},
				}},
				Elements: []planner.ElementView{},
			},
			LastResult: &planner.CompactResult{OK: true, Summary: "opened catalog"},
		},
		Usage: usage.Call(100, 10, 2, 80),
		Remaining: BudgetView{
			ModelCalls:          23,
			TotalTokens:         1499890,
			FreshTotalTokens:    299970,
			PromptTokensPerCall: 30000,
			RequestBytesPerCall: 98304,
		},
	}

	first, err := json.Marshal(BuildPlanningMessages(state))
	if err != nil {
		t.Fatalf("marshal first messages: %v", err)
	}
	second, err := json.Marshal(BuildPlanningMessages(state))
	if err != nil {
		t.Fatalf("marshal second messages: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("same state produced different messages:\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestOpenAIRequestSizeMatchesPostedEnvelope(t *testing.T) {
	posted := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		posted <- body
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}
		}`))
	}))
	defer server.Close()

	llm := NewOpenAILLM(OpenAIConfig{
		BaseURL: server.URL, Model: "test-model", Temperature: 0.2, MaxTokens: 321,
	}, planner.Tools())
	messages := BuildPlanningMessages(PlanningContext{
		Goal:  "open the catalog",
		State: planner.StateSnapshot{Version: 1, Steps: []planner.StepView{}},
	})

	size, err := llm.RequestSize(messages)
	if err != nil {
		t.Fatalf("request size: %v", err)
	}
	if _, _, err := llm.Next(context.Background(), messages); err != nil {
		t.Fatalf("next: %v", err)
	}
	body := <-posted
	if size != len(body) {
		t.Fatalf("request size = %d, posted bytes = %d", size, len(body))
	}
}

func TestMaxRequestBytesStopsBeforeOpenAINetworkCall(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","content":"unexpected"},"finish_reason":"stop"}]
		}`))
	}))
	defer server.Close()

	llm := NewOpenAILLM(OpenAIConfig{BaseURL: server.URL, Model: "test-model"}, planner.Tools())
	h := newHarnessWithLLM(t, llm, 0)
	h.runtime.maxRequestBytes = 1
	run := startRun(t, h, "open the catalog")

	err := h.runtime.Plan(context.Background(), run)
	if err == nil {
		t.Fatal("an oversized model request must fail the run")
	}
	if !strings.Contains(err.Error(), "LOOP_MAX_REQUEST_BYTES") {
		t.Fatalf("error must name the request-size limit: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("network calls = %d, want 0", got)
	}
}

func decodeContextMessage(t *testing.T, content, prefix string, target any) {
	t.Helper()
	if !strings.HasPrefix(content, prefix) {
		t.Fatalf("message %q does not start with %q", content, prefix)
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(content, prefix)), target); err != nil {
		t.Fatalf("decode context message: %v\ncontent: %s", err, content)
	}
}
