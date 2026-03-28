import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp, ConfigProvider } from "antd";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";

import { AppRouter } from "./AppRouter";
import { useAuth } from "../auth/AuthContext";

vi.mock("../auth/AuthContext", () => ({
  useAuth: vi.fn(),
}));

vi.mock("../pages/DashboardPage", () => ({
  DashboardPage: () => <div>Dashboard Mock</div>,
}));

vi.mock("../pages/CasesPage", () => ({
  CasesPage: () => <div>Cases Mock</div>,
}));

vi.mock("../pages/SuitesPage", () => ({
  SuitesPage: () => <div>Suites Mock</div>,
}));

vi.mock("../pages/SuiteWorkbenchPage", () => ({
  SuiteWorkbenchPage: () => <div>Suite Workbench Mock</div>,
}));

vi.mock("../pages/SuiteRunDetailPage", () => ({
  SuiteRunDetailPage: () => <div>Suite Run Detail Mock</div>,
}));

vi.mock("../pages/ExecutionsPage", () => ({
  ExecutionsPage: () => <div>Executions Mock</div>,
}));

vi.mock("../pages/CorrectionsPage", () => ({
  CorrectionsPage: () => <div>Corrections Mock</div>,
}));

vi.mock("../pages/AISettingsPage", () => ({
  AISettingsPage: () => <div>AI Settings Mock</div>,
}));

vi.mock("../pages/ExecutionDetailPage", () => ({
  ExecutionDetailPage: () => <div>Execution Detail Mock</div>,
}));

vi.mock("../pages/CaseWorkbenchPage", () => ({
  CaseWorkbenchPage: () => <div>Workbench Mock</div>,
}));

vi.mock("../pages/ReportCenterPage", () => ({
  ReportCenterPage: () => <div>Reports Mock</div>,
}));

vi.mock("../pages/LoginPage", () => ({
  LoginPage: () => <div>Login Mock</div>,
}));

const useAuthMock = vi.mocked(useAuth);

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
          <MemoryRouter initialEntries={initialEntries}>
            <AppRouter />
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>
    </ConfigProvider>,
  );
}

test("根路由默认跳转到 dashboard，并展示 v3.4 导航入口", async () => {
  useAuthMock.mockReturnValue({
    currentUser: { id: 1, email: "seed-owner@example.com", display_name: "Seed Owner" },
    isAuthResolved: true,
    isAuthenticated: true,
    login: vi.fn(),
    logout: vi.fn(),
  });
  renderRouter(["/"]);

  expect(await screen.findByText("Dashboard Mock")).toBeInTheDocument();
  expect(screen.getByText("混合定位稳定化 v3.4")).toBeInTheDocument();
  expect(screen.getByText("Seed Owner")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "仪表盘" })).toHaveAttribute("href", "/dashboard");
  expect(screen.getByRole("link", { name: "Suite 管理" })).toHaveAttribute("href", "/suites");
  expect(screen.getByRole("link", { name: "修正记录" })).toHaveAttribute("href", "/corrections");
  expect(screen.getByRole("link", { name: "AI 配置" })).toHaveAttribute("href", "/settings/ai");
  expect(screen.getByRole("link", { name: "报告中心" })).toHaveAttribute("href", "/reports");
});

test("报告中心路由激活时菜单选中正确", async () => {
  useAuthMock.mockReturnValue({
    currentUser: { id: 1, email: "seed-owner@example.com", display_name: "Seed Owner" },
    isAuthResolved: true,
    isAuthenticated: true,
    login: vi.fn(),
    logout: vi.fn(),
  });
  renderRouter(["/reports"]);

  expect(await screen.findByText("Reports Mock")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "报告中心" }).closest("li")).toHaveClass("ant-menu-item-selected");
});

test("suite 路由激活时菜单选中正确", async () => {
  useAuthMock.mockReturnValue({
    currentUser: { id: 1, email: "seed-owner@example.com", display_name: "Seed Owner" },
    isAuthResolved: true,
    isAuthenticated: true,
    login: vi.fn(),
    logout: vi.fn(),
  });
  renderRouter(["/suites"]);

  expect(await screen.findByText("Suites Mock")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Suite 管理" }).closest("li")).toHaveClass("ant-menu-item-selected");
});

test("suite run 详情路由可正常渲染", async () => {
  useAuthMock.mockReturnValue({
    currentUser: { id: 1, email: "seed-owner@example.com", display_name: "Seed Owner" },
    isAuthResolved: true,
    isAuthenticated: true,
    login: vi.fn(),
    logout: vi.fn(),
  });
  renderRouter(["/suites/2/runs/8"]);

  expect(await screen.findByText("Suite Run Detail Mock")).toBeInTheDocument();
});

test("corrections 路由激活时菜单选中正确", async () => {
  useAuthMock.mockReturnValue({
    currentUser: { id: 1, email: "seed-owner@example.com", display_name: "Seed Owner" },
    isAuthResolved: true,
    isAuthenticated: true,
    login: vi.fn(),
    logout: vi.fn(),
  });
  renderRouter(["/corrections"]);

  expect(await screen.findByText("Corrections Mock")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "修正记录" }).closest("li")).toHaveClass("ant-menu-item-selected");
});

test("AI 配置路由激活时菜单选中正确", async () => {
  useAuthMock.mockReturnValue({
    currentUser: { id: 1, email: "seed-owner@example.com", display_name: "Seed Owner" },
    isAuthResolved: true,
    isAuthenticated: true,
    login: vi.fn(),
    logout: vi.fn(),
  });
  renderRouter(["/settings/ai"]);

  expect(await screen.findByText("AI Settings Mock")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "AI 配置" }).closest("li")).toHaveClass("ant-menu-item-selected");
});

test("未登录访问受保护路由时跳转到 login", async () => {
  useAuthMock.mockReturnValue({
    currentUser: null,
    isAuthResolved: true,
    isAuthenticated: false,
    login: vi.fn(),
    logout: vi.fn(),
  });

  renderRouter(["/cases"]);

  expect(await screen.findByText("Login Mock")).toBeInTheDocument();
  expect(screen.queryByText("Cases Mock")).not.toBeInTheDocument();
});
