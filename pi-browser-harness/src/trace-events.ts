import type { AgentSessionEvent } from "@earendil-works/pi-coding-agent";
import type { AssistantMessage, TextContent } from "@earendil-works/pi-ai";

import {
  parseExecutionResult,
  validateCase,
  type Case,
  type ExecutionResult,
} from "./browser/contract.js";
import type { HarnessEventType, HarnessEventPayloads } from "./run-protocol.js";
import { mapAssistantMessage } from "./pi-events.js";

const BROWSER_HARNESS_TOOL_PREFIX = "mcp__browser_harness__";
const FINISH_CASE_TOOL = `${BROWSER_HARNESS_TOOL_PREFIX}finish_case`;

/**
 * Mutable state carried across Pi events so the mapper can derive settlement
 * verdicts and avoid emitting usage more than once per assistant message.
 */
export interface TraceMapperState {
  browser_case_verdicts: ("passed" | "failed")[];
  last_assistant_text: string;
  /** SDK message identities whose usage has already been emitted. */
  emitted_usage: WeakSet<AssistantMessage>;
}

export function createTraceMapperState(): TraceMapperState {
  return {
    browser_case_verdicts: [],
    last_assistant_text: "",
    emitted_usage: new WeakSet(),
  };
}

/** A mapped harness event discriminant and its payload. */
export type MappedEvent<Type extends HarnessEventType = HarnessEventType> = {
  [EventType in Type]: { type: EventType; payload: HarnessEventPayloads[EventType] };
}[Type];

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
      const text = extractAssistantText(message);
      state.last_assistant_text = text;
      return mapAssistantMessage(message, state.emitted_usage);
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
      const structured = extractStructuredContent(event.result, event.toolName);
      const events: MappedEvent[] = [
        {
          type: "pi.tool.completed",
          payload: {
            tool_call_id: event.toolCallId,
            tool_name: event.toolName,
            is_error: Boolean(event.isError),
            ...(structured !== undefined &&
            isBrowserHarnessMcpEnvelopeResult(event.result, event.toolName)
              ? { structured_result: structured }
              : {}),
          },
        },
      ];
      if (structured !== undefined) {
        const observation = structured.page;
        if (observation !== undefined) {
          events.push({
            type: "browser.observation",
            payload: { tool_call_id: event.toolCallId, observation },
          });
        }
        if (
          event.toolName === FINISH_CASE_TOOL &&
          isBrowserCase(structured.case) &&
          isDryRunArtifact(structured.dry_run)
        ) {
          const verdict: "passed" | "failed" =
            structured.dry_run.status === "passed" ? "passed" : "failed";
          state.browser_case_verdicts.push(verdict);
          events.push({
            type: "browser.case.completed",
            payload: {
              verdict,
              case: structured.case,
              dry_run: structured.dry_run,
            },
          });
        }
      }
      return events;
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

function isBrowserHarnessMcpEnvelopeResult(
  result: unknown,
  toolName: string,
): boolean {
  if (!isRecord(result) || !isRecord(result.structuredContent)) {
    return false;
  }
  return isBrowserHarnessMcpEnvelope(
    result,
    result.structuredContent,
    toolName,
  );
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

function isDryRunArtifact(value: unknown): value is ExecutionResult {
  try {
    parseExecutionResult(value);
    return true;
  } catch {
    return false;
  }
}

function isBrowserCase(value: unknown): value is Case {
  try {
    validateCase(value);
    return true;
  } catch {
    return false;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

interface HarnessStructuredContent {
  [key: string]: unknown;
  page?: unknown;
  case?: unknown;
  dry_run?: unknown;
}
