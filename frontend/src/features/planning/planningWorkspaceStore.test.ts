import { beforeEach, describe, expect, it, vi } from "vitest";

import { PlanningWorkspaceStore } from "./planningWorkspaceStore";
import * as api from "./api";
import type { AIPlanningSessionDetail } from "../../types/api";

vi.mock("./api", () => ({
  createPlanningSession: vi.fn(),
  deletePlanningSession: vi.fn(),
  getPlanningSession: vi.fn(),
  getSessionEvents: vi.fn(),
  listPlanningSessions: vi.fn(),
}));

function detailWithStreamingMessage(): AIPlanningSessionDetail {
  return {
    session: {
      id: 5,
      actor_user_id: 1,
      projects: [],
      case_id: null,
      title: "会话",
      status: "collecting",
      requirements: {
        app_under_test: null,
        business_goal: null,
        entry_url_or_page: null,
        core_user_flow: null,
        main_assertions: [],
        test_data_or_account: null,
        scope_limits: null,
      },
      plan: null,
      missing_slots: [],
      last_error_message: null,
      created_at: "2026-04-12T10:00:00",
      updated_at: "2026-04-12T10:00:00",
    },
    messages: [
      {
        id: 2,
        session_id: 5,
        role: "assistant",
        turn_type: "streaming",
        content: "",
        structured_payload: { _streaming: true },
        created_at: "2026-04-12T10:01:00",
      },
    ],
    drafts: [],
  };
}

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(api.listPlanningSessions).mockResolvedValue([]);
});

describe("PlanningWorkspaceStore", () => {
  it("preserves transcript while an active stream exists", async () => {
    const store = new PlanningWorkspaceStore();
    store.setSessionTranscript(5, [
      {
        id: 100,
        session_id: 5,
        role: "assistant",
        turn_type: "followup",
        content: "流式内容",
        structured_payload: { _streaming: true },
        created_at: "2026-04-12T10:01:00",
      },
    ]);
    store.beginStream(5, "chat", 100);
    vi.mocked(api.getPlanningSession).mockResolvedValue({
      ...detailWithStreamingMessage(),
      session: {
        ...detailWithStreamingMessage().session,
        plan: {
          summary: "方案",
          assumptions: [],
          risks: [],
          scenarios: [],
        },
      },
    });

    await store.loadSessionDetail(5);

    const state = store.getSnapshot().sessions[5];
    expect(state.transcript).toHaveLength(1);
    expect(state.transcript[0].content).toBe("流式内容");
    expect(state.plan?.summary).toBe("方案");
  });

  it("replays content_block events when no active stream exists", async () => {
    const store = new PlanningWorkspaceStore();
    vi.mocked(api.getPlanningSession).mockResolvedValue(
      detailWithStreamingMessage(),
    );
    vi.mocked(api.getSessionEvents).mockResolvedValue([
      {
        seq: 1,
        event_type: "content_block_start",
        event_data: {
          type: "content_block_start",
          content_index: 0,
          kind: "text",
        },
        message_id: 2,
        created_at: "2026-04-12T10:01:00",
      },
      {
        seq: 2,
        event_type: "content_block_delta",
        event_data: {
          type: "content_block_delta",
          content_index: 0,
          kind: "text",
          delta: "你好，世界",
        },
        message_id: 2,
        created_at: "2026-04-12T10:01:01",
      },
      {
        seq: 3,
        event_type: "content_block_end",
        event_data: {
          type: "content_block_end",
          content_index: 0,
          kind: "text",
          content: "你好，世界",
        },
        message_id: 2,
        created_at: "2026-04-12T10:01:02",
      },
    ]);

    await store.loadSessionDetail(5);

    const state = store.getSnapshot().sessions[5];
    expect(state.transcript[0].content).toBe("你好，世界");
    expect(state.transcript[0].structured_payload).toMatchObject({
      _streaming: true,
      _interrupted: false,
      _recovered: true,
    });
  });

  it("dispatches stream events to the active message", () => {
    const store = new PlanningWorkspaceStore();
    store.setSessionTranscript(5, [
      {
        id: 100,
        session_id: 5,
        role: "assistant",
        turn_type: "followup",
        content: "",
        structured_payload: { _streaming: true },
        created_at: "2026-04-12T10:01:00",
      },
    ]);
    store.beginStream(5, "chat", 100);

    store.dispatchStreamEvent(5, {
      type: "content_block_delta",
      content_index: 0,
      kind: "text",
      delta: "增量",
    });

    const state = store.getSnapshot().sessions[5];
    expect(state.transcript[0].content).toBe("增量");
  });
});
