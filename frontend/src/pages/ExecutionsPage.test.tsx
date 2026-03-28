import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { ExecutionsPage } from "./ExecutionsPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    getCases: vi.fn(),
    getExecutionOverview: vi.fn(),
    getExecutions: vi.fn(),
  };
});

test("执行中心展示 overview、支持失败分类筛选，并为最近失败生成跳转链接", async () => {
  vi.mocked(api.getCases).mockResolvedValue([
    {
      id: 1,
      project_id: 1,
      name: "登录冒烟",
      description: null,
      input_contract: [],
      output_contract: [],
      steps: [{ action: "goto", value: "/login" }],
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-09T10:00:00",
      updated_at: "2026-03-09T10:00:00",
    },
    {
      id: 2,
      project_id: 1,
      name: "异常场景",
      description: null,
      input_contract: [],
      output_contract: [],
      steps: [{ action: "click", target: "登录按钮" }],
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-09T10:00:00",
      updated_at: "2026-03-09T10:00:00",
    },
  ]);
  vi.mocked(api.getExecutionOverview).mockResolvedValue({
    scope_type: "project",
    scope_project_id: 1,
    scope_case_id: null,
    total_count: 2,
    passed_count: 1,
    failed_count: 1,
    running_count: 0,
    auto_completed_count: 2,
    intervention_count: 0,
    pass_rate: 0.5,
    automation_rate: 1,
    intervention_rate: 0,
    avg_duration_ms: 1500,
    current_window_range: {
      start_date: "2026-03-03",
      end_date: "2026-03-09",
    },
    previous_window_range: {
      start_date: "2026-02-25",
      end_date: "2026-03-02",
    },
    previous_window_stats: {
      total_count: 1,
      passed_count: 1,
      failed_count: 0,
      running_count: 0,
      pass_rate: 1,
      avg_duration_ms: 1200,
    },
    window_comparison: {
      total_count_delta: 1,
      passed_count_delta: 0,
      failed_count_delta: 1,
      running_count_delta: 0,
      pass_rate_delta: -0.5,
      avg_duration_ms_delta: 300,
    },
    latest_failed_runs: [
      {
        id: 4,
        case_id: 2,
        case_name: "异常场景",
        project_id: 1,
        triggered_by: 1,
        status: "failed",
        error_message: "按钮未找到",
        started_at: "2026-03-09T11:00:00",
        finished_at: "2026-03-09T11:00:02",
        duration_ms: 2000,
        total_steps: 3,
        failed_step_index: 1,
        failure_category: "navigation",
        failure_step_action: "goto",
        latest_url: "https://example.com/error",
        latest_screenshot_url: "/artifacts/executions/4/step-02.png",
      },
    ],
    latest_intervention_runs: [],
    failure_categories: [
      { category: "configuration", count: 0 },
      { category: "locator", count: 0 },
      { category: "assertion", count: 0 },
      { category: "navigation", count: 1 },
      { category: "network", count: 0 },
      { category: "runner", count: 0 },
    ],
    trend_points: [],
    failure_step_actions: [],
    top_failed_cases: [],
    failure_root_causes: [],
  });
  vi.mocked(api.getExecutions).mockImplementation(async (params) => {
    if (params.status === "failed" && params.case_id === 2 && params.failure_category === "navigation") {
      return [
        {
          id: 4,
          case_id: 2,
          case_name: "异常场景",
          project_id: 1,
          triggered_by: 1,
          status: "failed",
          error_message: "按钮未找到",
          started_at: "2026-03-09T11:00:00",
          finished_at: "2026-03-09T11:00:02",
          duration_ms: 2000,
          total_steps: 3,
          failed_step_index: 1,
          failure_category: "navigation",
          failure_step_action: "goto",
          latest_url: "https://example.com/error",
          latest_screenshot_url: "/artifacts/executions/4/step-02.png",
        },
      ];
    }
    if (params.status === "failed") {
      return [
        {
          id: 4,
          case_id: 2,
          case_name: "异常场景",
          project_id: 1,
          triggered_by: 1,
          status: "failed",
          error_message: "按钮未找到",
          started_at: "2026-03-09T11:00:00",
          finished_at: "2026-03-09T11:00:02",
          duration_ms: 2000,
          total_steps: 3,
          failed_step_index: 1,
          failure_category: "navigation",
          failure_step_action: "goto",
          latest_url: "https://example.com/error",
          latest_screenshot_url: "/artifacts/executions/4/step-02.png",
        },
      ];
    }
    return [
      {
        id: 3,
        case_id: 1,
        case_name: "登录冒烟",
        project_id: 1,
        triggered_by: 1,
        status: "passed",
        error_message: null,
        started_at: "2026-03-09T10:00:00",
        finished_at: "2026-03-09T10:00:01",
        duration_ms: 1000,
        total_steps: 1,
        failed_step_index: null,
        failure_category: null,
        failure_step_action: null,
        latest_url: "https://example.com/login",
        latest_screenshot_url: "/artifacts/executions/3/step-01.png",
      },
    ];
  });

  renderWithProviders(<ExecutionsPage />, {
    route: "/executions",
    path: "/executions",
  });

  expect(await screen.findByText("登录冒烟")).toBeInTheDocument();
  expect(screen.getByText("通过")).toBeInTheDocument();
  expect(screen.getByText("总执行数")).toBeInTheDocument();
  expect(screen.getByText("50.0%")).toBeInTheDocument();
  expect(screen.getByText("1500 ms")).toBeInTheDocument();
  expect(api.getExecutionOverview).toHaveBeenCalledWith({
    project_id: 1,
    case_id: undefined,
    window_days: undefined,
    failure_fingerprint: undefined,
  });
  expect(api.getExecutions).toHaveBeenCalledWith({
    project_id: 1,
    case_id: undefined,
    status: undefined,
    window_days: undefined,
    failure_category: undefined,
    failure_fingerprint: undefined,
    limit: 10,
    offset: 0,
  });

  const selectTriggers = document.querySelectorAll(".ant-select-selector");
  fireEvent.mouseDown(selectTriggers[0]);
  await userEvent.click(within(await screen.findByRole("listbox")).getByRole("option", { name: "失败" }));

  await waitFor(() => {
    expect(api.getExecutions).toHaveBeenLastCalledWith({
      project_id: 1,
      case_id: undefined,
      status: "failed",
      window_days: undefined,
      failure_category: undefined,
      failure_fingerprint: undefined,
      limit: 10,
      offset: 0,
    });
  });

  fireEvent.mouseDown(selectTriggers[1]);
  await userEvent.click(within(await screen.findByRole("listbox")).getByRole("option", { name: "异常场景" }));

  await waitFor(() => {
    expect(api.getExecutions).toHaveBeenLastCalledWith({
      project_id: 1,
      case_id: 2,
      status: "failed",
      window_days: undefined,
      failure_category: undefined,
      failure_fingerprint: undefined,
      limit: 10,
      offset: 0,
    });
  });

  await userEvent.click(screen.getByRole("button", { name: "导航 (1)" }));

  await waitFor(() => {
    expect(api.getExecutions).toHaveBeenLastCalledWith({
      project_id: 1,
      case_id: 2,
      status: "failed",
      window_days: undefined,
      failure_category: "navigation",
      failure_fingerprint: undefined,
      limit: 10,
      offset: 0,
    });
  });

  const failureLinks = await screen.findAllByRole("link", { name: "异常场景" });
  expect(failureLinks[0]).toHaveAttribute("href", "/executions/4#step-2");
  expect(screen.getByText("失败步骤：Step 2")).toBeInTheDocument();
  expect(screen.getByRole("img", { name: "execution-4-latest" })).toHaveAttribute(
    "src",
    "/artifacts/executions/4/step-02.png",
  );
  expect(screen.getByText("聚焦最近的失败执行，直接跳到失败步骤。")).toBeInTheDocument();
}, 10000);

