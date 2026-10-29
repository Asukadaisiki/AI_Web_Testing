import type { Writable } from "node:stream";

import type { BrowserEventPayloads } from "./browser-protocol.js";
import { parseExecutionSpec, type ExecutionSpec } from "./execution-spec.js";

export const RUN_PROTOCOL_VERSION = "pi-browser-harness.v1" as const;

export interface ModelRequest {
  provider: string;
  name: string;
  reasoning_effort: string;
}

export interface RunRequest {
  protocol_version: typeof RUN_PROTOCOL_VERSION;
  run_id: string;
  attempt_id: string;
  goal: string;
  model: ModelRequest;
  worker_url?: string;
  artifact_scope?: string;
  execution: ExecutionSpec;
  project_id?: string;
  environment_id?: string;
  profile_generation?: number;
  fencing_token?: number;
}

export interface HarnessEventPayloads extends BrowserEventPayloads {
  "harness.started": Record<string, never>;
  "pi.message.completed": {
    text: string;
    provider?: string;
    model?: string;
    input?: { messages: unknown[]; systemPrompt?: string; tools?: unknown[] };
    started_at?: string;
    ended_at?: string;
    [key: string]: unknown;
  };
  "pi.usage": {
    provider?: string;
    model?: string;
    input_tokens: number;
    output_tokens: number;
    cache_read_tokens?: number;
    cache_write_tokens?: number;
    total_tokens?: number;
    [key: string]: unknown;
  };
  "pi.tool.started": {
    tool_call_id: string;
    tool_name: string;
    [key: string]: unknown;
  };
  "pi.tool.completed": {
    tool_call_id: string;
    tool_name: string;
    is_error: boolean;
    structured_result?: Record<string, unknown>;
    [key: string]: unknown;
  };
  "harness.settled": {
    assistant_text: string;
    verdict: "succeeded" | "failed";
    error?: string;
    [key: string]: unknown;
  };
  "harness.failed": {
    error: string;
    [key: string]: unknown;
  };
}

interface HarnessEventEnvelope {
  run_id: string;
  attempt_id: string;
  sequence: number;
  timestamp: string;
}

export type HarnessEventType = keyof HarnessEventPayloads;

export type HarnessEvent = {
  [Type in HarnessEventType]: HarnessEventEnvelope & {
    type: Type;
    payload: HarnessEventPayloads[Type];
  };
}[HarnessEventType];

const RUN_REQUEST_KEYS = [
  "protocol_version",
  "run_id",
  "attempt_id",
  "goal",
  "model",
  "worker_url",
  "artifact_scope",
  "execution",
  "project_id",
  "environment_id",
  "profile_generation",
  "fencing_token",
] as const;

const MODEL_REQUEST_KEYS = [
  "provider",
  "name",
  "reasoning_effort",
] as const;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasExactKeys(
  value: Record<string, unknown>,
  expected: readonly string[],
): boolean {
  const actual = Object.keys(value);
  return (
    actual.length === expected.length &&
    actual.every((key) => expected.includes(key))
  );
}

function isNonBlankString(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}

