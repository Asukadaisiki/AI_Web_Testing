export const API_BASE = '/api/v1';

export type JSONValue =
  | string
  | number
  | boolean
  | null
  | JSONValue[]
  | { [key: string]: JSONValue };

export interface Project {
  id: string;
  name: string;
  metadata: JSONValue;
  created_at: string;
  updated_at: string;
}

export interface Conversation {
  id: string;
  project_id: string;
  title?: string;
  metadata: JSONValue;
  created_at: string;
  updated_at: string;
}

export interface Message {
  id: string;
  conversation_id: string;
  run_id?: string;
  ordinal: number;
  role: string;
  content: string;
  metadata?: JSONValue;
  created_at: string;
}

export type RunStatus =
  | 'queued'
  | 'running'
  | 'waiting_input'
  | 'waiting_approval'
  | 'cancelling'
  | 'succeeded'
  | 'failed'
  | 'cancelled';

export interface Run {
  id: string;
  project_id: string;
  conversation_id: string;
  source_message_id: string;
  retry_of_run_id?: string;
  status: RunStatus;
  checkpoint_version: number;
  cancel_requested_at?: string;
  started_at?: string;
  finished_at?: string;
  created_at: string;
  updated_at: string;
}

export type SpanKind = 'run' | 'model' | 'reasoning' | 'tool' | 'operation' | 'report';

export interface DecisionContext {
  objective?: { role: string; content: string };
  prior_observation?: { role: string; content: string };
  selected_operation?: { name: string; arguments: JSONValue };
  visible_explanation?: string;
}

export interface Span {
  id: string;
  run_id: string;
  trace_id: string;
  span_id: string;
  parent_span_id?: string;
  kind: SpanKind;
  name: string;
  status: string;
  input?: JSONValue;
  output?: JSONValue;
  decision_context?: DecisionContext;
  started_at: string;
  ended_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface SpanAttempt {
  id: string;
  span_id: string;
  attempt_number: number;
  provider?: string;
  operation_id?: string;
  status: string;
  error?: JSONValue;
  started_at: string;
  ended_at?: string | null;
}

export interface UsageRecord {
  id: string;
  span_id?: string;
  span_attempt_id?: string;
  provider?: string;
  model?: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cost_micros: number;
  details: JSONValue;
  created_at: string;
}

export interface OperationEvidence {
  id: string;
  span_id?: string;
  span_attempt_id?: string;
  operation_id: string;
  status: string;
  result?: JSONValue;
  artifacts: JSONValue;
  error?: JSONValue;
  created_at: string;
  updated_at: string;
}

export type DerivedTraceKind = 'step' | 'unknown_tool';

export interface DerivedTraceNode {
  id: string;
  parent_span_id: string;
  source_evidence_id: string;
  kind: DerivedTraceKind;
  name: string;
  status: string;
  started_at: string;
  ended_at?: string | null;
  data: JSONValue;
  evidence: JSONValue;
}

export interface Trace {
  spans: Span[];
  attempts: SpanAttempt[];
  usage: UsageRecord[];
  operation_evidence: OperationEvidence[];
  derived_nodes: DerivedTraceNode[];
}

export interface RunEvent {
  id: string | null;
  sequence: number | null;
  type: string;
  data: unknown;
}

export interface HealthDependency {
  status: string;
  required: boolean;
  mode?: string;
  pending_outbox: number;
}

export interface Health {
  status: 'ok' | 'degraded' | 'down';
  dependencies: Record<string, HealthDependency>;
}

export interface ModelRef {
  provider: string;
  name: string;
}

export interface Agent {
  id: string;
  product_name: string;
  model: ModelRef & { reasoning_effort: string };
  safety: JSONValue;
}

export interface ConversationDetail {
  conversation: Conversation;
  messages: Message[];
  runs: Run[];
}

export interface CreateMessageRequest {
  content: string;
  agent_id: string;
  model: ModelRef;
  idempotency_key: string;
}

export interface CreateMessageResponse {
  message_id: string;
  run_id: string;
  status: RunStatus;
}

export interface SubmitResponseRequest {
  kind: 'question' | 'approval';
  content: string;
  idempotency_key: string;
}

export interface RunActionResponse {
  run_id: string;
  status: string;
}

export interface RerunRequest {
  idempotency_key: string;
}

export interface RerunResponse {
  run_id: string;
  status: RunStatus;
  retry_of_run_id: string;
}

interface ErrorResponse {
  error?: {
    code?: string;
    message?: string;
  };
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

type ResponseParser<T> = (value: unknown, status: number) => T;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function invalidResponse(status: number, at: string, expected: string): never {
  throw new ApiError(
    status,
    'invalid_response',
    `Invalid API response at ${at}: expected ${expected}.`,
  );
}

function recordValue(value: unknown, status: number, at: string): Record<string, unknown> {
  if (!isRecord(value)) return invalidResponse(status, at, 'an object');
  return value;
}

function arrayValue(value: unknown, status: number, at: string): unknown[] {
  if (!Array.isArray(value)) return invalidResponse(status, at, 'an array');
  return value;
}

function stringValue(value: unknown, status: number, at: string): string {
  if (typeof value !== 'string') return invalidResponse(status, at, 'a string');
  return value;
}

function numberValue(value: unknown, status: number, at: string): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return invalidResponse(status, at, 'a finite number');
  }
  return value;
}

