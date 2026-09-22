/**
 * api.ts —— 全部 API 调用与类型定义的唯一位置。
 *
 * 类型镜像 `v2/CONTRACT.md`：
 *  - §2 Case 工件（case_version / name / goal / base_url / steps[]，Step / Condition）
 *  - §3 观测（本前端只在 SSE 摘要里读取，不重建观测结构）
 *  - §4 执行结果（execution_id / status / steps[] / conditions / evidence）
 *  - §4.1 失败信号 kind
 *  - §7 失败回灌候选
 *  - §8 runs.status 取值
 *
 * 字段名一律照抄契约与 Go API 列表，不自创字段。
 * 本文件不含任何业务推断：所有状态都来自后端响应或后端事件。
 */

/** Go 控制面 API 前缀（由 Vite dev server 代理到 http://127.0.0.1:8101）。 */
export const API_BASE = '/api';

/** CONTRACT §8：runs.status 的全部取值。 */
export type RunStatus =
  | 'planning'
  | 'awaiting_approval'
  | 'executing'
  | 'reporting'
  | 'completed'
  | 'failed'
  | 'awaiting_input';

export const RUN_STATUSES: readonly RunStatus[] = [
  'planning',
  'awaiting_approval',
  'executing',
  'reporting',
  'completed',
  'failed',
  'awaiting_input',
];

/** CONTRACT §2.1：Step.action 的全部取值。 */
export type StepAction = 'goto' | 'click' | 'input' | 'assert_text' | 'assert_url';

/** CONTRACT §2.2：Condition.type 的全部取值（表中未列出即为非法）。 */
export type ConditionType =
  | 'url_contains'
  | 'text_visible'
  | 'text_gone'
  | 'url_changes'
  | 'value_equals';

/** CONTRACT §3：定位器偏好顺序 role → text → css。 */
export type LocatorKind = 'role' | 'text' | 'css';

/** CONTRACT §4：执行结果 status。 */
export type ExecutionStatus = 'passed' | 'failed' | 'error';

/** CONTRACT §4：单步 status。 */
export type StepStatus = 'passed' | 'failed';

/** CONTRACT §2.2：条件阶段。 */
export type ConditionPhase = 'pre' | 'post';

/** CONTRACT §4.1：失败信号 kind。 */
export type SignalKind =
  | 'target_not_found'
  | 'condition_unmet'
  | 'step_timeout'
  | 'worker_error'
  | 'case_invalid';

/** Go API 列出的 SSE 事件类型；后端可能扩展，故用 `RunEventType | (string & {})`。 */
export type RunEventType =
  | 'run_status'
  | 'tool_call'
  | 'observation'
  | 'assistant'
  | 'model_usage'
  | 'case_ready'
  | 'execution_step'
  | 'execution_done'
  | 'report_ready'
  | 'error'
  | 'question';

/**
 * 模型累计用量。镜像 Go 的 `internal/usage.Usage`。
 *
 * `model_calls` 是调用次数；`reasoning_tokens` 已包含在 `completion_tokens` 里、
 * `cached_tokens` 已包含在 `prompt_tokens` 里，两者只是成本构成，不可再加一遍。
 */
export interface RunUsage {
  model_calls: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  reasoning_tokens: number;
  cached_tokens: number;
}

/** GET /api/runs、GET /api/runs/{id} 的 run 对象。 */
export interface Run {
  id: string;
  /** 所属会话（CONTRACT §9.1）。旧数据可能没有。 */
  session_id: string | null;
  input: string;
  status: RunStatus;
  parent_run_id: string | null;
  created_at: string;
  updated_at: string;
  error: string | null;
  usage: RunUsage;
}

/**
 * 会话：一个目标 + 它的全部轮次（CONTRACT §9）。
 *
 * `status` 是**最新一轮**的状态，只用于列表展示；会话自身没有状态机。
 * `usage` 是这个会话全部轮次的累计用量。
 */
export interface Session {
  id: string;
  goal: string;
  created_at: string;
  updated_at: string;
  run_count: number;
  status: RunStatus | '';
  usage: RunUsage;
}

/** GET /api/sessions/{id} 的响应：会话 + 它的全部轮次（第 1 轮在前）。 */
export interface SessionDetail {
  session: Session;
  runs: Run[];
}

/** CONTRACT §2.1：Step.target（仅 click / input 需要）。 */
export interface CaseLocator {
  kind: LocatorKind;
  role: string | null;
  name: string | null;
  exact: boolean | null;
  text: string | null;
  css: string | null;
  match_count: number | null;
}

