import type { CurrentUser, LoginPayload, LogoutResponse } from "./types";

import { request } from "../../shared/api/client";

export function login(payload: LoginPayload) {
  return request<CurrentUser>("/api/v2/auth/login", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function logout() {
  return request<LogoutResponse>("/api/v2/auth/logout", {
    method: "POST",
  });
}

export function getCurrentUser() {
  return request<CurrentUser>("/api/v2/auth/me");
}
