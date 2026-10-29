import { genericExecution } from "./fixtures/selected-harness.js";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Writable } from "node:stream";
import { fileURLToPath } from "node:url";
import {
  createAgentSession, ModelRuntime, type AgentSession, type AgentSessionEvent,
} from "@earendil-works/pi-coding-agent";
import { stream } from "@earendil-works/pi-ai/api/openai-completions";
import { normalizeContext } from "@earendil-works/pi-ai/utils/transcript";
import { afterEach, expect, it, vi } from "vitest";
import { createBrowserHarnessDefinition } from "../src/browser-harness.js";
import type { HarnessDefinition } from "../src/harness-definition.js";
import { createPiSession, type PiSession } from "../src/pi-session.js";
import { HarnessEventWriter, type HarnessEvent, type RunRequest } from "../src/run-protocol.js";

const fixture = fileURLToPath(new URL("./fixtures/runtime-mcp-server.mjs", import.meta.url));
const request: RunRequest = {
  execution: genericExecution,
  protocol_version: "pi-browser-harness.v1", run_id: "provider-error", attempt_id: "provider-error-attempt",
  goal: "complete the request",
  model: { provider: "offline-openai", name: "offline-fixture", reasoning_effort: "off" },
};
const generic: HarnessDefinition = {
  systemPrompt: "Complete the request", skillPaths: [], toolAllowlist: [], mcpServers: [],
  completionPolicy: ({ assistant_settled, aborted }) => ({
    verdict: assistant_settled && !aborted ? "succeeded" : "failed",
  }),
};

afterEach(() => vi.unstubAllEnvs());

function response(tool = false): Response {
  const delta = tool ? {
    role: "assistant", tool_calls: [{
      index: 0, id: "browser-proof", type: "function",
      function: { name: "mcp__browser_harness__finish_case", arguments: "{}" },
    }],
  } : { role: "assistant", content: "Recovered and completed" };
  const sse = [delta, {}].map((item, index) => `data: ${JSON.stringify({
    id: "offline-completion", object: "chat.completion.chunk", created: 1, model: "offline-fixture",
    choices: [{ index: 0, delta: item, finish_reason: index ? (tool ? "tool_calls" : "stop") : null }],
  })}\n\n`).join("") + "data: [DONE]\n\n";
  return new Response(sse, { headers: { "content-type": "text/event-stream" } });
}

async function runProvider(
  outcomes: readonly (number | "ok" | "proof")[],
  definition = generic,
  onSession?: (session: PiSession, sdk: AgentSession) => void,
) {
  vi.stubEnv("AGENTTRACE_MODEL_BASE_URL", "http://offline.invalid/v1");
  vi.stubEnv("AGENTTRACE_MODEL_API_KEY", "offline-test-not-a-credential");
  const cwd = await mkdtemp(join(tmpdir(), "pi-provider-regression-"));
  const events: HarnessEvent[] = [];
  const sdkEvents: AgentSessionEvent[] = [];
  const runtime = await ModelRuntime.create({ refreshOnCreate: false });
  let calls = 0;
  // Keep the real provider and agent/session loop; only replace the network boundary.
  runtime.streamSimple = (model, context, options) => stream(
    { ...model, api: "openai-completions", reasoning: false }, normalizeContext(context), {
      ...options, apiKey: "offline-test-not-a-credential", maxRetries: 0,
      fetch: async () => {
        const outcome = outcomes[calls++];
        if (outcome === undefined) throw new Error("unexpected provider request");
        return typeof outcome === "number" ? new Response(JSON.stringify({
          error: { message: outcome === 401 ? "invalid fixture credentials" : "503 service unavailable" },
        }), { status: outcome, headers: { "content-type": "application/json" } }) :
          response(outcome === "proof");
      },
    },
  );
  let session: PiSession | undefined;
  let sdk: AgentSession | undefined;
  try {
    session = await createPiSession(request, {
      cwd, modelRuntime: runtime,
      eventWriter: new HarnessEventWriter(request, new Writable({
        write(chunk, _encoding, done) { events.push(JSON.parse(chunk.toString())); done(); },
      })),
      createAgentSession: async (options) => {
        const result = await createAgentSession(options);
        sdk = result.session;
        sdk.subscribe((event) => sdkEvents.push(event));
        return result;
      },
    }, definition);
    onSession?.(session, sdk!);
    await session.prompt(request.goal);
    return { events, sdkEvents, calls, verdict: session.getVerdict() };
  } finally {
    await session?.close();
    await rm(cwd, { recursive: true, force: true });
  }
}

