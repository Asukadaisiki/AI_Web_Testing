import { readBrowserEvidence, screenshotPathsForRun } from "./trace-evidence";
import type { AgentTrace } from "./contracts";

const time = "2026-10-28T00:00:00Z";
const taskCase = {
  case_version: "loop.case.v1",
  name: "Cart",
  goal: "Open cart",
  base_url: "https://shop.test",
  steps: [],
};
const dryRun = {
  execution_id: "execution_1",
  status: "passed",
  started_at: time,
  finished_at: time,
  final_url: "https://shop.test/cart",
  steps: [],
  error: null,
};
const trace: AgentTrace = {
  spans: [
    {
      id: "span_1",
      span_id: "span_1",
      trace_id: "trace_1",
      run_id: "run_1",
      kind: "operation",
      name: "mcp__browser_harness__finish_case",
      status: "succeeded",
      input: { case: taskCase },
      started_at: time,
      created_at: time,
      updated_at: time,
    },
  ],
  operation_evidence: [
    {
      id: "ev_1",
      span_id: "span_1",
      operation_id: "op_1",
      status: "passed",
      result: dryRun,
      artifacts: {
        screenshot_paths: [
          "run_1/shot.png",
          "other/shot.png",
          "run_1/../other.png",
          "run_1/shot.png",
        ],
      },
      created_at: time,
      updated_at: time,
    },
  ],
  attempts: [],
  usage: [],
  derived_nodes: [],
};

describe("canonical browser evidence", () => {
  it("projects only canonical operation Case input and dry-run results", () => {
    expect(readBrowserEvidence(trace).cases).toEqual([
      { id: "span_1", case: taskCase },
    ]);
    expect(readBrowserEvidence(trace).dryRuns).toEqual([
      { id: "ev_1", dryRun },
    ]);
  });
  it("does not fabricate missing evidence from model output or assistant text", () => {
    const missing = {
      ...trace,
      spans: [
        {
          ...trace.spans[0]!,
          kind: "model",
          output: { case: taskCase, dry_run: dryRun },
          input: undefined,
        },
      ],
      operation_evidence: [],
    };
    expect(readBrowserEvidence(missing)).toMatchObject({
      cases: [],
      dryRuns: [],
      operations: [],
    });
  });
  it("requires exact persisted screenshot references and the Run namespace", () => {
    expect(screenshotPathsForRun(trace, "run_1")).toEqual(["run_1/shot.png"]);
    expect(
      screenshotPathsForRun(
        {
          ...trace,
          operation_evidence: [
            {
              ...trace.operation_evidence[0]!,
              artifacts: null,
              result: { screenshot_paths: ["run_1/unlisted.png"] },
            },
          ],
        },
        "run_1",
      ),
    ).toEqual([]);
  });
});
