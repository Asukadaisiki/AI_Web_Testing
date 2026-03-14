import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp, ConfigProvider } from "antd";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";

import { AppRouter } from "./AppRouter";

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

vi.mock("../pages/ExecutionDetailPage", () => ({
  ExecutionDetailPage: () => <div>Execution Detail Mock</div>,
}));

vi.mock("../pages/CaseWorkbenchPage", () => ({
  CaseWorkbenchPage: () => <div>Workbench Mock</div>,
}));

vi.mock("../pages/ReportCenterPage", () => ({
  ReportCenterPage: () => <div>Reports Mock</div>,
}));

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
  renderRouter(["/"]);

  expect(await screen.findByText("Dashboard Mock")).toBeInTheDocument();
  expect(screen.getByText("混合定位稳定化 v3.4")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "仪表盘" })).toHaveAttribute("href", "/dashboard");
  expect(screen.getByRole("link", { name: "Suite 管理" })).toHaveAttribute("href", "/suites");
  expect(screen.getByRole("link", { name: "修正记录" })).toHaveAttribute("href", "/corrections");
  expect(screen.getByRole("link", { name: "报告中心" })).toHaveAttribute("href", "/reports");
});

test("报告中心路由激活时菜单选中正确", async () => {
  renderRouter(["/reports"]);

  expect(await screen.findByText("Reports Mock")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "报告中心" }).closest("li")).toHaveClass("ant-menu-item-selected");
});

test("suite 路由激活时菜单选中正确", async () => {
  renderRouter(["/suites"]);

  expect(await screen.findByText("Suites Mock")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Suite 管理" }).closest("li")).toHaveClass("ant-menu-item-selected");
});

test("suite run 详情路由可正常渲染", async () => {
  renderRouter(["/suites/2/runs/8"]);

  expect(await screen.findByText("Suite Run Detail Mock")).toBeInTheDocument();
});

test("corrections 路由激活时菜单选中正确", async () => {
  renderRouter(["/corrections"]);

  expect(await screen.findByText("Corrections Mock")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "修正记录" }).closest("li")).toHaveClass("ant-menu-item-selected");
});