it("characterizes actual Pi provider failures and recovered retries before Harness settlement", async () => {
  const failed = await runProvider([401]);
  const failedMessage = failed.sdkEvents.find((event) =>
    event.type === "message_end" && event.message.role === "assistant");
  expect(failedMessage).toMatchObject({
    message: { stopReason: "error", errorMessage: expect.stringContaining("invalid fixture credentials") },
  });
  expect(failed.sdkEvents.at(-1)).toMatchObject({ type: "agent_settled", aborted: false });
  const recovered = await runProvider([503, "ok"]);
  expect(recovered.sdkEvents.filter((event) =>
    event.type === "message_end" && event.message.role === "assistant")
    .map((event) => event.type === "message_end" && event.message.role === "assistant" ?
      event.message.stopReason : undefined)).toEqual(["error", "stop"]);
  expect(recovered.sdkEvents).toContainEqual(expect.objectContaining({
    type: "auto_retry_end", success: true,
  }));
  expect(recovered.sdkEvents.filter((event) => event.type === "agent_settled")).toHaveLength(1);
  expect(recovered.sdkEvents.at(-1)).toMatchObject({ type: "agent_settled", aborted: false });
});

it.each([
  { name: "non-retryable error", outcomes: [401], calls: 1 },
  { name: "exhausted retry budget", outcomes: [503, 503, 503], calls: 3 },
] as const)("fails a generic Harness on a final SDK $name", async ({ outcomes, calls }) => {
  const result = await runProvider(outcomes);
  expect(result.calls).toBe(calls);
  expect(result.verdict).toBe("failed");
  expect(result.events.at(-1)).toMatchObject({
    type: "harness.failed", payload: { error: expect.stringMatching(/invalid fixture credentials|503/) },
  });
  expect(result.events.some((event) => event.type === "harness.settled")).toBe(false);
});

it("succeeds after an actual SDK retry recovers without emitting premature failure", async () => {
  const result = await runProvider([503, "ok"]);
  expect(result.calls).toBe(2);
  expect(result.verdict).toBe("succeeded");
  expect(result.events.at(-1)).toMatchObject({
    type: "harness.settled", payload: { verdict: "succeeded", assistant_text: "Recovered and completed" },
  });
  expect(result.events.some((event) => event.type === "harness.failed")).toBe(false);
});

it("fails a final provider error even after the real MCP tool produced passing browser proof", async () => {
  const browser: HarnessDefinition = {
    ...createBrowserHarnessDefinition(),
    skillPaths: [], formatPrompt: (goal) => goal,
    mcpServers: [{
      name: "browser-harness", command: process.execPath, args: [fixture],
      exposure: "direct", hostOnlyTools: ["close_session", "release"],
    }],
  };
  const result = await runProvider(["proof", 401], browser);
  expect(result.events).toContainEqual(expect.objectContaining({
    type: "browser.case.completed", payload: expect.objectContaining({ verdict: "passed" }),
  }));
  expect(result.events.at(-1)).toMatchObject({ type: "harness.failed" });
  expect(result.verdict).toBe("failed");
});

it("fails an actual SDK abort rather than allowing a custom completion policy to override it", async () => {
  const result = await runProvider([503], {
    ...generic, completionPolicy: () => ({ verdict: "succeeded" }),
  }, (session, sdk) => {
    sdk.subscribe((event) => {
      if (event.type === "auto_retry_start") void session.abort();
    });
  });
  expect(result.sdkEvents.at(-1)).toMatchObject({ type: "agent_settled", aborted: true });
  expect(result.verdict).toBe("failed");
  expect(result.events.at(-1)).toMatchObject({ type: "harness.failed" });
});
