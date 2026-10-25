package operation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/agentprofile"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/operation"
)

var browserArgumentsSchema = json.RawMessage(`{
	"type": "object",
	"required": ["session_id", "case"],
	"properties": {
		"session_id": {"type": "string", "minLength": 1},
		"case": {"type": "object"}
	},
	"additionalProperties": false
}`)

func TestHTTPDispatcherRejectsInvalidArgumentsBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()

	dispatcher := newDispatcher(t, server.Client(), operation.Definition{
		Name:           "browser_execute_case",
		InputSchema:    browserArgumentsSchema,
		CallbackURL:    server.URL,
		Timeout:        time.Second,
		MaxAttempts:    1,
		MaxResultBytes: 1024,
	}, server.URL)

	_, err := dispatcher.Dispatch(context.Background(), operation.Call{
		RunID:       "run_invalid",
		SpanID:      "span_invalid",
		OperationID: "op_invalid",
		ToolCallID:  "tool_invalid",
		Name:        "browser_execute_case",
		Arguments:   json.RawMessage(`{"session_id":"sess_invalid","callback_url":"https://attacker.test"}`),
	})
	if err == nil {
		t.Fatal("invalid arguments must be rejected")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("HTTP calls = %d, want 0", got)
	}
}

func TestHTTPDispatcherRejectsInvalidNestedCaseBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()

	dispatcher := newDispatcher(t, server.Client(), operation.Definition{
		Name:           "browser_execute_case",
		InputSchema:    browserArgumentsSchema,
		CallbackURL:    server.URL,
		Timeout:        time.Second,
		MaxAttempts:    1,
		MaxResultBytes: 1024,
	}, server.URL)

	_, err := dispatcher.Dispatch(context.Background(), operation.Call{
		RunID:       "run_invalid_case",
		SpanID:      "span_invalid_case",
		OperationID: "op_invalid_case",
		ToolCallID:  "tool_invalid_case",
		Name:        "browser_execute_case",
		Arguments:   json.RawMessage(`{"session_id":"sess_invalid_case","case":{}}`),
	})
	if err == nil {
		t.Fatal("invalid nested case must be rejected")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("HTTP calls = %d, want 0", got)
	}
}

func TestHTTPDispatcherRejectsCallbackOutsideServerProfile(t *testing.T) {
	_, err := operation.NewHTTPDispatcher(operation.HTTPConfig{
		Operations: []operation.Definition{{
			Name:           "browser_execute_case",
			InputSchema:    browserArgumentsSchema,
			CallbackURL:    "https://attacker.test/collect",
			Timeout:        time.Second,
			MaxAttempts:    1,
			MaxResultBytes: 1024,
		}},
		ApprovedCallbackURLs: []string{"http://worker:8100/operations/browser_execute_case"},
	})
	if err == nil {
		t.Fatal("a callback absent from the server profile must be rejected")
	}
	if !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("error = %v, want an approval error", err)
	}
}

func TestHTTPDispatcherDoesNotFollowCallbackRedirects(t *testing.T) {
	var redirectedCalls atomic.Int32
	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectedCalls.Add(1)
		_, _ = w.Write([]byte(`{"status":"succeeded"}`))
	}))
	defer redirected.Close()

	approved := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirected.URL, http.StatusTemporaryRedirect)
	}))
	defer approved.Close()

	dispatcher := newDispatcher(t, approved.Client(), operation.Definition{
		Name:           "browser_execute_case",
		InputSchema:    browserArgumentsSchema,
		CallbackURL:    approved.URL,
		Timeout:        time.Second,
		MaxAttempts:    1,
		MaxResultBytes: 1024,
	}, approved.URL)

	_, err := dispatcher.Dispatch(context.Background(), operation.Call{
		RunID:       "run_redirect",
		SpanID:      "span_redirect",
		OperationID: "op_redirect",
		ToolCallID:  "tool_redirect",
		Name:        "browser_execute_case",
		Arguments:   validBrowserArguments("sess_redirect"),
	})
	if err == nil {
		t.Fatal("callback redirects must be rejected")
	}
	if got := redirectedCalls.Load(); got != 0 {
		t.Fatalf("redirect target calls = %d, want 0", got)
	}
}

