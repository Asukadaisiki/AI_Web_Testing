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

const PLANNING_API = "/api/v2/planning";
const LEGACY_PLANNING_API = "/api/v1/ai-planning";

export function createPlanningSession(payload: CreatePlanningSessionPayload) {
  return request<AIPlanningSessionDetail>(`${PLANNING_API}/sessions`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function getPlanningSession(sessionId: number) {
  return request<AIPlanningSessionDetail>(`${PLANNING_API}/sessions/${sessionId}`);
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
    `${LEGACY_PLANNING_API}/sessions/${sessionId}/events?${params}`,
  );
}

export function listPlanningSessions() {
  return request<AIPlanningSessionSummary[]>(`${PLANNING_API}/sessions`);
}

export function deletePlanningSession(sessionId: number) {
  return request<void>(`${PLANNING_API}/sessions/${sessionId}`, {
    method: "DELETE",
  });
}

export function listSessionProjects(sessionId: number) {
  return request<ProjectSummaryInSession[]>(`${PLANNING_API}/sessions/${sessionId}/projects`);
}

export function linkProjectToSession(sessionId: number, payload: LinkProjectPayload) {
  return request<ProjectSummaryInSession>(`${PLANNING_API}/sessions/${sessionId}/projects`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function unlinkProjectFromSession(sessionId: number, projectId: number) {
  return request<void>(`${PLANNING_API}/sessions/${sessionId}/projects/${projectId}`, {
    method: "DELETE",
  });
}

export function createProjectInSession(sessionId: number, payload: CreateProjectInSessionPayload) {
  return request<ProjectSummaryInSession>(`${PLANNING_API}/sessions/${sessionId}/projects:create`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function saveAndExecuteDrafts(sessionId: number, draftIds: number[], execute: boolean = true) {
  return request<AIPlanningTurnResponse>(`${LEGACY_PLANNING_API}/sessions/${sessionId}/drafts:save-and-execute`, {
    method: "POST",
    body: JSON.stringify({ draft_ids: draftIds, execute }),
  });
}

export function sendPlanningMessage(sessionId: number, payload: SendPlanningMessagePayload) {
  return request<AIPlanningTurnResponse>(`${LEGACY_PLANNING_API}/sessions/${sessionId}/messages`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function generatePlanningDrafts(sessionId: number, payload: GeneratePlanningDraftsPayload) {
  return request<AIPlanningTurnResponse>(`${LEGACY_PLANNING_API}/sessions/${sessionId}/drafts:generate`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function updatePlanningDraftStatus(draftId: number, payload: UpdatePlanningDraftStatusPayload) {
  return request<AIPlanningDraft>(`${LEGACY_PLANNING_API}/drafts/${draftId}`, {
    method: "PATCH",
    body: JSON.stringify(payload),
  });
}

export function deletePlanningDraft(draftId: number) {
  return request<void>(`${LEGACY_PLANNING_API}/drafts/${draftId}`, {
    method: "DELETE",
  });
}

export function cancelExecution(sessionId: number) {
  return request<{ status: string }>(
    `${LEGACY_PLANNING_API}/sessions/${sessionId}/cancel`,
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