function optionalString(
  value: unknown,
  status: number,
  at: string,
): string | undefined {
  if (value === undefined || value === null) return undefined;
  return stringValue(value, status, at);
}

function optionalJSON(
  value: unknown,
  status: number,
  at: string,
): JSONValue | undefined {
  if (value === undefined) return undefined;
  return jsonValue(value, status, at);
}

function jsonValue(value: unknown, status: number, at: string): JSONValue {
  if (
    value === null ||
    typeof value === 'string' ||
    typeof value === 'boolean' ||
    (typeof value === 'number' && Number.isFinite(value))
  ) {
    return value;
  }
  if (Array.isArray(value)) {
    return value.map((item, index) => jsonValue(item, status, `${at}[${String(index)}]`));
  }
  if (isRecord(value)) {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [
        key,
        jsonValue(item, status, `${at}.${key}`),
      ]),
    );
  }
  return invalidResponse(status, at, 'a JSON value');
}

function enumValue<T extends string>(
  value: unknown,
  allowed: readonly T[],
  status: number,
  at: string,
): T {
  const candidate = stringValue(value, status, at);
  if (!allowed.includes(candidate as T)) {
    return invalidResponse(status, at, `one of ${allowed.join(', ')}`);
  }
  return candidate as T;
}

const runStatuses: readonly RunStatus[] = [
  'queued',
  'running',
  'waiting_input',
  'waiting_approval',
  'cancelling',
  'succeeded',
  'failed',
  'cancelled',
];

const spanKinds: readonly SpanKind[] = [
  'run',
  'model',
  'reasoning',
  'tool',
  'operation',
  'report',
];

const derivedTraceKinds: readonly DerivedTraceKind[] = ['step', 'unknown_tool'];

function parseProject(value: unknown, status: number, at = 'project'): Project {
  const item = recordValue(value, status, at);
  return {
    id: stringValue(item['id'], status, `${at}.id`),
    name: stringValue(item['name'], status, `${at}.name`),
    metadata: jsonValue(item['metadata'], status, `${at}.metadata`),
    created_at: stringValue(item['created_at'], status, `${at}.created_at`),
    updated_at: stringValue(item['updated_at'], status, `${at}.updated_at`),
  };
}

function parseConversation(
  value: unknown,
  status: number,
  at = 'conversation',
): Conversation {
  const item = recordValue(value, status, at);
  return {
    id: stringValue(item['id'], status, `${at}.id`),
    project_id: stringValue(item['project_id'], status, `${at}.project_id`),
    title: optionalString(item['title'], status, `${at}.title`),
    metadata: jsonValue(item['metadata'], status, `${at}.metadata`),
    created_at: stringValue(item['created_at'], status, `${at}.created_at`),
    updated_at: stringValue(item['updated_at'], status, `${at}.updated_at`),
  };
}

function parseMessage(value: unknown, status: number, at: string): Message {
  const item = recordValue(value, status, at);
  const metadata =
    item['metadata'] === undefined
      ? undefined
      : jsonValue(item['metadata'], status, `${at}.metadata`);
  return {
    id: stringValue(item['id'], status, `${at}.id`),
    conversation_id: stringValue(
      item['conversation_id'],
      status,
      `${at}.conversation_id`,
    ),
    run_id: optionalString(item['run_id'], status, `${at}.run_id`),
    ordinal: numberValue(item['ordinal'], status, `${at}.ordinal`),
    role: stringValue(item['role'], status, `${at}.role`),
    content: stringValue(item['content'], status, `${at}.content`),
    metadata,
    created_at: stringValue(item['created_at'], status, `${at}.created_at`),
  };
}

