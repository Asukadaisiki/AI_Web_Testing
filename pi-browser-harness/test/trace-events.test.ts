import { describe, expect, it } from "vitest";

import type { AgentSessionEvent } from "@earendil-works/pi-coding-agent";

import {
  createTraceMapperState,
  mapPiEvent,
} from "../src/trace-events.js";

function assistantMessage(
  text: string,
  usage = { input: 10, output: 5, cacheRead: 0, cacheWrite: 0 },
): AgentSessionEvent {
  return {
    type: "message_end",
    message: {
      role: "assistant",
      content: [{ type: "text", text }],
      api: "pi-messages",
      provider: "scripted",
      model: "browser-fixture",
      usage,
      stopReason: "stop",
      timestamp: 1,
    },
  } as unknown as AgentSessionEvent;
}

function userMessage(): AgentSessionEvent {
  return {
    type: "message_end",
    message: {
      role: "user",
      content: "hello",
      timestamp: 1,
    },
  } as unknown as AgentSessionEvent;
}

function toolStart(
  toolCallId: string,
  toolName: string,
): AgentSessionEvent {
  return {
    type: "tool_execution_start",
    toolCallId,
    toolName,
    args: {},
  } as unknown as AgentSessionEvent;
}

function toolEnd(
  toolCallId: string,
  toolName: string,
  result: unknown,
  isError = false,
): AgentSessionEvent {
  return {
    type: "tool_execution_end",
    toolCallId,
    toolName,
    result,
    isError,
  } as unknown as AgentSessionEvent;
}

const settled: AgentSessionEvent = {
  type: "agent_settled",
  aborted: false,
} as unknown as AgentSessionEvent;

describe("mapPiEvent", () => {
  it("maps an authoritative assistant message to pi.message.completed and pi.usage", () => {
    const state = createTraceMapperState();
    const events = mapPiEvent(assistantMessage("cart verified"), state);

    expect(events.map((e) => e.type)).toEqual([
      "pi.message.completed",
      "pi.usage",
    ]);
    expect(events[0]?.payload).toEqual({ text: "cart verified" });
    expect(events[1]?.payload).toEqual({
      input_tokens: 10,
      output_tokens: 5,
    });
  });

  it("emits usage at most once per assistant message", () => {
    const state = createTraceMapperState();
    const message = assistantMessage("done");

    const first = mapPiEvent(message, state);
    const second = mapPiEvent(message, state);

    expect(first.map((e) => e.type)).toContain("pi.usage");
    expect(second.map((e) => e.type)).not.toContain("pi.usage");
  });

  it("ignores non-assistant message_end events", () => {
    const state = createTraceMapperState();
    expect(mapPiEvent(userMessage(), state)).toEqual([]);
  });

  it("preserves tool call ids on tool start and completion", () => {
    const state = createTraceMapperState();
    const started = mapPiEvent(toolStart("call_1", "mcp__browser_harness__click"), state);
    const completed = mapPiEvent(
      toolEnd("call_1", "mcp__browser_harness__click", { structuredContent: { ok: true } }),
      state,
    );

    expect(started).toEqual([
      {
        type: "pi.tool.started",
        payload: { tool_call_id: "call_1", tool_name: "mcp__browser_harness__click" },
      },
    ]);
    expect(completed[0]).toEqual({
      type: "pi.tool.completed",
      payload: {
        tool_call_id: "call_1",
        tool_name: "mcp__browser_harness__click",
        is_error: false,
      },
    });
  });

  it("derives a compact browser observation from MCP structured content", () => {
    const state = createTraceMapperState();
    const page = { url: "https://a.test", title: "A", elements: [] };
    const events = mapPiEvent(
      toolEnd("call_1", "mcp__browser_harness__open_page", {
        structuredContent: { ok: true, page },
      }),
      state,
    );

    expect(events.map((e) => e.type)).toEqual([
      "pi.tool.completed",
      "browser.observation",
    ]);
    expect(events[1]?.payload).toEqual({ observation: page });
  });

  it("derives objective finish evidence from finish_case structured content", () => {
    const state = createTraceMapperState();
    const caseArtifact = { case_version: "loop.case.v1", steps: [] };
    const events = mapPiEvent(
      toolEnd("call_2", "mcp__browser_harness__finish_case", {
        structuredContent: {
          ok: true,
          case: caseArtifact,
          dry_run: { status: "passed", execution_id: "exec_1" },
        },
      }),
      state,
    );

    expect(events.map((e) => e.type)).toEqual([
      "pi.tool.completed",
      "browser.case.completed",
    ]);
    expect(events[1]?.payload).toEqual({ verdict: "passed" });
    expect(state.browser_case_verdicts).toEqual(["passed"]);
  });

  it("records a failed dry run as a failed browser case verdict", () => {
    const state = createTraceMapperState();
    mapPiEvent(
      toolEnd("call_2", "mcp__browser_harness__finish_case", {
        structuredContent: {
          ok: false,
          case: { case_version: "loop.case.v1", steps: [] },
          dry_run: { status: "failed", execution_id: "exec_2" },
        },
      }),
      state,
    );

    expect(state.browser_case_verdicts).toEqual(["failed"]);
  });

  it("emits harness.settled only on agent_settled with the last assistant text", () => {
    const state = createTraceMapperState();
    mapPiEvent(assistantMessage("final answer"), state);

    const events = mapPiEvent(settled, state);

    expect(events).toEqual([
      {
        type: "harness.settled",
        payload: { assistant_text: "final answer" },
      },
    ]);
  });

  it("harness.settled carries an empty assistant text when no message completed", () => {
    const state = createTraceMapperState();
    const events = mapPiEvent(settled, state);

    expect(events[0]?.payload).toEqual({ assistant_text: "" });
  });
});
