import { parseExecutionResult, validateCase, type Case, type ExecutionResult } from "./browser/contract.js";

export interface BrowserEventPayloads {
  "browser.observation": { tool_call_id: string; observation: unknown };
  "browser.case.completed": {
    verdict: "passed" | "failed";
    case: Case;
    dry_run: ExecutionResult;
  };
}

export function validateBrowserEventPayload(type: string, payload: unknown): void {
  if (!type.startsWith("browser.")) return;
  if (typeof payload !== "object" || payload === null || Array.isArray(payload)) {
    throw new Error(`invalid ${type} payload`);
  }
  const value = payload as Record<string, unknown>;
  const keys = type === "browser.observation" ? ["tool_call_id", "observation"] :
    ["verdict", "case", "dry_run"];
  try {
    if (Object.keys(value).length !== keys.length ||
      !keys.every((key) => Object.hasOwn(value, key))) throw new Error("invalid keys");
    if (type === "browser.observation") {
      if (typeof value.tool_call_id !== "string" || !value.tool_call_id.trim()) {
        throw new Error("invalid tool call id");
      }
      return;
    }
    if (value.verdict !== "passed" && value.verdict !== "failed") throw new Error("invalid verdict");
    const browserCase = validateCase(value.case);
    const dryRun = parseExecutionResult(value.dry_run);
    if (browserCase.steps.length !== dryRun.steps.length ||
      browserCase.steps.some((step, index) =>
        dryRun.steps[index]?.index !== index || dryRun.steps[index]?.action !== step.action) ||
      (value.verdict === "passed") !== (dryRun.status === "passed")) {
      throw new Error("inconsistent evidence");
    }
  } catch {
    throw new Error(`invalid ${type} payload`);
  }
}

export function deriveTerminalVerdict(state: {
  assistant_settled: boolean;
  browser_case_verdicts: readonly ("passed" | "failed")[];
}): "succeeded" | "failed" {
  return state.assistant_settled && state.browser_case_verdicts.at(-1) === "passed"
    ? "succeeded" : "failed";
}
