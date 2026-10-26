export const CASE_VERSION = "loop.case.v1" as const;
export const DEFAULT_CONDITION_TIMEOUT_MS = 10_000;
export const DEFAULT_STEP_TIMEOUT_MS = 20_000;

export const ACTIONS = [
  "goto",
  "click",
  "input",
  "select",
  "check",
  "uncheck",
  "scroll_into_view",
  "hover",
  "dismiss_dialog",
  "upload_file",
  "assert_text",
  "assert_url",
  "assert_element",
  "assert_attribute",
  "assert_count",
] as const;

export const CONDITION_TYPES = [
  "url_contains",
  "text_visible",
  "text_gone",
  "url_changes",
  "value_equals",
  "element_state",
  "attribute_equals",
  "count_equals",
] as const;

export const CASE_ERROR_CODES = [
  "case_invalid_json",
  "case_version_mismatch",
  "case_name_required",
  "case_empty_steps",
  "case_not_goto_first",
  "step_index_mismatch",
  "step_intent_required",
  "step_unknown_action",
  "step_missing_value",
  "step_unexpected_value",
  "step_missing_target",
  "step_unexpected_target",
  "step_target_ungrounded",
  "locator_invalid",
  "step_missing_precondition",
  "step_missing_postcondition",
  "goto_precondition_forbidden",
  "condition_unknown_type",
  "condition_phase",
  "condition_missing_value",
  "condition_missing_timeout",
  "goto_value_not_absolute",
  "step_unexpected_submit",
] as const;

export type Action = (typeof ACTIONS)[number];
export type ConditionType = (typeof CONDITION_TYPES)[number];
export type CaseErrorCode = (typeof CASE_ERROR_CODES)[number];

export interface Locator {
  kind: string;
  role?: string;
  name?: string;
  exact?: boolean;
  text?: string;
  css?: string;
  match_count: number;
}

export interface Grounding {
  observation_id: string;
  page_state_id: string;
  candidate_id: string;
  page_url: string;
}

export interface TargetObject {
  role?: string;
  text?: string;
  name?: string;
  aliases?: string[];
}

export interface TargetScope {
  kind?: string;
  contains_text?: string;
  ref?: string;
}

export interface TargetSpec {
  object: TargetObject;
  scope?: TargetScope;
  relation?: string;
  role?: string;
  text?: string;
  name?: string;
  aliases?: string[];
}

export interface Target {
  hint: string;
  locator: Locator;
  grounding: Grounding;
  spec?: TargetSpec;
}

export interface Condition {
  type: ConditionType;
  value: string;
  timeout_ms: number;
}

export interface Step {
  index: number;
  action: Action;
  intent: string;
  value?: string | null;
  submit: boolean;
  target?: Target | null;
  preconditions: Condition[];
  postconditions: Condition[];
  timeout_ms: number;
}

export interface Case {
  case_version: typeof CASE_VERSION;
  name: string;
  goal: string;
  base_url: string;
  steps: Step[];
}

