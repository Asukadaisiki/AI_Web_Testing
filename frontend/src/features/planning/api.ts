import type {
  AIPlanningDraft,
  AIPlanningSessionDetail,
  AIPlanningSessionSummary,
  AIPlanningTurnResponse,
  AISettings,
  AISettingsOverview,
  AISettingsUpdatePayload,
  CreatePlanningSessionPayload,
  CreateProjectInSessionPayload,
  GeneratePlanningDraftsPayload,
  LinkProjectPayload,
  ProjectSummaryInSession,
  SendPlanningMessagePayload,
  UpdatePlanningDraftStatusPayload,
} from "./types";

import { request } from "../../shared/api/client";

export function createPlanningSession(payload: CreatePlanningSessionPayload) {
  return request<AIPlanningSessionDetail>("/api/v1/ai-planning/sessions", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function getPlanningSession(sessionId: number) {
  return request<AIPlanningSessionDetail>(`/api/v1/ai-planning/sessions/${sessionId}`);
}

/** SSE event log entry returned by the replay API. */
export interface SessionEventLogEntry {
  seq: number;
  event_type: string;
  event_data: Record<string, unknown>;
  message_id: number | null;
  created_at: string;
}

/**
 * Fetch SSE event logs for a planning session.
 * Used to replay missed events after a page refresh.
 * @param sessionId - The planning session ID
 * @param afterSeq - Only return events with seq > afterSeq (default 0 = all)
 */
export function getSessionEvents(sessionId: number, afterSeq: number = 0) {
  const params = new URLSearchParams({ after_seq: String(afterSeq) });
  return request<SessionEventLogEntry[]>(
    `/api/v1/ai-planning/sessions/${sessionId}/events?${params}`,
  );
}

export function listPlanningSessions() {
  return request<AIPlanningSessionSummary[]>("/api/v1/ai-planning/sessions");
}

export function deletePlanningSession(sessionId: number) {
  return request<void>(`/api/v1/ai-planning/sessions/${sessionId}`, {
    method: "DELETE",
  });
}

export function listSessionProjects(sessionId: number) {
  return request<ProjectSummaryInSession[]>(`/api/v1/ai-planning/sessions/${sessionId}/projects`);
}

export function linkProjectToSession(sessionId: number, payload: LinkProjectPayload) {
  return request<ProjectSummaryInSession>(`/api/v1/ai-planning/sessions/${sessionId}/projects`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function unlinkProjectFromSession(sessionId: number, projectId: number) {
  return request<void>(`/api/v1/ai-planning/sessions/${sessionId}/projects/${projectId}`, {
    method: "DELETE",
  });
}

export function createProjectInSession(sessionId: number, payload: CreateProjectInSessionPayload) {
  return request<ProjectSummaryInSession>(`/api/v1/ai-planning/sessions/${sessionId}/projects:create`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function saveAndExecuteDrafts(sessionId: number, draftIds: number[], execute: boolean = true) {
  return request<AIPlanningTurnResponse>(`/api/v1/ai-planning/sessions/${sessionId}/drafts:save-and-execute`, {
    method: "POST",
    body: JSON.stringify({ draft_ids: draftIds, execute }),
  });
}

export function sendPlanningMessage(sessionId: number, payload: SendPlanningMessagePayload) {
  return request<AIPlanningTurnResponse>(`/api/v1/ai-planning/sessions/${sessionId}/messages`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function generatePlanningDrafts(sessionId: number, payload: GeneratePlanningDraftsPayload) {
  return request<AIPlanningTurnResponse>(`/api/v1/ai-planning/sessions/${sessionId}/drafts:generate`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function updatePlanningDraftStatus(draftId: number, payload: UpdatePlanningDraftStatusPayload) {
  return request<AIPlanningDraft>(`/api/v1/ai-planning/drafts/${draftId}`, {
    method: "PATCH",
    body: JSON.stringify(payload),
  });
}

export function deletePlanningDraft(draftId: number) {
  return request<void>(`/api/v1/ai-planning/drafts/${draftId}`, {
    method: "DELETE",
  });
}

export function cancelExecution(sessionId: number) {
  return request<{ status: string }>(
    `/api/v1/ai-planning/sessions/${sessionId}/cancel`,
    { method: "POST" },
  );
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
