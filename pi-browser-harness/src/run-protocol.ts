import type { Writable } from "node:stream";

import {
  parseExecutionResult,
  validateCase,
  type Case,
  type ExecutionResult,
} from "./browser/contract.js";

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
  worker_url: string;
  artifact_scope: string;
}

export interface HarnessEventPayloads {
  "harness.started": Record<string, never>;
  "pi.message.completed": {
    text: string;
    [key: string]: unknown;
  };
  "pi.usage": {
    input_tokens: number;
    output_tokens: number;
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
  "browser.observation": {
    tool_call_id: string;
    observation: unknown;
  };
  "browser.case.completed": {
    verdict: "passed" | "failed";
    case: Case;
    dry_run: ExecutionResult;
  };
  "harness.settled": {
    assistant_text: string;
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

export interface TerminalState {
  assistant_settled: boolean;
  browser_case_verdicts: readonly ("passed" | "failed")[];
}

const RUN_REQUEST_KEYS = [
  "protocol_version",
  "run_id",
  "attempt_id",
  "goal",
  "model",
  "worker_url",
  "artifact_scope",
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
  if (!isRecord(value) || !hasExactKeys(value, RUN_REQUEST_KEYS)) {
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
  if (!isNonBlankString(value.artifact_scope)) {
    throw new Error("artifact_scope must be non-blank");
  }
  if (!isAbsoluteHttpUrl(value.worker_url)) {
    throw new Error("worker_url must be an absolute HTTP URL");
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

  constructor(
    private readonly request: Pick<RunRequest, "run_id" | "attempt_id">,
    private readonly output: Pick<Writable, "write">,
    private readonly now: () => Date = () => new Date(),
  ) {}

  write<Type extends HarnessEventType>(
    type: Type,
    payload: HarnessEventPayloads[Type],
  ): Extract<HarnessEvent, { type: Type }> {
    validateOutgoingPayload(type, payload);
    const event = {
      type,
      run_id: this.request.run_id,
      attempt_id: this.request.attempt_id,
      sequence: ++this.sequence,
      timestamp: this.now().toISOString(),
      payload,
    } as Extract<HarnessEvent, { type: Type }>;

    this.output.write(`${JSON.stringify(event)}\n`);
    return event;
  }
}

function validateOutgoingPayload<Type extends HarnessEventType>(
  type: Type,
  payload: HarnessEventPayloads[Type],
): void {
  if (type === "browser.observation") {
    const observation = payload as HarnessEventPayloads["browser.observation"];
    if (
      !hasExactKeys(observation, ["tool_call_id", "observation"]) ||
      !isNonBlankString(observation.tool_call_id)
    ) {
      throw new Error("invalid browser.observation payload");
    }
    return;
  }
  if (type !== "browser.case.completed") {
    return;
  }

  const completed = payload as HarnessEventPayloads["browser.case.completed"];
  if (
    !hasExactKeys(completed, ["verdict", "case", "dry_run"]) ||
    (completed.verdict !== "passed" && completed.verdict !== "failed")
  ) {
    throw new Error("invalid browser.case.completed payload");
  }
  try {
    const browserCase = validateCase(completed.case);
    const dryRun = parseExecutionResult(completed.dry_run);
    if (
      browserCase.steps.length !== dryRun.steps.length ||
      browserCase.steps.some(
        (step, index) =>
          dryRun.steps[index]?.index !== index ||
          dryRun.steps[index]?.action !== step.action,
      ) ||
      (completed.verdict === "passed") !== (dryRun.status === "passed")
    ) {
      throw new Error("browser case evidence is inconsistent");
    }
  } catch {
    throw new Error("invalid browser.case.completed payload");
  }
}

export function deriveTerminalVerdict(
  state: TerminalState,
): "succeeded" | "failed" {
  return state.assistant_settled &&
    state.browser_case_verdicts.at(-1) === "passed"
    ? "succeeded"
    : "failed";
}
