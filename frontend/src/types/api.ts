export type ExecutionStatus = "running" | "passed" | "failed";
export type FailureCategory = "configuration" | "locator" | "assertion" | "navigation" | "network" | "runner";
export type OverviewWindowDays = 7 | 14 | 30;

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
  base_url?: string | null;
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
  base_url?: string | null;
  steps: DSLStep[];
}

export interface CaseMutationPayload extends DSLCasePayload {
  project_id: number;
  actor_user_id: number;
}

export interface SuiteCaseRefPayload {
  case_id: number;
}

export interface StoredSuiteCase {
  case_id: number;
  case_name: string;
  order_index: number;
}

export type SuiteRunSource = "manual" | "rerun_failed";

export interface StoredSuiteRunItem {
  id: number;
  case_id: number;
  case_name_snapshot: string;
  order_index: number;
  execution_id: number;
  status: ExecutionStatus;
}

export interface StoredSuiteRunSummary {
  id: number;
  suite_id: number;
  suite_name: string;
  triggered_by: number;
  source: SuiteRunSource;
  source_suite_run_id?: number | null;
  status: ExecutionStatus;
  total_cases: number;
  passed_cases: number;
  failed_cases: number;
  base_url_override?: string | null;
  started_at: string;
  finished_at?: string | null;
}

export interface StoredSuiteRunDetail extends StoredSuiteRunSummary {
  items: StoredSuiteRunItem[];
}

export interface StoredSuiteSummary {
  id: number;
  project_id: number;
  name: string;
  description: string | null;
  case_count: number;
  created_by: number;
  updated_by: number;
  created_at: string;
  updated_at: string;
  latest_run?: StoredSuiteRunSummary | null;
}

export interface StoredSuiteDetail extends StoredSuiteSummary {
  cases: StoredSuiteCase[];
}

export interface SuiteMutationPayload {
  project_id: number;
  actor_user_id: number;
  name: string;
  description?: string | null;
  cases: SuiteCaseRefPayload[];
}

export interface SuiteExecutionRequest {
  actor_user_id: number;
  base_url?: string;
}

export interface SuiteExecutionItem {
  execution_id: number;
  case_id: number;
  case_name: string;
  status: ExecutionStatus;
}

export interface SuiteExecutionResult {
  id: number;
  suite_id: number;
  suite_name: string;
  triggered_by: number;
  source: SuiteRunSource;
  source_suite_run_id?: number | null;
  started_at: string;
  finished_at?: string | null;
  total_cases: number;
  passed_cases: number;
  failed_cases: number;
  base_url_override?: string | null;
  status: ExecutionStatus;
  items: StoredSuiteRunItem[];
  executions: SuiteExecutionItem[];
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
  failure_category?: FailureCategory | null;
  failure_step_action?: string | null;
  latest_url?: string | null;
  latest_screenshot_url?: string | null;
}

export interface StoredCaseExecutionDetail extends StoredCaseExecutionSummary {
  report: ExecutionReport | null;
  origin_suite_run?: {
    suite_id: number;
    suite_name: string;
    suite_run_id: number;
  } | null;
}

export interface FailureCategoryCount {
  category: FailureCategory;
  count: number;
}

export interface ExecutionAggregateSnapshot {
  total_count: number;
  passed_count: number;
  failed_count: number;
  running_count: number;
  pass_rate: number;
  avg_duration_ms: number;
}

export interface ExecutionWindowRange {
  start_date?: string | null;
  end_date?: string | null;
}

export interface ExecutionWindowComparison {
  total_count_delta: number;
  passed_count_delta: number;
  failed_count_delta: number;
  running_count_delta: number;
  pass_rate_delta: number;
  avg_duration_ms_delta: number;
}

export interface ExecutionTrendPoint {
  date: string;
  total_count: number;
  passed_count: number;
  failed_count: number;
  pass_rate: number;
  avg_duration_ms: number;
}

export interface FailureStepActionCount {
  action: string;
  count: number;
}

export interface TopFailedCase {
  case_id: number;
  case_name: string;
  failure_count: number;
  latest_execution_id: number;
  latest_failure_category?: FailureCategory | null;
}

export interface FailureRootCause {
  fingerprint: string;
  title: string;
  count: number;
  affected_case_count: number;
  latest_execution_id: number;
  latest_failure_category?: FailureCategory | null;
}

export interface ExecutionsOverview {
  total_count: number;
  passed_count: number;
  failed_count: number;
  running_count: number;
  pass_rate: number;
  avg_duration_ms: number;
  current_window_range?: ExecutionWindowRange | null;
  previous_window_range?: ExecutionWindowRange | null;
  previous_window_stats: ExecutionAggregateSnapshot;
  window_comparison: ExecutionWindowComparison;
  latest_failed_runs: StoredCaseExecutionSummary[];
  failure_categories: FailureCategoryCount[];
  trend_points: ExecutionTrendPoint[];
  failure_step_actions: FailureStepActionCount[];
  top_failed_cases: TopFailedCase[];
  failure_root_causes: FailureRootCause[];
}
