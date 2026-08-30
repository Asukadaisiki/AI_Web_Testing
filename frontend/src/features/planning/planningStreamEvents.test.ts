import { describe, expect, it } from "vitest";

import {
  applyContentBlockEvent,
  createOptimisticMessage,
  readContentBlocks,
  reduceTranscriptEvent,
} from "./planningStreamEvents";
import type { ExecutionStreamEvent } from "../../types/api";

describe("applyContentBlockEvent", () => {
  it("builds ordered thinking and text blocks with content_index", () => {
    let blocks = applyContentBlockEvent([], {
      type: "content_block_start",
      content_index: 0,
      kind: "thinking",
    } satisfies ExecutionStreamEvent);
    blocks = applyContentBlockEvent(blocks, {
      type: "content_block_delta",
      content_index: 0,
      kind: "thinking",
      delta: "先想",
    });
    blocks = applyContentBlockEvent(blocks, {
      type: "content_block_start",
      content_index: 1,
      kind: "text",
    } satisfies ExecutionStreamEvent);
    blocks = applyContentBlockEvent(blocks, {
      type: "content_block_delta",
      content_index: 1,
      kind: "text",
      delta: "你好",
    });
    blocks = applyContentBlockEvent(blocks, {
      type: "content_block_end",
      content_index: 1,
      kind: "text",
      content: "你好，世界",
    });

    expect(blocks).toEqual([
      { type: "thinking", content: "先想" },
      { type: "text", content: "你好，世界" },
    ]);
  });

  it("resets a block when its kind changes on start", () => {
    const blocks = applyContentBlockEvent(
      [{ type: "text", content: "旧文本" }],
      {
        type: "content_block_start",
        content_index: 0,
        kind: "thinking",
      } satisfies ExecutionStreamEvent,
    );
    expect(blocks).toEqual([{ type: "thinking", content: "" }]);
  });
});

describe("readContentBlocks", () => {
  it("reads persisted content_blocks", () => {
    const blocks = readContentBlocks({
      content_blocks: [
        { type: "thinking", content: "思考" },
        { type: "text", content: "正文" },
      ],
    });
    expect(blocks).toEqual([
      { type: "thinking", content: "思考" },
      { type: "text", content: "正文" },
    ]);
  });

  it("falls back to legacy _thinkingContent", () => {
    const blocks = readContentBlocks({ _thinkingContent: "旧思考" });
    expect(blocks).toEqual([{ type: "thinking", content: "旧思考" }]);
  });
});

describe("reduceTranscriptEvent", () => {
  it("applies content_block events to the active message only", () => {
    const assistant = createOptimisticMessage(5, "assistant", "followup", "", {
      _streaming: true,
    });
    const other = createOptimisticMessage(5, "assistant", "followup", "别的消息");

    let transcript = reduceTranscriptEvent(
      [other, assistant],
      {
        type: "content_block_start",
        content_index: 0,
        kind: "text",
      } satisfies ExecutionStreamEvent,
      assistant.id,
    );
    transcript = reduceTranscriptEvent(transcript, {
      type: "content_block_delta",
      content_index: 0,
      kind: "text",
      delta: "你好",
    }, assistant.id);

    const target = transcript.find((item) => item.id === assistant.id);
    expect(target?.content).toBe("你好");
    expect(readContentBlocks(target?.structured_payload)).toEqual([
      { type: "text", content: "你好" },
    ]);
    expect(transcript.find((item) => item.id === other.id)?.content).toBe("别的消息");
  });

  it("does not overwrite streamed text on turn_complete when blocks exist", () => {
    const assistant = createOptimisticMessage(5, "assistant", "followup", "流式正文", {
      _streaming: true,
      content_blocks: [{ type: "text", content: "流式正文" }],
    });
    const transcript = reduceTranscriptEvent(
      [assistant],
      {
        type: "turn_complete",
        session_status: "plan_ready",
        payload: {
          assistant_message: "最终正文",
          missing_slots: [],
          suggested_questions: [],
          plan: null,
          tool_calls: [],
          todo_list: [],
        },
      },
      assistant.id,
    );
    expect(transcript[0].content).toBe("流式正文");
  });

  it("uses assistant_message on turn_complete when no content blocks exist", () => {
    const assistant = createOptimisticMessage(5, "assistant", "followup", "", {
      _streaming: true,
    });
    const transcript = reduceTranscriptEvent(
      [assistant],
      {
        type: "turn_complete",
        session_status: "plan_ready",
        payload: {
          assistant_message: "最终正文",
          missing_slots: [],
          suggested_questions: [],
          plan: null,
          tool_calls: [],
          todo_list: [],
        },
      },
      assistant.id,
    );
    expect(transcript[0].content).toBe("最终正文");
  });
});
