import { screen } from "@testing-library/react";
import { vi } from "vitest";

import { DashboardPage } from "./DashboardPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../components/OverviewChart", () => ({
  OverviewChart: ({ testId }: { testId?: string }) => <div data-testid={testId ?? "overview-chart"} />,
}));

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    getCases: vi.fn(),
    getExecutionOverview: vi.fn(),
  };
});

test("仪表盘展示 KPI、趋势图、最近失败和失败最多用例入口", async () => {
  vi.mocked(api.getCases).mockResolvedValue([
    {
      id: 1,
      project_id: 1,
      name: "登录冒烟",
      description: null,
      base_url: "https://example.com",
      input_contract: [],
      output_contract: [],
      steps: [{ action: "goto", value: "/" }],
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-10T10:00:00",
      updated_at: "2026-03-10T10:00:00",
    },
    {
      id: 2,
      project_id: 1,
      name: "异常场景",
      description: null,
      base_url: "https://example.com",
      input_contract: [],
      output_contract: [],
      steps: [{ action: "click", target: "登录按钮" }],
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-10T10:00:00",
      updated_at: "2026-03-10T10:00:00",
    },
  ]);
  vi.mocked(api.getExecutionOverview).mockResolvedValue({
    scope_type: "project",
    scope_project_id: 1,
    scope_case_id: null,
    total_count: 6,
    passed_count: 4,
    failed_count: 2,
    running_count: 0,
    auto_completed_count: 6,
    intervention_count: 0,
    pass_rate: 0.6667,
    automation_rate: 1,
    intervention_rate: 0,
    avg_duration_ms: 1200,
    current_window_range: {
      start_date: "2026-03-04",
      end_date: "2026-03-10",
    },
    previous_window_range: {
      start_date: "2026-02-26",
      end_date: "2026-03-03",
    },
    previous_window_stats: {
      total_count: 3,
      passed_count: 2,
      failed_count: 1,
      running_count: 0,
      pass_rate: 0.6667,
      avg_duration_ms: 1100,
    },
    window_comparison: {
      total_count_delta: 3,
      passed_count_delta: 2,
      failed_count_delta: 1,
      running_count_delta: 0,
      pass_rate_delta: 0,
      avg_duration_ms_delta: 100,
    },
    latest_failed_runs: [
      {
        id: 8,
        case_id: 2,
        case_name: "异常场景",
        project_id: 1,
        triggered_by: 1,
        status: "failed",
        error_message: "按钮未找到",
        started_at: "2026-03-10T11:00:00",
        finished_at: "2026-03-10T11:00:02",
        duration_ms: 2000,
        total_steps: 2,
        failed_step_index: 1,
        failure_category: "locator",
        failure_step_action: "click",
        latest_url: "https://example.com/login",
        latest_screenshot_url: "/artifacts/executions/8/step-02.png",
      },
    ],
    latest_intervention_runs: [],
    failure_categories: [
      { category: "configuration", count: 0 },
      { category: "locator", count: 2 },
      { category: "assertion", count: 0 },
      { category: "navigation", count: 0 },
      { category: "network", count: 0 },
      { category: "runner", count: 0 },
    ],
    trend_points: [
      {
        date: "2026-03-04",
        total_count: 1,
        passed_count: 1,
        failed_count: 0,
        auto_completed_count: 1,
        intervention_count: 0,
        pass_rate: 1,
        avg_duration_ms: 900,
      },
      {
        date: "2026-03-10",
        total_count: 2,
        passed_count: 1,
        failed_count: 1,
        auto_completed_count: 2,
        intervention_count: 0,
        pass_rate: 0.5,
        avg_duration_ms: 1400,
      },
    ],
    failure_step_actions: [{ action: "click", count: 2 }],
    top_failed_cases: [
      {
        case_id: 2,
        case_name: "异常场景",
        failure_count: 2,
        latest_execution_id: 8,
        latest_failure_category: "locator",
      },
    ],
    failure_root_causes: [],
  });

  renderWithProviders(<DashboardPage />, {
    route: "/dashboard",
    path: "/dashboard",
  });

  expect(await screen.findByText("仪表盘")).toBeInTheDocument();
  expect(await screen.findByText("总用例数")).toBeInTheDocument();
  expect(screen.getByText("近 7 天总执行数")).toBeInTheDocument();
  expect(screen.getByText("66.7%")).toBeInTheDocument();
  expect(screen.getByText("1200 ms")).toBeInTheDocument();
  expect(screen.getByTestId("dashboard-trend-chart")).toBeInTheDocument();
  expect(screen.getAllByRole("link", { name: "异常场景" })[0]).toHaveAttribute(
    "href",
    "/executions?window_days=7&status=failed&case_id=2&failure_category=locator",
  );
  expect(screen.getByText("2 次失败")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "报告中心" })).toHaveAttribute("href", "/reports?window_days=7");
  expect(api.getExecutionOverview).toHaveBeenCalledWith({
    project_id: 1,
    window_days: 7,
  });
});
