export type ExecutionStatus = "running" | "passed" | "failed";

export interface DSLStep {
  action: string;
  target?: string;
  value?: string;
  timeout_ms?: number;
  [key: string]: unknown;
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

export interface StoredCaseDetail extends StoredCaseSummary {}

export interface DSLCasePayload {
  name: string;
  description?: string | null;
  steps: DSLStep[];
}

export interface CaseMutationPayload extends DSLCasePayload {
  project_id: number;
  actor_user_id: number;
}

export interface DSLValidationResult {
  valid: boolean;
  case: DSLCasePayload;
  supported_actions: string[];
}

export interface CaseExecutionRequest {
  actor_user_id: number;
  base_url?: string;
}

export interface ViewportSnapshot {
  width: number;
  height: number;
}

export interface LocatorCandidateAttributes {
  aria_label?: string | null;
  placeholder?: string | null;
  data_testid?: string | null;
}

export interface LocatorCandidateEvidence {
  strategy: string;
  preview_text?: string | null;
  role?: string | null;
  attributes: LocatorCandidateAttributes;
  score: number;
  matched_rules: string[];
  rejected_reasons: string[];
  visible: boolean;
  enabled: boolean;
}

export interface LocatorTrace {
  target: string;
  match_strategy?: string | null;
  selection_reason?: string | null;
  candidates: LocatorCandidateEvidence[];
  selected_candidate?: LocatorCandidateEvidence | null;
  failure_reason?: string | null;
}

export interface DOMSummary {
  text_preview?: string | null;
  button_count: number;
  input_count: number;
  link_count: number;
}

export interface ConsoleEvent {
  level: "error" | "warning";
  text: string;
  source_url?: string | null;
  line_number?: number | null;
}

export interface NetworkEvent {
  url: string;
  method: string;
  status?: number | null;
  resource_type?: string | null;
  failure_text?: string | null;
}

export interface StepExecutionEvidence {
  step_index: number;
  action: string;
  target?: string | null;
  value?: string | null;
  status: "passed" | "failed";
  duration_ms?: number | null;
  resolved_by?: string | null;
  locator_trace?: LocatorTrace | null;
  url?: string | null;
  page_title?: string | null;
  viewport?: ViewportSnapshot | null;
  dom_summary?: DOMSummary | null;
  console_events: ConsoleEvent[];
  network_events: NetworkEvent[];
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
  duration_ms?: number | null;
  total_steps: number;
  failed_step_index?: number | null;
  latest_screenshot_url?: string | null;
}

export interface StoredCaseExecutionDetail extends StoredCaseExecutionSummary {
  report: ExecutionReport | null;
}
