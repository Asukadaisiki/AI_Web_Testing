export type ExecutionStatus =
  | "running"
  | "passed"
  | "failed"
  | "needs_intervention"
  | "cancelled";
export type ExecutionBatchStatus = "pending" | ExecutionStatus;
type FailureCategory = "configuration" | "locator" | "assertion" | "navigation" | "network" | "runner";
type ExecutionAnalysisStatus = "pending" | "running" | "completed" | "skipped" | "failed";
type ExecutionAnalysisSource = "deterministic" | "ai";
export type OverviewWindowDays = 7 | 14 | 30;
export type ReportScopeType = "global" | "project" | "case";
type DSLVariableType = "string" | "number" | "boolean" | "object" | "array";
export type CorrectionType = "css" | "xpath" | "test_id";
type DSLVariableSource =
  | "latest_url"
  | "error_message"
  | "status"
  | "last_step_url"
  | "last_step_page_title"
  | "last_step_target"
  | "last_step_value"
  | "last_step_error_message";

export interface DSLStep {
  action: string;
  target?: string;
  value?: string;
  timeout_ms?: number;
  [key: string]: unknown;
}

interface DSLCaseInputContract {
  name: string;
  context_key: string;
  value_type: DSLVariableType;
  required: boolean;
  description?: string | null;
}

interface DSLCaseOutputContract {
  name: string;
  context_key: string;
  value_type: DSLVariableType;
  source?: DSLVariableSource | null;
  description?: string | null;
}

export interface StoredCaseSummary {
  id: number;
  project_id: number;
  name: string;
  description: string | null;
  base_url?: string | null;
  input_contract: DSLCaseInputContract[];
  output_contract: DSLCaseOutputContract[];
  steps: DSLStep[];
  created_by: number;
  updated_by: number;
  created_at: string;
  updated_at: string;
}

export interface StoredCaseDetail extends StoredCaseSummary {}