/** CONTRACT §2.1：Step.target.grounding（接地证据）。 */
export interface CaseGrounding {
  observation_id: string;
  page_state_id: string;
  candidate_id: string;
  page_url: string;
}

/** CONTRACT §2.1：Step.target。 */
export interface CaseTarget {
  hint: string;
  locator: CaseLocator;
  grounding: CaseGrounding;
}

/** CONTRACT §2.2：Condition。 */
export interface CaseCondition {
  type: ConditionType;
  value: string;
  timeout_ms: number | null;
}

/** CONTRACT §2.1：Step。 */
export interface CaseStep {
  index: number;
  action: StepAction;
  intent: string;
  value: string | null;
  target: CaseTarget | null;
  preconditions: CaseCondition[];
  postconditions: CaseCondition[];
  timeout_ms: number | null;
}

/** CONTRACT §2：Case 工件。 */
export interface CaseArtifact {
  case_version: string;
  name: string;
  goal: string;
  base_url: string;
  steps: CaseStep[];
}

/** GET /api/runs/{id}/case 的响应。 */
export interface CaseResponse {
  case_id: string;
  case: CaseArtifact;
}

/** CONTRACT §4：条件的判定结果。 */
export interface ConditionResult {
  phase: ConditionPhase;
  type: ConditionType;
  value: string;
  satisfied: boolean;
  detail: string | null;
}

/** CONTRACT §4：evidence.console 条目。 */
export interface ConsoleEntry {
  level: string;
  text: string;
}

/** CONTRACT §4：evidence.network 条目。 */
export interface NetworkEntry {
  method: string;
  url: string;
  status: number | null;
}

/** CONTRACT §4：单步证据。 */
export interface StepEvidence {
  screenshot_path: string | null;
  console: ConsoleEntry[];
  network: NetworkEntry[];
}

/** CONTRACT §4：执行结果的单步。 */
export interface ExecutionStep {
  index: number;
  action: string;
  status: StepStatus;
  started_at: string | null;
  duration_ms: number | null;
  url_before: string | null;
  url_after: string | null;
  conditions: ConditionResult[];
  evidence: StepEvidence;
  error: string | null;
}

/**
 * CONTRACT §4：`error` 是对象 `{kind, message}`。
 * 前端只做展示，故压平成 `"kind: message"`；两种形态都容忍，便于兼容旧数据。
 */
function errorText(value: unknown): string | null {
  if (typeof value === 'string') return value === '' ? null : value;
  if (!isRecord(value)) return null;
  const kind = optionalString(value, 'kind') ?? '';
  const message = optionalString(value, 'message') ?? '';
  if (kind === '' && message === '') return null;
  if (kind === '') return message;
  if (message === '') return kind;
  return `${kind}: ${message}`;
}

/** CONTRACT §4：执行结果。 */
export interface ExecutionResult {
  execution_id: string;
  status: ExecutionStatus;
  started_at: string | null;
  finished_at: string | null;
  final_url: string | null;
  steps: ExecutionStep[];
}

/** GET /api/runs/{id}/execution 的响应（result 未生成时为 null）。 */
export interface ExecutionResponse {
  execution_id: string;
  status: string;
  result: ExecutionResult | null;
}

/** CONTRACT §4.1：失败信号。 */
export interface ReportSignal {
  step_index: number | null;
  kind: SignalKind;
  message: string;
}

/** GET /api/runs/{id}/report 的响应。 */
export interface RunReport {
  run_id: string;
  status: RunStatus;
  steps_total: number;
  steps_passed: number;
  steps_failed: number;
  duration_ms: number;
  signals: ReportSignal[];
  execution_id: string | null;
}

/** CONTRACT §7 / §8：失败回灌候选 id（SQLite 主键，可能是整数或字符串）。 */
export type FeedbackCandidateId = string | number;

/** CONTRACT §7：失败回灌候选。 */
export interface FeedbackCandidate {
  id: FeedbackCandidateId;
  signal_kind: SignalKind;
  proposed_input: string;
  status: string;
}

/** GET /api/runs/{id}/feedback 的响应。 */
export interface FeedbackResponse {
  candidates: FeedbackCandidate[];
}

/** GET /api/runs?limit=20 的响应。 */
export interface RunsResponse {
  runs: Run[];
}

