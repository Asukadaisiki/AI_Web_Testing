import { Writable } from "node:stream";

import { describe, expect, it } from "vitest";

import {
  HarnessEventWriter,
  deriveTerminalVerdict,
  parseRunRequest,
  type HarnessEventPayloads,
} from "../src/run-protocol.js";

const request = {
  protocol_version: "pi-browser-harness.v1",
  run_id: "run_1",
  attempt_id: "attempt_1",
  goal: "verify the cart",
  model: {
    provider: "scripted",
    name: "browser-fixture",
    reasoning_effort: "high",
  },
  worker_url: "http://worker:8100",
  artifact_scope: "run_1",
};

describe("parseRunRequest", () => {
  it("accepts the strict v1 request", () => {
    expect(parseRunRequest(JSON.stringify(request))).toEqual(request);
  });

  it.each([
    ["unknown fields", { ...request, unexpected: true }],
    ["blank run IDs", { ...request, run_id: " " }],
    ["blank attempt IDs", { ...request, attempt_id: "" }],
    ["relative worker URLs", { ...request, worker_url: "/worker" }],
    [
      "unsupported protocol versions",
      { ...request, protocol_version: "pi-browser-harness.v2" },
    ],
  ])("rejects %s", (_case, invalidRequest) => {
    expect(() => parseRunRequest(JSON.stringify(invalidRequest))).toThrow();
  });
});

describe("HarnessEventWriter", () => {
  it("writes identified UTC records with monotonic sequences", () => {
    const chunks: string[] = [];
    const output = new Writable({
      write(chunk, _encoding, callback) {
        chunks.push(chunk.toString());
        callback();
      },
    });
    const parsed = parseRunRequest(JSON.stringify(request));
    const writer = new HarnessEventWriter(
      parsed,
      output,
      () => new Date("2026-10-26T12:34:56.789Z"),
    );

    writer.write("harness.started", {});
    writer.write("pi.message.completed", { text: "cart verified" });
    writer.write("harness.settled", { assistant_text: "cart verified" });

    const records = chunks
      .join("")
      .trimEnd()
      .split("\n")
      .map((line) => JSON.parse(line));

    expect(records).toEqual([
      {
        type: "harness.started",
        run_id: "run_1",
        attempt_id: "attempt_1",
        sequence: 1,
        timestamp: "2026-10-26T12:34:56.789Z",
        payload: {},
      },
      {
        type: "pi.message.completed",
        run_id: "run_1",
        attempt_id: "attempt_1",
        sequence: 2,
        timestamp: "2026-10-26T12:34:56.789Z",
        payload: { text: "cart verified" },
      },
      {
        type: "harness.settled",
        run_id: "run_1",
        attempt_id: "attempt_1",
        sequence: 3,
        timestamp: "2026-10-26T12:34:56.789Z",
        payload: { assistant_text: "cart verified" },
      },
    ]);
  });

  it("defines every protocol event discriminant", () => {
    const payloads = {
      "harness.started": {},
      "pi.message.completed": { text: "done" },
      "pi.usage": { input_tokens: 1, output_tokens: 2 },
      "pi.tool.started": { tool_call_id: "tool_1", tool_name: "navigate" },
      "pi.tool.completed": {
        tool_call_id: "tool_1",
        tool_name: "navigate",
        is_error: false,
      },
      "browser.observation": { observation: {} },
      "browser.case.completed": { verdict: "passed" },
      "harness.settled": { assistant_text: "done" },
      "harness.failed": { error: "failed" },
    } satisfies HarnessEventPayloads;

    expect(Object.keys(payloads)).toEqual([
      "harness.started",
      "pi.message.completed",
      "pi.usage",
      "pi.tool.started",
      "pi.tool.completed",
      "browser.observation",
      "browser.case.completed",
      "harness.settled",
      "harness.failed",
    ]);
  });
});

describe("deriveTerminalVerdict", () => {
  it("fails assistant settlement without prior passing browser evidence", () => {
    expect(
      deriveTerminalVerdict({
        assistant_settled: true,
        browser_case_verdicts: [],
      }),
    ).toBe("failed");
    expect(
      deriveTerminalVerdict({
        assistant_settled: true,
        browser_case_verdicts: ["failed"],
      }),
    ).toBe("failed");
  });

  it("succeeds only with assistant settlement and passing browser evidence", () => {
    expect(
      deriveTerminalVerdict({
        assistant_settled: true,
        browser_case_verdicts: ["passed"],
      }),
    ).toBe("succeeded");
    expect(
      deriveTerminalVerdict({
        assistant_settled: false,
        browser_case_verdicts: ["passed"],
      }),
    ).toBe("failed");
  });
});
