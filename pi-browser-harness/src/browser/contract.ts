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

function validateStep(step: Step, index: number): void {
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

function validateObjectArray(values: unknown[], where: string): void {
  values.forEach((value, index) => {
    payloadRecord(value, `${where}[${index}]`);
  });
}

export function parseObservation(value: unknown): Observation {
  const record = payloadRecord(value, "observation");
  const elements = requireArray(record, "elements", "observation");
  const structures = requireArray(record, "structures", "observation");
  const blockers = requireArray(record, "blockers", "observation");
  const candidates = requireArray(record, "action_candidates", "observation");
  validateObjectArray(elements, "observation.elements");
  validateObjectArray(structures, "observation.structures");
  validateObjectArray(blockers, "observation.blockers");
  validateObjectArray(candidates, "observation.action_candidates");
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
    elements: elements as Element[],
    structures: structures as StructureNode[],
    blockers: blockers as Blocker[],
    action_candidates: candidates as ActionCandidate[],
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
      if (status !== null && typeof status !== "number") {
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

function parseStepResult(value: unknown, index: number): StepResult {
  const where = `execution result.steps[${index}]`;
  const record = payloadRecord(value, where);
  const action = requireString(record, "action", where);
  const status = requireString(record, "status", where);
  if (!ACTION_SET.has(action) || (status !== "passed" && status !== "failed")) {
    throw new WorkerPayloadError(`${where} has an invalid action or status`);
  }
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
      ? { blocker: record.blocker as Blocker | null }
      : {}),
    ...(record.hit_test !== undefined
      ? { hit_test: record.hit_test as HitTest | null }
      : {}),
    ...(record.recovery !== undefined
      ? { recovery: record.recovery as RecoveryAttempt[] }
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
