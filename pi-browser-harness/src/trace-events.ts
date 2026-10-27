import type { AgentSessionEvent } from "@earendil-works/pi-coding-agent";
import type { AssistantMessage, TextContent } from "@earendil-works/pi-ai";

import type { HarnessEventType, HarnessEventPayloads } from "./run-protocol.js";

const BROWSER_HARNESS_TOOL_PREFIX = "mcp__browser_harness__";
const FINISH_CASE_TOOL = `${BROWSER_HARNESS_TOOL_PREFIX}finish_case`;

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
      const structured = extractStructuredContent(event.result, event.toolName);
      if (structured !== undefined) {
        const observation = structured.page;
        if (observation !== undefined) {
          events.push({
            type: "browser.observation",
            payload: { observation },
          });
        }
        if (
          event.toolName === FINISH_CASE_TOOL &&
          isRecord(structured.case) &&
          isDryRunArtifact(structured.dry_run)
        ) {
          const verdict: "passed" | "failed" =
            structured.dry_run.status === "passed" ? "passed" : "failed";
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
 * results in an `AgentToolResult`, whose structured content is the complete MCP
 * `CallToolResult`. Direct tool results retain the existing single-level shape.
 */
function extractStructuredContent(
  result: unknown,
  toolName: string,
): HarnessStructuredContent | undefined {
  if (!isRecord(result)) {
    return undefined;
  }
  const structured = result.structuredContent;
  if (!isRecord(structured)) {
    return undefined;
  }
  if (isBrowserHarnessMcpEnvelope(result, structured, toolName)) {
    const mcpStructured = structured.structuredContent;
    if (!isRecord(mcpStructured)) {
      return undefined;
    }
    return mcpStructured as HarnessStructuredContent;
  }
  return structured as HarnessStructuredContent;
}

function isBrowserHarnessMcpEnvelope(
  result: Record<string, unknown>,
  structured: Record<string, unknown>,
  toolName: string,
): boolean {
  const details = result.details;
  if (!isRecord(details) || typeof details.tool !== "string") {
    return false;
  }
  return (
    Array.isArray(result.content) &&
    details.server === "browser-harness" &&
    toolName === `${BROWSER_HARNESS_TOOL_PREFIX}${details.tool}` &&
    Array.isArray(structured.content)
  );
}

function isDryRunArtifact(
  value: unknown,
): value is Record<string, unknown> & {
  status: "passed" | "failed" | "error";
} {
  if (!isRecord(value)) {
    return false;
  }
  return (
    value.status === "passed" ||
    value.status === "failed" ||
    value.status === "error"
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

interface HarnessStructuredContent {
  page?: unknown;
  case?: unknown;
  dry_run?: unknown;
}
