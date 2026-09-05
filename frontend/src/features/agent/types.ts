export type AgentRunStatus =
  | "running"
  | "waiting_user"
  | "completed"
  | "failed"
  | "cancelled";

type AgentEventType =
  | "run.started"
  | "message.started"
  | "message.delta"
  | "message.finished"
  | "tool.started"
  | "tool.args.delta"
  | "tool.pending"
  | "tool.result"
  | "tool.finished"
  | "tool.failed"
  | "artifact.published"
  | "run.finished"
  | "run.failed";

export interface AgentRun {
  id: string;
  conversation_id: string;
  project_id: number;
  status: AgentRunStatus;
  input: string;
  pending_tool_call_id?: string;
  pending_step_id?: string;
  latest_generation_id?: number;
  approved_generation_id?: number;
  created_at: string;
  updated_at: string;
}

interface AgentQuestionOption {
  value: string;
  label: string;
  description?: string;
}

export interface AgentQuestion {
  id: string;
  question: string;
  type: "single_select" | "multi_select" | "text" | "confirm";
  required: boolean;
  options?: AgentQuestionOption[];
}

export interface AgentEvent {
  seq: number;
  type: AgentEventType;
  conversation_id: string;
  run_id: string;
  step_id?: string;
  tool_call_id?: string;
  parent_id?: string;
  checkpoint_id?: string;
  timestamp: string;
  payload: Record<string, unknown>;
}

export interface AgentArtifact {
  id: string;
  type: string;
  seq: number;
}

export interface AgentToolActivity {
  id: string;
  name: string;
  status: "running" | "waiting_user" | "completed" | "failed";
  arguments?: unknown;
  result?: unknown;
  error?: string;
  questions?: AgentQuestion[];
  startedSeq: number;
}