export interface BoundingBox {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface ElementFormInfo {
  form_ref: string;
  submit_candidate_ref?: string | null;
  enter_submittable: boolean;
}

export interface Element {
  ref: string;
  tag: string;
  role?: string | null;
  name?: string | null;
  text?: string | null;
  value: string | null;
  visible: boolean;
  enabled: boolean;
  locators: Locator[];
  parent_ref?: string | null;
  container_ref?: string | null;
  own_text?: string | null;
  full_text?: string | null;
  bbox?: BoundingBox;
  visible_in_viewport?: boolean;
  z_index?: number | null;
  attributes?: Record<string, string>;
  form?: ElementFormInfo | null;
}

export interface StructureNode {
  ref: string;
  kind: string;
  tag: string;
  role?: string | null;
  parent_ref?: string | null;
  full_text: string;
  visible: boolean;
  bbox?: BoundingBox;
  attributes?: Record<string, string>;
  submit_candidate_ref?: string | null;
  enter_submittable?: boolean;
}

export interface Blocker {
  kind: string;
  ref?: string | null;
  confidence: string;
  covers_target_ref?: string | null;
  dismiss_candidates?: Locator[];
  reason: string;
}

export interface CandidateRelation {
  type: string;
  ref: string;
  label?: string | null;
}

export interface ActionCandidate {
  candidate_id: string;
  kind: string;
  action: string;
  target_ref: string;
  role?: string | null;
  name?: string | null;
  text?: string | null;
  aliases?: string[];
  attributes?: Record<string, string>;
  relations?: CandidateRelation[];
  locator: Locator;
  confidence: string;
}

export interface Observation {
  observation_id: string;
  page_state_id: string;
  browser_session_id: string | null;
  url: string;
  title: string;
  elements: Element[];
  structures: StructureNode[];
  blockers: Blocker[];
  action_candidates: ActionCandidate[];
  truncated: boolean;
  truncation_reason: string | null;
  screenshot_path: string | null;
}

export interface ActRequest {
  action: Exclude<Action, "goto" | "assert_text" | "assert_url">;
  locator: Locator;
  value?: string | null;
  submit?: boolean;
  postconditions: Condition[];
}

export interface StepError {
  kind: string;
  message: string;
}

export interface ConditionResult {
  phase: "pre" | "post";
  type: string;
  value: string;
  satisfied: boolean;
  detail: string | null;
}

export interface ActResponse {
  status: "passed" | "failed";
  observation: Observation;
  conditions: ConditionResult[];
  error: StepError | null;
}

export interface ConsoleEntry {
  level: string;
  text: string;
}

export interface NetworkEntry {
  method: string;
  url: string;
  status: number | null;
}

export interface Evidence {
  screenshot_path: string | null;
  console: ConsoleEntry[];
  network: NetworkEntry[];
}

export interface HitTest {
  target_ref?: string | null;
  x: number;
  y: number;
  hit_ref?: string | null;
  hit_tag?: string | null;
  hit_role?: string | null;
  hit_text?: string | null;
  covered: boolean;
  blocker_kind?: string | null;
}

export interface RecoveryAttempt {
  blocker: Blocker;
  action: string;
  succeeded: boolean;
  reason?: string;
  before_screenshot_path?: string | null;
  after_screenshot_path?: string | null;
  url_before?: string | null;
  url_after?: string | null;
  retried_original_action: boolean;
}

export interface StepResult {
  index: number;
  action: Action;
  status: "passed" | "failed";
  started_at: string;
  duration_ms: number;
  url_before: string;
  url_after: string;
  conditions: ConditionResult[];
  evidence: Evidence;
  error: StepError | null;
  blocker?: Blocker | null;
  hit_test?: HitTest | null;
  recovery?: RecoveryAttempt[];
}

export interface ExecutionResult {
  execution_id: string;
  status: "passed" | "failed" | "error";
  started_at: string;
  finished_at: string;
  final_url: string;
  steps: StepResult[];
  error: StepError | null;
}

export class CaseContractError extends Error {
  constructor(
    public readonly code: CaseErrorCode,
    public readonly step: number,
    public readonly detail: string,
  ) {
    super(detail);
    this.name = "CaseContractError";
  }
}

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
const VALUE_ACTIONS = new Set<Action>(["input", "select", "upload_file"]);
const PRECONDITIONS = new Set<ConditionType>([
  "url_contains",
  "text_visible",
  "text_gone",
  "element_state",
]);
const ACTION_SET = new Set<string>(ACTIONS);
const CONDITION_SET = new Set<string>(CONDITION_TYPES);

function contractError(
  code: CaseErrorCode,
  step: number,
  detail: string,
): never {
  throw new CaseContractError(code, step, detail);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function checkKeys(
  value: Record<string, unknown>,
  allowed: readonly string[],
  where: string,
): void {
  const allowedSet = new Set(allowed);
  const unknown = Object.keys(value).find((key) => !allowedSet.has(key));
  if (unknown !== undefined) {
    contractError(
      "case_invalid_json",
      -1,
      `${where} contains unknown field ${JSON.stringify(unknown)}`,
    );
  }
}

function stringField(
  value: Record<string, unknown>,
  key: string,
  fallback = "",
): string {
  const field = value[key];
  if (field === undefined) {
    return fallback;
  }
  if (typeof field !== "string") {
    contractError("case_invalid_json", -1, `${key} must be a string`);
  }
  return field;
}

function booleanField(
  value: Record<string, unknown>,
  key: string,
  fallback = false,
): boolean {
  const field = value[key];
  if (field === undefined) {
    return fallback;
  }
  if (typeof field !== "boolean") {
    contractError("case_invalid_json", -1, `${key} must be a boolean`);
  }
  return field;
}

function numberField(
  value: Record<string, unknown>,
  key: string,
  fallback = 0,
): number {
  const field = value[key];
  if (field === undefined) {
    return fallback;
  }
  if (typeof field !== "number" || !Number.isInteger(field)) {
    contractError("case_invalid_json", -1, `${key} must be an integer`);
  }
  return field;
}

function stringArray(value: unknown, where: string): string[] {
  if (!Array.isArray(value) || value.some((item) => typeof item !== "string")) {
    contractError("case_invalid_json", -1, `${where} must be a string array`);
  }
  return [...value] as string[];
}

function parseCondition(input: unknown): Condition {
  if (!isRecord(input)) {
    contractError("case_invalid_json", -1, "condition must be an object");
  }
  checkKeys(input, ["type", "value", "timeout_ms"], "condition");
  return {
    type: stringField(input, "type") as ConditionType,
    value: stringField(input, "value"),
    timeout_ms: numberField(input, "timeout_ms", DEFAULT_CONDITION_TIMEOUT_MS),
  };
}

function parseLocator(input: unknown): Locator {
  if (!isRecord(input)) {
    contractError("case_invalid_json", -1, "locator must be an object");
  }
  checkKeys(
    input,
    ["kind", "role", "name", "exact", "text", "css", "match_count"],
    "locator",
  );
  const locator: Locator = {
    kind: stringField(input, "kind"),
    match_count: numberField(input, "match_count"),
  };
  for (const key of ["role", "name", "text", "css"] as const) {
    if (input[key] !== undefined) {
      locator[key] = stringField(input, key);
    }
  }
  if (input.exact !== undefined) {
    locator.exact = booleanField(input, "exact");
  }
  return locator;
}

function parseGrounding(input: unknown): Grounding {
  if (!isRecord(input)) {
    contractError("case_invalid_json", -1, "grounding must be an object");
  }
  checkKeys(
    input,
    ["observation_id", "page_state_id", "candidate_id", "page_url"],
    "grounding",
  );
  return {
    observation_id: stringField(input, "observation_id"),
    page_state_id: stringField(input, "page_state_id"),
    candidate_id: stringField(input, "candidate_id"),
    page_url: stringField(input, "page_url"),
  };
}

function parseTargetObject(input: unknown): TargetObject {
  if (!isRecord(input)) {
    contractError("case_invalid_json", -1, "target.spec.object must be an object");
  }
  checkKeys(input, ["role", "text", "name", "aliases"], "target.spec.object");
  const result: TargetObject = {};
  for (const key of ["role", "text", "name"] as const) {
    if (input[key] !== undefined) {
      result[key] = stringField(input, key);
    }
  }
  if (input.aliases !== undefined) {
    result.aliases = stringArray(input.aliases, "target.spec.object.aliases");
  }
  return result;
}

function parseTargetScope(input: unknown): TargetScope {
  if (!isRecord(input)) {
    contractError("case_invalid_json", -1, "target.spec.scope must be an object");
  }
  checkKeys(input, ["kind", "contains_text", "ref"], "target.spec.scope");
  const result: TargetScope = {};
  for (const key of ["kind", "contains_text", "ref"] as const) {
    if (input[key] !== undefined) {
      result[key] = stringField(input, key);
    }
  }
  return result;
}

function parseTargetSpec(input: unknown): TargetSpec {
  if (!isRecord(input)) {
    contractError("case_invalid_json", -1, "target.spec must be an object");
  }
  checkKeys(
    input,
    ["object", "scope", "relation", "role", "text", "name", "aliases"],
    "target.spec",
  );
  const result: TargetSpec = {
    object:
      input.object === undefined ? {} : parseTargetObject(input.object),
  };
  if (input.scope !== undefined && input.scope !== null) {
    result.scope = parseTargetScope(input.scope);
  }
  for (const key of ["relation", "role", "text", "name"] as const) {
    if (input[key] !== undefined) {
      result[key] = stringField(input, key);
    }
  }
  if (input.aliases !== undefined) {
    result.aliases = stringArray(input.aliases, "target.spec.aliases");
  }
  return result;
}

function parseTarget(input: unknown): Target {
  if (!isRecord(input)) {
    contractError("case_invalid_json", -1, "target must be an object");
  }
  checkKeys(input, ["hint", "locator", "grounding", "spec"], "target");
  const target: Target = {
    hint: stringField(input, "hint"),
    locator:
      input.locator === undefined
        ? { kind: "", match_count: 0 }
        : parseLocator(input.locator),
    grounding:
      input.grounding === undefined
        ? {
            observation_id: "",
            page_state_id: "",
            candidate_id: "",
            page_url: "",
          }
        : parseGrounding(input.grounding),
  };
  if (input.spec !== undefined && input.spec !== null) {
    target.spec = parseTargetSpec(input.spec);
  }
  return target;
}

function parseStep(input: unknown): Step {
  if (!isRecord(input)) {
    contractError("case_invalid_json", -1, "step must be an object");
  }
  checkKeys(
    input,
    [
      "index",
      "action",
      "intent",
      "value",
      "submit",
      "target",
      "preconditions",
      "postconditions",
      "timeout_ms",
    ],
    "step",
  );
  const value = input.value;
  if (value !== undefined && value !== null && typeof value !== "string") {
    contractError("case_invalid_json", -1, "step.value must be a string or null");
  }
  const target = input.target;
  if (target !== undefined && target !== null && !isRecord(target)) {
    contractError("case_invalid_json", -1, "step.target must be an object or null");
  }
  const preconditions = input.preconditions ?? [];
  const postconditions = input.postconditions ?? [];
  if (!Array.isArray(preconditions) || !Array.isArray(postconditions)) {
    contractError("case_invalid_json", -1, "step conditions must be arrays");
  }

  return {
    index: numberField(input, "index"),
    action: stringField(input, "action") as Action,
    intent: stringField(input, "intent"),
    ...(value !== undefined ? { value: value as string | null } : {}),
    submit: booleanField(input, "submit"),
    ...(target !== undefined
      ? { target: target === null ? null : parseTarget(target) }
      : {}),
    preconditions: preconditions.map(parseCondition),
    postconditions: postconditions.map(parseCondition),
    timeout_ms: numberField(input, "timeout_ms", DEFAULT_STEP_TIMEOUT_MS),
  };
}

function normalizeCase(parsed: {
  case_version: string;
  name: string;
  goal: string;
  base_url: string;
  steps: Step[];
}): void {
  if (parsed.case_version === "") {
    parsed.case_version = CASE_VERSION;
  }
  parsed.steps.forEach((step, index) => {
    step.index = index;
    if (step.timeout_ms <= 0) {
      step.timeout_ms = DEFAULT_STEP_TIMEOUT_MS;
    }
    for (const condition of [...step.preconditions, ...step.postconditions]) {
      if (condition.timeout_ms <= 0) {
        condition.timeout_ms = DEFAULT_CONDITION_TIMEOUT_MS;
      }
    }
  });
}

function isAbsoluteUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol.length > 1 && url.host.length > 0;
  } catch {
    return false;
  }
}