function parseRun(value: unknown, status: number, at = 'run'): Run {
  const item = recordValue(value, status, at);
  return {
    id: stringValue(item['id'], status, `${at}.id`),
    project_id: stringValue(item['project_id'], status, `${at}.project_id`),
    conversation_id: stringValue(
      item['conversation_id'],
      status,
      `${at}.conversation_id`,
    ),
    source_message_id: stringValue(
      item['source_message_id'],
      status,
      `${at}.source_message_id`,
    ),
    retry_of_run_id: optionalString(
      item['retry_of_run_id'],
      status,
      `${at}.retry_of_run_id`,
    ),
    status: enumValue(item['status'], runStatuses, status, `${at}.status`),
    checkpoint_version: numberValue(
      item['checkpoint_version'],
      status,
      `${at}.checkpoint_version`,
    ),
    cancel_requested_at: optionalString(
      item['cancel_requested_at'],
      status,
      `${at}.cancel_requested_at`,
    ),
    started_at: optionalString(item['started_at'], status, `${at}.started_at`),
    finished_at: optionalString(item['finished_at'], status, `${at}.finished_at`),
    created_at: stringValue(item['created_at'], status, `${at}.created_at`),
    updated_at: stringValue(item['updated_at'], status, `${at}.updated_at`),
  };
}

function parseDecisionMessage(
  value: unknown,
  status: number,
  at: string,
): { role: string; content: string } {
  const item = recordValue(value, status, at);
  return {
    role: stringValue(item['role'], status, `${at}.role`),
    content: stringValue(item['content'], status, `${at}.content`),
  };
}

function parseDecisionContext(
  value: unknown,
  status: number,
  at: string,
): DecisionContext {
  const item = recordValue(value, status, at);
  const selectedOperation =
    item['selected_operation'] === undefined
      ? undefined
      : recordValue(
          item['selected_operation'],
          status,
          `${at}.selected_operation`,
        );
  return {
    objective:
      item['objective'] === undefined
        ? undefined
        : parseDecisionMessage(item['objective'], status, `${at}.objective`),
    prior_observation:
      item['prior_observation'] === undefined
        ? undefined
        : parseDecisionMessage(
            item['prior_observation'],
            status,
            `${at}.prior_observation`,
          ),
    selected_operation:
      selectedOperation === undefined
        ? undefined
        : {
            name: stringValue(
              selectedOperation['name'],
              status,
              `${at}.selected_operation.name`,
            ),
            arguments: jsonValue(
              selectedOperation['arguments'],
              status,
              `${at}.selected_operation.arguments`,
            ),
          },
    visible_explanation: optionalString(
      item['visible_explanation'],
      status,
      `${at}.visible_explanation`,
    ),
  };
}

function parseSpan(value: unknown, status: number, at: string): Span {
  const item = recordValue(value, status, at);
  return {
    id: stringValue(item['id'], status, `${at}.id`),
    run_id: stringValue(item['run_id'], status, `${at}.run_id`),
    trace_id: stringValue(item['trace_id'], status, `${at}.trace_id`),
    span_id: stringValue(item['span_id'], status, `${at}.span_id`),
    parent_span_id: optionalString(
      item['parent_span_id'],
      status,
      `${at}.parent_span_id`,
    ),
    kind: enumValue(item['kind'], spanKinds, status, `${at}.kind`),
    name: stringValue(item['name'], status, `${at}.name`),
    status: stringValue(item['status'], status, `${at}.status`),
    input: optionalJSON(item['input'], status, `${at}.input`),
    output: optionalJSON(item['output'], status, `${at}.output`),
    decision_context:
      item['decision_context'] === undefined
        ? undefined
        : parseDecisionContext(
            item['decision_context'],
            status,
            `${at}.decision_context`,
          ),
    started_at: stringValue(item['started_at'], status, `${at}.started_at`),
    ended_at:
      item['ended_at'] === null
        ? null
        : optionalString(item['ended_at'], status, `${at}.ended_at`),
    created_at: stringValue(item['created_at'], status, `${at}.created_at`),
    updated_at: stringValue(item['updated_at'], status, `${at}.updated_at`),
  };
}

