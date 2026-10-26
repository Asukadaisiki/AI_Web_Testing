import type {
  ActRequest,
  Condition,
  ExecutionResult,
  Observation,
  Step,
} from "./contract.js";
import type { PageView } from "./observation-view.js";
import type { CaseBuilderWorker, StepView, ToolResult } from "./case-builder.js";

export const CODE_COMMITTED_PREFIX_REPLAY_FAILED =
  "committed_prefix_replay_failed";
export const CODE_DRY_RUN_FAILED = "dry_run_failed";
export const CODE_NAVIGATION_FAILED = "navigation_failed";

export interface DryRunStep {
  index: number;
  action: string;
  status: "passed" | "failed";
  url: string;
  error?: string;
  unsatisfied: string[];
  blocker?: unknown;
  hit_test?: unknown;
  recovery?: unknown[];
}

export interface DryRunFailure {
  execution_id: string;
  status: string;
  final_url: string;
  current_url: string;
  error?: string;
  steps: DryRunStep[];
  failed_step?: DryRunStep;
  repair_hints?: string[];
}

export class FatalCaseBuilderError extends Error {
  constructor(
    public readonly code: typeof CODE_COMMITTED_PREFIX_REPLAY_FAILED,
    message: string,
    public readonly result?: ToolResult,
  ) {
    super(message);
    this.name = "FatalCaseBuilderError";
  }
}

export interface ReplayContext {
  readonly worker: CaseBuilderWorker;
  readonly artifactScope: string;
  readonly goal: string;
  getSteps(): Step[];
  setSteps(steps: Step[]): void;
  getObservation(): Observation | null;
  setObservation(observation: Observation | null): void;
  getHasPage(): boolean;
  setHasPage(value: boolean): void;
  getBaseUrl(): string;
  setBaseUrl(url: string): void;
  getBrowserSessionId(): string;
  setBrowserSessionId(id: string): void;
  navigateWithRetry(url: string, signal?: AbortSignal): Promise<Observation>;
  pageViewOf(observation: Observation): PageView;
  stepViewsOf(steps: readonly Step[]): StepView[];
}

export function observationHasText(
  observation: Observation,
  text: string,
): boolean {
  const needle = normalize(text);
  for (const element of observation.elements) {
    if (!element.visible) continue;
    if (
      normalize(element.name ?? "").includes(needle) ||
      normalize(element.text ?? "").includes(needle) ||
      normalize(element.full_text ?? "").includes(needle)
    ) {
      return true;
    }
  }
  for (const structure of observation.structures) {
    if (
      structure.visible &&
      normalize(structure.full_text).includes(needle)
    ) {
      return true;
    }
  }
  return false;
}

export function evaluatePageConditions(
  observation: Observation,
  conditions: Condition[],
): string | null {
  for (const condition of conditions) {
    let satisfied = false;
    switch (condition.type) {
      case "url_contains":
        satisfied = observation.url.includes(condition.value);
        break;
      case "text_visible":
        satisfied = observationHasText(observation, condition.value);
        break;
      case "text_gone":
        satisfied = !observationHasText(observation, condition.value);
        break;
      default:
        return `cannot evaluate page condition ${JSON.stringify(condition.type)} during replay`;
    }
    if (!satisfied) {
      return `replay condition ${condition.type}(${JSON.stringify(condition.value)}) was not satisfied`;
    }
  }
  return null;
}

export function firstFailedStepIndex(execution: ExecutionResult): number {
  for (const step of execution.steps) {
    if (step.status !== "passed") return step.index;
  }
  return -1;
}

export function dryRunFailure(execution: ExecutionResult): DryRunFailure {
  const failure: DryRunFailure = {
    execution_id: execution.execution_id,
    status: execution.status,
    final_url: execution.final_url,
    current_url: execution.final_url,
    steps: [],
  };
  if (execution.error !== null) {
    failure.error = `${execution.error.kind}: ${execution.error.message}`;
  }
  for (const step of execution.steps) {
    if (step.status === "passed") continue;
    const item: DryRunStep = {
      index: step.index,
      action: step.action,
      status: step.status,
      url: step.url_after,
      unsatisfied: [],
    };
    if (step.error !== null) {
      item.error = `${step.error.kind}: ${step.error.message}`;
    }
    if (step.blocker !== undefined) item.blocker = step.blocker;
    if (step.hit_test !== undefined) item.hit_test = step.hit_test;
    if (step.recovery !== undefined) item.recovery = step.recovery;
    for (const condition of step.conditions) {
      if (condition.satisfied) continue;
      item.unsatisfied.push(
        `${condition.type}(${condition.phase}):${condition.value} ${condition.detail ?? ""}`.trim(),
      );
    }
    failure.steps.push(item);
    if (failure.failed_step === undefined) {
      failure.failed_step = { ...item };
      if (item.url !== "") failure.current_url = item.url;
      failure.repair_hints = repairHintsForStep(step);
    }
  }
  return failure;
}

export function repairHintsForStep(
  step: ExecutionResult["steps"][number],
): string[] {
  const hints = ["reobserve current page before retrying"];
  if (step.error !== null) {
    switch (step.error.kind) {
      case "blocked_by_auth":
        hints.push(
          "ask the user to clear the auth wall, then open_page to reobserve",
        );
        break;
      case "blocked_by_captcha":
        hints.push(
          "ask the user to complete the captcha, then open_page to reobserve",
        );
        break;
      case "blocked_by_dialog":
      case "blocked_by_overlay":
      case "blocked_by_interstitial":
      case "blocked_by_cookie_banner":
        hints.push(
          "try dismiss_dialog or reobserve after the safe recovery attempt",
        );
        break;
      case "blocked_by_loading":
        hints.push(
          "wait for loading to disappear, then open_page to reobserve",
        );
        break;
      case "target_not_found":
        hints.push(
          "narrow target scope or use an alias from the current observation",
        );
        break;
      case "step_timeout":
        hints.push(
          "scroll target into view or wait for a stable visible element",
        );
        break;
      default:
        hints.push("ask the user only if required information is missing");
    }
  }
  return hints;
}

