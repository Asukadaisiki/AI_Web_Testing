import { describe, expect, it } from "vitest";

import { readLatestTaskPlan, readToolActivities } from "./events";
import type {
  AgentPipelineTracePayloadV1,
  AgentEvent,
  ResearchLLMCallPayloadV1,
} from "./types";

function event(type: AgentEvent["type"], seq: number): AgentEvent {
  return {
    seq,
    type,
    conversation_id: "conversation-1",
    run_id: "run-1",
    tool_call_id: "tool-1",
    timestamp: "2026-09-06T00:00:00Z",
    payload: { tool: "explore_page" },
  };
}

describe("readToolActivities", () => {
  it("does not mix research or cancellation events into tool activities", () => {
    const activities = readToolActivities([
      event("tool.started", 1),
      event("research.llm_call", 2),
      event("agent.pipeline.trace", 3),
      event("run.cancelled", 4),
      event("tool.finished", 5),
    ]);

    expect(activities).toHaveLength(1);
    expect(activities[0]).toMatchObject({
      id: "tool-1",
      name: "explore_page",
      status: "completed",
    });
  });

  it("types available and unavailable research tool associations explicitly", () => {
    const available = {
      schema_version: "research.llm_call.v1",
      logical_call_id: "llm-1",
      attempt: 1,
      attempt_status: "succeeded",
      tool_call_status: "available",
      tool_call_ids: ["tool-1", "tool-2"],
    } satisfies ResearchLLMCallPayloadV1;
    const unavailable = {
      schema_version: "research.llm_call.v1",
      logical_call_id: "llm-2",
      attempt: 1,
      attempt_status: "succeeded",
      tool_call_status: "unavailable",
      tool_call_unavailable_reason: "model_returned_final_text",
    } satisfies ResearchLLMCallPayloadV1;

    expect(available.tool_call_ids).toEqual(["tool-1", "tool-2"]);
    expect(unavailable.tool_call_unavailable_reason).toBe(
      "model_returned_final_text",
    );
  });

  it("types pipeline diagnostics without mixing them into tool activity", () => {
    const payload = {
      schema_version: "agent.pipeline.trace.v1",
      kind: "tool_call",
      state_epoch: "a".repeat(64),
    } satisfies AgentPipelineTracePayloadV1;

    expect(payload.kind).toBe("tool_call");
  });

  it("reads the latest persisted task plan snapshot", () => {
    const first = event("task_plan.updated", 2);
    first.payload = {
      schema_version: "agent.task_plan.v1",
      plan_id: "plan-1",
      version: 1,
      plan_sha256: "a".repeat(64),
      status: "grounding",
      steps: [],
    };
    const latest = event("task_plan.updated", 4);
    latest.payload = {
      schema_version: "agent.task_plan.v1",
      plan_id: "plan-1",
      version: 1,
      plan_sha256: "a".repeat(64),
      status: "ready_for_generation",
      steps: [],
    };

    expect(readLatestTaskPlan([first, latest])?.status).toBe(
      "ready_for_generation",
    );
  });
});
