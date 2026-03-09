export type ExecutionStatus = "running" | "passed" | "failed";

export interface DSLStep {
  action: string;
  target?: string;
  value?: string;
}

export interface StoredCaseSummary {
  id: number;
  project_id: number;
  name: string;
  description: string | null;
  steps: DSLStep[];
  created_by: number;
  updated_by: number;
  created_at: string;
  updated_at: string;
}

export interface CaseExecutionRequest {
  actor_user_id: number;
  base_url?: string;
}

export interface StepExecutionEvidence {
  step_index: number;
  action: string;
  target?: string | null;
  value?: string | null;
  status: "passed" | "failed";
  resolved_by?: string | null;
  url?: string | null;
  screenshot_path?: string | null;
  screenshot_url?: string | null;
  error_message?: string | null;
}

export interface ExecutionReport {
  status: ExecutionStatus;
  steps: StepExecutionEvidence[];
}

export interface StoredCaseExecutionSummary {
  id: number;
  case_id: number;
  case_name: string;
  project_id: number;
  triggered_by: number;
  status: ExecutionStatus;
  error_message: string | null;
  started_at: string;
  finished_at: string | null;
}

export interface StoredCaseExecutionDetail extends StoredCaseExecutionSummary {
  report: ExecutionReport | null;
}