export function dryRunFailureSummary(failure: DryRunFailure): string {
  const step = failure.failed_step;
  if (step !== undefined) {
    const summary = `dry-run failed at step ${step.index} (${step.action})`;
    if (step.error !== undefined) return `${summary}: ${step.error}`;
    if (step.unsatisfied.length > 0)
      return `${summary}: ${step.unsatisfied.join("; ")}`;
    return summary;
  }
  if (failure.error !== undefined) return `dry-run failed: ${failure.error}`;
  return "dry-run failed";
}

export function authoringFailureSummary(result: ToolResult): string {
  const code = result.error ?? "unknown_error";
  const summary = `authoring action failed (${code})`;
  if (result.detail !== undefined && result.detail !== "") {
    return `${summary}: ${result.detail}`;
  }
  return summary;
}

export async function replayCommitted(
  context: ReplayContext,
  prefixLength: number,
  signal?: AbortSignal,
): Promise<void> {
  const steps = context.getSteps();
  if (prefixLength < 0 || prefixLength > steps.length) {
    throw new Error(
      `invalid committed prefix length ${prefixLength} for ${steps.length} steps`,
    );
  }
  context.setSteps(steps.slice(0, prefixLength));
  context.setObservation(null);
  context.setHasPage(false);
  context.setBaseUrl("");

  const previous = context.getBrowserSessionId();
  context.setBrowserSessionId("");
  if (previous !== "") {
    try {
      await context.worker.closeSession(previous, signal);
    } catch {
      // best-effort cleanup
    }
  }
  // The new session is opened lazily by the first replayed goto step.

  for (const step of context.getSteps()) {
    await replayStep(context, step, signal);
  }
}

export async function replayStep(
  context: ReplayContext,
  step: Step,
  signal?: AbortSignal,
): Promise<void> {
  if (step.action === "goto") {
    if (step.value === undefined || step.value === null) {
      throw new Error("goto step has no url");
    }
    const observation = await context.navigateWithRetry(step.value, signal);
    const conditionError = evaluatePageConditions(
      observation,
      step.postconditions,
    );
    if (conditionError !== null) {
      throw new Error(conditionError);
    }
    context.setObservation(observation);
    context.setHasPage(true);
    if (context.getBaseUrl() === "") {
      context.setBaseUrl(origin(step.value));
    }
    return;
  }
  if (!context.getHasPage() || context.getObservation() === null) {
    throw new Error("step has no replayed page");
  }
  if (step.target === undefined || step.target === null) {
    if (step.action !== "assert_text" && step.action !== "assert_url") {
      throw new Error("non-assertion step has no target");
    }
    const conditionError = evaluatePageConditions(
      context.getObservation()!,
      step.postconditions,
    );
    if (conditionError !== null) {
      throw new Error(conditionError);
    }
    return;
  }
  const request: ActRequest = {
    action: step.action as ActRequest["action"],
    locator: step.target.locator,
    ...(step.value !== undefined && step.value !== null
      ? { value: step.value }
      : {}),
    submit: step.submit,
    postconditions: step.postconditions,
  };
  const response = await context.worker.act(
    context.getBrowserSessionId(),
    request,
    signal,
  );
  context.setObservation(response.observation);
  context.setHasPage(true);
  if (response.status !== "passed") {
    if (response.error !== null) {
      throw new Error(`${response.error.kind}: ${response.error.message}`);
    }
    throw new Error(`worker returned status ${JSON.stringify(response.status)}`);
  }
}

export async function prepareDryRunRepair(
  context: ReplayContext,
  execution: ExecutionResult,
  signal?: AbortSignal,
): Promise<ToolResult> {
  const failure = dryRunFailure(execution);
  const result: ToolResult = {
    ok: false,
    error: CODE_DRY_RUN_FAILED,
    detail:
      "the authored case did not pass a full dry run; the failed tail was removed",
    failure,
    steps: context.stepViewsOf(context.getSteps()),
  };
  const failedIndex = firstFailedStepIndex(execution);
  if (failedIndex < 0) {
    if (context.getHasPage() && context.getObservation() !== null) {
      result.page = context.pageViewOf(context.getObservation()!);
    }
    return result;
  }
  if (failedIndex >= context.getSteps().length) {
    throw new FatalCaseBuilderError(
      CODE_COMMITTED_PREFIX_REPLAY_FAILED,
      `${dryRunFailureSummary(failure)}; dry-run failed step index ${failedIndex} does not identify a committed step among ${context.getSteps().length} steps`,
    );
  }
  try {
    await replayCommitted(context, failedIndex, signal);
  } catch (error) {
    throw new FatalCaseBuilderError(
      CODE_COMMITTED_PREFIX_REPLAY_FAILED,
      `${dryRunFailureSummary(failure)}; replay committed prefix: ${errorMessage(error)}`,
    );
  }
  result.steps = context.stepViewsOf(context.getSteps());
  if (context.getHasPage() && context.getObservation() !== null) {
    result.page = context.pageViewOf(context.getObservation()!);
  }
  return result;
}

function origin(rawURL: string): string {
  try {
    const parsed = new URL(rawURL);
    return `${parsed.protocol}//${parsed.host}`;
  } catch {
    return "";
  }
}

function normalize(value: string): string {
  return value.toLowerCase().trim().split(/\s+/u).filter(Boolean).join(" ");
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
