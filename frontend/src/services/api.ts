import type {
  AISettingsOverview,
  AISettings,
  AISettingsUpdatePayload,
  BatchUpdateCorrectionStatePayload,
  CaseMutationPayload,
  CaseExecutionRequest,
  CreateCorrectionPayload,
  DSLCasePayload,
  GenerateDslRequest,
  GenerateDslResponse,
  DSLValidationResult,
  ExecutionsOverview,
  LocatorCorrectionsOverview,
  OverviewWindowDays,
  StoredCaseDetail,
  StoredCaseExecutionDetail,
  StoredCaseExecutionSummary,
  StoredCaseSummary,
  StoredLocatorCorrection,
  StoredLocatorCorrectionEvent,
  StoredSuiteDetail,
  StoredSuiteRunDetail,
  StoredSuiteRunSummary,
  StoredSuiteSummary,
  SuiteExecutionRequest,
  SuiteExecutionResult,
  SuiteMutationPayload,
  UpdateCorrectionStatePayload,
} from "../types/api";

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? "";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
    ...init,
  });

  if (!response.ok) {
    let message = `${response.status} ${response.statusText}`;
    try {
      const payload = (await response.json()) as { detail?: string };
      if (payload.detail) {
        message = payload.detail;
      }
    } catch {
      // Ignore non-JSON errors.
    }
    throw new Error(message);
  }

  return (await response.json()) as T;
}

export function getCases() {
  return request<StoredCaseSummary[]>("/api/v1/cases");
}

export function getCaseDetail(caseId: number) {
  return request<StoredCaseDetail>(`/api/v1/cases/${caseId}`);
}