test("执行中心支持翻页并按 offset 继续查询", async () => {
  vi.mocked(api.getCases).mockResolvedValue([]);
  vi.mocked(api.getExecutionOverview).mockResolvedValue({
    scope_type: "project",
    scope_project_id: 1,
    scope_case_id: null,
    total_count: 11,
    passed_count: 11,
    failed_count: 0,
    running_count: 0,
    auto_completed_count: 11,
    intervention_count: 0,
    pass_rate: 1,
    automation_rate: 1,
    intervention_rate: 0,
    avg_duration_ms: 100,
    current_window_range: null,
    previous_window_range: null,
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
    failure_step_actions: [],
    top_failed_cases: [],
    failure_root_causes: [],
  });
  vi.mocked(api.getExecutions).mockImplementation(async (params) => {
    if (params.offset === 10) {
      return [
        {
          id: 11,
          case_id: 1,
          case_name: "执行 11",
          project_id: 1,
          triggered_by: 1,
          status: "passed",
          error_message: null,
          started_at: "2026-03-09T10:00:00",
          finished_at: "2026-03-09T10:00:01",
          duration_ms: 100,
          total_steps: 1,
          failed_step_index: null,
          failure_category: null,
          failure_step_action: null,
          latest_url: null,
          latest_screenshot_url: null,
        },
      ];
    }
    return Array.from({ length: 10 }, (_, index) => ({
      id: index + 1,
      case_id: 1,
      case_name: `执行 ${index + 1}`,
      project_id: 1,
      triggered_by: 1,
      status: "passed" as const,
      error_message: null,
      started_at: "2026-03-09T10:00:00",
      finished_at: "2026-03-09T10:00:01",
      duration_ms: 100,
      total_steps: 1,
      failed_step_index: null,
      failure_category: null,
      failure_step_action: null,
      latest_url: null,
      latest_screenshot_url: null,
    }));
  });

  renderWithProviders(<ExecutionsPage />, {
    route: "/executions",
    path: "/executions",
  });

  expect(await screen.findByRole("link", { name: "执行 1" })).toBeInTheDocument();

  await userEvent.click(screen.getByTitle("2"));

  await waitFor(() => {
    expect(api.getExecutions).toHaveBeenLastCalledWith({
      project_id: 1,
      case_id: undefined,
      status: undefined,
      window_days: undefined,
      failure_category: undefined,
      failure_fingerprint: undefined,
      limit: 10,
      offset: 10,
    });
  });

  expect(await screen.findByText("执行 11")).toBeInTheDocument();
});

