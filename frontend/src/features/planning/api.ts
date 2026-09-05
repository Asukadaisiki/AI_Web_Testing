import type {
  AIPlanningSessionDetail,
  AIPlanningSessionSummary,
  AISettings,
  CreatePlanningSessionPayload,
  CreateProjectInSessionPayload,
  LinkProjectPayload,
  ProjectSummaryInSession,
} from "./types";

import { request } from "../../shared/api/client";

const PLANNING_API = "/api/v2/planning";

export function createPlanningSession(payload: CreatePlanningSessionPayload) {
  return request<AIPlanningSessionDetail>(`${PLANNING_API}/sessions`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function getPlanningSession(sessionId: number) {
  return request<AIPlanningSessionDetail>(`${PLANNING_API}/sessions/${sessionId}`);
}

export function listPlanningSessions() {
  return request<AIPlanningSessionSummary[]>(`${PLANNING_API}/sessions`);
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

export function getAISettings() {
  return request<AISettings>("/api/v1/settings/ai");
}