/** GET /api/sessions?limit=20 的响应。 */
export interface SessionsResponse {
  sessions: Session[];
}

/** POST /api/sessions 的响应：新会话与它的第 1 轮。 */
export interface CreateSessionResponse {
  session_id: string;
  run_id: string;
}

/** POST /api/runs 的响应（在既有会话里再开一轮）。 */
export interface CreateRunResponse {
  session_id: string;
  run_id: string;
}

/** POST /api/runs/{id}/approve 的响应。 */
export interface ApproveResponse {
  status: string;
}

/** POST /api/runs/{id}/answer 的响应。 */
export interface AnswerResponse {
  status: string;
}

/** POST /api/runs/{id}/feedback/confirm 的响应（同一会话里的新 run）。 */
export interface ConfirmFeedbackResponse {
  session_id: string;
  run_id: string;
}

/** GET /api/runs/{id}/events 的 SSE 事件。payload 形状契约未固定，按 unknown 处理。 */
export interface RunEvent {
  seq: number;
  type: RunEventType | (string & {});
  payload: unknown;
}

/** API 调用失败。status 为 0 表示网络层失败。 */
export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

/* ------------------------------------------------------------------ */
/* 类型守卫（不使用 any；外部输入一律 unknown）                          */
/* ------------------------------------------------------------------ */

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function asString(value: unknown): string | null {
  return typeof value === 'string' ? value : null;
}

export function asNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}

export function asBoolean(value: unknown): boolean | null {
  return typeof value === 'boolean' ? value : null;
}

export function asArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

/** 从 payload 里读一个字符串字段（payload 形状未固定，故全部走守卫）。 */
export function payloadString(payload: unknown, key: string): string | null {
  if (!isRecord(payload)) return null;
  return asString(payload[key]);
}

/** 从 payload 里读一个数字字段。 */
export function payloadNumber(payload: unknown, key: string): number | null {
  if (!isRecord(payload)) return null;
  return asNumber(payload[key]);
}

/** 从 payload 里读一个对象字段。 */
export function payloadRecord(payload: unknown, key: string): Record<string, unknown> | null {
  if (!isRecord(payload)) return null;
  const value = payload[key];
  return isRecord(value) ? value : null;
}

/** 取最后一个指定类型的事件（用于「当前提问」「最近状态」等展示）。 */
export function lastEventOfType(events: readonly RunEvent[], type: string): RunEvent | null {
  let found: RunEvent | null = null;
  for (const event of events) {
    if (event.type === type) found = event;
  }
  return found;
}

export function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === 'string') return err;
  return String(err);
}

/* ------------------------------------------------------------------ */
/* 解析器：unknown → 契约类型                                           */
/* ------------------------------------------------------------------ */

function requireString(record: Record<string, unknown>, key: string, where: string): string {
  const value = asString(record[key]);
  if (value === null) throw new ApiError(0, `${where} 缺少字段 ${key}`);
  return value;
}

function optionalNumber(record: Record<string, unknown>, key: string): number | null {
  return asNumber(record[key]);
}

function optionalString(record: Record<string, unknown>, key: string): string | null {
  return asString(record[key]);
}

function parseRunStatus(value: unknown): RunStatus {
  const raw = asString(value);
  for (const status of RUN_STATUSES) {
    if (status === raw) return status;
  }
  throw new ApiError(0, `未知的 run.status：${String(value)}`);
}

const STEP_ACTIONS: readonly StepAction[] = ['goto', 'click', 'input', 'assert_text', 'assert_url'];
const CONDITION_TYPES: readonly ConditionType[] = [
  'url_contains',
  'text_visible',
  'text_gone',
  'url_changes',
  'value_equals',
];
const LOCATOR_KINDS: readonly LocatorKind[] = ['role', 'text', 'css'];
const SIGNAL_KINDS: readonly SignalKind[] = [
  'target_not_found',
  'condition_unmet',
  'step_timeout',
  'worker_error',
  'case_invalid',
];
const EXECUTION_STATUSES: readonly ExecutionStatus[] = ['passed', 'failed', 'error'];

function oneOf<T extends string>(allowed: readonly T[], value: unknown, fallback: T): T {
  const raw = asString(value);
  for (const item of allowed) {
    if (item === raw) return item;
  }
  return fallback;
}