export interface PaginatedCases {
  items: StoredCaseSummary[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
  has_next: boolean;
  has_prev: boolean;
}

export interface ProjectSummary {
  id: number;
  name: string;
  description: string | null;
}

interface DSLCasePayload {
  name: string;
  description?: string | null;
  base_url?: string | null;
  input_contract: DSLCaseInputContract[];
  output_contract: DSLCaseOutputContract[];
  steps: DSLStep[];
}

type AIPlanningSessionStatus =
  | "collecting"
  | "plan_ready"
  | "drafts_ready"
  | "reviewing"
  | "saving"
  | "executing"
  | "completed"
  | "closed"
  | "error";
interface AIPlanningRequirements {
  app_under_test?: string | null;
  business_goal?: string | null;
  entry_url_or_page?: string | null;
  core_user_flow?: string | null;
  main_assertions: string[];
  test_data_or_account?: string | null;
  scope_limits?: string | null;
}

interface AIPlanningTestDataRequirement {
  key: string;
  label: string;
  value_type: DSLVariableType;
  required: boolean;
  source_hint?: string | null;
}

interface AIPlanningScenario {
  scenario_key: string;
  title: string;
  goal: string;
  preconditions: string[];
  priority: "high" | "medium" | "low";
  test_data_requirements: AIPlanningTestDataRequirement[];
  assertions: string[];
  draft_prompt: string;
}

interface AIPlanningPlan {
  summary: string;
  assumptions: string[];
  risks: string[];
  scenarios: AIPlanningScenario[];
}

export interface ProjectSummaryInSession {
  id: number;
  name: string;
  description: string | null;
  is_active: boolean;
}

interface AIPlanningSession {
  id: number;
  actor_user_id: number;
  active_project_id?: number | null;
  projects: ProjectSummaryInSession[];
  case_id?: number | null;
  title?: string | null;
  status: AIPlanningSessionStatus;
  requirements: AIPlanningRequirements;
  plan?: AIPlanningPlan | null;
  missing_slots: string[];
  last_error_message?: string | null;
  created_at: string;
  updated_at: string;
}

export interface AIPlanningSessionDetail {
  session: AIPlanningSession;
}

export interface AIPlanningSessionSummary {
  id: number;
  active_project_id?: number | null;
  title: string | null;
  status: AIPlanningSessionStatus;
  projects: ProjectSummaryInSession[];
  created_at: string;
  updated_at: string;
}

export interface CreatePlanningSessionPayload {
  case_id?: number | null;
}

interface FailureSignalBase {
  category: FailureCategory;
  fingerprint: string;
  title: string;
  step_index?: number | null;
  action?: string | null;
  target?: string | null;
  error_message?: string | null;
  locator_failure_reason?: string | null;
  screenshot_url?: string | null;
}

interface FailureSignalV1 extends FailureSignalBase {
  schema_version?: null;
}

interface FailureSourceReference {
  type: "execution_report" | "execution_error";
  execution_id: number;
  step_index?: number | null;
  json_pointer: string;
}

interface FailureSignalV2 extends FailureSignalBase {
  schema_version: "failure.signal.v2";
  stage:
    | "configuration"
    | "precondition"
    | "locator"
    | "action"
    | "postcondition"
    | "network"
    | "runner";
  code: string;
  retryable: boolean;
  side_effect_committed: boolean | null;
  source_reference: FailureSourceReference;
  agent_event_reference?: {
    run_id: string;
    seq: number;
  } | null;
}

export type FailureSignal = FailureSignalV1 | FailureSignalV2;

interface FailureDetail {
  case_name: string;
  step_index: number;
  action: string;
  target?: string | null;
  error_message?: string | null;
  suspected_cause: string;
  cause_probability: "high" | "medium" | "low";
}

interface CaseAnalysisResult {
  case_id: number;
  case_name: string;
  status: string;
  passed_steps: number;
  total_steps: number;
  failure_summary?: string | null;
}

export interface ExecutionAnalysis {
  source: ExecutionAnalysisSource;
  summary: string;
  conclusion: "all_passed" | "partial" | "all_failed" | "cancelled";
  case_results: CaseAnalysisResult[];
  failure_details: FailureDetail[];
  failure_signals: FailureSignal[];
  suspected_root_cause?: string | null;
  impact_scope?: string | null;
  recommended_action: "targeted_retest" | "regression" | "manual" | "done";
  recommended_scope?: string | null;
}

export interface CaseMutationPayload extends DSLCasePayload {
  project_id: number;
}

export interface CaseExecutionRequest {
  base_url?: string;
  input_values?: Record<string, string>;
}

interface ViewportSnapshot {
  width: number;
  height: number;
}

interface LocatorCandidateAttributes {
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

interface LocatorTrace {
  target: string;
  match_strategy?: string | null;
  selection_reason?: string | null;
  candidates: LocatorCandidateEvidence[];
  selected_candidate?: LocatorCandidateEvidence | null;
  failure_reason?: string | null;
}

interface DOMSummary {
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
  event_type?: "request" | "response" | "requestfailed";
  url: string;
  method: string;
  status?: number | null;
  resource_type?: string | null;
  failure_text?: string | null;
}

interface PageStateSnapshot {
  url: string;
  dom_hash: string;
  visible_texts: string[];
  input_values: Record<string, string>;
}

interface ConditionResult {
  phase: "precondition" | "postcondition";
  index: number;
  type: string;
  expected: unknown;
  actual: unknown;
  status: "passed" | "failed" | "error";
  duration_ms: number;
  error?: string | null;
}

interface ActionOutcome {
  status: "not_executed" | "succeeded" | "failed" | "unknown";
  side_effect_state: "not_applicable" | "not_committed" | "committed" | "unknown";
  error?: string | null;
}

export interface DOMElementSnapshot {
  tag: string;
  text?: string | null;
  role?: string | null;
  aria_label?: string | null;
  placeholder?: string | null;
  data_testid?: string | null;
  css_selector?: string | null;
  xpath?: string | null;
  rect?: { x: number; y: number; width: number; height: number } | null;
  visible: boolean;
  enabled: boolean;
}

interface AILocateCandidate {
  center: [number, number];
  bbox: [number, number, number, number];
  confidence: number;
  raw_response?: string | null;
}

export interface InterventionRequest {
  screenshot_url?: string | null;
  page_url: string;
  target_description: string;
  dom_snapshot: DOMElementSnapshot[];
  ai_candidate?: AILocateCandidate | null;
  locator_trace?: LocatorTrace | null;
}

export interface StepExecutionEvidence {
  step_index: number;
  action: string;
  target?: string | null;
  value?: string | null;
  status: "passed" | "failed";
  duration_ms?: number | null;
  pre_state?: PageStateSnapshot | null;
  condition_results?: ConditionResult[];
  action_outcome?: ActionOutcome;
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
  intervention_request?: InterventionRequest | null;
}

interface ExecutionReport {
  status: ExecutionStatus;
  steps: StepExecutionEvidence[];
}

export interface StoredCaseExecutionSummary {
  id: number;
  case_id: number;
  case_name: string;
  project_id: number;
  batch_id?: number | null;
  job_id?: number | null;
  attempt_number: number;
  dsl_sha256?: string | null;
  report_schema_version: string;
  triggered_by: number;
  status: ExecutionStatus;
  error_message: string | null;
  started_at: string;
  finished_at: string | null;
  duration_ms?: number | null;
  total_steps: number;
  failed_step_index?: number | null;
  failure_category?: FailureCategory | null;
  failure_signal?: FailureSignal | null;
  failure_step_action?: string | null;
  latest_url?: string | null;
  latest_screenshot_url?: string | null;
}

export interface StoredCaseExecutionDetail extends StoredCaseExecutionSummary {
  report: ExecutionReport | null;
  analysis_status: ExecutionAnalysisStatus;
  analysis?: ExecutionAnalysis | null;
}

export interface ExecutionBatchCreatePayload {
  project_id: number;
  case_ids: number[];
  concurrency_limit: number;
  input_values?: Record<string, string>;
  idempotency_key?: string;
}

interface ExecutionJobSummary {
  id: number;
  batch_id: number;
  project_id: number;
  case_id: number;
  case_name: string;
  order_index: number;
  status: ExecutionBatchStatus;
  attempt_count: number;
  max_attempts: number;
  cancel_requested: boolean;
  last_error_message?: string | null;
  created_at: string;
  started_at?: string | null;
  heartbeat_at?: string | null;
  finished_at?: string | null;
  latest_execution?: StoredCaseExecutionDetail | null;
}

export interface ExecutionBatchSummary {
  id: number;
  project_id: number;
  planning_session_id?: number | null;
  triggered_by: number;
  status: ExecutionBatchStatus;
  idempotency_key?: string | null;
  concurrency_limit: number;
  total_jobs: number;
  pending_jobs: number;
  running_jobs: number;
  passed_jobs: number;
  failed_jobs: number;
  intervention_jobs: number;
  cancelled_jobs: number;
  analysis_status: ExecutionAnalysisStatus;
  analysis?: ExecutionAnalysis | null;
  created_at: string;
  started_at?: string | null;
  finished_at?: string | null;
}

export interface ExecutionBatchDetail extends ExecutionBatchSummary {
  jobs: ExecutionJobSummary[];
}

export interface ExecutionBatchReport extends ExecutionBatchDetail {
  pass_rate: number;
  completed_jobs: number;
}

export interface CreateCorrectionPayload {
  page_url: string;
  target_description: string;
  correction_type: CorrectionType;
  correction_value: string;
  source_execution_id: number;
}

export interface StoredLocatorCorrection {
  id: number;
  page_url_pattern: string;
  target_description: string;
  correction_type: CorrectionType;
  correction_value: string;
  verified_count: number;
  consecutive_failures: number;
  is_active: boolean;
  source_execution_id: number | null;
  created_by: number;
  created_at: string;
  updated_at: string;
}

interface FailureCategoryCount {
  category: FailureCategory;
  count: number;
}

interface ExecutionAggregateSnapshot {
  total_count: number;
  passed_count: number;
  failed_count: number;
  running_count: number;
  pass_rate: number;
  avg_duration_ms: number;
}

interface ExecutionWindowRange {
  start_date?: string | null;
  end_date?: string | null;
}

interface ExecutionWindowComparison {
  total_count_delta: number;
  passed_count_delta: number;
  failed_count_delta: number;
  running_count_delta: number;
  pass_rate_delta: number;
  avg_duration_ms_delta: number;
}

interface ExecutionTrendPoint {
  date: string;
  total_count: number;
  passed_count: number;
  failed_count: number;
  auto_completed_count: number;
  intervention_count: number;
  pass_rate: number;
  avg_duration_ms: number;
}

interface FailureStepActionCount {
  action: string;
  count: number;
}

interface TopFailedCase {
  case_id: number;
  case_name: string;
  failure_count: number;
  latest_execution_id: number;
  latest_failure_category?: FailureCategory | null;
}

interface FailureRootCause {
  fingerprint: string;
  title: string;
  count: number;
  affected_case_count: number;
  latest_execution_id: number;
  latest_failure_category?: FailureCategory | null;
}

export interface ExecutionsOverview {
  scope_type: ReportScopeType;
  scope_project_id?: number | null;
  scope_case_id?: number | null;
  total_count: number;
  passed_count: number;
  failed_count: number;
  running_count: number;
  auto_completed_count: number;
  intervention_count: number;
  pass_rate: number;
  automation_rate: number;
  intervention_rate: number;
  avg_duration_ms: number;
  current_window_range?: ExecutionWindowRange | null;
  previous_window_range?: ExecutionWindowRange | null;
  previous_window_stats: ExecutionAggregateSnapshot;
  window_comparison: ExecutionWindowComparison;
  latest_failed_runs: StoredCaseExecutionSummary[];
  latest_intervention_runs: StoredCaseExecutionSummary[];
  failure_categories: FailureCategoryCount[];
  trend_points: ExecutionTrendPoint[];
  failure_step_actions: FailureStepActionCount[];
  top_failed_cases: TopFailedCase[];
  failure_root_causes: FailureRootCause[];
}

export interface LinkProjectPayload {
  project_id: number;
}

export interface CreateProjectInSessionPayload {
  name: string;
  description?: string | null;
}
