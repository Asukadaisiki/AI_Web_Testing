import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";

import type {
  ToolOutcome,
  ToolResult,
  StepView,
} from "../browser/case-builder.js";
import type { Case, ExecutionResult } from "../browser/contract.js";
import type { PageView } from "../browser/observation-view.js";
import type { DryRunFailure } from "../browser/replay.js";

/**
 * The structured payload returned alongside the model-facing text. It mirrors
 * the case builder `ToolResult` plus finish-case artifacts, and never includes
 * screenshot bytes or the raw DOM.
 */
export interface McpToolStructuredContent {
  [key: string]: unknown;
  ok: boolean;
  error?: string;
  summary?: string;
  detail?: string;
  page?: PageView;
  steps?: StepView[];
  failure?: DryRunFailure;
  case?: Case;
  dry_run?: ExecutionResult;
  redacted_args?: string[];
}

/**
 * Encodes a case builder outcome into an MCP tool result: a short model-facing
 * text summary plus the structured observation, committed steps, and any
 * recoverable failure.
 *
 * Recoverable grounding, action, and dry-run failures are returned as typed
 * tool results (ok=false with a stable error code). Fatal builder errors are
 * handled by the server, not here.
 */
export function encodeToolOutcome(outcome: ToolOutcome): CallToolResult {
  const result = outcome.result;
  const structured: McpToolStructuredContent = {
    ok: result.ok,
    ...(result.error !== undefined ? { error: result.error } : {}),
    ...(result.summary !== undefined ? { summary: result.summary } : {}),
    ...(result.detail !== undefined ? { detail: result.detail } : {}),
    ...(result.page !== undefined ? { page: result.page } : {}),
    steps: result.steps ?? [],
    ...(result.failure !== undefined ? { failure: result.failure } : {}),
    ...(result.redacted_args !== undefined ? { redacted_args: result.redacted_args } : {}),
  };
  if (outcome.case !== undefined) {
    structured.case = outcome.case;
  }
  if (outcome.dryRun !== undefined) {
    structured.dry_run = outcome.dryRun;
  }
  return {
    content: [{ type: "text", text: modelFacingText(result) }],
    structuredContent: structured,
  };
}

function modelFacingText(result: ToolResult): string {
  if (result.ok) {
    return result.summary ?? "ok";
  }
  const code = result.error ?? "unknown_error";
  if (result.detail !== undefined && result.detail !== "") {
    return `failed (${code}): ${result.detail}`;
  }
  return `failed (${code})`;
}