function isAbsoluteHttpUrl(value: unknown): value is string {
  if (typeof value !== "string") {
    return false;
  }

  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

export function parseRunRequest(input: string): RunRequest {
  const value: unknown = JSON.parse(input);
  if (!isRecord(value) ||
    Object.keys(value).some((key) => !(RUN_REQUEST_KEYS as readonly string[]).includes(key)) ||
    !RUN_REQUEST_KEYS.slice(0, 5).every((key) => Object.hasOwn(value, key))) {
    throw new Error("invalid run request shape");
  }

  if (value.protocol_version !== RUN_PROTOCOL_VERSION) {
    throw new Error(`unsupported protocol version: ${String(value.protocol_version)}`);
  }
  if (!isNonBlankString(value.run_id)) {
    throw new Error("run_id must be non-blank");
  }
  if (!isNonBlankString(value.attempt_id)) {
    throw new Error("attempt_id must be non-blank");
  }
  if (!isNonBlankString(value.goal)) {
    throw new Error("goal must be non-blank");
  }
  value.execution = parseExecutionSpec(value.execution);
  if (Object.hasOwn(value, "artifact_scope") && !isNonBlankString(value.artifact_scope)) {
    throw new Error("artifact_scope must be non-blank");
  }
  if (Object.hasOwn(value, "worker_url") && !isAbsoluteHttpUrl(value.worker_url)) {
    throw new Error("worker_url must be an absolute HTTP URL");
  }
  const leaseKeys = ["project_id", "environment_id", "profile_generation", "fencing_token"];
  const hasBrowser = Object.values((value.execution as ExecutionSpec).harness.mcp)
    .some((server) => server.capability === "browser-harness");
  if (hasBrowser || leaseKeys.some((key) => Object.hasOwn(value, key))) {
    if (!isNonBlankString(value.project_id) || !isNonBlankString(value.environment_id) ||
      !Number.isSafeInteger(value.profile_generation) || (value.profile_generation as number) < 1 ||
      !Number.isSafeInteger(value.fencing_token) || (value.fencing_token as number) < 1) {
      throw new Error("complete managed environment lease identity is required");
    }
  }

  const model = value.model;
  if (!isRecord(model) || !hasExactKeys(model, MODEL_REQUEST_KEYS)) {
    throw new Error("invalid model request shape");
  }
  if (!isNonBlankString(model.provider)) {
    throw new Error("model.provider must be non-blank");
  }
  if (!isNonBlankString(model.name)) {
    throw new Error("model.name must be non-blank");
  }
  if (!isNonBlankString(model.reasoning_effort)) {
    throw new Error("model.reasoning_effort must be non-blank");
  }
  return value as unknown as RunRequest;
}

export class HarnessEventWriter {
  private sequence = 0;
  private terminal = false;
  private verdict: "succeeded" | "failed" | undefined;

  get hasTerminalEvent(): boolean {
    return this.terminal;
  }

  get terminalVerdict(): "succeeded" | "failed" | undefined {
    return this.verdict;
  }

  constructor(
    private readonly request: Pick<RunRequest, "run_id" | "attempt_id">,
    private readonly output: Pick<Writable, "write">,
    private readonly now: () => Date = () => new Date(),
  ) {}

  write<Type extends HarnessEventType>(
    type: Type,
    payload: HarnessEventPayloads[Type],
  ): Extract<HarnessEvent, { type: Type }> | undefined {
    if (this.terminal) return undefined;
    validateOutgoingPayload(type, payload);
    const event = {
      type,
      run_id: this.request.run_id,
      attempt_id: this.request.attempt_id,
      sequence: ++this.sequence,
      timestamp: this.now().toISOString(),
      payload,
    } as Extract<HarnessEvent, { type: Type }>;

    if (type === "harness.settled" || type === "harness.failed") this.terminal = true;
    if (type === "harness.settled") {
      this.verdict = (payload as HarnessEventPayloads["harness.settled"]).verdict;
    }
    this.output.write(`${JSON.stringify(event)}\n`);
    return event;
  }
}

function validateOutgoingPayload<Type extends HarnessEventType>(
  type: Type,
  payload: HarnessEventPayloads[Type],
): void {
  if (type === "pi.message.completed" || type === "pi.usage") {
    const identity = payload as HarnessEventPayloads["pi.message.completed"] | HarnessEventPayloads["pi.usage"];
    for (const key of ["provider", "model"]) {
      if (Object.hasOwn(identity, key) && !isNonBlankString(identity[key])) {
        throw new Error(`invalid ${type} payload`);
      }
    }
  }
  if (type === "pi.message.completed") {
    const completed = payload as HarnessEventPayloads["pi.message.completed"];
    const hasStart = Object.hasOwn(completed, "started_at");
    const hasEnd = Object.hasOwn(completed, "ended_at");
    if (typeof completed.text !== "string" || hasStart !== hasEnd ||
      (hasStart && (!isUtcTimestamp(completed.started_at) ||
        !isUtcTimestamp(completed.ended_at) ||
        Date.parse(completed.ended_at) < Date.parse(completed.started_at))) ||
      (Object.hasOwn(completed, "input") && (!isRecord(completed.input) ||
        !Array.isArray(completed.input.messages) ||
        (Object.hasOwn(completed.input, "systemPrompt") && typeof completed.input.systemPrompt !== "string") ||
        (Object.hasOwn(completed.input, "tools") && !Array.isArray(completed.input.tools))))) {
      throw new Error("invalid pi.message.completed payload");
    }
    return;
  }
  if (type === "pi.usage") {
    const usage = payload as HarnessEventPayloads["pi.usage"];
    for (const key of ["input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "total_tokens"]) {
      if ((key === "input_tokens" || key === "output_tokens" || Object.hasOwn(usage, key)) &&
        (typeof usage[key] !== "number" || !Number.isSafeInteger(usage[key]) || usage[key] < 0)) {
        throw new Error("invalid pi.usage payload");
      }
    }
    return;
  }
  if (type === "harness.settled") {
    const settled = payload as HarnessEventPayloads["harness.settled"];
    if ((settled.verdict !== "succeeded" && settled.verdict !== "failed") ||
      typeof settled.assistant_text !== "string" ||
      (settled.error !== undefined && typeof settled.error !== "string")) {
      throw new Error("invalid harness.settled payload");
    }
    return;
  }
}

function isUtcTimestamp(value: unknown): value is string {
  return typeof value === "string" &&
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|\+00:00)$/.test(value) &&
    Number.isFinite(Date.parse(value));
}