export function createCase(payload: CaseMutationPayload) {
  return request<StoredCaseDetail>("/api/v1/cases", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function getSuites() {
  return request<StoredSuiteSummary[]>("/api/v1/suites");
}

export function getSuiteDetail(suiteId: number) {
  return request<StoredSuiteDetail>(`/api/v1/suites/${suiteId}`);
}

export function createSuite(payload: SuiteMutationPayload) {
  return request<StoredSuiteDetail>("/api/v1/suites", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function updateSuite(suiteId: number, payload: SuiteMutationPayload) {
  return request<StoredSuiteDetail>(`/api/v1/suites/${suiteId}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  });
}

export function executeSuite(suiteId: number, payload: SuiteExecutionRequest) {
  return request<SuiteExecutionResult>(`/api/v1/suites/${suiteId}/execute`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function getSuiteRuns(suiteId: number) {
  return request<StoredSuiteRunSummary[]>(`/api/v1/suites/${suiteId}/runs`);
}

export function getSuiteRunDetail(suiteId: number, runId: number) {
  return request<StoredSuiteRunDetail>(`/api/v1/suites/${suiteId}/runs/${runId}`);
}

export function rerunFailedSuiteRun(suiteId: number, runId: number, payload: SuiteExecutionRequest) {
  return request<SuiteExecutionResult>(`/api/v1/suites/${suiteId}/runs/${runId}/rerun-failed`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function updateCase(caseId: number, payload: CaseMutationPayload) {
  return request<StoredCaseDetail>(`/api/v1/cases/${caseId}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  });
}

export function validateDslCase(payload: DSLCasePayload) {
  return request<DSLValidationResult>("/api/v1/dsl/validate", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function generateDslCase(payload: GenerateDslRequest) {
  return request<GenerateDslResponse>("/api/v1/dsl/generate", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function getAISettings() {
  return request<AISettings>("/api/v1/settings/ai");
}

export function getAISettingsOverview() {
  return request<AISettingsOverview>("/api/v1/settings/ai/overview");
}

export function updateAISettings(payload: AISettingsUpdatePayload) {
  return request<AISettings>("/api/v1/settings/ai", {
    method: "PUT",
    body: JSON.stringify(payload),
  });
}

export function executeCase(caseId: number, payload: CaseExecutionRequest) {
  return request<StoredCaseExecutionDetail>(`/api/v1/cases/${caseId}/execute`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function createCorrection(payload: CreateCorrectionPayload) {
  return request<StoredLocatorCorrection>("/api/v1/corrections", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function getCorrections(params: {
  target_description?: string;
  page_url?: string;
  is_active?: boolean;
  limit?: number;
  offset?: number;
}) {
  const search = new URLSearchParams();
  if (params.target_description) {
    search.set("target_description", params.target_description);
  }
  if (params.page_url) {
    search.set("page_url", params.page_url);
  }
  if (typeof params.is_active === "boolean") {
    search.set("is_active", String(params.is_active));
  }
  if (params.limit) {
    search.set("limit", String(params.limit));
  }
  if (params.offset != null) {
    search.set("offset", String(params.offset));
  }
  const query = search.toString();
  return request<StoredLocatorCorrection[]>(`/api/v1/corrections${query ? `?${query}` : ""}`);
}

export function updateCorrectionState(correctionId: number, payload: UpdateCorrectionStatePayload) {
  return request<StoredLocatorCorrection>(`/api/v1/corrections/${correctionId}`, {
    method: "PATCH",
    body: JSON.stringify(payload),
  });
}

export function batchUpdateCorrectionState(payload: BatchUpdateCorrectionStatePayload) {
  return request<StoredLocatorCorrection[]>("/api/v1/corrections/bulk", {
    method: "PATCH",
    body: JSON.stringify(payload),
  });
}

export function getCorrectionsOverview(window_days: OverviewWindowDays) {
  return request<LocatorCorrectionsOverview>(`/api/v1/corrections/overview?window_days=${window_days}`);
}

export function getCorrectionEvents(correctionId: number, params?: { limit?: number; offset?: number }) {
  const search = new URLSearchParams();
  if (params?.limit != null) {
    search.set("limit", String(params.limit));
  }
  if (params?.offset != null) {
    search.set("offset", String(params.offset));
  }
  const query = search.toString();
  return request<StoredLocatorCorrectionEvent[]>(
    `/api/v1/corrections/${correctionId}/events${query ? `?${query}` : ""}`,
  );
}

export function getExecutions(params: {
  project_id?: number;
  case_id?: number;
  status?: string;
  window_days?: OverviewWindowDays;
  failure_category?: string;
  failure_fingerprint?: string;
  limit?: number;
  offset?: number;
}) {
  const search = new URLSearchParams();
  if (params.project_id) {
    search.set("project_id", String(params.project_id));
  }
  if (params.case_id) {
    search.set("case_id", String(params.case_id));
  }
  if (params.status) {
    search.set("status", params.status);
  }
  if (params.window_days) {
    search.set("window_days", String(params.window_days));
  }
  if (params.failure_category) {
    search.set("failure_category", params.failure_category);
  }
  if (params.failure_fingerprint) {
    search.set("failure_fingerprint", params.failure_fingerprint);
  }
  if (params.limit) {
    search.set("limit", String(params.limit));
  }
  if (params.offset != null) {
    search.set("offset", String(params.offset));
  }
  return request<StoredCaseExecutionSummary[]>(`/api/v1/executions?${search.toString()}`);
}

export function getExecutionOverview(params: {
  project_id?: number;
  case_id?: number;
  window_days?: OverviewWindowDays;
  failure_fingerprint?: string;
}) {
  const search = new URLSearchParams();
  if (params.project_id) {
    search.set("project_id", String(params.project_id));
  }
  if (params.case_id) {
    search.set("case_id", String(params.case_id));
  }
  if (params.window_days) {
    search.set("window_days", String(params.window_days));
  }
  if (params.failure_fingerprint) {
    search.set("failure_fingerprint", params.failure_fingerprint);
  }
  const query = search.toString();
  return request<ExecutionsOverview>(`/api/v1/executions/overview${query ? `?${query}` : ""}`);
}

export function getExecutionDetail(executionId: number) {
  return request<StoredCaseExecutionDetail>(`/api/v1/executions/${executionId}`);
}