function validateLocator(locator: Locator, step: number): void {
  if (locator.kind === "role") {
    if (!locator.role || !locator.name) {
      contractError("locator_invalid", step, "role locator requires role and name");
    }
  } else if (locator.kind === "text") {
    if (!locator.text) {
      contractError("locator_invalid", step, "text locator requires text");
    }
  } else if (locator.kind === "css") {
    if (!locator.css) {
      contractError("locator_invalid", step, "css locator requires css");
    }
  } else {
    contractError(
      "locator_invalid",
      step,
      `unknown locator kind ${JSON.stringify(locator.kind)}`,
    );
  }
}

function validateTarget(target: Target, step: number): void {
  if (target.hint.trim() === "") {
    contractError("step_missing_target", step, "target.hint is required");
  }
  validateLocator(target.locator, step);
  if (
    target.grounding.observation_id === "" ||
    target.grounding.candidate_id === "" ||
    target.grounding.page_url === ""
  ) {
    contractError(
      "step_target_ungrounded",
      step,
      "target requires grounding metadata",
    );
  }
}

function validateCondition(
  condition: Condition,
  phase: "pre" | "post",
  step: number,
): void {
  if (!CONDITION_SET.has(condition.type)) {
    contractError(
      "condition_unknown_type",
      step,
      `condition has unknown type ${JSON.stringify(condition.type)}`,
    );
  }
  if (phase === "pre" && !PRECONDITIONS.has(condition.type)) {
    contractError(
      "condition_phase",
      step,
      `condition ${JSON.stringify(condition.type)} cannot hold before the action`,
    );
  }
  if (condition.value === "") {
    contractError(
      "condition_missing_value",
      step,
      `condition ${JSON.stringify(condition.type)} requires a value`,
    );
  }
  if (condition.timeout_ms <= 0) {
    contractError(
      "condition_missing_timeout",
      step,
      `condition ${JSON.stringify(condition.type)} requires a positive timeout_ms`,
    );
  }
}