func TestHTTPDispatcherRetriesWithStableTracingHeaders(t *testing.T) {
	var (
		mu      sync.Mutex
		headers []http.Header
		bodies  []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)

		mu.Lock()
		headers = append(headers, r.Header.Clone())
		bodies = append(bodies, string(body))
		attempt := len(headers)
		mu.Unlock()

		if attempt == 1 {
			http.Error(w, "retry", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(
			`{"status":"succeeded","output":{"execution_id":"exec_123"},` +
				`"artifacts":{"screenshot_paths":[]},"error":null}`,
		))
	}))
	defer server.Close()

	dispatcher := newDispatcher(t, server.Client(), operation.Definition{
		Name:           "browser_execute_case",
		InputSchema:    browserArgumentsSchema,
		CallbackURL:    server.URL,
		Timeout:        time.Second,
		MaxAttempts:    2,
		RetryBackoff:   time.Millisecond,
		MaxResultBytes: 1024,
	}, server.URL)
	arguments := validBrowserArguments("sess_123")

	result, err := dispatcher.Dispatch(context.Background(), operation.Call{
		RunID:       "run_123",
		SpanID:      "span_123",
		OperationID: "op_123",
		ToolCallID:  "tool_123",
		Name:        "browser_execute_case",
		Arguments:   arguments,
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Status != "succeeded" || string(result.Output) != `{"execution_id":"exec_123"}` {
		t.Fatalf("result = %#v", result)
	}
	if string(result.Artifacts) != `{"screenshot_paths":[]}` || string(result.Error) != "null" {
		t.Fatalf("result envelope = %#v", result)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(headers) != 2 {
		t.Fatalf("HTTP attempts = %d, want 2", len(headers))
	}
	for index, header := range headers {
		if got := header.Get("Idempotency-Key"); got != "op_123" {
			t.Fatalf("attempt %d Idempotency-Key = %q", index+1, got)
		}
		if got := header.Get("X-AgentTrace-Run-ID"); got != "run_123" {
			t.Fatalf("attempt %d X-AgentTrace-Run-ID = %q", index+1, got)
		}
		if got := header.Get("X-AgentTrace-Span-ID"); got != "span_123" {
			t.Fatalf("attempt %d X-AgentTrace-Span-ID = %q", index+1, got)
		}
		if got := header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("attempt %d Content-Type = %q", index+1, got)
		}
		if bodies[index] != string(arguments) {
			t.Fatalf("attempt %d body = %s", index+1, bodies[index])
		}
	}
}

func TestHTTPDispatcherRejectsMalformedCallbackEnvelopes(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing output",
			body: `{"status":"succeeded","artifacts":{},"error":null}`,
		},
		{
			name: "missing artifacts",
			body: `{"status":"succeeded","output":{},"error":null}`,
		},
		{
			name: "missing error",
			body: `{"status":"succeeded","output":{},"artifacts":{}}`,
		},
		{
			name: "scalar output",
			body: `{"status":"succeeded","output":"bad","artifacts":{},"error":null}`,
		},
		{
			name: "scalar artifacts",
			body: `{"status":"succeeded","output":{},"artifacts":7,"error":null}`,
		},
		{
			name: "scalar error",
			body: `{"status":"failed","output":{},"artifacts":{},"error":"bad"}`,
		},
		{
			name: "succeeded with error",
			body: `{"status":"succeeded","output":{},"artifacts":{},` +
				`"error":{"kind":"worker_error","message":"bad"}}`,
		},
		{
			name: "failed without error",
			body: `{"status":"failed","output":{},"artifacts":{},"error":null}`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			dispatcher := newDispatcher(t, server.Client(), operation.Definition{
				Name:           "browser_execute_case",
				InputSchema:    browserArgumentsSchema,
				CallbackURL:    server.URL,
				Timeout:        time.Second,
				MaxAttempts:    1,
				MaxResultBytes: 1024,
			}, server.URL)

			_, err := dispatcher.Dispatch(context.Background(), validCall(
				"op_malformed_"+strings.ReplaceAll(test.name, " ", "_"),
			))
			if err == nil {
				t.Fatalf("callback body %s must be rejected", test.body)
			}
		})
	}
}

func TestHTTPDispatcherEnforcesTotalTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(
		_ http.ResponseWriter,
		request *http.Request,
	) {
		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)

	dispatcher := newDispatcher(t, server.Client(), operation.Definition{
		Name:           "browser_execute_case",
		InputSchema:    browserArgumentsSchema,
		CallbackURL:    server.URL,
		Timeout:        30 * time.Millisecond,
		MaxAttempts:    2,
		RetryBackoff:   time.Second,
		MaxResultBytes: 1024,
	}, server.URL)

	started := time.Now()
	_, err := dispatcher.Dispatch(context.Background(), validCall("op_timeout"))
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("blocking callback must time out")
	}
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if elapsed >= 500*time.Millisecond {
		t.Fatalf("elapsed = %v, total timeout was not enforced", elapsed)
	}
}