function parseSpanAttempt(value: unknown, status: number, at: string): SpanAttempt {
  const item = recordValue(value, status, at);
  return {
    id: stringValue(item['id'], status, `${at}.id`),
    span_id: stringValue(item['span_id'], status, `${at}.span_id`),
    attempt_number: numberValue(
      item['attempt_number'],
      status,
      `${at}.attempt_number`,
    ),
    provider: optionalString(item['provider'], status, `${at}.provider`),
    operation_id: optionalString(
      item['operation_id'],
      status,
      `${at}.operation_id`,
    ),
    status: stringValue(item['status'], status, `${at}.status`),
    error: optionalJSON(item['error'], status, `${at}.error`),
    started_at: stringValue(item['started_at'], status, `${at}.started_at`),
    ended_at:
      item['ended_at'] === null
        ? null
        : optionalString(item['ended_at'], status, `${at}.ended_at`),
  };
}

function parseUsageRecord(value: unknown, status: number, at: string): UsageRecord {
  const item = recordValue(value, status, at);
  return {
    id: stringValue(item['id'], status, `${at}.id`),
    span_id: optionalString(item['span_id'], status, `${at}.span_id`),
    span_attempt_id: optionalString(
      item['span_attempt_id'],
      status,
      `${at}.span_attempt_id`,
    ),
    provider: optionalString(item['provider'], status, `${at}.provider`),
    model: optionalString(item['model'], status, `${at}.model`),
    input_tokens: numberValue(item['input_tokens'], status, `${at}.input_tokens`),
    output_tokens: numberValue(
      item['output_tokens'],
      status,
      `${at}.output_tokens`,
    ),
    total_tokens: numberValue(item['total_tokens'], status, `${at}.total_tokens`),
    cost_micros: numberValue(item['cost_micros'], status, `${at}.cost_micros`),
    details: jsonValue(item['details'], status, `${at}.details`),
    created_at: stringValue(item['created_at'], status, `${at}.created_at`),
  };
}

function parseOperationEvidence(
  value: unknown,
  status: number,
  at: string,
): OperationEvidence {
  const item = recordValue(value, status, at);
  return {
    id: stringValue(item['id'], status, `${at}.id`),
    span_id: optionalString(item['span_id'], status, `${at}.span_id`),
    span_attempt_id: optionalString(
      item['span_attempt_id'],
      status,
      `${at}.span_attempt_id`,
    ),
    operation_id: stringValue(item['operation_id'], status, `${at}.operation_id`),
    status: stringValue(item['status'], status, `${at}.status`),
    result: optionalJSON(item['result'], status, `${at}.result`),
    artifacts: jsonValue(item['artifacts'], status, `${at}.artifacts`),
    error: optionalJSON(item['error'], status, `${at}.error`),
    created_at: stringValue(item['created_at'], status, `${at}.created_at`),
    updated_at: stringValue(item['updated_at'], status, `${at}.updated_at`),
  };
}

function parseDerivedTraceNode(
  value: unknown,
  status: number,
  at: string,
): DerivedTraceNode {
  const item = recordValue(value, status, at);
  return {
    id: stringValue(item['id'], status, `${at}.id`),
    parent_span_id: stringValue(
      item['parent_span_id'],
      status,
      `${at}.parent_span_id`,
    ),
    source_evidence_id: stringValue(
      item['source_evidence_id'],
      status,
      `${at}.source_evidence_id`,
    ),
    kind: enumValue(item['kind'], derivedTraceKinds, status, `${at}.kind`),
    name: stringValue(item['name'], status, `${at}.name`),
    status: stringValue(item['status'], status, `${at}.status`),
    started_at: stringValue(item['started_at'], status, `${at}.started_at`),
    ended_at:
      item['ended_at'] === null
        ? null
        : optionalString(item['ended_at'], status, `${at}.ended_at`),
    data: jsonValue(item['data'], status, `${at}.data`),
    evidence: jsonValue(item['evidence'], status, `${at}.evidence`),
  };
}

function parseConversationDetail(value: unknown, status: number): ConversationDetail {
  const item = recordValue(value, status, 'conversation_detail');
  return {
    conversation: parseConversation(
      item['conversation'],
      status,
      'conversation_detail.conversation',
    ),
    messages: arrayValue(item['messages'], status, 'conversation_detail.messages').map(
      (message, index) =>
        parseMessage(message, status, `conversation_detail.messages[${String(index)}]`),
    ),
    runs: arrayValue(item['runs'], status, 'conversation_detail.runs').map(
      (run, index) =>
        parseRun(run, status, `conversation_detail.runs[${String(index)}]`),
    ),
  };
}

