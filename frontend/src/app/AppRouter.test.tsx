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

vi.mock("../pages/ExecutionsPage", () => ({
  ExecutionsPage: () => <div>Executions Mock</div>,
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

test("根路由默认跳转到 dashboard，并展示 v2.0 导航入口", async () => {
  renderRouter(["/"]);

  expect(await screen.findByText("Dashboard Mock")).toBeInTheDocument();
  expect(screen.getByText("单 Case 平台体验打磨 v2.0")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "仪表盘" })).toHaveAttribute("href", "/dashboard");
  expect(screen.getByRole("link", { name: "报告中心" })).toHaveAttribute("href", "/reports");
});

test("报告中心路由激活时菜单选中正确", async () => {
  renderRouter(["/reports"]);

  expect(await screen.findByText("Reports Mock")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "报告中心" }).closest("li")).toHaveClass("ant-menu-item-selected");
});