export function validateStep(step: Step, index: number): void {
  if (step.index !== index) {
    contractError(
      "step_index_mismatch",
      index,
      `steps[${index}].index must be ${index}, got ${step.index}`,
    );
  }
  if (step.intent.trim() === "") {
    contractError("step_intent_required", index, `steps[${index}].intent is required`);
  }

  const action = step.action;
  if (action === "goto") {
    if (step.target !== undefined && step.target !== null) {
      contractError("step_unexpected_target", index, "goto must not carry a target");
    }
    if (step.value === undefined || step.value === null || step.value.trim() === "") {
      contractError("step_missing_value", index, "goto requires an absolute URL");
    }
    if (!isAbsoluteUrl(step.value)) {
      contractError("goto_value_not_absolute", index, "goto value must be absolute");
    }
    if (step.preconditions.length !== 0) {
      contractError(
        "goto_precondition_forbidden",
        index,
        "goto must not declare preconditions",
      );
    }
  } else if (TARGET_ACTIONS.has(action)) {
    if (step.target === undefined || step.target === null) {
      contractError(
        "step_missing_target",
        index,
        `${action} requires a grounded target`,
      );
    }
    validateTarget(step.target, index);
    if (action === "click" && step.value !== undefined && step.value !== null) {
      contractError("step_unexpected_value", index, "click must not carry a value");
    }
    if (
      VALUE_ACTIONS.has(action) &&
      (step.value === undefined || step.value === null)
    ) {
      contractError("step_missing_value", index, `${action} requires a value`);
    }
    if (
      !VALUE_ACTIONS.has(action) &&
      action !== "click" &&
      step.value !== undefined &&
      step.value !== null
    ) {
      contractError(
        "step_unexpected_value",
        index,
        `${action} must not carry a value`,
      );
    }
    if (step.preconditions.length === 0) {
      contractError(
        "step_missing_precondition",
        index,
        `${action} requires at least one precondition`,
      );
    }
  } else if (action === "assert_text" || action === "assert_url") {
    if (step.target !== undefined && step.target !== null) {
      contractError(
        "step_unexpected_target",
        index,
        `${action} must not carry a target`,
      );
    }
    if (step.value === undefined || step.value === null || step.value === "") {
      contractError("step_missing_value", index, `${action} requires a value`);
    }
    if (step.preconditions.length === 0) {
      contractError(
        "step_missing_precondition",
        index,
        `${action} requires at least one precondition`,
      );
    }
  } else if (!ACTION_SET.has(action)) {
    contractError(
      "step_unknown_action",
      index,
      `unknown action ${JSON.stringify(action)}`,
    );
  }

  if (step.submit && action !== "input") {
    contractError(
      "step_unexpected_submit",
      index,
      `only input may carry submit, got ${action}`,
    );
  }
  if (step.postconditions.length === 0) {
    contractError(
      "step_missing_postcondition",
      index,
      `${action} requires at least one postcondition`,
    );
  }
  step.preconditions.forEach((condition) =>
    validateCondition(condition, "pre", index),
  );
  step.postconditions.forEach((condition) =>
    validateCondition(condition, "post", index),
  );
}

export function validateCase(input: unknown): Case {
  if (!isRecord(input)) {
    contractError("case_invalid_json", -1, "case must be an object");
  }
  checkKeys(
    input,
    ["case_version", "name", "goal", "base_url", "steps"],
    "case",
  );
  const rawSteps = input.steps ?? [];
  if (!Array.isArray(rawSteps)) {
    contractError("case_invalid_json", -1, "case.steps must be an array");
  }
  const parsed = {
    case_version: stringField(input, "case_version"),
    name: stringField(input, "name"),
    goal: stringField(input, "goal"),
    base_url: stringField(input, "base_url"),
    steps: rawSteps.map(parseStep),
  };
  normalizeCase(parsed);

  if (parsed.case_version !== CASE_VERSION) {
    contractError(
      "case_version_mismatch",
      -1,
      `case.case_version must be ${JSON.stringify(CASE_VERSION)}`,
    );
  }
  if (parsed.name.trim() === "") {
    contractError("case_name_required", -1, "case.name is required");
  }
  if (parsed.steps.length === 0) {
    contractError("case_empty_steps", -1, "case.steps must not be empty");
  }
  if (parsed.steps[0]?.action !== "goto") {
    contractError("case_not_goto_first", 0, "the first step must be goto");
  }
  parsed.steps.forEach(validateStep);
  return parsed as Case;
}

/**
 * The expectations a model declares for a target action. The planner translates
 * these into postconditions; at least one must be declared.
 */
export interface Expects {
  text?: string;
  gone?: string;
  url?: string;
  value?: string;
  element?: string;
  attribute?: string;
  count?: string;
}

export function expectsIsEmpty(expects: Expects): boolean {
  return (
    expects.text === undefined &&
    expects.gone === undefined &&
    expects.url === undefined &&
    expects.value === undefined &&
    expects.element === undefined &&
    expects.attribute === undefined &&
    expects.count === undefined
  );
}

function expectsToConditions(expects: Expects): Condition[] {
  const conditions: Condition[] = [];
  const add = (type: ConditionType, value: string): void => {
    conditions.push({
      type,
      value,
      timeout_ms: DEFAULT_CONDITION_TIMEOUT_MS,
    });
  };
  if (expects.text !== undefined) add("text_visible", expects.text);
  if (expects.gone !== undefined) add("text_gone", expects.gone);
  if (expects.url !== undefined) add("url_contains", expects.url);
  if (expects.value !== undefined) add("value_equals", expects.value);
  if (expects.element !== undefined) add("element_state", expects.element);
  if (expects.attribute !== undefined) add("attribute_equals", expects.attribute);
  if (expects.count !== undefined) add("count_equals", expects.count);
  return conditions;
}

/**
 * Reduces a page URL to a stable anchor used by `url_contains` preconditions.
 * A root path is replaced by the host so the anchor never degenerates to "/".
 */
export function pageAnchor(rawURL: string): string {
  try {
    const parsed = new URL(rawURL);
    const path = parsed.pathname;
    if (path === "" || path === "/") {
      if (parsed.host !== "") {
        return parsed.host;
      }
      return rawURL;
    }
    return path;
  } catch {
    return rawURL;
  }
}

