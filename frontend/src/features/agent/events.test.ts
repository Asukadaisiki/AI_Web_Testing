import { describe, expect, it } from "vitest";

import { readToolActivities } from "./events";
import type {
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
      event("run.cancelled", 3),
      event("tool.finished", 4),
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
});