export function parseRun(value: unknown): Run {
  if (!isRecord(value)) throw new ApiError(0, 'run 响应不是对象');
  return {
    id: requireString(value, 'id', 'run'),
    session_id: optionalString(value, 'session_id'),
    input: optionalString(value, 'input') ?? '',
    status: parseRunStatus(value['status']),
    parent_run_id: optionalString(value, 'parent_run_id'),
    created_at: optionalString(value, 'created_at') ?? '',
    updated_at: optionalString(value, 'updated_at') ?? '',
    error: optionalString(value, 'error'),
    usage: parseRunUsage(value['usage']),
  };
}

/** 会话状态可能是空串（会话刚建出来还没轮次时后端给 ''）。 */
function parseOptionalRunStatus(value: unknown): RunStatus | '' {
  const raw = asString(value);
  if (raw === null || raw === '') return '';
  return parseRunStatus(raw);
}

export function parseSession(value: unknown): Session {
  if (!isRecord(value)) throw new ApiError(0, 'session 响应不是对象');
  return {
    id: requireString(value, 'id', 'session'),
    goal: optionalString(value, 'goal') ?? '',
    created_at: optionalString(value, 'created_at') ?? '',
    updated_at: optionalString(value, 'updated_at') ?? '',
    run_count: optionalNumber(value, 'run_count') ?? 0,
    status: parseOptionalRunStatus(value['status']),
    usage: parseRunUsage(value['usage']),
  };
}

/** 用量：字段缺失时按 0 处理（老 run 没有这一行记录）。 */
export function parseRunUsage(value: unknown): RunUsage {
  const record = isRecord(value) ? value : {};
  return {
    model_calls: optionalNumber(record, 'model_calls') ?? 0,
    prompt_tokens: optionalNumber(record, 'prompt_tokens') ?? 0,
    completion_tokens: optionalNumber(record, 'completion_tokens') ?? 0,
    total_tokens: optionalNumber(record, 'total_tokens') ?? 0,
    reasoning_tokens: optionalNumber(record, 'reasoning_tokens') ?? 0,
    cached_tokens: optionalNumber(record, 'cached_tokens') ?? 0,
  };
}

/** 千分位，用于展示 token 数。 */
export function formatTokens(value: number): string {
  return value.toLocaleString('en-US');
}

function parseCaseLocator(value: unknown): CaseLocator {
  const record = isRecord(value) ? value : {};
  return {
    kind: oneOf(LOCATOR_KINDS, record['kind'], 'css'),
    role: optionalString(record, 'role'),
    name: optionalString(record, 'name'),
    exact: asBoolean(record['exact']),
    text: optionalString(record, 'text'),
    css: optionalString(record, 'css'),
    match_count: optionalNumber(record, 'match_count'),
  };
}

function parseCaseGrounding(value: unknown): CaseGrounding {
  const record = isRecord(value) ? value : {};
  return {
    observation_id: optionalString(record, 'observation_id') ?? '',
    page_state_id: optionalString(record, 'page_state_id') ?? '',
    candidate_id: optionalString(record, 'candidate_id') ?? '',
    page_url: optionalString(record, 'page_url') ?? '',
  };
}

function parseCaseTarget(value: unknown): CaseTarget | null {
  if (!isRecord(value)) return null;
  return {
    hint: optionalString(value, 'hint') ?? '',
    locator: parseCaseLocator(value['locator']),
    grounding: parseCaseGrounding(value['grounding']),
  };
}

function parseCaseCondition(value: unknown): CaseCondition {
  const record = isRecord(value) ? value : {};
  return {
    type: oneOf(CONDITION_TYPES, record['type'], 'url_contains'),
    value: optionalString(record, 'value') ?? '',
    timeout_ms: optionalNumber(record, 'timeout_ms'),
  };
}

function parseCaseStep(value: unknown): CaseStep {
  const record = isRecord(value) ? value : {};
  return {
    index: optionalNumber(record, 'index') ?? 0,
    action: oneOf(STEP_ACTIONS, record['action'], 'goto'),
    intent: optionalString(record, 'intent') ?? '',
    value: optionalString(record, 'value'),
    target: parseCaseTarget(record['target']),
    preconditions: asArray(record['preconditions']).map(parseCaseCondition),
    postconditions: asArray(record['postconditions']).map(parseCaseCondition),
    timeout_ms: optionalNumber(record, 'timeout_ms'),
  };
}