test("执行中心支持从报告中心带入 failure_fingerprint 根因筛选并清除", async () => {
  vi.mocked(api.getCases).mockResolvedValue([]);
  vi.mocked(api.getExecutionOverview).mockResolvedValue({
    scope_type: "project",
    scope_project_id: 1,
    scope_case_id: null,
    total_count: 2,
    passed_count: 0,
    failed_count: 2,
    running_count: 0,
    auto_completed_count: 2,
    intervention_count: 0,
    pass_rate: 0,
    automation_rate: 1,
    intervention_rate: 0,
    avg_duration_ms: 800,
    current_window_range: null,
    previous_window_range: null,
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
    latest_intervention_runs: [],
    failure_categories: [
      { category: "configuration", count: 0 },
      { category: "locator", count: 0 },
      { category: "assertion", count: 0 },
      { category: "navigation", count: 0 },
      { category: "network", count: 0 },
      { category: "runner", count: 2 },
    ],
    trend_points: [],
    failure_step_actions: [],
    top_failed_cases: [],
    failure_root_causes: [],
  });
  vi.mocked(api.getExecutions).mockImplementation(async (params) => {
    if (params.failure_fingerprint === "fp-1234") {
      return [
        {
          id: 9,
          case_id: 3,
          case_name: "共享根因场景",
          project_id: 1,
          triggered_by: 1,
          status: "failed",
          error_message: "shared runner boom",
          started_at: "2026-03-10T10:00:00",
          finished_at: "2026-03-10T10:00:01",
          duration_ms: 1000,
          total_steps: 2,
          failed_step_index: 0,
          failure_category: "runner",
          failure_step_action: "click",
          latest_url: null,
          latest_screenshot_url: null,
        },
      ];
    }
    return [];
  });

  renderWithProviders(<ExecutionsPage />, {
    route: "/executions?status=failed&failure_fingerprint=fp-1234&root_cause_title=shared%20runner%20boom",
    path: "/executions",
  });

  expect(await screen.findByText("根因筛选已启用")).toBeInTheDocument();
  expect(screen.getByText("shared runner boom")).toBeInTheDocument();
  expect(api.getExecutionOverview).toHaveBeenCalledWith({
    project_id: 1,
    case_id: undefined,
    window_days: undefined,
    failure_fingerprint: "fp-1234",
  });
  expect(api.getExecutions).toHaveBeenCalledWith({
    project_id: 1,
    case_id: undefined,
    status: "failed",
    window_days: undefined,
    failure_category: undefined,
    failure_fingerprint: "fp-1234",
    limit: 10,
    offset: 0,
  });

  await userEvent.click(screen.getByRole("button", { name: "清除根因筛选" }));

  await waitFor(() => {
    expect(api.getExecutions).toHaveBeenLastCalledWith({
      project_id: 1,
      case_id: undefined,
      status: "failed",
      window_days: undefined,
      failure_category: undefined,
      failure_fingerprint: undefined,
      limit: 10,
      offset: 0,
    });
  });
});

