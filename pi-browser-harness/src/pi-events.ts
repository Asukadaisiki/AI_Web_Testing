import type { AgentSessionEvent } from "@earendil-works/pi-coding-agent";
import type { ObservedEvent } from "./harness-definition.js";
import type { AssistantMessage } from "@earendil-works/pi-ai";

export function mapToolCompleted(
  event: Extract<AgentSessionEvent, { type: "tool_execution_end" }>,
): Extract<ObservedEvent, { type: "pi.tool.completed" }> {
  const outer: unknown = event.result;
  let result = isRecord(outer) ? outer : undefined;
  // MCP tools retain the original CallToolResult inside AgentToolResult.structuredContent.
  // Direct/legacy tools may use structuredContent for their own domain data.
  if (result && event.toolName.startsWith("mcp__") && isRecord(result.details) &&
    typeof result.details.server === "string" && typeof result.details.tool === "string" &&
    isRecord(result.structuredContent) && Array.isArray(result.structuredContent.content)) {
    result = result.structuredContent;
  }
  return {
    type: "pi.tool.completed",
    payload: {
      tool_call_id: event.toolCallId, tool_name: event.toolName, is_error: Boolean(event.isError),
      ...(result && Array.isArray(result.content) ? {
        result: {
          content: result.content,
          ...(Object.hasOwn(result, "structuredContent") ? { structuredContent: result.structuredContent } : {}),
          ...(typeof result.isError === "boolean" ? { isError: result.isError } : {}),
        },
      } : {}),
      ...(result && isRecord(result.structuredContent) ? { structured_result: result.structuredContent } : {}),
    },
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function mapAssistantMessage(message: AssistantMessage, emittedUsage: WeakSet<AssistantMessage>): ObservedEvent[] {
  const text = message.content.filter((item) => item.type === "text").map((item) => item.text).join("");
  const timestamp = message.timestamp;
  const duration = message.durationMs;
  const timed = Number.isFinite(timestamp) && Number.isFinite(duration) &&
    duration !== undefined && duration >= 0 &&
    Number.isFinite(new Date(timestamp + duration).getTime());
  const events: ObservedEvent[] = [{
    type: "pi.message.completed",
    payload: {
      text,
      provider: message.provider,
      model: message.model,
      ...(timed ? {
        started_at: new Date(timestamp).toISOString(),
        ended_at: new Date(timestamp + duration!).toISOString(),
      } : {}),
    },
  }];
  const usage = message.usage;
  // The SDK initializes unreported usage to zeros without a presence flag.
  // Conservatively omit all-zero counts, but retain cache-only usage.
  const hasReportedTokens = usage &&
    [usage.input, usage.output, usage.cacheRead, usage.cacheWrite, usage.totalTokens]
      .some((tokens) => Number.isFinite(tokens) && tokens > 0);
  if (hasReportedTokens && !emittedUsage.has(message)) {
    emittedUsage.add(message);
    events.push({
      type: "pi.usage",
      payload: {
        provider: message.provider, model: message.model,
        input_tokens: usage.input, output_tokens: usage.output,
        ...(usage.cacheRead !== undefined ? { cache_read_tokens: usage.cacheRead } : {}),
        ...(usage.cacheWrite !== undefined ? { cache_write_tokens: usage.cacheWrite } : {}),
        ...(usage.totalTokens !== undefined ? { total_tokens: usage.totalTokens } : {}),
      },
    });
  }
  return events;
}

/** Standard observer events, independent of the selected tools or completion policy. */
export function createStandardEventMapper(): (event: unknown) => ObservedEvent[] {
  const emittedUsage = new WeakSet<AssistantMessage>();
  return (raw) => {
    const event = raw as AgentSessionEvent;
    if (event.type === "message_end" && event.message.role === "assistant") {
      return mapAssistantMessage(event.message, emittedUsage);
    }
    if (event.type === "tool_execution_start") {
      return [{
        type: "pi.tool.started",
        payload: { tool_call_id: event.toolCallId, tool_name: event.toolName },
      }];
    }
    if (event.type === "tool_execution_end") {
      return [mapToolCompleted(event)];
    }
    return [];
  };
}
