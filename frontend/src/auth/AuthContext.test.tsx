import { useQueryClient } from "@tanstack/react-query";
import { act, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { renderWithProviders } from "../test/test-utils";
import { AuthProvider, useAuth } from "./AuthContext";

const getCurrentUserMock = vi.fn();
const loginMock = vi.fn();
const logoutMock = vi.fn();
const clearQueryCacheMock = vi.fn();
const queryClientMock = {
  clear: clearQueryCacheMock,
};

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>("@tanstack/react-query");
  return {
    ...actual,
    useQueryClient: () => queryClientMock,
  };
});

vi.mock("../services/api", () => ({
  AUTH_UNAUTHORIZED_EVENT: "auth:unauthorized",
  getCurrentUser: () => getCurrentUserMock(),
  login: (payload: { email: string; password: string }) => loginMock(payload),
  logout: () => logoutMock(),
}));

function AuthProbe() {
  const auth = useAuth();

  return (
    <div>
      <div>resolved:{String(auth.isAuthResolved)}</div>
      <div>authenticated:{String(auth.isAuthenticated)}</div>
      <div>user:{auth.currentUser?.display_name ?? "none"}</div>
      <div>error:{auth.authErrorMessage ?? "none"}</div>
      <button type="button" onClick={() => auth.login({ email: "seed-owner@example.com", password: "password123" })}>
        login
      </button>
      <button type="button" onClick={() => auth.logout()}>
        logout
      </button>
    </div>
  );
}

beforeEach(() => {
  getCurrentUserMock.mockResolvedValue(null);
  loginMock.mockResolvedValue({
    id: 1,
    email: "seed-owner@example.com",
    display_name: "Seed Owner",
  });
  logoutMock.mockResolvedValue({ success: true });
});

afterEach(() => {
  vi.clearAllMocks();
});

test("刷新后会通过 me 接口恢复登录态", async () => {
  getCurrentUserMock.mockResolvedValue({
    id: 1,
    email: "seed-owner@example.com",
    display_name: "Seed Owner",
  });

  renderWithProviders(
    <AuthProvider>
      <AuthProbe />
    </AuthProvider>,
  );

  expect(await screen.findByText("resolved:true")).toBeInTheDocument();
  expect(screen.getByText("authenticated:true")).toBeInTheDocument();
  expect(screen.getByText("user:Seed Owner")).toBeInTheDocument();
});

test("login 成功后会写入当前用户", async () => {
  const userEvent = (await import("@testing-library/user-event")).default;

  renderWithProviders(
    <AuthProvider>
      <AuthProbe />
    </AuthProvider>,
  );

  await userEvent.click(await screen.findByRole("button", { name: "login" }));

  await waitFor(() => {
    expect(screen.getByText("authenticated:true")).toBeInTheDocument();
  });
  expect(screen.getByText("user:Seed Owner")).toBeInTheDocument();
});

test("logout 和 401 事件都会清空登录态", async () => {
  getCurrentUserMock.mockResolvedValue({
    id: 1,
    email: "seed-owner@example.com",
    display_name: "Seed Owner",
  });
  const userEvent = (await import("@testing-library/user-event")).default;

  renderWithProviders(
    <AuthProvider>
      <AuthProbe />
    </AuthProvider>,
  );

  expect(await screen.findByText("authenticated:true")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "logout" }));
  await waitFor(() => {
    expect(screen.getByText("authenticated:false")).toBeInTheDocument();
  });
  expect(clearQueryCacheMock).toHaveBeenCalledTimes(1);

  await act(async () => {
    window.dispatchEvent(new CustomEvent("auth:unauthorized"));
  });
  await waitFor(() => {
    expect(screen.getByText("user:none")).toBeInTheDocument();
  });
  expect(clearQueryCacheMock).toHaveBeenCalledTimes(2);
});

test("me 接口非 401 失败时保留错误态而不是误报未登录", async () => {
  getCurrentUserMock.mockRejectedValue(Object.assign(new Error("服务暂时不可用"), { status: 503 }));

  renderWithProviders(
    <AuthProvider>
      <AuthProbe />
    </AuthProvider>,
  );

  expect(await screen.findByText("resolved:true")).toBeInTheDocument();
  expect(screen.getByText("authenticated:false")).toBeInTheDocument();
  expect(screen.getByText("error:服务暂时不可用")).toBeInTheDocument();
});
