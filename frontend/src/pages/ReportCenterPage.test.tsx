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
    getCases: vi.fn(),
    getExecutionOverview: vi.fn(),
    getProjects: vi.fn(),
    getReportPreference: vi.fn(),
    updateReportPreference: vi.fn(),
  };
});

function buildOverview(windowDays: 7 | 14 | 30 = 7) {
  return {
    scope_type: "project" as const,
    scope_project_id: 1,
    scope_case_id: null,
    total_count: windowDays === 14 ? 8 : 4,
    passed_count: windowDays === 14 ? 4 : 2,
    failed_count: windowDays === 14 ? 4 : 2,
    running_count: 0,
    auto_completed_count: windowDays === 14 ? 6 : 3,
    intervention_count: windowDays === 14 ? 2 : 1,
    pass_rate: windowDays === 14 ? 0.6667 : 0.5,
    automation_rate: 0.75,
    intervention_rate: 0.25,
    avg_duration_ms: windowDays === 14 ? 1500 : 1200,
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
      total_count_delta: windowDays === 14 ? 7 : 3,
      passed_count_delta: windowDays === 14 ? 4 : 2,
      failed_count_delta: windowDays === 14 ? 3 : 1,
      running_count_delta: 0,
      pass_rate_delta: windowDays === 14 ? 0.6667 : 0.5,
      avg_duration_ms_delta: windowDays === 14 ? 600 : 300,
    },
    latest_failed_runs: [
      {
        id: 6,
        case_id: 2,
        case_name: "异常场景",
        project_id: 1,
        triggered_by: 1,
        status: "failed" as const,
        error_message: "断言失败",
        started_at: "2026-03-10T11:00:00",
        finished_at: "2026-03-10T11:00:01",
        duration_ms: 1000,
        total_steps: 3,
        failed_step_index: 2,
        failure_category: "assertion" as const,
        failure_step_action: "assert_text",
        latest_url: "https://example.com/dashboard",
        latest_screenshot_url: "/artifacts/executions/6/step-03.png",
      },
    ],
    latest_intervention_runs: [
      {
        id: 7,
        case_id: 3,
        case_name: "人工处理场景",
        project_id: 1,
        triggered_by: 1,
        status: "needs_intervention" as const,
        error_message: "等待人工介入",
        started_at: "2026-03-10T12:00:00",
        finished_at: "2026-03-10T12:00:01",
        duration_ms: 1200,
        total_steps: 2,
        failed_step_index: 1,
        failure_category: "locator" as const,
        failure_step_action: "click",
        latest_url: "https://example.com/login",
        latest_screenshot_url: "/artifacts/executions/7/step-02.png",
      },
    ],
    failure_categories: [
      { category: "configuration" as const, count: 0 },
      { category: "locator" as const, count: 1 },
      { category: "assertion" as const, count: 1 },
      { category: "navigation" as const, count: 0 },
      { category: "network" as const, count: 0 },
      { category: "runner" as const, count: 0 },
    ],
    trend_points: [
      {
        date: "2026-03-09",
        total_count: 2,
        passed_count: 1,
        failed_count: 1,
        auto_completed_count: 2,
        intervention_count: 0,
        pass_rate: 0.5,
        avg_duration_ms: 1000,
      },
      {
        date: "2026-03-10",
        total_count: 2,
        passed_count: 1,
        failed_count: 1,
        auto_completed_count: 1,
        intervention_count: 1,
        pass_rate: 0.5,
        avg_duration_ms: 1400,
      },
    ],
    failure_step_actions: [
      { action: "assert_text", count: 1 },
      { action: "click", count: 1 },
    ],
    top_failed_cases: [],
    failure_root_causes: [
      {
        fingerprint: "fingerprint-1234",
        title: "点击保存按钮后接口持续返回 500，导致保存流程在 click 步骤失败并需要在执行中心按根因回流排查。",
        count: 2,
        affected_case_count: 1,
        latest_execution_id: 6,
        latest_failure_category: "assertion" as const,
      },
    ],
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.getProjects).mockResolvedValue([{ id: 1, name: "Default Project", description: null }]);
  vi.mocked(api.getCases).mockResolvedValue([]);
  vi.mocked(api.getReportPreference).mockResolvedValue({
    scope_type: "project",
    project_id: 1,
    case_id: null,
    window_days: 7,
  });
  vi.mocked(api.updateReportPreference).mockImplementation(async (payload) => payload);
});