function parseCreateMessageResponse(
  value: unknown,
  status: number,
): CreateMessageResponse {
  const item = recordValue(value, status, 'create_message');
  return {
    message_id: stringValue(item['message_id'], status, 'create_message.message_id'),
    run_id: stringValue(item['run_id'], status, 'create_message.run_id'),
    status: enumValue(item['status'], runStatuses, status, 'create_message.status'),
  };
}

function parseRunActionResponse(value: unknown, status: number): RunActionResponse {
  const item = recordValue(value, status, 'run_action');
  return {
    run_id: stringValue(item['run_id'], status, 'run_action.run_id'),
    status: stringValue(item['status'], status, 'run_action.status'),
  };
}

function parseRerunResponse(value: unknown, status: number): RerunResponse {
  const item = recordValue(value, status, 'rerun');
  return {
    run_id: stringValue(item['run_id'], status, 'rerun.run_id'),
    status: enumValue(item['status'], runStatuses, status, 'rerun.status'),
    retry_of_run_id: stringValue(
      item['retry_of_run_id'],
      status,
      'rerun.retry_of_run_id',
    ),
  };
}

function parseAgent(value: unknown, status: number, at: string): Agent {
  const item = recordValue(value, status, at);
  const model = recordValue(item['model'], status, `${at}.model`);
  return {
    id: stringValue(item['id'], status, `${at}.id`),
    product_name: stringValue(item['product_name'], status, `${at}.product_name`),
    model: {
      provider: stringValue(model['provider'], status, `${at}.model.provider`),
      name: stringValue(model['name'], status, `${at}.model.name`),
      reasoning_effort: stringValue(
        model['reasoning_effort'],
        status,
        `${at}.model.reasoning_effort`,
      ),
    },
    safety: jsonValue(item['safety'], status, `${at}.safety`),
  };
}

function parseHealth(value: unknown, status: number): Health {
  const item = recordValue(value, status, 'health');
  const dependencies = recordValue(item['dependencies'], status, 'health.dependencies');
  const parsedDependencies: Record<string, HealthDependency> = {};
  for (const [name, rawDependency] of Object.entries(dependencies)) {
    const dependency = recordValue(
      rawDependency,
      status,
      `health.dependencies.${name}`,
    );
    const required = dependency['required'];
    if (typeof required !== 'boolean') {
      invalidResponse(status, `health.dependencies.${name}.required`, 'a boolean');
    }
    parsedDependencies[name] = {
      status: stringValue(
        dependency['status'],
        status,
        `health.dependencies.${name}.status`,
      ),
      required,
      mode: optionalString(
        dependency['mode'],
        status,
        `health.dependencies.${name}.mode`,
      ),
      pending_outbox: numberValue(
        dependency['pending_outbox'],
        status,
        `health.dependencies.${name}.pending_outbox`,
      ),
    };
  }
  return {
    status: enumValue(item['status'], ['ok', 'degraded', 'down'], status, 'health.status'),
    dependencies: parsedDependencies,
  };
}

async function requestJSON<T>(
  path: string,
  parse: ResponseParser<T>,
  init?: RequestInit,
): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`${API_BASE}${path}`, init);
  } catch (error: unknown) {
    throw new ApiError(0, 'network_error', `Network request failed: ${errorMessage(error)}`);
  }

  const text = await response.text();
  let body: unknown = null;
  if (text !== '') {
    try {
      body = JSON.parse(text) as unknown;
    } catch {
      if (response.ok) {
        throw new ApiError(response.status, 'invalid_response', 'The API returned invalid JSON.');
      }
    }
  }
  if (!response.ok) {
    const failure = isRecord(body) ? (body as ErrorResponse) : null;
    throw new ApiError(
      response.status,
      failure?.error?.code ?? 'request_failed',
      failure?.error?.message ?? `Request failed with HTTP ${String(response.status)}.`,
    );
  }
  return parse(body, response.status);
}

function postJSON<T>(
  path: string,
  body: unknown,
  parse: ResponseParser<T>,
): Promise<T> {
  return requestJSON<T>(
    path,
    parse,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  );
}

export async function listProjects(): Promise<Project[]> {
  return requestJSON('/projects', (value, status) => {
    const response = recordValue(value, status, 'response');
    return arrayValue(response['projects'], status, 'projects').map((project, index) =>
      parseProject(project, status, `projects[${String(index)}]`),
    );
  });
}

export function createProject(name: string): Promise<Project> {
  return postJSON('/projects', { name, metadata: {} }, parseProject);
}