func TestHTTPDispatcherRejectsOversizedCallbackResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 65)))
	}))
	defer server.Close()

	dispatcher := newDispatcher(t, server.Client(), operation.Definition{
		Name:           "browser_execute_case",
		InputSchema:    browserArgumentsSchema,
		CallbackURL:    server.URL,
		Timeout:        time.Second,
		MaxAttempts:    1,
		MaxResultBytes: 64,
	}, server.URL)

	_, err := dispatcher.Dispatch(context.Background(), validCall("op_oversized"))
	if err == nil {
		t.Fatal("oversized callback response must be rejected")
	}
	if !strings.Contains(err.Error(), "response exceeds 64 bytes") {
		t.Fatalf("error = %v, want response size error", err)
	}
}

func TestBrowserEvalProfileUsesServerOwnedWorkerEndpointAndModelDefaults(t *testing.T) {
	profile, err := agentprofile.LoadBrowserEval("https://worker.internal/base")
	if err != nil {
		t.Fatalf("load browser profile: %v", err)
	}
	if profile.ProductName != "AgentTrace Eval Hub" {
		t.Fatalf("product name = %q", profile.ProductName)
	}
	if profile.Model.Name != "glm-5.3-flash" || profile.Model.ReasoningEffort != "high" {
		t.Fatalf("model defaults = %#v", profile.Model)
	}
	if profile.Model.Endpoint != "" || profile.Model.APIKey != "" {
		t.Fatalf("profile must not contain model credentials or endpoint: %#v", profile.Model)
	}

	definitions := profile.OperationDefinitions()
	if len(definitions) != 1 {
		t.Fatalf("operation definitions = %d, want 1", len(definitions))
	}
	definition := definitions[0]
	if definition.Name != "browser_execute_case" {
		t.Fatalf("operation name = %q", definition.Name)
	}
	if definition.CallbackURL != "https://worker.internal/base/operations/browser_execute_case" {
		t.Fatalf("callback URL = %q", definition.CallbackURL)
	}
	if definition.Timeout < 20*time.Minute {
		t.Fatalf(
			"operation timeout = %v, want at least 20m including delivery overhead",
			definition.Timeout,
		)
	}
	if definition.Timeout <= 0 || definition.MaxAttempts < 1 || definition.MaxResultBytes < 1 {
		t.Fatalf("operation limits are not bounded: %#v", definition)
	}
	if !profile.Safety.FreshContext ||
		!profile.Safety.DisposableFixtureData ||
		profile.Safety.IrreversibleProductionOperations != "unsupported" {
		t.Fatalf("browser safety = %#v", profile.Safety)
	}

	runOperations := profile.RunOperations()
	if len(runOperations) != 1 ||
		runOperations[0].Callback.Kind != "http" ||
		runOperations[0].Callback.URL != definition.CallbackURL {
		t.Fatalf("run operations = %#v", runOperations)
	}
}

func validCall(operationID string) operation.Call {
	return operation.Call{
		RunID:       "run_valid",
		SpanID:      "span_valid",
		OperationID: operationID,
		ToolCallID:  "tool_valid",
		Name:        "browser_execute_case",
		Arguments:   validBrowserArguments("sess_valid"),
	}
}

func validBrowserArguments(sessionID string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
		"session_id":%q,
		"case":{
			"case_version":"loop.case.v1",
			"name":"callback case",
			"goal":"exercise callback dispatch",
			"base_url":"https://fixture.invalid",
			"steps":[{
				"index":0,
				"action":"goto",
				"intent":"open fixture",
				"value":"https://fixture.invalid/start",
				"preconditions":[],
				"postconditions":[{
					"type":"url_contains",
					"value":"/start",
					"timeout_ms":10000
				}],
				"timeout_ms":20000
			}]
		}
	}`, sessionID))
}

func newDispatcher(
	t *testing.T,
	client *http.Client,
	definition operation.Definition,
	approvedCallbackURL string,
) operation.Dispatcher {
	t.Helper()
	dispatcher, err := operation.NewHTTPDispatcher(operation.HTTPConfig{
		Client:               client,
		Operations:           []operation.Definition{definition},
		ApprovedCallbackURLs: []string{approvedCallbackURL},
	})
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	return dispatcher
}
