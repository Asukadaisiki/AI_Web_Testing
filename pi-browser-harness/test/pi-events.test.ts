import { describe, expect, it } from "vitest";
import { stream as streamCompletions } from "@earendil-works/pi-ai/api/openai-completions";
import { stream as streamResponses } from "@earendil-works/pi-ai/api/openai-responses";
import { normalizeContext } from "@earendil-works/pi-ai/utils/transcript";
import { convertMcpResult } from "../node_modules/@earendil-works/pi-coding-agent/dist/extensions/mcp/tools.js";
import { createStandardEventMapper } from "../src/pi-events.js";
import { createTraceMapperState, mapPiEvent } from "../src/trace-events.js";

async function sdkMessage(
  api: "openai-completions" | "openai-responses",
  usage?: Record<string, unknown>,
) {
  const model = {
    id: "glm-5", name: "offline GLM fixture", provider: "glm",
    baseUrl: "http://offline.invalid/v1", reasoning: false,
    input: ["text" as const],
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    contextWindow: 128_000, maxTokens: 4096,
  };
  const chunks = api === "openai-completions" ? [
    {
      id: "offline-completion", object: "chat.completion.chunk", created: 1, model: model.id,
      choices: [{ index: 0, delta: { role: "assistant", content: "done" }, finish_reason: null }],
      usage: null,
    },
    {
      id: "offline-completion", object: "chat.completion.chunk", created: 1, model: model.id,
      choices: [{ index: 0, delta: {}, finish_reason: "stop" }],
    },
    ...(usage ? [{
      id: "offline-completion", object: "chat.completion.chunk", created: 1, model: model.id,
      choices: [], usage,
    }] : []),
  ] : [{
    type: "response.completed",
    response: { id: "offline-response", status: "completed", output: [], ...(usage ? { usage } : {}) },
  }];
  const sse = chunks.map((chunk) => `data: ${JSON.stringify(chunk)}\n\n`).join("") +
    "data: [DONE]\n\n";
  const options = {
    apiKey: "offline-test-not-a-credential", maxRetries: 0,
    fetch: async () => new Response(sse, { headers: { "content-type": "text/event-stream" } }),
  };
  const context = normalizeContext({
    messages: [{ role: "user", content: "test", timestamp: 1 }],
  });
  const stream = api === "openai-completions"
    ? streamCompletions({ ...model, api }, context, options)
    : streamResponses({ ...model, api }, context, options);
  const message = await stream.result();
  expect(message.stopReason).toBe("stop");
  return { type: "message_end" as const, message };
}

function message(timestamp = Date.parse("2026-10-28T00:00:00Z")) {
  return {
    type: "message_end" as const,
    message: {
      role: "assistant" as const, content: [{ type: "text" as const, text: "same" }],
      api: "pi-messages", provider: "scripted", model: "fixture", stopReason: "stop",
      timestamp, durationMs: 1250,
      usage: {
        input: 10, output: 5, cacheRead: 3, cacheWrite: 2, totalTokens: 20,
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
      },
    },
  };
}

describe.each(["standard", "browser"])("%s model observations", (kind) => {
  function mapper() {
    const state = createTraceMapperState();
    return kind === "standard" ? createStandardEventMapper() :
      (event: unknown) => mapPiEvent(event as Parameters<typeof mapPiEvent>[0], state);
  }

  it("preserves SDK model timing and cache usage without claiming billed cost", () => {
    const events = mapper()(message());
    expect(events[0]?.payload).toEqual({
      text: "same", provider: "scripted", model: "fixture", started_at: "2026-10-28T00:00:00.000Z",
      ended_at: "2026-10-28T00:00:01.250Z",
    });
    expect(events[1]?.payload).toEqual({
      provider: "scripted", model: "fixture",
      input_tokens: 10, output_tokens: 5, cache_read_tokens: 3,
      cache_write_tokens: 2, total_tokens: 20,
    });
  });

  it("counts distinct messages with identical text and suppresses duplicate delivery", () => {
    const map = mapper();
    const first = message();
    expect(map(first).some((event) => event.type === "pi.usage")).toBe(true);
    expect(map(first).some((event) => event.type === "pi.usage")).toBe(false);
    expect(map(message(first.message.timestamp + 2000))
      .some((event) => event.type === "pi.usage")).toBe(true);
  });

  it("counts distinct SDK messages even when text and millisecond timestamps coincide", () => {
    const map = mapper();
    expect(map(message()).some((event) => event.type === "pi.usage")).toBe(true);
    expect(map(message()).some((event) => event.type === "pi.usage")).toBe(true);
  });

  it("does not invent timing when the SDK duration is unavailable", () => {
    const event = message();
    Reflect.deleteProperty(event.message, "durationMs");
    expect(mapper()(event)[0]?.payload).toEqual({ text: "same", provider: "scripted", model: "fixture" });
  });

  it("does not expose nonzero SDK price estimates as billed cost", () => {
    const event = message();
    event.message.usage.cost.total = 2;
    expect(mapper()(event).find((item) => item.type === "pi.usage")?.payload)
      .not.toHaveProperty("cost");
  });

  it.each(["openai-completions", "openai-responses"] as const)(
    "omits unknown usage from an actual offline %s SDK stream",
    async (api) => {
      const event = await sdkMessage(api);
      expect(event.message.usage).toMatchObject({
        input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0,
      });
      expect(mapper()(event).map((item) => item.type)).toEqual(["pi.message.completed"]);
    },
  );

  it.each(["openai-completions", "openai-responses"] as const)(
    "preserves reported GLM tokens and caches from an actual offline %s SDK stream",
    async (api) => {
      const event = await sdkMessage(api, api === "openai-completions" ? {
        prompt_tokens: 15, completion_tokens: 5, total_tokens: 20,
        prompt_tokens_details: { cached_tokens: 3, cache_write_tokens: 2 },
      } : {
        input_tokens: 15, output_tokens: 5, total_tokens: 20,
        input_tokens_details: { cached_tokens: 3, cache_write_tokens: 2 },
      });
      const map = mapper();
      expect(map(event).find((item) => item.type === "pi.usage")?.payload).toEqual({
        provider: "glm", model: "glm-5", input_tokens: 10, output_tokens: 5,
        cache_read_tokens: 3, cache_write_tokens: 2, total_tokens: 20,
      });
      expect(map(event).some((item) => item.type === "pi.usage")).toBe(false);
    },
  );

  it.each(["input", "output", "cacheRead", "cacheWrite", "totalTokens"] as const)(
    "retains usage when only %s is positive",
    (counter) => {
      const event = message();
      Object.assign(event.message.usage, {
        input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, [counter]: 7,
      });
      const usage = mapper()(event).find((item) => item.type === "pi.usage");
      expect(usage?.payload).toEqual({
        provider: "scripted", model: "fixture",
        input_tokens: counter === "input" ? 7 : 0,
        output_tokens: counter === "output" ? 7 : 0,
        cache_read_tokens: counter === "cacheRead" ? 7 : 0,
        cache_write_tokens: counter === "cacheWrite" ? 7 : 0,
        total_tokens: counter === "totalTokens" ? 7 : 0,
      });
    },
  );

  it("does not treat price estimates as evidence of reported tokens", () => {
    const event = message();
    Object.assign(event.message.usage, {
      input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0,
    });
    event.message.usage.cost.total = 2;
    expect(mapper()(event).map((item) => item.type)).toEqual(["pi.message.completed"]);
  });
});

