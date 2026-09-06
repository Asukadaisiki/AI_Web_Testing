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
  | "research.llm_call"
  | "run.finished"
  | "run.failed"
  | "run.cancelled";

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

type ToolCallUnavailableReason =
  | "model_returned_final_text"
  | "model_attempt_failed_without_response";

type ResearchLLMCallToolAssociation =
  | {
      tool_call_status: "available";
      tool_call_ids: string[];
      tool_call_unavailable_reason?: never;
    }
  | {
      tool_call_status: "unavailable";
      tool_call_ids?: never;
      tool_call_unavailable_reason: ToolCallUnavailableReason;
    };

export type ResearchLLMCallPayloadV1 = Record<string, unknown> & {
  schema_version: "research.llm_call.v1";
  logical_call_id: string;
  attempt: number;
  attempt_status: string;
} & ResearchLLMCallToolAssociation;

export interface AgentEvent<
  Payload extends Record<string, unknown> = Record<string, unknown>,
> {
  seq: number;
  type: AgentEventType;
  conversation_id: string;
  run_id: string;
  step_id?: string;
  tool_call_id?: string;
  parent_id?: string;
  checkpoint_id?: string;
  timestamp: string;
  payload: Payload;
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
