import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { ReportCenterPage } from "./ReportCenterPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../components/OverviewChart", () => ({
  OverviewChart: ({ testId }: { testId?: string }) => <div data-testid={testId ?? "overview-chart"} />,
}));

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    getExecutionOverview: vi.fn(),
  };
});

test("报告中心支持窗口切换并展示聚合图表、Top Failed Cases 和最近失败跳转", async () => {
  vi.mocked(api.getExecutionOverview).mockImplementation(async (params) => ({
    total_count: params.window_days === 14 ? 9 : 4,
    passed_count: params.window_days === 14 ? 6 : 2,
    failed_count: params.window_days === 14 ? 3 : 2,
    running_count: 0,
    pass_rate: params.window_days === 14 ? 0.6667 : 0.5,
    avg_duration_ms: params.window_days === 14 ? 1500 : 1200,
    latest_failed_runs: [
      {
        id: 6,
        case_id: 2,
        case_name: "异常场景",
        project_id: 1,
        triggered_by: 1,
        status: "failed",
        error_message: "断言失败",
        started_at: "2026-03-10T11:00:00",
        finished_at: "2026-03-10T11:00:01",
        duration_ms: 1000,
        total_steps: 3,
        failed_step_index: 2,
        failure_category: "assertion",
        failure_step_action: "assert_text",
        latest_url: "https://example.com/dashboard",
        latest_screenshot_url: "/artifacts/executions/6/step-03.png",
      },
    ],
    failure_categories: [
      { category: "configuration", count: 0 },
      { category: "locator", count: 1 },
      { category: "assertion", count: 1 },
      { category: "navigation", count: 0 },
      { category: "network", count: 0 },
      { category: "runner", count: 0 },
    ],
    trend_points: [],
    failure_step_actions: [
      { action: "assert_text", count: 1 },
      { action: "click", count: 1 },
    ],
    top_failed_cases: [
      {
        case_id: 2,
        case_name: "异常场景",
        failure_count: params.window_days === 14 ? 3 : 2,
        latest_execution_id: 6,
        latest_failure_category: "assertion",
      },
    ],
  }));

  renderWithProviders(<ReportCenterPage />, {
    route: "/reports",
    path: "/reports",
  });

  expect(await screen.findByText("报告中心")).toBeInTheDocument();
  expect(await screen.findByTestId("report-category-chart")).toBeInTheDocument();
  expect(screen.getByTestId("report-action-chart")).toBeInTheDocument();
  expect(screen.getAllByRole("link", { name: "异常场景" })[0]).toHaveAttribute("href", "/executions/6");
  expect(screen.getByRole("link", { name: "#6" })).toHaveAttribute("href", "/executions/6");
  expect(screen.getByText("断言失败 · https://example.com/dashboard")).toBeInTheDocument();
  expect(api.getExecutionOverview).toHaveBeenCalledWith({
    project_id: 1,
    window_days: 7,
  });

  await userEvent.click(screen.getByRole("button", { name: "14 天" }));

  await waitFor(() => {
    expect(api.getExecutionOverview).toHaveBeenLastCalledWith({
      project_id: 1,
      window_days: 14,
    });
  });
  expect(await screen.findByText("66.7%")).toBeInTheDocument();
});
