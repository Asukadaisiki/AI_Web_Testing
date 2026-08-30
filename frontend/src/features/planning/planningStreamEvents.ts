import type {
  AIPlanningMessage,
  AIPlanningRequirements,
  AssistantContentBlock,
  ExecutionStreamEvent,
} from "../../types/api";

export const DEFAULT_REQUIREMENTS: AIPlanningRequirements = {
  app_under_test: null,
  business_goal: null,
  entry_url_or_page: null,
  core_user_flow: null,
  main_assertions: [],
  test_data_or_account: null,
  scope_limits: null,
};

export function createOptimisticMessage(
  sessionId: number,
  role: AIPlanningMessage["role"],
  turnType: AIPlanningMessage["turn_type"],
  content: string,
  structuredPayload?: Record<string, unknown> | null,
): AIPlanningMessage {
  return {
    id: -Date.now() - Math.floor(Math.random() * 1000),
    session_id: sessionId,
    role,
    turn_type: turnType,
    content,
    structured_payload: structuredPayload ?? null,
    created_at: new Date().toISOString(),
  };
}

export function readContentBlocks(
  payload: Record<string, unknown> | null | undefined,
): AssistantContentBlock[] {
  const raw = payload?.content_blocks;
  if (Array.isArray(raw)) {
    return raw
      .filter(
        (block): block is Record<string, unknown> =>
          Boolean(block) && typeof block === "object",
      )
      .map((block) => ({
        type: block.type === "thinking" ? "thinking" : "text",
        content: String(block.content ?? ""),
      }));
  }
  // Legacy fallback: thinking block (if any) followed by the plain text mirror.
  const blocks: AssistantContentBlock[] = [];
  const thinking = payload?._thinkingContent;
  if (typeof thinking === "string" && thinking.length > 0) {
    blocks.push({ type: "thinking", content: thinking });
  }
  return blocks;
}

export function applyContentBlockEvent(
  blocks: AssistantContentBlock[],
  event: ExecutionStreamEvent,
): AssistantContentBlock[] {
  if (
    event.type !== "content_block_start" &&
    event.type !== "content_block_delta" &&
    event.type !== "content_block_end"
  ) {
    return blocks;
  }

  const next = [...blocks];
  while (next.length <= event.content_index) {
    next.push({ type: "text", content: "" });
  }

  const block = next[event.content_index];
  const kind: AssistantContentBlock["type"] =
    event.kind === "thinking" ? "thinking" : "text";

  if (event.type === "content_block_start") {
    next[event.content_index] = { type: kind, content: "" };
    return next;
  }

  if (event.type === "content_block_delta") {
    if (block.type !== kind) {
      next[event.content_index] = { type: kind, content: "" };
    }
    next[event.content_index] = {
      type: kind,
      content: block.content + event.delta,
    };
    return next;
  }

  // content_block_end
  next[event.content_index] = { type: kind, content: event.content };
  return next;
}

function applyStreamEventToContent(
  currentContent: string,
  event: ExecutionStreamEvent,
): string {
  switch (event.type) {
    case "save_progress":
      return `已保存 ${event.saved_count}/${event.total} 个用例…`;
    case "case_start":
      return `正在执行：${event.case_name}（${event.total_steps}步）`;
    case "step_start":
      return `步骤 ${event.step_index + 1}：${event.action}…`;
    case "step_complete":
      return `步骤 ${event.step_index + 1}：${event.action} — ${
        event.status === "passed" ? "✅" : "❌"
      }（${event.duration_ms}ms）`;
    default:
      return currentContent;
  }
}

function applyStreamEventToPayload(
  current: Record<string, unknown> | null,
  event: ExecutionStreamEvent,
): Record<string, unknown> {
  const base = current ?? {
    type: "execution_progress",
    saved_count: 0,
    total: 0,
    cases: [],
  };
  switch (event.type) {
    case "save_progress":
      return { ...base, saved_count: event.saved_count, total: event.total };
    case "case_start":
      return {
        ...base,
        cases: [
          ...((base.cases as unknown[]) ?? []),
          {
            case_id: event.case_id,
            case_name: event.case_name,
            total_steps: event.total_steps,
            steps: [],
          },
        ],
      };
    case "step_start":
    case "step_complete":
      return base;
    default:
      return base;
  }
}

