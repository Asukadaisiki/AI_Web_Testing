import type { HarnessEventPayloads, HarnessEventType, RunRequest } from "./run-protocol.js";

export type CompletionResult = {
  verdict: "succeeded" | "failed";
  error?: string;
};

export type ObservedEvent = {
  [Type in HarnessEventType]: { type: Type; payload: HarnessEventPayloads[Type] };
}[HarnessEventType];

export interface CompletionContext {
  assistant_settled: boolean;
  aborted: boolean;
  assistant_text: string;
  events: readonly ObservedEvent[];
}

/** Transport-neutral MCP descriptors; the Pi adapter translates them to SDK configs. */
export interface HarnessMcpServer {
  name: string;
  command: string;
  args?: string[];
  env?: Record<string, string>;
  exposure?: "direct" | "deferred" | "codemode" | "hidden";
  /** Lifecycle tools callable by the host, never exposed to the agent. */
  hostOnlyTools?: readonly string[];
  executionMode?: "sequential" | "parallel";
}

export type JsonValue = string | number | boolean | null | JsonValue[] | JsonObject;
export interface JsonObject { [key: string]: JsonValue }

export interface HarnessCleanupContext {
  /** Calls a discovered MCP tool without a model turn, including hidden lifecycle tools. */
  callMcpTool(name: string, arguments_: JsonObject): Promise<unknown>;
}

export type ScriptedResponse =
  | { tool: { name: string; arguments: JsonObject } }
  | { text: string };

/** No SDK or observer-vendor types cross this boundary. One definition serves many attempts. */
export interface HarnessDefinition {
  systemPrompt: string;
  skillPaths: readonly string[];
  toolAllowlist: readonly string[];
  mcpServers: readonly HarnessMcpServer[] |
    ((request: RunRequest) => readonly HarnessMcpServer[]);
  completionPolicy(context: CompletionContext): CompletionResult;
  /** Await resource-release acknowledgement before the adapter tears down MCP transports. */
  cleanup?(context: HarnessCleanupContext): Promise<void>;
  /** Retain at most the latest event of each selected type for completion evidence. */
  completionEventTypes?: readonly HarnessEventType[];
  formatPrompt?(goal: string): string;
  createEventMapper?(): (event: unknown) => ObservedEvent[];
  scriptedResponse?(goal: string, priorToolResults: number): ScriptedResponse;
}
