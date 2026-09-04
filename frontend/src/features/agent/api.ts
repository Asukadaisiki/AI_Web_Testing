import type { AgentEvent, AgentRun } from "./types";

const AGENT_BASE = import.meta.env.VITE_AGENTCORE_BASE_URL ?? "";

async function agentRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${AGENT_BASE}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
    ...init,
  });
  if (!response.ok) {
    let detail = `${response.status} ${response.statusText}`;
    try {
      const payload = (await response.json()) as {
        message?: string;
        error?: string;
      };
      detail = payload.message ?? payload.error ?? detail;
    } catch {
      // Keep the HTTP status for non-JSON errors.
    }
    throw new Error(detail);
  }
  return (await response.json()) as T;
}

export function startAgentRun(payload: {
  conversation_id: string;
  project_id: number;
  message: string;
}) {
  return agentRequest<AgentRun>("/api/v2/agent/runs", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function getAgentRun(runId: string) {
  return agentRequest<AgentRun>(`/api/v2/agent/runs/${runId}`);
}

export async function listAgentEvents(runId: string, afterSeq = 0) {
  const response = await agentRequest<{ events: AgentEvent[] }>(
    `/api/v2/agent/runs/${runId}/events?after_seq=${afterSeq}`,
  );
  return response.events;
}

export function resumeAgentToolCall(
  runId: string,
  toolCallId: string,
  answers: Record<string, unknown>,
  nextStep?: string,
) {
  return agentRequest<AgentRun>(
    `/api/v2/agent/runs/${runId}/tool-calls/${toolCallId}/resume`,
    {
      method: "POST",
      body: JSON.stringify({
        answers,
        next_step: nextStep ?? "",
      }),
    },
  );
}

export function subscribeAgentEvents(
  runId: string,
  afterSeq: number,
  onEvent: (event: AgentEvent) => void,
  onError: () => void,
): () => void {
  const source = new EventSource(
    `${AGENT_BASE}/api/v2/agent/runs/${runId}/events/stream?after_seq=${afterSeq}`,
  );
  const eventTypes = [
    "run.started",
    "message.started",
    "message.delta",
    "message.finished",
    "tool.started",
    "tool.args.delta",
    "tool.pending",
    "tool.result",
    "tool.finished",
    "tool.failed",
    "artifact.published",
    "run.finished",
    "run.failed",
  ];
  for (const type of eventTypes) {
    source.addEventListener(type, (rawEvent) => {
      const messageEvent = rawEvent as MessageEvent<string>;
      try {
        onEvent(JSON.parse(messageEvent.data) as AgentEvent);
      } catch {
        source.close();
        onError();
      }
    });
  }
  source.onerror = () => {
    source.close();
    onError();
  };
  return () => source.close();
}
