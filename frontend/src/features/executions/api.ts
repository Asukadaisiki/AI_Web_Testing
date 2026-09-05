import type {
  CaseExecutionRequest,
  CreateCorrectionPayload,
  ExecutionBatchCreatePayload,
  ExecutionBatchDetail,
  ExecutionBatchReport,
  ExecutionBatchSummary,
  ExecutionsOverview,
  OverviewWindowDays,
  ReportScopeType,
  StoredCaseExecutionDetail,
  StoredCaseExecutionSummary,
  StoredLocatorCorrection,
} from "./types";

import { request } from "../../shared/api/client";

export function executeCase(caseId: number, payload: CaseExecutionRequest) {
  return request<StoredCaseExecutionDetail>(`/api/v1/cases/${caseId}/execute`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function createExecutionBatch(payload: ExecutionBatchCreatePayload) {
  return request<ExecutionBatchDetail>("/api/v1/execution-batches", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function getExecutionBatches(projectId: number, limit = 50) {
  return request<ExecutionBatchSummary[]>(
    `/api/v1/execution-batches?project_id=${projectId}&limit=${limit}`,
  );
}

export function getExecutionBatchReport(batchId: number) {
  return request<ExecutionBatchReport>(
    `/api/v1/execution-batches/${batchId}/report`,
  );
}

export function cancelExecutionBatch(batchId: number) {
  return request<ExecutionBatchDetail>(
    `/api/v1/execution-batches/${batchId}/cancel`,
    { method: "POST" },
  );
}

export function createCorrection(payload: CreateCorrectionPayload) {
  return request<StoredLocatorCorrection>("/api/v1/corrections", {
    method: "POST",
    body: JSON.stringify(payload),
  });
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
  scope_type?: ReportScopeType;
  project_id?: number;
  case_id?: number;
  window_days?: OverviewWindowDays;
  failure_fingerprint?: string;
}) {
  const search = new URLSearchParams();
  if (params.scope_type) {
    search.set("scope_type", params.scope_type);
  }
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

export function deleteExecution(executionId: number) {
  return request<void>(`/api/v1/executions/${executionId}`, {
    method: "DELETE",
  });
}