function reduceMessage(
  message: AIPlanningMessage,
  event: ExecutionStreamEvent,
): AIPlanningMessage {
  const payload = (message.structured_payload ?? {}) as Record<string, unknown>;

  switch (event.type) {
    case "status":
      return {
        ...message,
        structured_payload: {
          ...payload,
          _phase: event.phase,
          _phaseMessage: event.message,
          _streaming: true,
        },
      };
    case "text_chunk": {
      if (event.thinking) {
        const prev = (payload._thinkingContent as string) ?? "";
        return {
          ...message,
          structured_payload: {
            ...payload,
            _thinkingContent: prev + event.text,
            _streaming: true,
          },
        };
      }
      return { ...message, content: message.content + event.text };
    }
    case "content_block_start":
    case "content_block_delta":
    case "content_block_end": {
      const nextBlocks = applyContentBlockEvent(readContentBlocks(payload), event);
      const textContent = nextBlocks
        .filter((block) => block.type === "text")
        .map((block) => block.content)
        .join("");
      return {
        ...message,
        content: textContent,
        structured_payload: {
          ...payload,
          content_blocks: nextBlocks,
          _streaming: true,
        },
      };
    }
    case "tool_call_start":
      return {
        ...message,
        structured_payload: {
          ...payload,
          _phase: "tool_calling",
          _phaseMessage: `正在调用工具: ${event.tool}`,
        },
      };
    case "tool_call_end":
      return {
        ...message,
        structured_payload: {
          ...payload,
          _phase: "thinking",
          _phaseMessage: "正在分析需求...",
        },
      };
    case "draft_generating":
      return {
        ...message,
        structured_payload: {
          ...payload,
          _phase: "draft_generating",
          _phaseMessage: event.message,
          _streaming: true,
        },
      };
    case "save_progress":
    case "case_start":
    case "step_start":
    case "step_complete":
      return {
        ...message,
        content: applyStreamEventToContent(message.content, event),
        structured_payload: applyStreamEventToPayload(
          message.structured_payload as Record<string, unknown> | null,
          event,
        ),
      };
    case "execution_summary":
      return {
        ...message,
        content: event.message,
        structured_payload: {
          ...payload,
          ...event.structured_payload,
          _streaming: false,
        },
      };
    case "turn_complete": {
      const contentBlocks = readContentBlocks(payload);
      return {
        ...message,
        content:
          contentBlocks.length > 0
            ? message.content
            : event.payload.assistant_message || message.content,
        structured_payload: {
          ...payload,
          _streaming: false,
          todo_list: event.payload.todo_list,
          missing_slots: event.payload.missing_slots,
          suggested_questions: event.payload.suggested_questions,
        },
      };
    }
    case "done":
    case "cancelled":
      return {
        ...message,
        structured_payload: { ...payload, _streaming: false },
      };
    case "error": {
      const phaseLabel = event.phase ? `[${event.phase}] ` : "";
      const errorTypeLabel = event.error_type ? ` (${event.error_type})` : "";
      const tracebackSection = event.traceback
        ? `\n\n<details><summary>错误追踪</summary>\n\n\`\`\`\n${event.traceback}\n\`\`\`\n</details>`
        : "";
      return {
        ...message,
        content: `❌ **${phaseLabel}错误${errorTypeLabel}**\n\n${event.message}${tracebackSection}`,
        structured_payload: {
          ...payload,
          _streaming: false,
          _phase: "error",
          _phaseMessage: event.message,
          error_type: event.error_type,
          error_phase: event.phase,
        },
      };
    }
    default:
      return message;
  }
}

/**
 * Apply one stream event to the message with ``activeMessageId``.
 *
 * Live SSE handling and refresh-time event replay both use this reducer so
 * they can never drift apart.
 */
export function reduceTranscriptEvent(
  transcript: AIPlanningMessage[],
  event: ExecutionStreamEvent,
  activeMessageId: number | null,
): AIPlanningMessage[] {
  if (activeMessageId == null) {
    return transcript;
  }
  return transcript.map((message) =>
    message.id === activeMessageId ? reduceMessage(message, event) : message,
  );
}