test("执行中心在空状态下稳定展示 overview 与空列表", async () => {
  vi.mocked(api.getCases).mockResolvedValue([]);
  vi.mocked(api.getExecutionOverview).mockResolvedValue({
    scope_type: "project",
    scope_project_id: 1,
    scope_case_id: null,
    total_count: 0,
    passed_count: 0,
    failed_count: 0,
    running_count: 0,
    auto_completed_count: 0,
    intervention_count: 0,
    pass_rate: 0,
    automation_rate: 0,
    intervention_rate: 0,
    avg_duration_ms: 0,
    current_window_range: null,
    previous_window_range: null,
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
    failure_step_actions: [],
    top_failed_cases: [],
    failure_root_causes: [],
  });
  vi.mocked(api.getExecutions).mockResolvedValue([]);

  renderWithProviders(<ExecutionsPage />, {
    route: "/executions",
    path: "/executions",
  });

  expect(await screen.findByText("暂无失败执行记录。")).toBeInTheDocument();
  expect(await screen.findByText("当前筛选条件下没有执行记录。")).toBeInTheDocument();
  expect(await screen.findByText("0.0%")).toBeInTheDocument();
});

test("执行中心从 URL 初始化筛选，并在修改后回写 query 参数", async () => {
  vi.mocked(api.getCases).mockResolvedValue([
    {
      id: 2,
      project_id: 1,
      name: "异常场景",
      description: null,
      input_contract: [],
      output_contract: [],
      steps: [{ action: "click", target: "登录按钮" }],
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-09T10:00:00",
      updated_at: "2026-03-09T10:00:00",
    },
  ]);
  vi.mocked(api.getExecutionOverview).mockResolvedValue({
    scope_type: "project",
    scope_project_id: 1,
    scope_case_id: 2,
    total_count: 1,
    passed_count: 0,
    failed_count: 1,
    running_count: 0,
    auto_completed_count: 1,
    intervention_count: 0,
    pass_rate: 0,
    automation_rate: 1,
    intervention_rate: 0,
    avg_duration_ms: 900,
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
      total_count_delta: 1,
      passed_count_delta: 0,
      failed_count_delta: 1,
      running_count_delta: 0,
      pass_rate_delta: 0,
      avg_duration_ms_delta: 900,
    },
    latest_failed_runs: [],
    latest_intervention_runs: [],
    failure_categories: [
      { category: "configuration", count: 0 },
      { category: "locator", count: 1 },
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
  vi.mocked(api.getExecutions).mockResolvedValue([
    {
      id: 4,
      case_id: 2,
      case_name: "异常场景",
      project_id: 1,
      triggered_by: 1,
      status: "failed",
      error_message: "按钮未找到",
      started_at: "2026-03-09T11:00:00",
      finished_at: "2026-03-09T11:00:02",
      duration_ms: 2000,
      total_steps: 3,
      failed_step_index: 1,
      failure_category: "locator",
      failure_step_action: "click",
      latest_url: "https://example.com/error",
      latest_screenshot_url: "/artifacts/executions/4/step-02.png",
    },
  ]);

  renderWithProviders(<ExecutionsPage />, {
    route: "/executions?window_days=14&status=failed&case_id=2&failure_category=locator&page=2",
    path: "/executions",
  });

  expect(await screen.findByRole("link", { name: "异常场景" })).toBeInTheDocument();
  expect(api.getExecutionOverview).toHaveBeenCalledWith({
    project_id: 1,
    case_id: 2,
    window_days: 14,
    failure_fingerprint: undefined,
  });
  expect(api.getExecutions).toHaveBeenCalledWith({
    project_id: 1,
    case_id: 2,
    status: "failed",
    window_days: 14,
    failure_category: "locator",
    failure_fingerprint: undefined,
    limit: 10,
    offset: 10,
  });

  await userEvent.click(screen.getByRole("button", { name: "30 天" }));

  await waitFor(() => {
    expect(api.getExecutions).toHaveBeenLastCalledWith({
      project_id: 1,
      case_id: 2,
      status: "failed",
      window_days: 30,
      failure_category: "locator",
      failure_fingerprint: undefined,
      limit: 10,
      offset: 0,
    });
  });
});