test("报告中心支持窗口切换，并展示环比、高频错误点和执行中心回流链接", async () => {
  const longRootCauseTitle =
    "点击保存按钮后接口持续返回 500，导致保存流程在 click 步骤失败并需要在执行中心按根因回流排查。";

  vi.mocked(api.getExecutionOverview).mockImplementation(async (params) => ({
    ...buildOverview((params.window_days ?? 7) as 7 | 14 | 30),
    scope_type: "project",
    scope_project_id: 1,
    failure_root_causes: [
      {
        ...buildOverview().failure_root_causes[0],
        title: longRootCauseTitle,
      },
    ],
  }));

  renderWithProviders(<ReportCenterPage />, {
    route: "/reports?scope_type=project&project_id=1&window_days=7",
    path: "/reports",
  });

  expect(await screen.findByText("报告中心")).toBeInTheDocument();
  expect(await screen.findByTestId("report-trend-chart")).toBeInTheDocument();
  expect(screen.getByTestId("report-automation-chart")).toBeInTheDocument();
  expect(screen.getByText("当前窗口：03-04 ~ 03-10")).toBeInTheDocument();
  expect(screen.getByText("上一窗口：02-26 ~ 03-03")).toBeInTheDocument();
  expect(screen.getByText("较上一窗口 +3")).toBeInTheDocument();
  expect(screen.getByText("较上一窗口 +50.0 pp")).toBeInTheDocument();
  expect(screen.getByText("较上一窗口 +300 ms")).toBeInTheDocument();
  expect(screen.getByText(truncateText(longRootCauseTitle, 48))).toBeInTheDocument();

  const rootCauseSearch = new URLSearchParams({
    scope_type: "project",
    project_id: "1",
    window_days: "7",
    status: "failed",
    failure_fingerprint: "fingerprint-1234",
    root_cause_title: longRootCauseTitle,
  }).toString();
  expect(screen.getByRole("link", { name: "筛选执行" })).toHaveAttribute("href", `/executions?${rootCauseSearch}`);
  expect(screen.getByRole("link", { name: "人工处理场景" })).toHaveAttribute(
    "href",
    "/executions?scope_type=project&project_id=1&window_days=7&status=needs_intervention",
  );
  expect(api.getExecutionOverview).toHaveBeenCalledWith({
    scope_type: "project",
    project_id: 1,
    case_id: undefined,
    window_days: 7,
  });

  await userEvent.click(screen.getByRole("button", { name: "14 天" }));

  await waitFor(() => {
    expect(api.getExecutionOverview).toHaveBeenLastCalledWith({
      scope_type: "project",
      project_id: 1,
      case_id: undefined,
      window_days: 14,
    });
  });
  await waitFor(() => {
    expect(api.updateReportPreference).toHaveBeenCalledWith({
      scope_type: "project",
      project_id: 1,
      case_id: null,
      window_days: 14,
    });
  });
  expect(await screen.findByText("66.7%")).toBeInTheDocument();
});

test("报告中心在无 URL 范围时读取账号偏好并展示新增指标与人工介入列表", async () => {
  vi.mocked(api.getProjects).mockResolvedValue([
    { id: 1, name: "Default Project", description: null },
    { id: 2, name: "Checkout Project", description: "checkout" },
  ]);
  vi.mocked(api.getCases).mockResolvedValue([
    {
      id: 2,
      project_id: 2,
      name: "异常场景",
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
  ]);
  vi.mocked(api.getReportPreference).mockResolvedValue({
    scope_type: "project",
    project_id: 2,
    case_id: null,
    window_days: 14,
  });
  vi.mocked(api.getExecutionOverview).mockResolvedValue({
    ...buildOverview(14),
    scope_type: "project",
    scope_project_id: 2,
  });

  renderWithProviders(<ReportCenterPage />, {
    route: "/reports",
    path: "/reports",
  });

  expect(await screen.findByText("75.0%")).toBeInTheDocument();
  expect(screen.getAllByText("25.0%")).toHaveLength(2);
  expect(screen.getByText("人工处理场景")).toBeInTheDocument();
  await waitFor(() => {
    expect(api.getExecutionOverview).toHaveBeenCalledWith({
      scope_type: "project",
      project_id: 2,
      case_id: undefined,
      window_days: 14,
    });
  });
  expect(api.updateReportPreference).not.toHaveBeenCalled();

  await userEvent.click(screen.getByRole("button", { name: "全局" }));

  await waitFor(() => {
    expect(api.updateReportPreference).toHaveBeenCalledWith({
      scope_type: "global",
      project_id: null,
      case_id: null,
      window_days: 14,
    });
  });
});

test("报告中心在空状态下稳定展示根因榜和人工介入占位", async () => {
  vi.mocked(api.getExecutionOverview).mockResolvedValue({
    ...buildOverview(),
    total_count: 0,
    passed_count: 0,
    failed_count: 0,
    auto_completed_count: 0,
    intervention_count: 0,
    pass_rate: 0,
    automation_rate: 0,
    intervention_rate: 0,
    avg_duration_ms: 0,
    latest_failed_runs: [],
    latest_intervention_runs: [],
    failure_categories: [
      { category: "configuration", count: 0 },
      { category: "locator", count: 0 },
      { category: "assertion", count: 0 },
      { category: "navigation", count: 0 },
      { category: "network", count: 0 },
      { category: "runner", count: 0 },
    ],
    trend_points: [],
    failure_root_causes: [],
  });

  renderWithProviders(<ReportCenterPage />, {
    route: "/reports?scope_type=project&project_id=1&window_days=7",
    path: "/reports",
  });

  expect(await screen.findByText("当前窗口内暂无可聚合的高频错误点。")).toBeInTheDocument();
  expect(screen.getByText("当前窗口内暂无人工介入执行。")).toBeInTheDocument();
});

test("报告中心在接口异常时展示错误提示", async () => {
  vi.mocked(api.getExecutionOverview).mockRejectedValue(new Error("overview failed"));

  renderWithProviders(<ReportCenterPage />, {
    route: "/reports?scope_type=project&project_id=1&window_days=7",
    path: "/reports",
  });

  expect(await screen.findByText("overview failed")).toBeInTheDocument();
});