export function parseCaseArtifact(value: unknown): CaseArtifact {
  if (!isRecord(value)) throw new ApiError(0, 'case 不是对象');
  return {
    case_version: optionalString(value, 'case_version') ?? '',
    name: optionalString(value, 'name') ?? '',
    goal: optionalString(value, 'goal') ?? '',
    base_url: optionalString(value, 'base_url') ?? '',
    steps: asArray(value['steps']).map(parseCaseStep),
  };
}

function parseConditionResult(value: unknown): ConditionResult {
  const record = isRecord(value) ? value : {};
  return {
    phase: oneOf(['pre', 'post'] as const, record['phase'], 'post'),
    type: oneOf(CONDITION_TYPES, record['type'], 'url_contains'),
    value: optionalString(record, 'value') ?? '',
    satisfied: asBoolean(record['satisfied']) ?? false,
    detail: optionalString(record, 'detail'),
  };
}

function parseConsoleEntry(value: unknown): ConsoleEntry {
  const record = isRecord(value) ? value : {};
  return {
    level: optionalString(record, 'level') ?? '',
    text: optionalString(record, 'text') ?? '',
  };
}

function parseNetworkEntry(value: unknown): NetworkEntry {
  const record = isRecord(value) ? value : {};
  return {
    method: optionalString(record, 'method') ?? '',
    url: optionalString(record, 'url') ?? '',
    status: optionalNumber(record, 'status'),
  };
}

function parseEvidence(value: unknown): StepEvidence {
  const record = isRecord(value) ? value : {};
  return {
    screenshot_path: optionalString(record, 'screenshot_path'),
    console: asArray(record['console']).map(parseConsoleEntry),
    network: asArray(record['network']).map(parseNetworkEntry),
  };
}

function parseExecutionStep(value: unknown): ExecutionStep {
  const record = isRecord(value) ? value : {};
  return {
    index: optionalNumber(record, 'index') ?? 0,
    action: optionalString(record, 'action') ?? '',
    status: oneOf(['passed', 'failed'] as const, record['status'], 'failed'),
    started_at: optionalString(record, 'started_at'),
    duration_ms: optionalNumber(record, 'duration_ms'),
    url_before: optionalString(record, 'url_before'),
    url_after: optionalString(record, 'url_after'),
    conditions: asArray(record['conditions']).map(parseConditionResult),
    evidence: parseEvidence(record['evidence']),
    error: errorText(record['error']),
  };
}

export function parseExecutionResult(value: unknown): ExecutionResult {
  if (!isRecord(value)) throw new ApiError(0, 'execution result 不是对象');
  return {
    execution_id: optionalString(value, 'execution_id') ?? '',
    status: oneOf(EXECUTION_STATUSES, value['status'], 'error'),
    started_at: optionalString(value, 'started_at'),
    finished_at: optionalString(value, 'finished_at'),
    final_url: optionalString(value, 'final_url'),
    steps: asArray(value['steps']).map(parseExecutionStep),
  };
}

function parseReportSignal(value: unknown): ReportSignal {
  const record = isRecord(value) ? value : {};
  return {
    step_index: optionalNumber(record, 'step_index'),
    kind: oneOf(SIGNAL_KINDS, record['kind'], 'worker_error'),
    message: optionalString(record, 'message') ?? '',
  };
}

export function parseRunReport(value: unknown): RunReport {
  if (!isRecord(value)) throw new ApiError(0, 'report 不是对象');
  return {
    run_id: requireString(value, 'run_id', 'report'),
    status: parseRunStatus(value['status']),
    steps_total: optionalNumber(value, 'steps_total') ?? 0,
    steps_passed: optionalNumber(value, 'steps_passed') ?? 0,
    steps_failed: optionalNumber(value, 'steps_failed') ?? 0,
    duration_ms: optionalNumber(value, 'duration_ms') ?? 0,
    signals: asArray(value['signals']).map(parseReportSignal),
    execution_id: optionalString(value, 'execution_id'),
  };
}

function parseFeedbackCandidate(value: unknown): FeedbackCandidate {
  const record = isRecord(value) ? value : {};
  const rawId = record['id'];
  const id: FeedbackCandidateId =
    typeof rawId === 'string' || typeof rawId === 'number' ? rawId : '';
  return {
    id,
    signal_kind: oneOf(SIGNAL_KINDS, record['signal_kind'], 'worker_error'),
    proposed_input: optionalString(record, 'proposed_input') ?? '',
    status: optionalString(record, 'status') ?? 'pending',
  };
}

