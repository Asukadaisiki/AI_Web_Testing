import type {
  ActRequest,
  ActResponse,
  Case,
  ExecutionResult,
  Expects,
  Observation,
  Target,
  TargetSpec,
} from "./contract.js";
import {
  CASE_VERSION,
  CaseContractError,
  deriveActionStep,
  deriveAssertTextStep,
  deriveAssertURLStep,
  deriveGotoStep,
  validateCase,
  type Action,
  type Locator,
  type Step,
} from "./contract.js";
import { GroundingError, resolveTarget } from "./grounding.js";
import { buildPageView, type PageView } from "./observation-view.js";
import { FailureLedger } from "./failure-ledger.js";
import {
  WorkerTransportError,
  type WorkerClient,
} from "./worker-client.js";
import {
  CODE_COMMITTED_PREFIX_REPLAY_FAILED,
  CODE_DRY_RUN_FAILED,
  CODE_NAVIGATION_FAILED,
  FatalCaseBuilderError,
  authoringFailureSummary,
  evaluatePageConditions,
  observationHasText,
  prepareDryRunRepair as runPrepareDryRunRepair,
  replayCommitted,
  type DryRunFailure,
  type ReplayContext,
} from "./replay.js";

export const CODE_SITE_OVERLOADED = "site_overloaded";
export const CODE_STRATEGY_REPEATED = "strategy_repeated";

const NAVIGATION_MAX_ATTEMPTS = 3;
const NAVIGATION_RETRY_DELAY_MS = 100;

export {
  CODE_COMMITTED_PREFIX_REPLAY_FAILED,
  CODE_DRY_RUN_FAILED,
  CODE_NAVIGATION_FAILED,
  FatalCaseBuilderError,
  observationHasText,
  type DryRunFailure,
  type DryRunStep,
} from "./replay.js";

const TARGET_ACTIONS = new Set<Action>([
  "click",
  "input",
  "select",
  "check",
  "uncheck",
  "scroll_into_view",
  "hover",
  "dismiss_dialog",
  "upload_file",
  "assert_element",
  "assert_attribute",
  "assert_count",
]);

export interface CaseBuilderDeps {
  worker: WorkerClient;
  artifactScope: string;
  goal: string;
}

export interface ToolResult {
  ok: boolean;
  summary?: string;
  error?: string;
  detail?: string;
  page?: PageView;
  steps?: StepView[];
  candidates?: unknown[];
  failure?: DryRunFailure;
}

export interface StepView {
  index: number;
  action: string;
  intent: string;
  target?: string;
  value?: string;
  preconditions: string[];
  postconditions: string[];
}

export interface ToolOutcome {
  result: ToolResult;
  case?: Case;
  dryRun?: ExecutionResult;
}

export interface CaseBuilderWorker {
  openSession(
    artifactScope: string,
    signal?: AbortSignal,
  ): Promise<string>;
  navigate(
    browserSessionId: string,
    url: string,
    signal?: AbortSignal,
  ): Promise<Observation>;
  act(
    browserSessionId: string,
    request: ActRequest,
    signal?: AbortSignal,
  ): Promise<ActResponse>;
  execute(
    artifactScope: string,
    artifact: Case,
    signal?: AbortSignal,
  ): Promise<ExecutionResult>;
  closeSession(
    browserSessionId: string,
    signal?: AbortSignal,
  ): Promise<void>;
}

export class CaseBuilder implements ReplayContext {
  readonly #worker: CaseBuilderWorker;
  readonly #artifactScope: string;
  readonly #goal: string;
  #browserSessionId = "";
  #baseUrl = "";
  #observation: Observation | null = null;
  #hasPage = false;
  #steps: Step[] = [];
  #failures = new FailureLedger();
  #closed = false;

  private constructor(deps: CaseBuilderDeps) {
    this.#worker = deps.worker;
    this.#artifactScope = deps.artifactScope;
    this.#goal = deps.goal;
  }

  static async create(deps: CaseBuilderDeps): Promise<CaseBuilder> {
    if (deps.artifactScope.trim() === "") {
      throw new Error("planning requires a session id");
    }
    // The browser session is opened lazily on the first navigation so that
    // tests (and replay) can inject a session handle before any worker call.
    return new CaseBuilder(deps);
  }