export async function listConversations(projectId: string): Promise<Conversation[]> {
  return requestJSON(
    `/projects/${encodeURIComponent(projectId)}/conversations`,
    (value, status) => {
      const response = recordValue(value, status, 'response');
      return arrayValue(response['conversations'], status, 'conversations').map(
        (conversation, index) =>
          parseConversation(
            conversation,
            status,
            `conversations[${String(index)}]`,
          ),
      );
    },
  );
}

export function createConversation(projectId: string, title: string): Promise<Conversation> {
  return postJSON(
    `/projects/${encodeURIComponent(projectId)}/conversations`,
    { title, metadata: {} },
    parseConversation,
  );
}

export function getConversation(conversationId: string): Promise<ConversationDetail> {
  return requestJSON(
    `/conversations/${encodeURIComponent(conversationId)}`,
    parseConversationDetail,
  );
}

export function createMessage(
  conversationId: string,
  request: CreateMessageRequest,
): Promise<CreateMessageResponse> {
  return postJSON(
    `/conversations/${encodeURIComponent(conversationId)}/messages`,
    request,
    parseCreateMessageResponse,
  );
}

export function getRun(runId: string): Promise<Run> {
  return requestJSON(`/runs/${encodeURIComponent(runId)}`, parseRun);
}

export async function getTrace(runId: string): Promise<Trace> {
  return requestJSON(
    `/runs/${encodeURIComponent(runId)}/trace`,
    (value, status) => {
      const response = recordValue(value, status, 'response');
      return {
        spans: arrayValue(response['spans'], status, 'spans').map((span, index) =>
          parseSpan(span, status, `spans[${String(index)}]`),
        ),
        attempts: arrayValue(response['attempts'], status, 'attempts').map(
          (attempt, index) =>
            parseSpanAttempt(attempt, status, `attempts[${String(index)}]`),
        ),
        usage: arrayValue(response['usage'], status, 'usage').map((record, index) =>
          parseUsageRecord(record, status, `usage[${String(index)}]`),
        ),
        operation_evidence: arrayValue(
          response['operation_evidence'],
          status,
          'operation_evidence',
        ).map((evidence, index) =>
          parseOperationEvidence(
            evidence,
            status,
            `operation_evidence[${String(index)}]`,
          ),
        ),
        derived_nodes: arrayValue(
          response['derived_nodes'],
          status,
          'derived_nodes',
        ).map((node, index) =>
          parseDerivedTraceNode(
            node,
            status,
            `derived_nodes[${String(index)}]`,
          ),
        ),
      };
    },
  );
}

export function cancelRun(runId: string): Promise<RunActionResponse> {
  return postJSON(
    `/runs/${encodeURIComponent(runId)}/cancel`,
    {},
    parseRunActionResponse,
  );
}

export function rerunRun(
  runId: string,
  request: RerunRequest,
): Promise<RerunResponse> {
  return postJSON(
    `/runs/${encodeURIComponent(runId)}/reruns`,
    request,
    parseRerunResponse,
  );
}

export function submitRunResponse(
  runId: string,
  request: SubmitResponseRequest,
): Promise<RunActionResponse> {
  return postJSON(
    `/runs/${encodeURIComponent(runId)}/responses`,
    request,
    parseRunActionResponse,
  );
}

export async function listAgents(): Promise<Agent[]> {
  return requestJSON('/agents', (value, status) => {
    const response = recordValue(value, status, 'response');
    return arrayValue(response['agents'], status, 'agents').map((agent, index) =>
      parseAgent(agent, status, `agents[${String(index)}]`),
    );
  });
}

export function getHealth(): Promise<Health> {
  return requestJSON('/health', parseHealth);
}

export function runEventsURL(runId: string): string {
  return `${API_BASE}/runs/${encodeURIComponent(runId)}/events`;
}

export function parseRunEvent(type: string, message: MessageEvent<string>): RunEvent {
  const ephemeral = type === 'token.delta' || type === 'error';
  const eventId = ephemeral ? '' : message.lastEventId;
  const sequence = /^\d+$/.test(eventId)
    ? Number.parseInt(eventId, 10)
    : null;
  let data: unknown = message.data;
  try {
    data = JSON.parse(message.data) as unknown;
  } catch {
    // Plain text is a valid event payload and remains available for display.
  }
  return {
    id: eventId === '' ? null : eventId,
    sequence,
    type,
    data,
  };
}