const TARGET_ACTIONS_FOR_DERIVE = new Set<Action>([
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

/**
 * Derives a navigation step. The goto has no preconditions and a single
 * `url_contains` postcondition anchored on the page URL.
 */
export function deriveGotoStep(
  index: number,
  intent: string,
  rawURL: string,
): Step {
  if (!isAbsoluteUrl(rawURL)) {
    throw new Error(`open_page url must be absolute, got ${JSON.stringify(rawURL)}`);
  }
  const step: Step = {
    index,
    action: "goto",
    intent: intent.trim() === "" ? `open ${rawURL}` : intent,
    value: rawURL,
    submit: false,
    preconditions: [],
    postconditions: [
      {
        type: "url_contains",
        value: pageAnchor(rawURL),
        timeout_ms: DEFAULT_CONDITION_TIMEOUT_MS,
      },
    ],
    timeout_ms: DEFAULT_STEP_TIMEOUT_MS,
  };
  validateStep(step, index);
  return step;
}

/**
 * Derives a target-bearing action or assertion step. The precondition is the
 * observed page anchor; the postconditions come from the declared expectations.
 */
export function deriveActionStep(
  index: number,
  action: Action,
  intent: string,
  target: Target,
  value: string | null | undefined,
  submit: boolean,
  observedPageURL: string,
  expects: Expects,
): Step {
  if (!TARGET_ACTIONS_FOR_DERIVE.has(action)) {
    throw new Error(`derive action step: unsupported action ${JSON.stringify(action)}`);
  }
  const anchor = pageAnchor(observedPageURL);
  if (anchor === "") {
    throw new Error("derive action step: the grounding observation has no page url");
  }
  const postconditions = expectsToConditions(expects);
  if (postconditions.length === 0) {
    throw new Error(
      "an action step must declare at least one expectation: expect_text, expect_gone, expect_url, expect_value, expect_element, expect_attribute, or expect_count",
    );
  }
  const step: Step = {
    index,
    action,
    intent,
    submit,
    target,
    preconditions: [
      {
        type: "url_contains",
        value: anchor,
        timeout_ms: DEFAULT_CONDITION_TIMEOUT_MS,
      },
    ],
    postconditions,
    timeout_ms: DEFAULT_STEP_TIMEOUT_MS,
  };
  if (action === "input" || action === "select" || action === "upload_file") {
    step.value = value === undefined || value === null ? "" : value;
  }
  validateStep(step, index);
  return step;
}

/**
 * Derives a page-level text assertion. The text must already be visible on the
 * current observation; it is not sent to the worker.
 */
export function deriveAssertTextStep(
  index: number,
  intent: string,
  text: string,
  observedPageURL: string,
): Step {
  if (text === "") {
    throw new Error("assert_text requires a non-empty text");
  }
  const anchor = pageAnchor(observedPageURL);
  if (anchor === "") {
    throw new Error("assert_text requires a grounded page url");
  }
  const step: Step = {
    index,
    action: "assert_text",
    intent,
    value: text,
    submit: false,
    preconditions: [
      {
        type: "url_contains",
        value: anchor,
        timeout_ms: DEFAULT_CONDITION_TIMEOUT_MS,
      },
    ],
    postconditions: [
      {
        type: "text_visible",
        value: text,
        timeout_ms: DEFAULT_CONDITION_TIMEOUT_MS,
      },
    ],
    timeout_ms: DEFAULT_STEP_TIMEOUT_MS,
  };
  validateStep(step, index);
  return step;
}

/**
 * Derives a page-level URL substring assertion.
 */
export function deriveAssertURLStep(
  index: number,
  intent: string,
  contains: string,
  observedPageURL: string,
): Step {
  if (contains === "") {
    throw new Error("assert_url requires a non-empty substring");
  }
  const anchor = pageAnchor(observedPageURL);
  if (anchor === "") {
    throw new Error("assert_url requires a grounded page url");
  }
  const step: Step = {
    index,
    action: "assert_url",
    intent,
    value: contains,
    submit: false,
    preconditions: [
      {
        type: "url_contains",
        value: anchor,
        timeout_ms: DEFAULT_CONDITION_TIMEOUT_MS,
      },
    ],
    postconditions: [
      {
        type: "url_contains",
        value: contains,
        timeout_ms: DEFAULT_CONDITION_TIMEOUT_MS,
      },
    ],
    timeout_ms: DEFAULT_STEP_TIMEOUT_MS,
  };
  validateStep(step, index);
  return step;
}

export class WorkerPayloadError extends Error {
  constructor(detail: string) {
    super(detail);
    this.name = "WorkerPayloadError";
  }
}

function payloadRecord(value: unknown, where: string): Record<string, unknown> {
  if (!isRecord(value)) {
    throw new WorkerPayloadError(`${where} must be an object`);
  }
  return value;
}

function requireString(
  value: Record<string, unknown>,
  key: string,
  where: string,
): string {
  const field = value[key];
  if (typeof field !== "string") {
    throw new WorkerPayloadError(`${where}.${key} must be a string`);
  }
  return field;
}

function requireNumber(
  value: Record<string, unknown>,
  key: string,
  where: string,
): number {
  const field = value[key];
  if (typeof field !== "number" || !Number.isFinite(field)) {
    throw new WorkerPayloadError(`${where}.${key} must be a number`);
  }
  return field;
}

function requireBoolean(
  value: Record<string, unknown>,
  key: string,
  where: string,
): boolean {
  const field = value[key];
  if (typeof field !== "boolean") {
    throw new WorkerPayloadError(`${where}.${key} must be a boolean`);
  }
  return field;
}

function requireArray(
  value: Record<string, unknown>,
  key: string,
  where: string,
): unknown[] {
  const field = value[key];
  if (!Array.isArray(field)) {
    throw new WorkerPayloadError(`${where}.${key} must be an array`);
  }
  return field;
}

function requireNullableString(
  value: Record<string, unknown>,
  key: string,
  where: string,
): string | null {
  const field = value[key];
  if (field !== null && typeof field !== "string") {
    throw new WorkerPayloadError(`${where}.${key} must be a string or null`);
  }
  return field;
}

function optionalNullableString(
  value: Record<string, unknown>,
  key: string,
  where: string,
): string | null {
  return requireNullableString(value, key, where);
}

function optionalBoolean(
  value: Record<string, unknown>,
  key: string,
  where: string,
): boolean {
  return requireBoolean(value, key, where);
}

function optionalNullableNumber(
  value: Record<string, unknown>,
  key: string,
  where: string,
): number | null {
  const field = value[key];
  if (field === null) {
    return null;
  }
  if (typeof field !== "number" || !Number.isFinite(field)) {
    throw new WorkerPayloadError(`${where}.${key} must be a number or null`);
  }
  return field;
}

function optionalArray(
  value: Record<string, unknown>,
  key: string,
  where: string,
): unknown[] | undefined {
  if (value[key] === undefined) {
    return undefined;
  }
  return requireArray(value, key, where);
}

function parseStringArray(value: unknown, where: string): string[] {
  if (!Array.isArray(value)) {
    throw new WorkerPayloadError(`${where} must be an array`);
  }
  return value.map((item, index) => {
    if (typeof item !== "string") {
      throw new WorkerPayloadError(`${where}[${index}] must be a string`);
    }
    return item;
  });
}

function parseStringRecord(value: unknown, where: string): Record<string, string> {
  const record = payloadRecord(value, where);
  return Object.fromEntries(
    Object.entries(record).map(([key, item]) => {
      if (typeof item !== "string") {
        throw new WorkerPayloadError(`${where}.${key} must be a string`);
      }
      return [key, item];
    }),
  );
}

function parseWorkerLocator(value: unknown, where: string): Locator {
  const record = payloadRecord(value, where);
  const kind = requireString(record, "kind", where);
  const matchCount = requireNumber(record, "match_count", where);
  if (!Number.isInteger(matchCount)) {
    throw new WorkerPayloadError(`${where}.match_count must be an integer`);
  }
  if (kind === "role") {
    return {
      kind,
      role: requireString(record, "role", where),
      name: requireString(record, "name", where),
      exact: requireBoolean(record, "exact", where),
      match_count: matchCount,
    };
  }
  if (kind === "text") {
    return {
      kind,
      text: requireString(record, "text", where),
      exact: requireBoolean(record, "exact", where),
      match_count: matchCount,
    };
  }
  if (kind === "css") {
    return {
      kind,
      css: requireString(record, "css", where),
      match_count: matchCount,
    };
  }
  throw new WorkerPayloadError(`${where}.kind is invalid`);
}

function parseBoundingBox(value: unknown, where: string): BoundingBox {
  const record = payloadRecord(value, where);
  return {
    x: requireNumber(record, "x", where),
    y: requireNumber(record, "y", where),
    width: requireNumber(record, "width", where),
    height: requireNumber(record, "height", where),
  };
}

function parseElementFormInfo(value: unknown, where: string): ElementFormInfo {
  const record = payloadRecord(value, where);
  return {
    form_ref: requireString(record, "form_ref", where),
    ...(record.submit_candidate_ref !== undefined
      ? {
          submit_candidate_ref: optionalNullableString(
            record,
            "submit_candidate_ref",
            where,
          ),
        }
      : {}),
    enter_submittable: requireBoolean(record, "enter_submittable", where),
  };
}

function parseElement(value: unknown, index: number): Element {
  const where = `observation.elements[${index}]`;
  const record = payloadRecord(value, where);
  return {
    ref: requireString(record, "ref", where),
    tag: requireString(record, "tag", where),
    ...(record.role !== undefined
      ? { role: optionalNullableString(record, "role", where) }
      : {}),
    ...(record.name !== undefined
      ? { name: optionalNullableString(record, "name", where) }
      : {}),
    ...(record.text !== undefined
      ? { text: optionalNullableString(record, "text", where) }
      : {}),
    value: requireNullableString(record, "value", where),
    visible: requireBoolean(record, "visible", where),
    enabled: requireBoolean(record, "enabled", where),
    locators: requireArray(record, "locators", where).map((locator, locatorIndex) =>
      parseWorkerLocator(locator, `${where}.locators[${locatorIndex}]`),
    ),
    ...(record.parent_ref !== undefined
      ? { parent_ref: optionalNullableString(record, "parent_ref", where) }
      : {}),
    ...(record.container_ref !== undefined
      ? { container_ref: optionalNullableString(record, "container_ref", where) }
      : {}),
    ...(record.own_text !== undefined
      ? { own_text: optionalNullableString(record, "own_text", where) }
      : {}),
    ...(record.full_text !== undefined
      ? { full_text: optionalNullableString(record, "full_text", where) }
      : {}),
    ...(record.bbox !== undefined
      ? { bbox: parseBoundingBox(record.bbox, `${where}.bbox`) }
      : {}),
    ...(record.visible_in_viewport !== undefined
      ? {
          visible_in_viewport: optionalBoolean(
            record,
            "visible_in_viewport",
            where,
          ),
        }
      : {}),
    ...(record.z_index !== undefined
      ? { z_index: optionalNullableNumber(record, "z_index", where) }
      : {}),
    ...(record.attributes !== undefined
      ? {
          attributes: parseStringRecord(
            record.attributes,
            `${where}.attributes`,
          ),
        }
      : {}),
    ...(record.form !== undefined
      ? {
          form:
            record.form === null
              ? null
              : parseElementFormInfo(record.form, `${where}.form`),
        }
      : {}),
  };
}

function parseStructureNode(value: unknown, index: number): StructureNode {
  const where = `observation.structures[${index}]`;
  const record = payloadRecord(value, where);
  return {
    ref: requireString(record, "ref", where),
    kind: requireString(record, "kind", where),
    tag: requireString(record, "tag", where),
    ...(record.role !== undefined
      ? { role: optionalNullableString(record, "role", where) }
      : {}),
    ...(record.parent_ref !== undefined
      ? { parent_ref: optionalNullableString(record, "parent_ref", where) }
      : {}),
    full_text: requireString(record, "full_text", where),
    visible: requireBoolean(record, "visible", where),
    ...(record.bbox !== undefined
      ? { bbox: parseBoundingBox(record.bbox, `${where}.bbox`) }
      : {}),
    ...(record.attributes !== undefined
      ? {
          attributes: parseStringRecord(
            record.attributes,
            `${where}.attributes`,
          ),
        }
      : {}),
    ...(record.submit_candidate_ref !== undefined
      ? {
          submit_candidate_ref: optionalNullableString(
            record,
            "submit_candidate_ref",
            where,
          ),
        }
      : {}),
    ...(record.enter_submittable !== undefined
      ? {
          enter_submittable: optionalBoolean(
            record,
            "enter_submittable",
            where,
          ),
        }
      : {}),
  };
}

function parseBlocker(value: unknown, where: string): Blocker {
  const record = payloadRecord(value, where);
  const confidence = requireString(record, "confidence", where);
  if (
    confidence !== "high" &&
    confidence !== "medium" &&
    confidence !== "low"
  ) {
    throw new WorkerPayloadError(`${where}.confidence is invalid`);
  }
  const dismissCandidates = optionalArray(
    record,
    "dismiss_candidates",
    where,
  );
  return {
    kind: requireString(record, "kind", where),
    ...(record.ref !== undefined
      ? { ref: optionalNullableString(record, "ref", where) }
      : {}),
    confidence,
    ...(record.covers_target_ref !== undefined
      ? {
          covers_target_ref: optionalNullableString(
            record,
            "covers_target_ref",
            where,
          ),
        }
      : {}),
    ...(dismissCandidates !== undefined
      ? {
          dismiss_candidates: dismissCandidates.map((locator, index) =>
            parseWorkerLocator(
              locator,
              `${where}.dismiss_candidates[${index}]`,
            ),
          ),
        }
      : {}),
    reason: requireString(record, "reason", where),
  };
}

function parseCandidateRelation(
  value: unknown,
  where: string,
): CandidateRelation {
  const record = payloadRecord(value, where);
  return {
    type: requireString(record, "type", where),
    ref: requireString(record, "ref", where),
    ...(record.label !== undefined
      ? { label: optionalNullableString(record, "label", where) }
      : {}),
  };
}

function parseActionCandidate(
  value: unknown,
  index: number,
): ActionCandidate {
  const where = `observation.action_candidates[${index}]`;
  const record = payloadRecord(value, where);
  const confidence = requireString(record, "confidence", where);
  if (
    confidence !== "high" &&
    confidence !== "medium" &&
    confidence !== "low"
  ) {
    throw new WorkerPayloadError(`${where}.confidence is invalid`);
  }
  const aliases = optionalArray(record, "aliases", where);
  const relations = optionalArray(record, "relations", where);
  return {
    candidate_id: requireString(record, "candidate_id", where),
    kind: requireString(record, "kind", where),
    action: requireString(record, "action", where),
    target_ref: requireString(record, "target_ref", where),
    ...(record.role !== undefined
      ? { role: optionalNullableString(record, "role", where) }
      : {}),
    ...(record.name !== undefined
      ? { name: optionalNullableString(record, "name", where) }
      : {}),
    ...(record.text !== undefined
      ? { text: optionalNullableString(record, "text", where) }
      : {}),
    ...(aliases !== undefined
      ? { aliases: parseStringArray(aliases, `${where}.aliases`) }
      : {}),
    ...(record.attributes !== undefined
      ? {
          attributes: parseStringRecord(
            record.attributes,
            `${where}.attributes`,
          ),
        }
      : {}),
    ...(relations !== undefined
      ? {
          relations: relations.map((relation, relationIndex) =>
            parseCandidateRelation(
              relation,
              `${where}.relations[${relationIndex}]`,
            ),
          ),
        }
      : {}),
    locator: parseWorkerLocator(record.locator, `${where}.locator`),
    confidence,
  };
}

function parseStepError(value: unknown, where: string): StepError | null {
  if (value === null) {
    return null;
  }
  const record = payloadRecord(value, where);
  return {
    kind: requireString(record, "kind", where),
    message: requireString(record, "message", where),
  };
}

function parseConditionResult(value: unknown, where: string): ConditionResult {
  const record = payloadRecord(value, where);
  const phase = requireString(record, "phase", where);
  if (phase !== "pre" && phase !== "post") {
    throw new WorkerPayloadError(`${where}.phase is invalid`);
  }
  return {
    phase,
    type: requireString(record, "type", where),
    value: requireString(record, "value", where),
    satisfied: requireBoolean(record, "satisfied", where),
    detail: requireNullableString(record, "detail", where),
  };
}

export function parseObservation(value: unknown): Observation {
  const record = payloadRecord(value, "observation");
  return {
    observation_id: requireString(record, "observation_id", "observation"),
    page_state_id: requireString(record, "page_state_id", "observation"),
    browser_session_id: requireNullableString(
      record,
      "browser_session_id",
      "observation",
    ),
    url: requireString(record, "url", "observation"),
    title: requireString(record, "title", "observation"),
    elements: requireArray(record, "elements", "observation").map(parseElement),
    structures: requireArray(record, "structures", "observation").map(
      parseStructureNode,
    ),
    blockers: requireArray(record, "blockers", "observation").map(
      (blocker, index) =>
        parseBlocker(blocker, `observation.blockers[${index}]`),
    ),
    action_candidates: requireArray(
      record,
      "action_candidates",
      "observation",
    ).map(parseActionCandidate),
    truncated: requireBoolean(record, "truncated", "observation"),
    truncation_reason: requireNullableString(
      record,
      "truncation_reason",
      "observation",
    ),
    screenshot_path: requireNullableString(
      record,
      "screenshot_path",
      "observation",
    ),
  };
}

export function parseActResponse(value: unknown): ActResponse {
  const record = payloadRecord(value, "act response");
  const status = requireString(record, "status", "act response");
  if (status !== "passed" && status !== "failed") {
    throw new WorkerPayloadError("act response.status is invalid");
  }
  return {
    status,
    observation: parseObservation(record.observation),
    conditions: requireArray(record, "conditions", "act response").map(
      (condition, index) =>
        parseConditionResult(condition, `act response.conditions[${index}]`),
    ),
    error: parseStepError(record.error, "act response.error"),
  };
}

function parseEvidence(value: unknown, where: string): Evidence {
  const record = payloadRecord(value, where);
  const consoleEntries = requireArray(record, "console", where);
  const networkEntries = requireArray(record, "network", where);
  return {
    screenshot_path: requireNullableString(record, "screenshot_path", where),
    console: consoleEntries.map((entry, index) => {
      const item = payloadRecord(entry, `${where}.console[${index}]`);
      return {
        level: requireString(item, "level", `${where}.console[${index}]`),
        text: requireString(item, "text", `${where}.console[${index}]`),
      };
    }),
    network: networkEntries.map((entry, index) => {
      const item = payloadRecord(entry, `${where}.network[${index}]`);
      const status = item.status;
      if (
        status !== null &&
        (typeof status !== "number" ||
          !Number.isFinite(status) ||
          !Number.isInteger(status))
      ) {
        throw new WorkerPayloadError(
          `${where}.network[${index}].status must be a number or null`,
        );
      }
      return {
        method: requireString(item, "method", `${where}.network[${index}]`),
        url: requireString(item, "url", `${where}.network[${index}]`),
        status,
      };
    }),
  };
}

function parseHitTest(value: unknown, where: string): HitTest {
  const record = payloadRecord(value, where);
  return {
    ...(record.target_ref !== undefined
      ? { target_ref: optionalNullableString(record, "target_ref", where) }
      : {}),
    x: requireNumber(record, "x", where),
    y: requireNumber(record, "y", where),
    ...(record.hit_ref !== undefined
      ? { hit_ref: optionalNullableString(record, "hit_ref", where) }
      : {}),
    ...(record.hit_tag !== undefined
      ? { hit_tag: optionalNullableString(record, "hit_tag", where) }
      : {}),
    ...(record.hit_role !== undefined
      ? { hit_role: optionalNullableString(record, "hit_role", where) }
      : {}),
    ...(record.hit_text !== undefined
      ? { hit_text: optionalNullableString(record, "hit_text", where) }
      : {}),
    covered: requireBoolean(record, "covered", where),
    ...(record.blocker_kind !== undefined
      ? {
          blocker_kind: optionalNullableString(
            record,
            "blocker_kind",
            where,
          ),
        }
      : {}),
  };
}

function parseRecoveryAttempt(
  value: unknown,
  where: string,
): RecoveryAttempt {
  const record = payloadRecord(value, where);
  return {
    blocker: parseBlocker(record.blocker, `${where}.blocker`),
    action: requireString(record, "action", where),
    succeeded: requireBoolean(record, "succeeded", where),
    ...(record.reason !== undefined
      ? { reason: requireString(record, "reason", where) }
      : {}),
    ...(record.before_screenshot_path !== undefined
      ? {
          before_screenshot_path: optionalNullableString(
            record,
            "before_screenshot_path",
            where,
          ),
        }
      : {}),
    ...(record.after_screenshot_path !== undefined
      ? {
          after_screenshot_path: optionalNullableString(
            record,
            "after_screenshot_path",
            where,
          ),
        }
      : {}),
    ...(record.url_before !== undefined
      ? {
          url_before: optionalNullableString(record, "url_before", where),
        }
      : {}),
    ...(record.url_after !== undefined
      ? { url_after: optionalNullableString(record, "url_after", where) }
      : {}),
    retried_original_action: requireBoolean(
      record,
      "retried_original_action",
      where,
    ),
  };
}

function parseStepResult(value: unknown, index: number): StepResult {
  const where = `execution result.steps[${index}]`;
  const record = payloadRecord(value, where);
  const action = requireString(record, "action", where);
  const status = requireString(record, "status", where);
  if (!ACTION_SET.has(action) || (status !== "passed" && status !== "failed")) {
    throw new WorkerPayloadError(`${where} has an invalid action or status`);
  }
  const recovery = optionalArray(record, "recovery", where);
  return {
    index: requireNumber(record, "index", where),
    action: action as Action,
    status,
    started_at: requireString(record, "started_at", where),
    duration_ms: requireNumber(record, "duration_ms", where),
    url_before: requireString(record, "url_before", where),
    url_after: requireString(record, "url_after", where),
    conditions: requireArray(record, "conditions", where).map(
      (condition, conditionIndex) =>
        parseConditionResult(
          condition,
          `${where}.conditions[${conditionIndex}]`,
        ),
    ),
    evidence: parseEvidence(record.evidence, `${where}.evidence`),
    error: parseStepError(record.error, `${where}.error`),
    ...(record.blocker !== undefined
      ? {
          blocker:
            record.blocker === null
              ? null
              : parseBlocker(record.blocker, `${where}.blocker`),
        }
      : {}),
    ...(record.hit_test !== undefined
      ? {
          hit_test:
            record.hit_test === null
              ? null
              : parseHitTest(record.hit_test, `${where}.hit_test`),
        }
      : {}),
    ...(recovery !== undefined
      ? {
          recovery: recovery.map((attempt, recoveryIndex) =>
            parseRecoveryAttempt(
              attempt,
              `${where}.recovery[${recoveryIndex}]`,
            ),
          ),
        }
      : {}),
  };
}

export function parseExecutionResult(value: unknown): ExecutionResult {
  const record = payloadRecord(value, "execution result");
  const status = requireString(record, "status", "execution result");
  if (status !== "passed" && status !== "failed" && status !== "error") {
    throw new WorkerPayloadError("execution result.status is invalid");
  }
  return {
    execution_id: requireString(record, "execution_id", "execution result"),
    status,
    started_at: requireString(record, "started_at", "execution result"),
    finished_at: requireString(record, "finished_at", "execution result"),
    final_url: requireString(record, "final_url", "execution result"),
    steps: requireArray(record, "steps", "execution result").map(parseStepResult),
    error: parseStepError(record.error, "execution result.error"),
  };
}