  get worker(): CaseBuilderWorker {
    return this.#worker;
  }

  get artifactScope(): string {
    return this.#artifactScope;
  }

  get committedSteps(): readonly Step[] {
    return this.#steps.map((step) => ({ ...step }));
  }

  get browserSessionId(): string {
    return this.#browserSessionId;
  }

  get goal(): string {
    return this.#goal;
  }

  seedSteps(steps: Step[]): void {
    this.#steps = steps.map((step) => ({ ...step }));
  }

  seedBrowserSession(sessionId: string): void {
    if (this.#browserSessionId !== "" && this.#browserSessionId !== sessionId) {
      // Drop the builder-owned session; tests inject their own handle.
      this.#browserSessionId = sessionId;
      return;
    }
    this.#browserSessionId = sessionId;
  }

  seedObservation(observation: Observation): void {
    this.#observation = observation;
    this.#hasPage = true;
  }

  // ReplayContext accessors
  getSteps(): Step[] {
    return this.#steps;
  }

  setSteps(steps: Step[]): void {
    this.#steps = steps;
  }

  getObservation(): Observation | null {
    return this.#observation;
  }

  setObservation(observation: Observation | null): void {
    this.#observation = observation;
  }

  getHasPage(): boolean {
    return this.#hasPage;
  }

  setHasPage(value: boolean): void {
    this.#hasPage = value;
  }

  getBaseUrl(): string {
    return this.#baseUrl;
  }

  setBaseUrl(url: string): void {
    this.#baseUrl = url;
  }

  getBrowserSessionId(): string {
    return this.#browserSessionId;
  }

  setBrowserSessionId(id: string): void {
    this.#browserSessionId = id;
  }

