import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { truncateText } from "../components/executionPresentation";
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

test("报告中心支持窗口切换，并展示环比、根因榜和回流执行中心链接", async () => {
  const longRootCauseTitle =
    "点击保存按钮后接口持续返回 500，导致保存流程在 click 步骤失败并需要在执行中心按根因回流排查。";

  vi.mocked(api.getExecutionOverview).mockImplementation(async (params) => ({
    total_count: params.window_days === 14 ? 9 : 4,
    passed_count: params.window_days === 14 ? 6 : 2,
    failed_count: params.window_days === 14 ? 3 : 2,
    running_count: 0,
    pass_rate: params.window_days === 14 ? 0.6667 : 0.5,
    avg_duration_ms: params.window_days === 14 ? 1500 : 1200,
    current_window_range: {
      start_date: "2026-03-04",
      end_date: "2026-03-10",
    },
    previous_window_range: {
      start_date: "2026-02-26",
      end_date: "2026-03-03",
    },
    previous_window_stats: {
      total_count: 1,
      passed_count: 0,
      failed_count: 1,
      running_count: 0,
      pass_rate: 0,
      avg_duration_ms: 900,
    },
    window_comparison: {
      total_count_delta: params.window_days === 14 ? 8 : 3,
      passed_count_delta: params.window_days === 14 ? 6 : 2,
      failed_count_delta: params.window_days === 14 ? 2 : 1,
      running_count_delta: 0,
      pass_rate_delta: params.window_days === 14 ? 0.6667 : 0.5,
      avg_duration_ms_delta: params.window_days === 14 ? 600 : 300,
    },
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
    trend_points: [
      {
        date: "2026-03-09",
        total_count: 2,
        passed_count: 1,
        failed_count: 1,
        pass_rate: 0.5,
        avg_duration_ms: 1000,
      },
      {
        date: "2026-03-10",
        total_count: 2,
        passed_count: 1,
        failed_count: 1,
        pass_rate: 0.5,
        avg_duration_ms: 1400,
      },
    ],
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
    failure_root_causes: [
      {
        fingerprint: "fingerprint-1234",
        title: longRootCauseTitle,
        count: 2,
        affected_case_count: 1,
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
  expect(await screen.findByTestId("report-trend-chart")).toBeInTheDocument();
  expect(screen.getByTestId("report-category-chart")).toBeInTheDocument();
  expect(screen.getByTestId("report-action-chart")).toBeInTheDocument();
  expect(screen.getByText("当前窗口：03-04 ~ 03-10")).toBeInTheDocument();
  expect(screen.getByText("上一窗口：02-26 ~ 03-03")).toBeInTheDocument();
  expect(screen.getByText("较上一窗口 +3")).toBeInTheDocument();
  expect(screen.getByText("较上一窗口 +50.0 pp")).toBeInTheDocument();
  expect(screen.getByText("较上一窗口 +300 ms")).toBeInTheDocument();
  expect(screen.getByText(truncateText(longRootCauseTitle, 48))).toBeInTheDocument();

  const rootCauseSearch = new URLSearchParams({
    status: "failed",
    failure_fingerprint: "fingerprint-1234",
    root_cause_title: longRootCauseTitle,
  }).toString();
  expect(screen.getByRole("link", { name: "筛选执行" })).toHaveAttribute(
    "href",
    `/executions?${rootCauseSearch}`,
  );
  expect(screen.getAllByRole("link", { name: "异常场景" })[0]).toHaveAttribute("href", "/executions/6");
  expect(screen.getAllByRole("link", { name: "#6" })[0]).toHaveAttribute("href", "/executions/6");
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

test("报告中心在空状态下稳定展示根因榜和失败列表占位", async () => {
  vi.mocked(api.getExecutionOverview).mockResolvedValue({
    total_count: 0,
    passed_count: 0,
    failed_count: 0,
    running_count: 0,
    pass_rate: 0,
    avg_duration_ms: 0,
    current_window_range: {
      start_date: "2026-03-04",
      end_date: "2026-03-10",
    },
    previous_window_range: {
      start_date: "2026-02-26",
      end_date: "2026-03-03",
    },
    previous_window_stats: {
      total_count: 0,
      passed_count: 0,
      failed_count: 0,
      running_count: 0,
      pass_rate: 0,
      avg_duration_ms: 0,
    },
    window_comparison: {
      total_count_delta: 0,
      passed_count_delta: 0,
      failed_count_delta: 0,
      running_count_delta: 0,
      pass_rate_delta: 0,
      avg_duration_ms_delta: 0,
    },
    latest_failed_runs: [],
    failure_categories: [
      { category: "configuration", count: 0 },
      { category: "locator", count: 0 },
      { category: "assertion", count: 0 },
      { category: "navigation", count: 0 },
      { category: "network", count: 0 },
      { category: "runner", count: 0 },
    ],
    trend_points: [],
    failure_step_actions: [],
    top_failed_cases: [],
    failure_root_causes: [],
  });

  renderWithProviders(<ReportCenterPage />, {
    route: "/reports",
    path: "/reports",
  });

  expect(await screen.findByText("当前窗口内暂无可聚合的失败根因。")).toBeInTheDocument();
  expect(screen.getByText("当前窗口内暂无高频失败用例。")).toBeInTheDocument();
  expect(screen.getByText("当前窗口内暂无失败执行。")).toBeInTheDocument();
});

test("报告中心在接口异常时展示错误提示", async () => {
  vi.mocked(api.getExecutionOverview).mockRejectedValue(new Error("overview failed"));

  renderWithProviders(<ReportCenterPage />, {
    route: "/reports",
    path: "/reports",
  });

  expect(await screen.findByText("overview failed")).toBeInTheDocument();
});
