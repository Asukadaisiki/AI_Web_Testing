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

function browserMcpResult(tool: string, structuredContent: unknown) {
  return {
    content: [], details: { server: "browser-harness", tool },
    structuredContent: { content: [], structuredContent },
  };
}

const settled: AgentSessionEvent = {
  type: "agent_settled",
  aborted: false,
} as unknown as AgentSessionEvent;

const completeCase = {
  case_version: "loop.case.v1",
  name: "open example",
  goal: "open the example page",
  base_url: "https://example.test",
  steps: [
    {
      index: 0,
      action: "goto",
      intent: "open the example page",
      value: "https://example.test",
      submit: false,
      preconditions: [],
      postconditions: [
        { type: "url_contains", value: "example.test", timeout_ms: 1000 },
      ],
      timeout_ms: 1000,
    },
  ],
};

function completeDryRun(status: "passed" | "failed" | "error") {
  return {
    execution_id: `execution-${status}`,
    status,
    started_at: "2026-10-26T12:00:00.000Z",
    finished_at: "2026-10-26T12:00:01.000Z",
    final_url: "https://example.test",
    steps: [
      {
        index: 0,
        action: "goto",
        status: status === "passed" ? "passed" : "failed",
        started_at: "2026-10-26T12:00:00.000Z",
        duration_ms: 10,
        url_before: "about:blank",
        url_after: "https://example.test",
        conditions: [],
        evidence: { screenshot_path: null, console: [], network: [] },
        error: null,
      },
    ],
    error: null,
  };
}

