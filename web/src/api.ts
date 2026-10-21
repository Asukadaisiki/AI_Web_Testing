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
  status: RunStatus;
  checkpoint_version: number;
  cancel_requested_at?: string;
  started_at?: string;
  finished_at?: string;
  created_at: string;
  updated_at: string;
}

export type SpanKind = 'run' | 'model' | 'reasoning' | 'tool' | 'operation' | 'report';

export interface Span {
  id: string;
  run_id: string;
  trace_id: string;
  span_id: string;
  parent_span_id?: string;
  kind: SpanKind;
  name: string;
  status: string;
  input?: unknown;
  output?: unknown;
  started_at: string;
  ended_at?: string | null;
  created_at: string;
  updated_at: string;
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
  safety: unknown;
}

export interface ConversationDetail {
  conversation: Conversation;
  messages: Message[];
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

async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
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
    const failure = body as ErrorResponse | null;
    throw new ApiError(
      response.status,
      failure?.error?.code ?? 'request_failed',
      failure?.error?.message ?? `Request failed with HTTP ${String(response.status)}.`,
    );
  }
  return body as T;
}

function postJSON<T>(path: string, body: unknown): Promise<T> {
  return requestJSON<T>(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

export async function listProjects(): Promise<Project[]> {
  const response = await requestJSON<{ projects: Project[] }>('/projects');
  return response.projects;
}

export function createProject(name: string): Promise<Project> {
  return postJSON<Project>('/projects', { name, metadata: {} });
}

export async function listConversations(projectId: string): Promise<Conversation[]> {
  const response = await requestJSON<{ conversations: Conversation[] }>(
    `/projects/${encodeURIComponent(projectId)}/conversations`,
  );
  return response.conversations;
}

export function createConversation(projectId: string, title: string): Promise<Conversation> {
  return postJSON<Conversation>(
    `/projects/${encodeURIComponent(projectId)}/conversations`,
    { title, metadata: {} },
  );
}

export function getConversation(conversationId: string): Promise<ConversationDetail> {
  return requestJSON<ConversationDetail>(
    `/conversations/${encodeURIComponent(conversationId)}`,
  );
}

export function createMessage(
  conversationId: string,
  request: CreateMessageRequest,
): Promise<CreateMessageResponse> {
  return postJSON<CreateMessageResponse>(
    `/conversations/${encodeURIComponent(conversationId)}/messages`,
    request,
  );
}

export function getRun(runId: string): Promise<Run> {
  return requestJSON<Run>(`/runs/${encodeURIComponent(runId)}`);
}

export async function getTrace(runId: string): Promise<Span[]> {
  const response = await requestJSON<{ spans: Span[] }>(
    `/runs/${encodeURIComponent(runId)}/trace`,
  );
  return response.spans;
}

export function cancelRun(runId: string): Promise<RunActionResponse> {
  return postJSON<RunActionResponse>(`/runs/${encodeURIComponent(runId)}/cancel`, {});
}

export function submitRunResponse(
  runId: string,
  request: SubmitResponseRequest,
): Promise<RunActionResponse> {
  return postJSON<RunActionResponse>(
    `/runs/${encodeURIComponent(runId)}/responses`,
    request,
  );
}

export async function listAgents(): Promise<Agent[]> {
  const response = await requestJSON<{ agents: Agent[] }>('/agents');
  return response.agents;
}

export function getHealth(): Promise<Health> {
  return requestJSON<Health>('/health');
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
