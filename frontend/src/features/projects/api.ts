import type { ProjectSummary } from "./types";

import { request } from "../../shared/api/client";

export function getProjects() {
  return request<ProjectSummary[]>("/api/v2/projects");
}

export function createProject(payload: { name: string; description?: string }) {
  return request<ProjectSummary>("/api/v2/projects", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function updateProject(projectId: number, payload: { name?: string; description?: string }) {
  return request<ProjectSummary>(`/api/v2/projects/${projectId}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  });
}

export function deleteProject(projectId: number) {
  return request<void>(`/api/v2/projects/${projectId}`, { method: "DELETE" });
}
