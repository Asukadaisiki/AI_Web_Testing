import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp, ConfigProvider } from "antd";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, vi } from "vitest";

import { AppRouter } from "./AppRouter";
import * as authApi from "../features/auth/api";
import { getSafeDestination } from "../features/auth/LoginPage";
import { ApiError } from "../shared/api/client";

vi.mock("../features/auth/api", () => ({
  getCurrentUser: vi.fn(),
  login: vi.fn(),
}));

vi.mock("../pages/SessionListPage", () => ({
  SessionListPage: () => <div>Session List Mock</div>,
}));
vi.mock("../pages/PlanningPage", () => ({
  PlanningPage: () => <div>Planning Mock</div>,
}));
vi.mock("../pages/CasesPage", () => ({
  CasesPage: () => <div>Cases Mock</div>,
}));
vi.mock("../pages/CaseEditPage", () => ({
  CaseEditPage: () => <div>Case Editor Mock</div>,
}));
vi.mock("../pages/ExecutionDetailPage", () => ({
  ExecutionDetailPage: () => <div>Execution Detail Mock</div>,
}));
vi.mock("../pages/ReportPage", () => ({
  ReportPage: () => <div>Report Mock</div>,
}));

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(authApi.getCurrentUser).mockResolvedValue({
    id: 1,
    email: "owner@example.com",
    display_name: "Owner",
  });
});

function renderRouter(initialEntries: string[]) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return render(
    <ConfigProvider>
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter
            initialEntries={initialEntries}
          >
            <AppRouter />
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>
    </ConfigProvider>,
  );
}

test("root route renders session list after authentication", async () => {
  renderRouter(["/"]);
  expect(await screen.findByText("Session List Mock")).toBeInTheDocument();
});

test("protected routes redirect unauthenticated users to login", async () => {
  vi.mocked(authApi.getCurrentUser).mockReset();
  vi.mocked(authApi.getCurrentUser).mockRejectedValue(
    new ApiError("未登录", 401),
  );
  renderRouter(["/cases"]);
  expect(await screen.findByRole("button", { name: "登录" })).toBeInTheDocument();
});

test("/cases renders cases page", async () => {
  renderRouter(["/cases"]);
  expect(await screen.findByText("Cases Mock")).toBeInTheDocument();
});

test("/cases/new renders case create page", async () => {
  renderRouter(["/cases/new?project_id=1"]);
  expect(await screen.findByText("Case Editor Mock")).toBeInTheDocument();
});

test("/cases/:id/edit renders case edit page", async () => {
  renderRouter(["/cases/12/edit"]);
  expect(await screen.findByText("Case Editor Mock")).toBeInTheDocument();
});

test("/run/:id renders execution detail page", async () => {
  renderRouter(["/run/12"]);
  expect(await screen.findByText("Execution Detail Mock")).toBeInTheDocument();
});

test("legacy execution detail path redirects to /run/:id", async () => {
  renderRouter(["/executions/12"]);
  expect(await screen.findByText("Execution Detail Mock")).toBeInTheDocument();
});

test("/dashboard redirects to root", async () => {
  renderRouter(["/dashboard"]);
  expect(await screen.findByText("Session List Mock")).toBeInTheDocument();
});

test("/login renders the login form", async () => {
  renderRouter(["/login"]);
  expect(await screen.findByRole("button", { name: "登录" })).toBeInTheDocument();
});

test("login destination rejects protocol-relative and backslash paths", () => {
  expect(getSafeDestination({ from: "//evil.example" })).toBe("/planning");
  expect(getSafeDestination({ from: "/\\evil.example" })).toBe("/planning");
  expect(getSafeDestination({ from: "/cases?project_id=1" })).toBe(
    "/cases?project_id=1",
  );
});

test("successful login returns to the planning workspace", async () => {
  vi.mocked(authApi.login).mockResolvedValue({
    id: 1,
    email: "owner@example.com",
    display_name: "Owner",
  });
  renderRouter(["/login"]);

  await userEvent.type(
    await screen.findByPlaceholderText("name@example.com"),
    "owner@example.com",
  );
  await userEvent.type(screen.getByPlaceholderText("请输入密码"), "password123");
  await userEvent.click(screen.getByRole("button", { name: "登录" }));

  expect(await screen.findByText("Session List Mock")).toBeInTheDocument();
  expect(authApi.login).toHaveBeenCalledWith({
    email: "owner@example.com",
    password: "password123",
  });
});

test("/executions redirects to /cases", async () => {
  renderRouter(["/executions"]);
  expect(await screen.findByText("Cases Mock")).toBeInTheDocument();
});