export function parseFeedbackResponse(value: unknown): FeedbackResponse {
  if (!isRecord(value)) throw new ApiError(0, 'feedback 响应不是对象');
  return { candidates: asArray(value['candidates']).map(parseFeedbackCandidate) };
}

/* ------------------------------------------------------------------ */
/* HTTP 底层                                                            */
/* ------------------------------------------------------------------ */

async function readTextSafe(res: Response): Promise<string> {
  try {
    return await res.text();
  } catch {
    return '';
  }
}

async function requestJson(path: string, init?: RequestInit): Promise<unknown> {
  let res: Response;
  try {
    res = await fetch(path, init);
  } catch (err: unknown) {
    throw new ApiError(0, `网络请求失败：${errorMessage(err)}`);
  }
  const text = await readTextSafe(res);
  if (!res.ok) {
    throw new ApiError(res.status, text === '' ? `HTTP ${res.status}` : `HTTP ${res.status}: ${text.slice(0, 300)}`);
  }
  if (text === '') return null;
  try {
    return JSON.parse(text) as unknown;
  } catch {
    throw new ApiError(res.status, '响应不是合法 JSON');
  }
}

function postJson(path: string, body: unknown): Promise<unknown> {
  return requestJson(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

/* ------------------------------------------------------------------ */
/* API 调用（与 Go 端点一一对应）                                        */
/* ------------------------------------------------------------------ */

/**
 * POST /api/sessions —— 人输入一个目标的唯一入口（CONTRACT §9.1）。
 *
 * 建会话的同时产出第 1 轮，所以返回两个 id。
 */
export async function createSession(goal: string): Promise<CreateSessionResponse> {
  const data = await postJson(`${API_BASE}/sessions`, { goal });
  if (!isRecord(data)) throw new ApiError(0, 'createSession 响应不是对象');
  return {
    session_id: requireString(data, 'session_id', 'createSession'),
    run_id: requireString(data, 'run_id', 'createSession'),
  };
}

/** GET /api/sessions?limit=20 */
export async function listSessions(limit = 20): Promise<Session[]> {
  const data = await requestJson(`${API_BASE}/sessions?limit=${String(limit)}`);
  if (!isRecord(data)) throw new ApiError(0, 'sessions 响应不是对象');
  return asArray(data['sessions']).map(parseSession);
}

/** GET /api/sessions/{id} —— 会话 + 它的全部轮次。 */
export async function getSession(id: string): Promise<SessionDetail> {
  const data = await requestJson(`${API_BASE}/sessions/${encodeURIComponent(id)}`);
  if (!isRecord(data)) throw new ApiError(0, 'session 响应不是对象');
  return {
    session: parseSession(data),
    runs: asArray(data['runs']).map(parseRun),
  };
}

/** GET /api/runs?limit=20 */
export async function listRuns(limit = 20): Promise<Run[]> {
  const data = await requestJson(`${API_BASE}/runs?limit=${String(limit)}`);
  if (!isRecord(data)) throw new ApiError(0, 'runs 响应不是对象');
  return asArray(data['runs']).map(parseRun);
}

/**
 * POST /api/runs —— 在既有会话里再开一轮（CONTRACT §9.1，新轮次属于同一会话）。
 *
 * `parentRunId` 记录轮次链条；失败回灌的正规入口是错误注入页的 confirmFeedback，
 * 这里服务的是人想对同一目标手动重跑/换个说法再试的场景。
 */
export async function createRun(
  sessionId: string,
  input: string,
  parentRunId: FeedbackCandidateId | null = null,
): Promise<CreateRunResponse> {
  const data = await postJson(`${API_BASE}/runs`, {
    session_id: sessionId,
    input,
    parent_run_id: parentRunId,
  });
  if (!isRecord(data)) throw new ApiError(0, 'createRun 响应不是对象');
  return {
    session_id: requireString(data, 'session_id', 'createRun'),
    run_id: requireString(data, 'run_id', 'createRun'),
  };
}

/** GET /api/runs/{id} */
export async function getRun(id: string): Promise<Run> {
  return parseRun(await requestJson(`${API_BASE}/runs/${encodeURIComponent(id)}`));
}

/** GET /api/runs/{id}/case —— 未生成时后端返回 404，这里返回 null。 */
export async function getRunCase(id: string): Promise<CaseResponse | null> {
  try {
    const data = await requestJson(`${API_BASE}/runs/${encodeURIComponent(id)}/case`);
    if (!isRecord(data)) throw new ApiError(0, 'case 响应不是对象');
    return {
      case_id: requireString(data, 'case_id', 'case'),
      case: parseCaseArtifact(data['case']),
    };
  } catch (err: unknown) {
    if (err instanceof ApiError && err.status === 404) return null;
    throw err;
  }
}

/** POST /api/runs/{id}/approve */
export async function approveRun(id: string): Promise<ApproveResponse> {
  const data = await postJson(`${API_BASE}/runs/${encodeURIComponent(id)}/approve`, {});
  if (!isRecord(data)) throw new ApiError(0, 'approve 响应不是对象');
  return { status: optionalString(data, 'status') ?? '' };
}

/** POST /api/runs/{id}/answer */
export async function answerRun(id: string, text: string): Promise<AnswerResponse> {
  const data = await postJson(`${API_BASE}/runs/${encodeURIComponent(id)}/answer`, { text });
  if (!isRecord(data)) throw new ApiError(0, 'answer 响应不是对象');
  return { status: optionalString(data, 'status') ?? '' };
}

/** GET /api/runs/{id}/execution */
export async function getRunExecution(id: string): Promise<ExecutionResponse> {
  const data = await requestJson(`${API_BASE}/runs/${encodeURIComponent(id)}/execution`);
  if (!isRecord(data)) throw new ApiError(0, 'execution 响应不是对象');
  const raw = data['result'];
  return {
    execution_id: optionalString(data, 'execution_id') ?? '',
    status: optionalString(data, 'status') ?? '',
    result: raw === null || raw === undefined ? null : parseExecutionResult(raw),
  };
}

/** GET /api/runs/{id}/report */
export async function getRunReport(id: string): Promise<RunReport> {
  return parseRunReport(await requestJson(`${API_BASE}/runs/${encodeURIComponent(id)}/report`));
}

/** GET /api/runs/{id}/feedback */
export async function getRunFeedback(id: string): Promise<FeedbackResponse> {
  return parseFeedbackResponse(await requestJson(`${API_BASE}/runs/${encodeURIComponent(id)}/feedback`));
}

/** POST /api/runs/{id}/feedback/confirm */
export async function confirmFeedback(
  id: string,
  candidateId: FeedbackCandidateId,
  input: string,
): Promise<ConfirmFeedbackResponse> {
  const data = await postJson(`${API_BASE}/runs/${encodeURIComponent(id)}/feedback/confirm`, {
    candidate_id: candidateId,
    input,
  });
  if (!isRecord(data)) throw new ApiError(0, 'feedback/confirm 响应不是对象');
  return {
    session_id: requireString(data, 'session_id', 'feedback/confirm'),
    run_id: requireString(data, 'run_id', 'feedback/confirm'),
  };
}

/** SSE 地址；from 为已收到的最大 seq，重连时带上以实现重放。 */
export function runEventsUrl(id: string, from: number): string {
  return `${API_BASE}/runs/${encodeURIComponent(id)}/events?from=${String(from)}`;
}

/** 解析一条 SSE data 负载；形状不符时返回 null（忽略该条）。 */
export function parseRunEvent(data: string): RunEvent | null {
  let value: unknown;
  try {
    value = JSON.parse(data) as unknown;
  } catch {
    return null;
  }
  if (!isRecord(value)) return null;
  const seq = asNumber(value['seq']);
  const type = asString(value['type']);
  if (seq === null || type === null) return null;
  return { seq, type, payload: value['payload'] };
}

/**
 * 证据图片路径 → `/artifacts/<path>` URL。
 *
 * CONTRACT §4/§9.2 里 screenshot_path 是**相对产物根**的路径，形如
 * `sess_4d1a/exec_..._0.png`，控制面直接按 `/artifacts/<path>` 提供，
 * 所以这里几乎不用加工。旧数据里出现过仓库相对路径（`.../data/artifacts/x.png`），
 * 仍然兼容：取最后一个 `artifacts/` 之后的部分。
 */
export function artifactUrl(path: string): string {
  if (path === '') return '';
  if (/^https?:\/\//i.test(path)) return path;
  if (path.startsWith('/artifacts/')) return path;
  const normalized = path.split('\\').join('/').replace(/^[\\/]+/, '');
  const marker = 'artifacts/';
  const at = normalized.lastIndexOf(marker);
  const relative = at >= 0 ? normalized.slice(at + marker.length) : normalized;
  return `/artifacts/${relative}`;
}