describe("mapPiEvent", () => {
  it("maps an authoritative assistant message to pi.message.completed and pi.usage", () => {
    const state = createTraceMapperState();
    const events = mapPiEvent(assistantMessage("cart verified"), state);

    expect(events.map((e) => e.type)).toEqual([
      "pi.message.completed",
      "pi.usage",
    ]);
    expect(events[0]?.payload).toEqual({
      text: "cart verified", provider: "scripted", model: "browser-fixture",
    });
    expect(events[1]?.payload).toEqual({
      provider: "scripted", model: "browser-fixture",
      input_tokens: 10,
      output_tokens: 5,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
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

  it("carries a recoverable browser MCP failure as bounded completion evidence", () => {
    const state = createTraceMapperState();
    const structured = {
      ok: false,
      error: "condition_unmet",
      detail: "the text \"DoesNotExist\" is not present",
      page: {
        url: "https://shop.test/",
        title: "Shop",
        screenshot_path: "run_1/obs_failed.png",
      },
      steps: [{ index: 0, action: "goto", intent: "open shop" }],
    };

    const events = mapPiEvent(
      toolEnd("call_failed", "mcp__browser_harness__assert_text", {
        content: [{ type: "text", text: "failed" }],
        details: { server: "browser-harness", tool: "assert_text" },
        structuredContent: {
          content: [{ type: "text", text: "failed" }],
          structuredContent: structured,
        },
      }),
      state,
    );

    expect(events[0]).toEqual({
      type: "pi.tool.completed",
      payload: {
        tool_call_id: "call_failed",
        tool_name: "mcp__browser_harness__assert_text",
        is_error: false,
        structured_result: structured,
      },
    });
  });

  it("derives a compact browser observation from MCP structured content", () => {
    const state = createTraceMapperState();
    const page = { url: "https://a.test", title: "A", elements: [] };
    const events = mapPiEvent(
      toolEnd("call_1", "mcp__browser_harness__open_page", browserMcpResult("open_page", { ok: true, page })),
      state,
    );

    expect(events.map((e) => e.type)).toEqual([
      "pi.tool.completed",
      "browser.observation",
    ]);
    expect(events[1]?.payload).toEqual({
      tool_call_id: "call_1",
      observation: page,
    });
  });

  it("derives a browser observation from the Pi MCP adapter result envelope", () => {
    const state = createTraceMapperState();
    const page = { url: "https://nested.test", title: "Nested", elements: [] };
    const events = mapPiEvent(
      toolEnd("call_1", "mcp__browser_harness__open_page", {
        content: [{ type: "text", text: "Opened page" }],
        details: { server: "browser-harness", tool: "open_page" },
        structuredContent: {
          content: [{ type: "text", text: "Opened page" }],
          structuredContent: { ok: true, page },
        },
      }),
      state,
    );

    expect(events.map((e) => e.type)).toEqual([
      "pi.tool.completed",
      "browser.observation",
    ]);
    expect(events[1]?.payload).toEqual({
      tool_call_id: "call_1",
      observation: page,
    });
  });

  it("unwraps only the exact browser-harness Pi MCP adapter envelope", () => {
    const directPage = { url: "https://direct.test", title: "Direct", elements: [] };
    const nestedPage = { url: "https://nested.test", title: "Nested", elements: [] };
    const baseResult = {
      content: [{ type: "text", text: "Opened page" }],
      details: { server: "browser-harness", tool: "open_page" },
      structuredContent: {
        content: [{ type: "text", text: "Opened page" }],
        page: directPage,
        structuredContent: { ok: true, page: nestedPage },
      },
    };
    const cases = [
      {
        name: "missing outer content",
        result: { ...baseResult, content: undefined },
      },
      {
        name: "wrong MCP server",
        result: {
          ...baseResult,
          details: { server: "other-server", tool: "open_page" },
        },
      },
      {
        name: "wrong MCP tool detail",
        result: {
          ...baseResult,
          details: { server: "browser-harness", tool: "click" },
        },
      },
      {
        name: "missing inner MCP content",
        result: {
          ...baseResult,
          structuredContent: {
            page: directPage,
            structuredContent: { ok: true, page: nestedPage },
          },
        },
      },
    ];

    for (const testCase of cases) {
      const events = mapPiEvent(
        toolEnd("call_1", "mcp__browser_harness__open_page", testCase.result),
        createTraceMapperState(),
      );

      expect(
        events.find((event) => event.type === "browser.observation")?.payload,
        testCase.name,
      ).toBeUndefined();
    }
  });

  it("rejects ambiguous direct result data as browser evidence", () => {
    const state = createTraceMapperState();
    const page = { url: "https://direct.test", title: "Direct", elements: [] };
    const events = mapPiEvent(
      toolEnd("call_1", "mcp__browser_harness__open_page", {
        structuredContent: {
          ok: true,
          page,
          structuredContent: { kind: "domain-data" },
        },
      }),
      state,
    );

    expect(events.map((event) => event.type)).toEqual(["pi.tool.completed"]);
  });

  it("derives objective finish evidence from finish_case structured content", () => {
    const state = createTraceMapperState();
    const caseArtifact = completeCase;
    const dryRun = completeDryRun("passed");
    const events = mapPiEvent(
      toolEnd("call_2", "mcp__browser_harness__finish_case", browserMcpResult("finish_case", {
          ok: true,
          case: caseArtifact,
          dry_run: dryRun,
        })),
      state,
    );

    expect(events.map((e) => e.type)).toEqual([
      "pi.tool.completed",
      "browser.case.completed",
    ]);
    expect(events[1]?.payload).toEqual({
      verdict: "passed",
      case: caseArtifact,
      dry_run: dryRun,
    });
    expect(state.browser_case_verdicts).toEqual(["passed"]);
  });

  it("derives objective finish evidence from the Pi MCP adapter result envelope", () => {
    const state = createTraceMapperState();
    const caseArtifact = completeCase;
    const dryRun = completeDryRun("passed");
    const events = mapPiEvent(
      toolEnd("call_2", "mcp__browser_harness__finish_case", {
        content: [{ type: "text", text: "Finished case" }],
        details: { server: "browser-harness", tool: "finish_case" },
        structuredContent: {
          content: [{ type: "text", text: "Finished case" }],
          structuredContent: {
            ok: true,
            case: caseArtifact,
            dry_run: dryRun,
          },
        },
      }),
      state,
    );

    expect(events.map((e) => e.type)).toEqual([
      "pi.tool.completed",
      "browser.case.completed",
    ]);
    expect(events[1]?.payload).toEqual({
      verdict: "passed",
      case: caseArtifact,
      dry_run: dryRun,
    });
    expect(state.browser_case_verdicts).toEqual(["passed"]);
  });

  it("ignores passing finish evidence from any other tool", () => {
    const state = createTraceMapperState();
    const events = mapPiEvent(
      toolEnd("call_2", "mcp__browser_harness__click", browserMcpResult("click", {
          ok: true,
          case: { case_version: "loop.case.v1", steps: [] },
          dry_run: { status: "passed", execution_id: "exec_wrong_tool" },
        })),
      state,
    );

    expect(events.map((event) => event.type)).toEqual(["pi.tool.completed"]);
    expect(state.browser_case_verdicts).toEqual([]);
  });

  it.each([
    {
      name: "missing case",
      caseValue: undefined,
      dryRunValue: { status: "passed", execution_id: "exec_valid" },
    },
    {
      name: "null case",
      caseValue: null,
      dryRunValue: { status: "passed", execution_id: "exec_valid" },
    },
    {
      name: "array case",
      caseValue: [],
      dryRunValue: { status: "passed", execution_id: "exec_valid" },
    },
    {
      name: "missing dry run",
      caseValue: { case_version: "loop.case.v1", steps: [] },
      dryRunValue: undefined,
    },
    {
      name: "null dry run",
      caseValue: { case_version: "loop.case.v1", steps: [] },
      dryRunValue: null,
    },
    {
      name: "array dry run",
      caseValue: { case_version: "loop.case.v1", steps: [] },
      dryRunValue: [],
    },
    {
      name: "invalid dry-run status",
      caseValue: { case_version: "loop.case.v1", steps: [] },
      dryRunValue: { status: "unknown", execution_id: "exec_invalid" },
    },
  ])(
    "requires valid object case and dry-run artifacts: $name",
    ({ caseValue, dryRunValue }) => {
      const state = createTraceMapperState();
      const events = mapPiEvent(
        toolEnd("call_2", "mcp__browser_harness__finish_case", browserMcpResult("finish_case", {
            ok: true,
            case: caseValue,
            dry_run: dryRunValue,
          })),
        state,
      );

      expect(events.map((event) => event.type)).toEqual([
        "pi.tool.completed",
      ]);
      expect(state.browser_case_verdicts).toEqual([]);
    },
  );

  it("records a failed dry run as a failed browser case verdict", () => {
    const state = createTraceMapperState();
    mapPiEvent(
      toolEnd("call_2", "mcp__browser_harness__finish_case", browserMcpResult("finish_case", {
          ok: false,
          case: completeCase,
          dry_run: completeDryRun("failed"),
        })),
      state,
    );

    expect(state.browser_case_verdicts).toEqual(["failed"]);
  });

  it.each(["failed", "error"] as const)(
    "records a nested %s dry run as a failed browser case verdict",
    (status) => {
      const state = createTraceMapperState();
      const caseArtifact = completeCase;
      const dryRun = completeDryRun(status);
      const events = mapPiEvent(
        toolEnd("call_2", "mcp__browser_harness__finish_case", {
          content: [{ type: "text", text: "Finished case" }],
          details: { server: "browser-harness", tool: "finish_case" },
          structuredContent: {
            content: [{ type: "text", text: "Finished case" }],
            structuredContent: {
              ok: false,
              case: caseArtifact,
              dry_run: dryRun,
            },
          },
        }),
        state,
      );

      expect(events[1]?.payload).toEqual({
        verdict: "failed",
        case: caseArtifact,
        dry_run: dryRun,
      });
      expect(state.browser_case_verdicts).toEqual(["failed"]);
    },
  );

  it("leaves settlement to the selected harness completion policy", () => {
    const state = createTraceMapperState();
    mapPiEvent(assistantMessage("final answer"), state);

    const events = mapPiEvent(settled, state);

    expect(events).toEqual([]);
  });

  it("does not synthesize settlement when no message completed", () => {
    const state = createTraceMapperState();
    const events = mapPiEvent(settled, state);

    expect(events).toEqual([]);
  });
});
