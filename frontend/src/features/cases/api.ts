import type {
  CaseMutationPayload,
  DSLCasePayload,
  DSLValidationResult,
  DslGenerationFeedbackPayload,
  DslGenerationFeedbackStatus,
  DslGenerationPromptVariant,
  DslGenerationRejectionReasonCode,
  DslGenerationRunStatus,
  GenerateDslImportMode,
  GenerateDslMode,
  GenerateDslRequest,
  GenerateDslResponse,
  PaginatedCases,
  StoredCaseDetail,
  StoredDslGenerationRunDetail,
  StoredDslGenerationRunSummary,
} from "./types";

import { request } from "../../shared/api/client";

export function getCases(params?: { project_id?: number }) {
  const search = new URLSearchParams();
  if (params?.project_id != null) {
    search.set("project_id", String(params.project_id));
  }
  const query = search.toString();
  return request<PaginatedCases>(`/api/v1/cases${query ? `?${query}` : ""}`);
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

export function deleteCase(caseId: number) {
  return request<void>(`/api/v1/cases/${caseId}`, {
    method: "DELETE",
  });
}

export function batchDeleteCases(caseIds: number[]) {
  return request<void>("/api/v1/cases/batch", {
    method: "DELETE",
    body: JSON.stringify({ case_ids: caseIds }),
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

export function getDslGenerationRuns(params?: {
  status?: DslGenerationRunStatus;
  feedback_status?: DslGenerationFeedbackStatus;
  generation_mode?: GenerateDslMode;
  import_mode?: GenerateDslImportMode;
  prompt_variant?: DslGenerationPromptVariant;
  rejection_reason_code?: DslGenerationRejectionReasonCode;
  has_risk_flags?: boolean;
  model_name?: string;
  project_id?: number;
  case_id?: number;
  created_from?: string;
  created_to?: string;
  limit?: number;
  offset?: number;
}) {
  const search = new URLSearchParams();
  if (params?.status) {
    search.set("status", params.status);
  }
  if (params?.feedback_status) {
    search.set("feedback_status", params.feedback_status);
  }
  if (params?.generation_mode) {
    search.set("generation_mode", params.generation_mode);
  }
  if (params?.import_mode) {
    search.set("import_mode", params.import_mode);
  }
  if (params?.prompt_variant) {
    search.set("prompt_variant", params.prompt_variant);
  }
  if (params?.rejection_reason_code) {
    search.set("rejection_reason_code", params.rejection_reason_code);
  }
  if (typeof params?.has_risk_flags === "boolean") {
    search.set("has_risk_flags", String(params.has_risk_flags));
  }
  if (params?.model_name) {
    search.set("model_name", params.model_name);
  }
  if (params?.project_id != null) {
    search.set("project_id", String(params.project_id));
  }
  if (params?.case_id != null) {
    search.set("case_id", String(params.case_id));
  }
  if (params?.created_from) {
    search.set("created_from", params.created_from);
  }
  if (params?.created_to) {
    search.set("created_to", params.created_to);
  }
  if (params?.limit != null) {
    search.set("limit", String(params.limit));
  }
  if (params?.offset != null) {
    search.set("offset", String(params.offset));
  }
  const query = search.toString();
  return request<StoredDslGenerationRunSummary[]>(`/api/v1/dsl/generations${query ? `?${query}` : ""}`);
}

export function getDslGenerationRunDetail(generationId: number) {
  return request<StoredDslGenerationRunDetail>(`/api/v1/dsl/generations/${generationId}`);
}

export function recordDslGenerationFeedback(generationId: number, payload: DslGenerationFeedbackPayload) {
  return request<StoredDslGenerationRunSummary>(`/api/v1/dsl/generations/${generationId}/feedback`, {
    method: "PATCH",
    body: JSON.stringify(payload),
  });
}

export function deleteDslGenerationRun(generationId: number) {
  return request<void>(`/api/v1/dsl/generations/${generationId}`, { method: "DELETE" });
}