describe("generic tool result evidence", () => {
  it.each([
    {
      name: "structured and content reply",
      reply: { content: [{ type: "text" as const, text: "Found 42" }], structuredContent: { answer: 42 } },
      expected: { content: [{ type: "text", text: "Found 42" }], structuredContent: { answer: 42 } },
    },
    {
      name: "text-only reply",
      reply: { content: [{ type: "text" as const, text: "Actual observation" }] },
      expected: { content: [{ type: "text", text: "Actual observation" }] },
    },
    {
      name: "text-only error reply",
      reply: { content: [{ type: "text" as const, text: "Access denied by upstream" }], isError: true },
      expected: { content: [{ type: "text", text: "Access denied by upstream" }], isError: true },
    },
  ])("normalizes an actual SDK-converted $name", async ({ reply, expected }) => {
    const result = await convertMcpResult("generic-tools", "inspect", {
      ...reply, _meta: { private: "not observation evidence" },
    });
    const completed = createStandardEventMapper()({
      type: "tool_execution_end", toolCallId: "inspect-1", toolName: "mcp__generic_tools__inspect",
      result, isError: result.isError === true,
    }).find((event) => event.type === "pi.tool.completed");
    expect(completed?.payload).toMatchObject({
      tool_call_id: "inspect-1", tool_name: "mcp__generic_tools__inspect", is_error: result.isError === true,
      result: expected,
    });
    expect(completed?.payload.result).toEqual(expected);
    if ("structuredContent" in reply) {
      expect(completed?.payload.structured_result).toEqual({ answer: 42 });
    } else {
      expect(completed?.payload).not.toHaveProperty("structured_result");
    }
    expect(completed?.payload.result).not.toHaveProperty("_meta");
    expect(completed?.payload.result).not.toHaveProperty("details");
  });

  it("retains original MCP text rather than the SDK's truncated model-facing rendering", async () => {
    const text = "observed data ".repeat(2000);
    const result = await convertMcpResult("generic-tools", "inspect", {
      content: [{ type: "text", text }],
    }, { saveOutput: async () => "/tmp/not-written-fixture.txt" });
    expect(result.content).not.toEqual([{ type: "text", text }]);
    const completed = createStandardEventMapper()({
      type: "tool_execution_end", toolCallId: "inspect-1", toolName: "mcp__generic_tools__inspect",
      result, isError: false,
    }).find((event) => event.type === "pi.tool.completed");
    expect(completed?.payload.result).toEqual({ content: [{ type: "text", text }] });
  });

  it("preserves a legacy direct tool's structured result without mistaking its data for an MCP envelope", () => {
    const completed = createStandardEventMapper()({
      type: "tool_execution_end", toolCallId: "legacy-1", toolName: "mcp__legacy__inspect",
      result: {
        content: [{ type: "text", text: "Legacy" }],
        structuredContent: { content: [{ type: "text", text: "Domain data" }], answer: 42 },
      },
      isError: false,
    }).find((event) => event.type === "pi.tool.completed");
    expect(completed?.payload.result).toEqual({
      content: [{ type: "text", text: "Legacy" }],
      structuredContent: { content: [{ type: "text", text: "Domain data" }], answer: 42 },
    });
  });
});
