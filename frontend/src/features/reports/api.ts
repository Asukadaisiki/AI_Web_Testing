import type { ReportPreference } from "./types";

import { request } from "../../shared/api/client";

export function getReportPreference() {
  return request<ReportPreference>("/api/v1/reports/preferences");
}

export function updateReportPreference(payload: ReportPreference) {
  return request<ReportPreference>("/api/v1/reports/preferences", {
    method: "PUT",
    body: JSON.stringify(payload),
  });
}