  pageViewOf(observation: Observation): PageView {
    return buildPageView(observation, this.#goal, null);
  }

  stepViewsOf(steps: readonly Step[]): StepView[] {
    return stepViews(steps);
  }

  async close(): Promise<void> {
    if (this.#closed) return;
    this.#closed = true;
    if (this.#browserSessionId !== "") {
      try {
        await this.#worker.closeSession(this.#browserSessionId);
      } catch {
        // Closing is best-effort; the session is owned by the worker.
      }
      this.#browserSessionId = "";
    }
  }

  async call(
    toolName: string,
    args: Record<string, unknown>,
    signal?: AbortSignal,
  ): Promise<ToolOutcome> {
    switch (toolName) {
      case "open_page":
        return {
          result: await this.openPage(
            stringArg(args, "url"),
            stringArg(args, "intent"),
            signal,
          ),
        };
      case "click":
        return {
          result: await this.targetAction(
            "click",
            args,
            undefined,
            false,
            signal,
          ),
        };
      case "input": {
        const value = stringArg(args, "value", true);
        return {
          result: await this.targetAction(
            "input",
            args,
            value,
            booleanArg(args, "submit"),
            signal,
          ),
        };
      }
      case "select":
      case "check":
      case "uncheck":
      case "scroll_into_view":
      case "hover":
      case "dismiss_dialog":
      case "upload_file":
      case "assert_element":
      case "assert_attribute":
      case "assert_count":
        return {
          result: await this.targetAction(
            toolName as Action,
            args,
            stringArg(args, "value", true),
            false,
            signal,
          ),
        };
      case "assert_text":
        return {
          result: this.assertText(
            stringArg(args, "text"),
            stringArg(args, "intent"),
          ),
        };
      case "assert_url":
        return {
          result: this.assertURL(
            stringArg(args, "contains"),
            stringArg(args, "intent"),
          ),
        };
      case "finish_case":
        return this.finishCase(stringArg(args, "name"), signal);
      default:
        return {
          result: {
            ok: false,
            error: "unknown_tool",
            detail: `unknown tool ${JSON.stringify(toolName)}`,
          },
        };
    }
  }

  private async openPage(
    url: string,
    intent: string,
    signal?: AbortSignal,
  ): Promise<ToolResult> {
    if (!isAbsoluteUrl(url)) {
      return failure(
        "url_not_absolute",
        `open_page requires an absolute url, got ${JSON.stringify(url)}`,
      );
    }
    const priorObservation = this.#observation;
    if (
      priorObservation !== null &&
      this.#failures.failed(priorObservation, "goto", undefined, undefined, url)
    ) {
      return strategyRepeated();
    }
    let step: Step;
    try {
      step = deriveGotoStep(this.#steps.length, intent, url);
    } catch (error) {
      if (priorObservation !== null) {
        this.#failures.record(
          priorObservation,
          "goto",
          "step_rejected",
          undefined,
          undefined,
          url,
        );
      }
      return failure("step_rejected", errorMessage(error));
    }
    let observation: Observation;
    try {
      observation = await this.navigateWithRetry(url, signal);
    } catch (error) {
      if (error instanceof SiteOverloadedError) {
        return failure(CODE_SITE_OVERLOADED, error.message);
      }
      // Navigation failures are reported back to the model as a repairable
      // tool result rather than aborting the run: no step was committed, so
      // the model can change the URL or retry.
      return failure(CODE_NAVIGATION_FAILED, errorMessage(error));
    }
    const conditionError = evaluatePageConditions(
      observation,
      step.postconditions,
    );
    if (conditionError !== null) {
      if (priorObservation !== null) {
        this.#failures.record(
          priorObservation,
          "goto",
          "condition_unmet",
          undefined,
          undefined,
          url,
        );
      }
      return failure("condition_unmet", conditionError);
    }
    if (this.#steps.length === 0) {
      this.#baseUrl = origin(url);
    }
    this.#steps.push(step);
    this.#observation = observation;
    this.#hasPage = true;
    return {
      ok: true,
      summary: `navigated to ${observation.url} and recorded goto step ${step.index}`,
      page: pageView(observation, this.#goal),
      steps: stepViews(this.#steps),
    };
  }

  private async targetAction(
    action: Action,
    args: Record<string, unknown>,
    value: string | undefined,
    submit: boolean,
    signal?: AbortSignal,
  ): Promise<ToolResult> {
    if (!this.#hasPage || this.#observation === null) {
      return failure(
        "no_observation",
        "no page has been observed yet; call open_page first",
      );
    }
    const hint = stringArg(args, "hint", true);
    const candidateId = stringArg(args, "candidate_id", true);
    const spec = targetSpecArg(args, "target");
    const intent = stringArg(args, "intent");
    const expects = parseExpects(args);
    const observation = this.#observation;
    if (this.#failures.failed(observation, action, candidateId || undefined, spec, hint)) {
      return strategyRepeated();
    }
    let grounded: {
      element: { name?: string | null; text?: string | null; ref: string };
      locator: Locator;
      candidateId: string;
    };
    try {
      const resolved = resolveTarget(
        {
          action,
          hint,
          ...(spec !== undefined ? { spec } : {}),
          ...(candidateId !== "" ? { candidate_id: candidateId } : {}),
        },
        observation,
      );
      grounded = {
        element: resolved.element,
        locator: resolved.locator,
        candidateId: resolved.candidate_id,
      };
    } catch (error) {
      const code =
        error instanceof GroundingError ? error.code : "target_error";
      this.#failures.record(
        observation,
        action,
        code,
        candidateId || undefined,
        spec,
        hint,
      );
      return failure(code, errorMessage(error));
    }
    const targetHint =
      hint.trim() !== ""
        ? hint
        : intent.trim() !== ""
          ? intent
          : grounded.candidateId;
    const target: Target = {
      hint: targetHint,
      locator: grounded.locator,
      grounding: {
        observation_id: this.#observation.observation_id,
        page_state_id: this.#observation.page_state_id,
        candidate_id: grounded.candidateId,
        page_url: this.#observation.url,
      },
      ...(spec !== undefined ? { spec } : {}),
    };
    let step: Step;
    try {
      step = deriveActionStep(
        this.#steps.length,
        action,
        intent,
        target,
        value,
        submit,
        this.#observation.url,
        expects,
      );
    } catch (error) {
      this.#failures.record(
        observation,
        action,
        "step_rejected",
        candidateId || undefined,
        spec,
        hint,
      );
      return failure("step_rejected", errorMessage(error));
    }
    const request: ActRequest = {
      action: action as ActRequest["action"],
      locator: grounded.locator,
      ...(value !== undefined ? { value } : {}),
      submit,
      postconditions: step.postconditions,
    };
    let response: ActResponse;
    try {
      response = await this.#worker.act(this.#browserSessionId, request, signal);
    } catch (error) {
      this.#failures.record(
        observation,
        action,
        "action_failed",
        candidateId || undefined,
        spec,
        hint,
      );
      return this.restoreAfterFailedAction(
        failure("action_failed", errorMessage(error)),
        signal,
      );
    }
    if (response.status !== "passed") {
      const code =
        response.error !== null ? response.error.kind : "action_failed";
      const detail =
        response.error !== null
          ? response.error.message
          : `worker returned authoring action status ${JSON.stringify(response.status)}`;
      this.#failures.record(
        observation,
        action,
        code,
        candidateId || undefined,
        spec,
        hint,
      );
      return this.restoreAfterFailedAction(
        { ok: false, error: code, detail },
        signal,
      );
    }
    this.#observation = response.observation;
    this.#steps.push(step);
    return {
      ok: true,
      summary: `recorded ${action} step ${step.index} on ${JSON.stringify(displayName(grounded.element))}`,
      page: pageView(response.observation, this.#goal),
      steps: stepViews(this.#steps),
    };
  }

  private assertText(text: string, intent: string): ToolResult {
    if (!this.#hasPage || this.#observation === null) {
      return failure(
        "no_observation",
        "no page has been observed yet; call open_page first",
      );
    }
    const observation = this.#observation;
    if (this.#failures.failed(observation, "assert_text", undefined, undefined, text)) {
      return this.assertionFailure(
        strategyRepeated(),
        observation,
      );
    }
    let step: Step;
    try {
      step = deriveAssertTextStep(
        this.#steps.length,
        intent,
        text,
        observation.url,
      );
    } catch (error) {
      this.#failures.record(
        observation,
        "assert_text",
        "step_rejected",
        undefined,
        undefined,
        text,
      );
      return this.assertionFailure(
        failure("step_rejected", errorMessage(error)),
        observation,
      );
    }
    if (!observationHasText(observation, text)) {
      this.#failures.record(
        observation,
        "assert_text",
        "condition_unmet",
        undefined,
        undefined,
        text,
      );
      return this.assertionFailure(
        failure(
          "condition_unmet",
          `the text ${JSON.stringify(text)} is not present in the current observation`,
        ),
        observation,
      );
    }
    this.#steps.push(step);
    return {
      ok: true,
      summary: `recorded assert_text step ${step.index}`,
      page: pageView(observation, this.#goal),
      steps: stepViews(this.#steps),
    };
  }

  private assertURL(contains: string, intent: string): ToolResult {
    if (!this.#hasPage || this.#observation === null) {
      return failure(
        "no_observation",
        "no page has been observed yet; call open_page first",
      );
    }
    const observation = this.#observation;
    if (this.#failures.failed(observation, "assert_url", undefined, undefined, contains)) {
      return this.assertionFailure(
        strategyRepeated(),
        observation,
      );
    }
    let step: Step;
    try {
      step = deriveAssertURLStep(
        this.#steps.length,
        intent,
        contains,
        observation.url,
      );
    } catch (error) {
      this.#failures.record(
        observation,
        "assert_url",
        "step_rejected",
        undefined,
        undefined,
        contains,
      );
      return this.assertionFailure(
        failure("step_rejected", errorMessage(error)),
        observation,
      );
    }
    if (!observation.url.includes(contains)) {
      this.#failures.record(
        observation,
        "assert_url",
        "condition_unmet",
        undefined,
        undefined,
        contains,
      );
      return this.assertionFailure(
        failure(
          "condition_unmet",
          `the current url ${JSON.stringify(this.#observation.url)} does not contain ${JSON.stringify(contains)}`,
        ),
        observation,
      );
    }
    this.#steps.push(step);
    return {
      ok: true,
      summary: `recorded assert_url step ${step.index}`,
      page: pageView(this.#observation, this.#goal),
      steps: stepViews(this.#steps),
    };
  }

  private async finishCase(
    name: string,
    signal?: AbortSignal,
  ): Promise<ToolOutcome> {
    const artifact = this.build(name);
    if (artifact === null) {
      return {
        result: failure("case_invalid", "no steps have been authored yet"),
      };
    }
    let execution: ExecutionResult;
    try {
      execution = await this.#worker.execute(
        this.#artifactScope,
        artifact,
        signal,
      );
    } catch (error) {
      return {
        result: failure(CODE_DRY_RUN_FAILED, errorMessage(error)),
      };
    }
    if (execution.status !== "passed") {
      const repair = await this.prepareDryRunRepair(execution, signal);
      return { result: repair, case: artifact, dryRun: execution };
    }
    return {
      result: {
        ok: true,
        summary: `the case passed a full dry run (${artifact.steps.length} steps); it is ready for approval`,
        steps: stepViews(this.#steps),
      },
      case: artifact,
      dryRun: execution,
    };
  }

  private build(name: string): Case | null {
    if (this.#steps.length === 0) {
      return null;
    }
    const artifact: Case = {
      case_version: CASE_VERSION,
      name,
      goal: this.#goal,
      base_url: this.#baseUrl,
      steps: this.#steps.map((step) => ({ ...step })),
    };
    try {
      validateCase(artifact);
    } catch (error) {
      if (error instanceof CaseContractError) {
        throw new Error(`${error.code}: ${error.message}`);
      }
      throw error;
    }
    return artifact;
  }

  async prepareDryRunRepair(
    execution: ExecutionResult,
    signal?: AbortSignal,
  ): Promise<ToolResult> {
    return runPrepareDryRunRepair(this, execution, signal);
  }

  private assertionFailure(
    result: ToolResult,
    observation: Observation,
  ): ToolResult {
    return {
      ...result,
      page: pageView(observation, this.#goal),
      steps: stepViews(this.#steps),
    };
  }

  private async restoreAfterFailedAction(
    result: ToolResult,
    signal?: AbortSignal,
  ): Promise<ToolResult> {
    try {
      await replayCommitted(this, this.#steps.length, signal);
    } catch (error) {
      throw new FatalCaseBuilderError(
        CODE_COMMITTED_PREFIX_REPLAY_FAILED,
        `${authoringFailureSummary(result)}; replay committed prefix: ${errorMessage(error)}`,
        result,
      );
    }
    result.steps = stepViews(this.#steps);
    if (this.#hasPage && this.#observation !== null) {
      result.page = pageView(this.#observation, this.#goal);
    }
    return result;
  }

  async navigateWithRetry(
    url: string,
    signal?: AbortSignal,
  ): Promise<Observation> {
    if (this.#browserSessionId === "") {
      this.#browserSessionId = await this.#worker.openSession(
        this.#artifactScope,
        signal,
      );
    }
    let lastError: unknown;
    for (let attempt = 1; attempt <= NAVIGATION_MAX_ATTEMPTS; attempt += 1) {
      try {
        const observation = await this.#worker.navigate(
          this.#browserSessionId,
          url,
          signal,
        );
        const marker = siteOverloadMarker(observation);
        if (marker !== "") {
          throw new SiteOverloadedError(
            `${CODE_SITE_OVERLOADED}: navigation returned an overload page matching ${JSON.stringify(marker)} (title ${JSON.stringify(observation.title.trim())})`,
          );
        }
        if (isEmptyNavigationObservation(observation)) {
          lastError = new Error("navigation returned an empty observation");
        } else {
          return observation;
        }
      } catch (error) {
        lastError = error;
        if (error instanceof SiteOverloadedError) {
          throw error;
        }
        if (!isTransientNavigationError(error)) {
          throw error;
        }
      }
      if (attempt === NAVIGATION_MAX_ATTEMPTS) {
        break;
      }
      await sleep(NAVIGATION_RETRY_DELAY_MS, signal);
    }
    throw lastError instanceof Error
      ? lastError
      : new Error(`navigate to ${url} failed`);
  }
}

class SiteOverloadedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "SiteOverloadedError";
  }
}

function isTransientNavigationError(error: unknown): boolean {
  if (error instanceof WorkerTransportError) {
    return (
      error.statusCode === 0 ||
      error.statusCode === 408 ||
      error.statusCode === 429 ||
      error.statusCode >= 500
    );
  }
  if (error instanceof DOMException && error.name === "AbortError") {
    return false;
  }
  return false;
}

function isEmptyNavigationObservation(observation: Observation): boolean {
  return (
    observation.title.trim() === "" &&
    observation.elements.length === 0 &&
    observation.structures.length === 0 &&
    observation.action_candidates.length === 0 &&
    observation.blockers.length === 0 &&
    (observation.screenshot_path === null ||
      observation.screenshot_path.trim() === "")
  );
}

function siteOverloadMarker(observation: Observation): string {
  const contains = (text: string): boolean => {
    const needle = normalize(text);
    return (
      normalize(observation.title).includes(needle) ||
      observationHasText(observation, text)
    );
  };
  if (contains("this website is under heavy load")) {
    return "this website is under heavy load";
  }
  if (
    contains("queue full") &&
    contains("too many people are accessing this website at the same time")
  ) {
    return "queue full";
  }
  return "";
}

function parseExpects(args: Record<string, unknown>): Expects {
  const expects: Expects = {};
  const map: Record<keyof Expects, string> = {
    text: "expect_text",
    gone: "expect_gone",
    url: "expect_url",
    value: "expect_value",
    element: "expect_element",
    attribute: "expect_attribute",
    count: "expect_count",
  };
  for (const [key, argName] of Object.entries(map) as Array<
    [keyof Expects, string]
  >) {
    const value = args[argName];
    if (typeof value === "string") {
      expects[key] = value;
    }
  }
  return expects;
}

function stringArg(
  args: Record<string, unknown>,
  key: string,
  optional = false,
): string {
  const value = args[key];
  if (typeof value !== "string") {
    if (optional) return "";
    throw new Error(`tool argument ${JSON.stringify(key)} must be a string`);
  }
  return value;
}

function booleanArg(args: Record<string, unknown>, key: string): boolean {
  const value = args[key];
  return value === true;
}

function targetSpecArg(
  args: Record<string, unknown>,
  key: string,
): TargetSpec | undefined {
  const value = args[key];
  if (value === undefined || value === null) return undefined;
  if (typeof value !== "object") {
    throw new Error(`tool argument ${JSON.stringify(key)} must be an object`);
  }
  return value as TargetSpec;
}

function failure(error: string, detail: string): ToolResult {
  return { ok: false, error, detail };
}

function strategyRepeated(): ToolResult {
  return {
    ok: false,
    error: CODE_STRATEGY_REPEATED,
    detail:
      "this action and target already failed on the unchanged page; change target, scope, action, or page state",
  };
}

function pageView(observation: Observation, goal: string): PageView {
  return buildPageView(observation, goal, null);
}

function stepViews(steps: readonly Step[]): StepView[] {
  return steps.map((step) => ({
    index: step.index,
    action: step.action,
    intent: step.intent,
    ...(step.value !== undefined && step.value !== null
      ? { value: step.value }
      : {}),
    ...(step.target !== undefined && step.target !== null
      ? { target: step.target.hint }
      : {}),
    preconditions: step.preconditions.map(
      (condition) => `${condition.type}:${condition.value}`,
    ),
    postconditions: step.postconditions.map(
      (condition) => `${condition.type}:${condition.value}`,
    ),
  }));
}

function displayName(element: {
  name?: string | null;
  text?: string | null;
  ref: string;
}): string {
  if ((element.name ?? "").trim() !== "") return element.name!;
  if ((element.text ?? "").trim() !== "") return element.text!;
  return element.ref;
}

function origin(rawURL: string): string {
  try {
    const parsed = new URL(rawURL);
    return `${parsed.protocol}//${parsed.host}`;
  } catch {
    return "";
  }
}

function isAbsoluteUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol.length > 1 && url.host.length > 0;
  } catch {
    return false;
  }
}

function normalize(value: string): string {
  return value.toLowerCase().trim().split(/\s+/u).filter(Boolean).join(" ");
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted === true) {
      reject(signal.reason);
      return;
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    const onAbort = (): void => {
      clearTimeout(timer);
      reject(signal?.reason);
    };
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}
