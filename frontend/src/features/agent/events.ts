import type {
  AgentArtifact,
  AgentEvent,
  AgentQuestion,
  AgentToolActivity,
} from "./types";

export function mergeAgentEvents(
  current: AgentEvent[],
  incoming: AgentEvent[],
): AgentEvent[] {
  const bySequence = new Map(current.map((event) => [event.seq, event]));
  for (const event of incoming) {
    bySequence.set(event.seq, event);
  }
  return [...bySequence.values()].sort((left, right) => left.seq - right.seq);
}

export function readAssistantMessages(events: AgentEvent[]): Array<{
  seq: number;
  content: string;
}> {
  return events
    .filter((event) => event.type === "message.finished")
    .map((event) => ({
      seq: event.seq,
      content: String(event.payload.content ?? ""),
    }))
    .filter((message) => message.content.length > 0);
}

export function readToolActivities(events: AgentEvent[]): AgentToolActivity[] {
  const activities = new Map<string, AgentToolActivity>();
  for (const event of events) {
    if (!event.tool_call_id || !event.type.startsWith("tool.")) {
      continue;
    }
    const current = activities.get(event.tool_call_id) ?? {
      id: event.tool_call_id,
      name: String(event.payload.tool ?? "tool"),
      status: "running",
      startedSeq: event.seq,
    };
    if (event.type === "tool.args.delta") {
      const raw = event.payload.arguments;
      try {
        current.arguments = typeof raw === "string" ? JSON.parse(raw) : raw;
      } catch {
        current.arguments = raw;
      }
    } else if (event.type === "tool.pending") {
      current.status = "waiting_user";
      current.questions = Array.isArray(event.payload.questions)
        ? (event.payload.questions as AgentQuestion[])
        : [];
    } else if (event.type === "tool.result") {
      current.result = event.payload.content ?? event.payload.answers;
    } else if (event.type === "tool.finished") {
      current.status = "completed";
    } else if (event.type === "tool.failed") {
      current.status = "failed";
      current.error = String(event.payload.error ?? "工具执行失败");
    }
    activities.set(event.tool_call_id, current);
  }
  return [...activities.values()].sort(
    (left, right) => left.startedSeq - right.startedSeq,
  );
}

export function readArtifacts(events: AgentEvent[]): AgentArtifact[] {
  return events
    .filter((event) => event.type === "artifact.published")
    .map((event) => ({
      id: String(event.payload.id ?? ""),
      type: String(event.payload.type ?? "artifact"),
      seq: event.seq,
    }))
    .filter((artifact) => artifact.id.length > 0);
}
