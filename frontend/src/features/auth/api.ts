import type { CurrentUser, LoginPayload, LogoutResponse } from "./types";

import { request } from "../../shared/api/client";

export function login(payload: LoginPayload) {
  return request<CurrentUser>("/api/v1/auth/login", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function logout() {
  return request<LogoutResponse>("/api/v1/auth/logout", {
    method: "POST",
  });
}

export function getCurrentUser() {
  return request<CurrentUser>("/api/v1/auth/me");
}
