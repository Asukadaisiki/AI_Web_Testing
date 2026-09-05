import type {
  CaseMutationPayload,
  PaginatedCases,
  StoredCaseDetail,
} from "./types";

import { request } from "../../shared/api/client";

export function getCases(params?: { project_id?: number }) {
  const search = new URLSearchParams();
  if (params?.project_id != null) {
    search.set("project_id", String(params.project_id));
  }
  const query = search.toString();
  return request<PaginatedCases>(`/api/v2/cases${query ? `?${query}` : ""}`);
}


export function getCaseDetail(caseId: number) {
  return request<StoredCaseDetail>(`/api/v2/cases/${caseId}`);
}

export function createCase(payload: CaseMutationPayload) {
  return request<StoredCaseDetail>("/api/v2/cases", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function deleteCase(caseId: number) {
  return request<void>(`/api/v2/cases/${caseId}`, {
    method: "DELETE",
  });
}

export function batchDeleteCases(caseIds: number[]) {
  return request<void>("/api/v2/cases/batch", {
    method: "DELETE",
    body: JSON.stringify({ case_ids: caseIds }),
  });
}

export function updateCase(caseId: number, payload: CaseMutationPayload) {
  return request<StoredCaseDetail>(`/api/v2/cases/${caseId}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  });
}
