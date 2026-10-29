import { Writable } from "node:stream";
import { expect, it } from "vitest";
import { ModelRuntime } from "@earendil-works/pi-coding-agent";
import { createBrowserSession } from "../src/pi-session.js";
import { HarnessEventWriter, type HarnessEvent, type RunRequest } from "../src/run-protocol.js";

const secret = "public-password-canary";
const request: RunRequest = {
  protocol_version: "pi-browser-harness.v1", run_id: "password-review", attempt_id: "password-review-attempt",
  goal: `Log in with ${secret}`,
  model: { provider: "scripted", name: "browser-fixture", reasoning_effort: "high" },
  worker_url: "http://worker.invalid:8100", artifact_scope: "password-review",
};

async function fixture() {
  const events: HarnessEvent[] = [];
  let emit: (event: unknown) => void = () => {};
  let privateContext: unknown;
  const runtime = await ModelRuntime.create({ refreshOnCreate: false });
  runtime.streamSimple = (_model, context) => {
    privateContext = context;
    return {} as never;
  };
  const session = await createBrowserSession(request, {
    modelRuntime: runtime, eventWriter: new HarnessEventWriter(request, new Writable({
      write(chunk, _encoding, done) { events.push(JSON.parse(String(chunk))); done(); },
    })),
    createAgentSession: async () => ({
      session: {
        prompt: async () => {}, abort: async () => {}, dispose() {}, bindExtensions: async () => {},
        subscribe(callback: (event: unknown) => void) { emit = callback; return () => {}; },
        getActiveToolNames: () => [],
        extensionRunner: {
          emit: async () => {}, createToolContext: () => ({}),
          getToolDefinition: () => ({
            execute: async () => ({ structuredContent: { structuredContent: { closed: true } } }),
          }),
        },
      } as never,
      extensionsResult: {} as never,
    }),
  });
  const start = (id = "password", value = secret) => emit({
    type: "tool_execution_start", toolCallId: id, toolName: "mcp__browser_harness__input", args: { value },
  });
  const end = (id = "password") => emit({
    type: "tool_execution_end", toolCallId: id, toolName: "mcp__browser_harness__input",
    result: { content: [], structuredContent: { redacted_args: ["value"] } },
  });
  const assistant = (text: string) => emit({ type: "message_end", message: {
    role: "assistant", content: [{ type: "text", text }], provider: "scripted",
    model: "browser-fixture", stopReason: "stop", timestamp: 1,
    usage: { input: 3, output: 2, totalTokens: 5, cacheRead: 0, cacheWrite: 0 },
  } });
  return { events, session, start, end, assistant,
    settle: (aborted = false) => emit({ type: "agent_settled", aborted }),
    capture: () => {
      runtime.streamSimple(runtime.getModel("scripted", "browser-fixture")!, {
        messages: [{ role: "user", content: request.goal, timestamp: 1 },
          { role: "assistant", content: [{ type: "text", text: `history ${secret}` }], timestamp: 2 }],
      } as never);
      return privateContext;
    },
  };
}

it("redacts every public historical input copy after preserving the private canonical goal", async () => {
  const f = await fixture();
  try {
    f.start(); f.end();
    const context = f.capture() as { messages: { content: unknown }[] };
    expect(context.messages[0]!.content).toBe(request.goal);
    expect(JSON.stringify(context.messages[1])).not.toContain(secret);
    f.assistant("Login completed");
    f.settle();
    const model = f.events.find((event) => event.type === "pi.message.completed");
    expect(model?.payload.input?.messages).toHaveLength(2);
    expect(JSON.stringify(f.events)).not.toContain(secret);
    expect(JSON.stringify(model?.payload.input)).toContain("Log in with [redacted]");
  } finally { await f.session.close(); }
});

it("streams tool progress while withholding model text/input and retaining model/usage order", async () => {
  const f = await fixture();
  try {
    f.capture();
    f.assistant(`I will enter ${secret}`);
    expect(JSON.stringify(f.events)).not.toContain(secret);
    f.start(); f.end();
    expect(f.events.map((event) => event.type)).toEqual(["pi.tool.started", "pi.tool.completed"]);
    f.assistant(`I entered ${secret} and will enter second-password-canary`);
    expect(JSON.stringify(f.events)).not.toContain("second-password-canary");
    f.start("second", "second-password-canary"); f.end("second");
    f.assistant(`I entered ${secret}`);
    f.settle();
    expect(f.events.map((event) => event.type)).toEqual([
      "pi.tool.started", "pi.tool.completed", "pi.tool.started", "pi.tool.completed",
      "pi.message.completed", "pi.usage", "pi.message.completed", "pi.usage",
      "pi.message.completed", "pi.usage", "harness.settled",
    ]);
    expect(JSON.stringify(f.events)).not.toContain(secret);
    expect(JSON.stringify(f.events)).not.toContain("second-password-canary");
    expect(f.events.filter((event) => event.type === "pi.message.completed").at(-1)?.payload.text)
      .toBe("I entered [redacted]");
  } finally { await f.session.close(); }
});

it("omits unclassified model text and input on interruption rather than flushing a password", async () => {
  const f = await fixture();
  try {
    f.capture(); f.assistant(`I will enter ${secret}`); f.start();
    f.settle(true);
    expect(f.events.at(-1)?.type).toBe("harness.failed");
    expect(JSON.stringify(f.events)).not.toContain(secret);
    expect(f.events.find((event) => event.type === "pi.message.completed")?.payload.input).toBeUndefined();
  } finally { await f.session.close(); }
});
