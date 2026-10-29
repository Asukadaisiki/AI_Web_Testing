package piharness

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
)

// This opt-in test crosses the production Go transport, Node host, Pi SDK
// and a selected Harness snapshot. The controlled MCP tool replaces only external
// execution; it does not mock any of the session or cancellation machinery.
func TestSharedHostWithRealPiAndReplacementHarness(t *testing.T) {
	entry := os.Getenv("AGENTTRACE_TEST_HARNESS_ENTRY")
	if entry == "" {
		t.Skip("AGENTTRACE_TEST_HARNESS_ENTRY is not configured")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	packageRoot := filepath.Dir(filepath.Dir(entry))
	sdkRoot := filepath.Join(packageRoot, "node_modules", "@modelcontextprotocol", "sdk", "dist", "esm")
	temp := t.TempDir()
	started := make(chan string, 8)
	closed := make(chan string, 8)
	cleanupStarted := make(chan string, 8)
	closeGate := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/wait/") {
			started <- id
			if id == "attempt-cancel" {
				select {
				case <-r.Context().Done():
				case <-ctx.Done():
				}
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/closed/") {
			cleanupStarted <- id
			if id == "attempt-cancel" {
				select {
				case <-closeGate:
				case <-ctx.Done():
				}
			}
			closed <- id
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer worker.Close()
	bootstrapPath := filepath.Join(temp, "shared-host-fixture.mjs")
	childPath := filepath.Join(temp, "tool-server.mjs")
	jsString := func(value string) string {
		encoded, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return string(encoded)
	}
	fileURL := func(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }
	child := fmt.Sprintf(`
import { McpServer } from %s;
import { StdioServerTransport } from %s;
import { appendFileSync } from "node:fs";
const server = new McpServer({name:"replacement-tools",version:"1.0.0"});
const attempt = process.env.ATTEMPT;
const endpoint = process.env.ENDPOINT;
const probe = %s + "/" + attempt + ".events";
process.on("exit", (code) => appendFileSync(probe, "exit:" + code + "\n"));
server.tool("wait", "Exercise an isolated external operation", {}, async (_args, extra) => {
  await fetch(endpoint + "/wait/" + attempt, {signal:extra.signal});
  return {content:[{type:"text",text:attempt}]};
});
let releasing;
const release = () => releasing ??= fetch(endpoint + "/closed/" + attempt, {method:"POST"});
server.tool("close_session", "Acknowledge fixture resource cleanup", {}, async () => {
  const response = await release();
  if (!response.ok) throw new Error("fixture cleanup failed");
  return {content:[],structuredContent:{closed:true}};
});
const transport = new StdioServerTransport();
await server.connect(transport);
let stopping = false;
const stop = () => {
  if (stopping) return;
  stopping = true;
  appendFileSync(probe, "stop\n");
  void release()
    .finally(async () => { await server.close(); process.exit(0); });
};
const previousClose = transport.onclose;
transport.onclose = () => {
  previousClose?.();
  stop();
};
process.on("SIGTERM", stop);
process.on("SIGINT", stop);
process.stdin.once("end", stop);
`, jsString(fileURL(filepath.Join(sdkRoot, "server", "mcp.js"))),
		jsString(fileURL(filepath.Join(sdkRoot, "server", "stdio.js"))), jsString(temp))
	bootstrap := fmt.Sprintf(`
import { appendFileSync } from "node:fs";
import { main } from %s;
import { createExecutionSessionFactory } from %s;
import { createAgentSession } from %s;
import { createAssistantMessageEventStream } from %s;
appendFileSync(%s, String(process.pid) + "\n");
const factory = await createExecutionSessionFactory({
  mcpResolution: {capabilities:new Map([["fixture-wait",{
    command:process.execPath,args:[%s],env:{ENDPOINT:%s},
    config:{attempt:{type:"string",env:"ATTEMPT",required:true}},secrets:{},
    hostOnlyTools:["close_session"]
  }]])},
  createAgentSession: async (options) => {
    options.modelRuntime.streamSimple = (model, context) => {
      const prior = context.messages.filter((message) => message.role === "toolResult").length;
      const stream = createAssistantMessageEventStream();
      stream.end({
        role:"assistant", api:model.api, provider:model.provider, model:model.id,
        content:prior === 0
          ? [{type:"toolCall",id:"fixture-wait",name:"mcp__replacement__wait",arguments:{}}]
          : [{type:"text",text:"selected Harness completed"}],
        stopReason:prior === 0 ? "toolUse" : "stop", timestamp:Date.now(),
        usage:{input:10,output:5,cacheRead:0,cacheWrite:0,totalTokens:15,
          cost:{input:0,output:0,cacheRead:0,cacheWrite:0,total:0}}
      });
      return stream;
    };
    return createAgentSession(options);
  }
});
process.exit(await main({argv:["--server"],sessionFactory:factory}));
`, jsString(fileURL(entry)), jsString(fileURL(filepath.Join(packageRoot, "dist", "session-factory.js"))),
		jsString(fileURL(filepath.Join(packageRoot, "node_modules", "@earendil-works", "pi-coding-agent", "dist", "index.js"))),
		jsString(fileURL(filepath.Join(packageRoot, "node_modules", "@earendil-works", "pi-ai", "dist", "index.js"))),
		jsString(filepath.Join(temp, "host-starts")), jsString(childPath), jsString(worker.URL))
	for path, content := range map[string]string{childPath: child, bootstrapPath: bootstrap} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runner := &SharedProcessRunner{
		Command: node, Args: []string{bootstrapPath, "--server"},
		WorkingDirectory: temp,
		Environment:      append(os.Environ(), "PI_CODING_AGENT_DIR="+temp),
		TerminationGrace: 5 * time.Second,
		Timeout:          15 * time.Second,
	}
	defer func() {
		_ = runner.Close()
		if t.Failed() {
			runner.mu.Lock()
			defer runner.mu.Unlock()
			t.Logf("shared Host stderr: %s", runner.lastStderr)
		}
	}()
	request := func(id string) Request {
		return Request{
			RunID: id, AttemptID: "attempt-" + id, Goal: "run " + id,
			Model: ModelRequest{Provider: "scripted", Name: "replacement-model", ReasoningEffort: "off"},
			Execution: &execution.Spec{
				RuntimeID: "pi",
				Harness: execution.HarnessSnapshot{
					ID: "selected-fixture", Name: "Selected fixture", Revision: 1,
					SystemPrompt: "Use the selected wait tool to complete this task.",
					Skills:       []execution.Skill{},
					MCP: map[string]execution.MCPServer{
						"replacement": {
							Capability: "fixture-wait", Config: map[string]any{"attempt": "attempt-" + id},
							SecretRefs: map[string]string{},
						},
					},
				},
			},
		}
	}
	var mu sync.Mutex
	traces := map[string][]Event{}
	collect := func(id string) func(Event) {
		return func(ev Event) {
			mu.Lock()
			defer mu.Unlock()
			traces[id] = append(traces[id], ev)
		}
	}
	cancelCtx, cancelAttempt := context.WithCancel(ctx)
	defer cancelAttempt()
	cancelDone := make(chan error, 1)
	otherDone := make(chan error, 1)
	go func() { cancelDone <- runner.Run(cancelCtx, request("cancel"), collect("cancel")) }()
	go func() { otherDone <- runner.Run(ctx, request("other"), collect("other")) }()
	seen := map[string]bool{}
	for len(seen) != 2 {
		select {
		case id := <-started:
			seen[id] = true
		case err := <-cancelDone:
			t.Fatalf("first session failed before tool startup: %v", err)
		case err := <-otherDone:
			t.Fatalf("sibling session failed before tool startup: %v", err)
		case <-ctx.Done():
			t.Fatal("Pi sessions did not execute replacement tools concurrently")
		}
	}
	cancelAttempt()
	for {
		select {
		case id := <-cleanupStarted:
			if id == "attempt-cancel" {
				goto cancellingCleanup
			}
		case err := <-cancelDone:
			probe, _ := os.ReadFile(filepath.Join(temp, "attempt-cancel.events"))
			t.Fatalf("cancelled session returned without starting resource cleanup: %v; child=%q", err, probe)
		case <-ctx.Done():
			t.Fatal("cancelled session did not begin cleanup")
		}
	}
cancellingCleanup:
	select {
	case err := <-cancelDone:
		t.Fatalf("cancelled session returned before tool cleanup completed: %v", err)
	default:
	}
	close(closeGate)
	select {
	case err := <-otherDone:
		if err != nil {
			t.Fatalf("sibling session failed after targeted cancellation: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("sibling session did not complete")
	}
	select {
	case err := <-cancelDone:
		if err != context.Canceled {
			t.Fatalf("cancelled session = %v, want context.Canceled", err)
		}
	case <-ctx.Done():
		t.Fatal("cancelled session was not released")
	}
	if err := runner.Run(ctx, request("next"), collect("next")); err != nil {
		t.Fatalf("shared process was not reusable: %v", err)
	}
	starts, err := os.ReadFile(filepath.Join(temp, "host-starts"))
	if err != nil || len(strings.Fields(string(starts))) != 1 {
		t.Fatalf("expected one reused shared host: %q, %v", starts, err)
	}
	for _, id := range []string{"cancel", "other", "next"} {
		probe, err := os.ReadFile(filepath.Join(temp, "attempt-"+id+".events"))
		if err != nil || !strings.Contains(string(probe), "exit:0") {
			t.Fatalf("MCP child has no successful exit witness by scenario end: %s: %q, %v", id, probe, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for _, id := range []string{"other", "next"} {
		events := traces[id]
		if len(events) < 4 {
			t.Fatalf("incomplete real Pi trace for %s: %#v", id, events)
		}
		for index, ev := range events {
			if ev.RunID != id || ev.AttemptID != "attempt-"+id || ev.Sequence != int64(index+1) {
				t.Fatalf("event escaped its attempt: %#v", ev)
			}
			if ev.Type == EventCaseCompleted || ev.Type == EventObservation {
				t.Fatal("replacement Harness unexpectedly required browser evidence")
			}
		}
		last := events[len(events)-1]
		var payload SettledPayload
		_ = json.Unmarshal(last.Payload, &payload)
		if last.Type != EventSettled || payload.Verdict != "succeeded" {
			t.Fatalf("replacement Harness did not publish its own success policy: %#v", last)
		}
	}
}
