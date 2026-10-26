import type { AgentSessionEvent } from "@earendil-works/pi-coding-agent";
import type { AssistantMessage, TextContent } from "@earendil-works/pi-ai";

import type { HarnessEventType, HarnessEventPayloads } from "./run-protocol.js";

/**
 * Mutable state carried across Pi events so the mapper can derive settlement
 * verdicts and avoid emitting usage more than once per assistant message.
 */
export interface TraceMapperState {
  browser_case_verdicts: ("passed" | "failed")[];
  last_assistant_text: string;
  /** Message ids whose usage has already been emitted. */
  emitted_usage: Set<string>;
}

export function createTraceMapperState(): TraceMapperState {
  return {
    browser_case_verdicts: [],
    last_assistant_text: "",
    emitted_usage: new Set(),
  };
}

/** A mapped harness event discriminant and its payload. */
export interface MappedEvent<Type extends HarnessEventType = HarnessEventType> {
  type: Type;
  payload: HarnessEventPayloads[Type];
}

/**
 * Maps a single Pi agent-session event to zero or more harness events.
 *
 * The mapper is pure with respect to `state`: it reads and mutates `state`
 * (tracking browser case verdicts, the last assistant text, and emitted usage
 * ids) but performs no I/O. The caller writes the returned events through a
 * `HarnessEventWriter`.
 */
export function mapPiEvent(
  event: AgentSessionEvent,
  state: TraceMapperState,
): MappedEvent[] {
  switch (event.type) {
    case "message_end": {
      const message = event.message;
      if (!isAssistantMessage(message)) {
        return [];
      }
      const events: MappedEvent[] = [];
      const text = extractAssistantText(message);
      state.last_assistant_text = text;
      events.push({
        type: "pi.message.completed",
        payload: { text },
      });
      if (message.usage !== undefined) {
        const messageId = messageKey(message);
        if (!state.emitted_usage.has(messageId)) {
          state.emitted_usage.add(messageId);
          events.push({
            type: "pi.usage",
            payload: {
              input_tokens: message.usage.input,
              output_tokens: message.usage.output,
            },
          });
        }
      }
      return events;
    }

    case "tool_execution_start": {
      return [
        {
          type: "pi.tool.started",
          payload: {
            tool_call_id: event.toolCallId,
            tool_name: event.toolName,
          },
        },
      ];
    }

    case "tool_execution_end": {
      const events: MappedEvent[] = [
        {
          type: "pi.tool.completed",
          payload: {
            tool_call_id: event.toolCallId,
            tool_name: event.toolName,
            is_error: Boolean(event.isError),
          },
        },
      ];
      const structured = extractStructuredContent(event.result);
      if (structured !== undefined) {
        const observation = structured.page;
        if (observation !== undefined) {
          events.push({
            type: "browser.observation",
            payload: { observation },
          });
        }
        if (
          structured.case !== undefined &&
          structured.dry_run !== undefined
        ) {
          const dryRun = structured.dry_run as {
            status?: "passed" | "failed" | "error";
          };
          const verdict: "passed" | "failed" =
            dryRun.status === "passed" ? "passed" : "failed";
          state.browser_case_verdicts.push(verdict);
          events.push({
            type: "browser.case.completed",
            payload: { verdict },
          });
        }
      }
      return events;
    }

    case "agent_settled": {
      return [
        {
          type: "harness.settled",
          payload: { assistant_text: state.last_assistant_text },
        },
      ];
    }

    default:
      return [];
  }
}

function isAssistantMessage(
  message: unknown,
): message is AssistantMessage {
  return (
    typeof message === "object" &&
    message !== null &&
    (message as { role?: string }).role === "assistant"
  );
}

function extractAssistantText(message: AssistantMessage): string {
  const parts: string[] = [];
  for (const item of message.content) {
    if (isTextContent(item)) {
      parts.push(item.text);
    }
  }
  return parts.join("");
}

function isTextContent(item: unknown): item is TextContent {
  return (
    typeof item === "object" &&
    item !== null &&
    (item as { type?: string }).type === "text"
  );
}

function messageKey(message: AssistantMessage): string {
  const content = message.content;
  if (content.length > 0) {
    const first = content[0];
    if (first !== undefined && "text" in first && typeof first.text === "string") {
      return first.text;
    }
  }
  return String(message.timestamp);
}

/**
 * Extracts the MCP tool structured content from a Pi tool result. Pi wraps MCP
 * results in a `ToolResultMessage`-shaped object; the structured content is the
 * model-facing observation plus any case/dry-run artifacts from `finish_case`.
 */
function extractStructuredContent(
  result: unknown,
): HarnessStructuredContent | undefined {
  if (typeof result !== "object" || result === null) {
    return undefined;
  }
  const structured = (result as { structuredContent?: unknown })
    .structuredContent;
  if (typeof structured !== "object" || structured === null) {
    return undefined;
  }
  return structured as HarnessStructuredContent;
}

interface HarnessStructuredContent {
  page?: unknown;
  case?: unknown;
  dry_run?: unknown;
}
