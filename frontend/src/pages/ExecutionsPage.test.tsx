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
      steps: [{ action: "click", target: "登录按钮" }],
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-09T10:00:00",
      updated_at: "2026-03-09T10:00:00",
    },
  ]);
  vi.mocked(api.getExecutionOverview).mockResolvedValue({
    total_count: 2,
    passed_count: 1,
    failed_count: 1,
    running_count: 0,
    pass_rate: 0.5,
    avg_duration_ms: 1500,
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
  });
  expect(api.getExecutions).toHaveBeenCalledWith({
    project_id: 1,
    case_id: undefined,
    status: undefined,
    failure_category: undefined,
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
      failure_category: undefined,
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
      failure_category: undefined,
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
      failure_category: "navigation",
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
});

test("执行中心支持翻页并按 offset 继续查询", async () => {
  vi.mocked(api.getCases).mockResolvedValue([]);
  vi.mocked(api.getExecutionOverview).mockResolvedValue({
    total_count: 11,
    passed_count: 11,
    failed_count: 0,
    running_count: 0,
    pass_rate: 1,
    avg_duration_ms: 100,
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
      failure_category: undefined,
      limit: 10,
      offset: 10,
    });
  });

  expect(await screen.findByText("执行 11")).toBeInTheDocument();
});

test("执行中心在空状态下稳定展示 overview 与空列表", async () => {
  vi.mocked(api.getCases).mockResolvedValue([]);
  vi.mocked(api.getExecutionOverview).mockResolvedValue({
    total_count: 0,
    passed_count: 0,
    failed_count: 0,
    running_count: 0,
    pass_rate: 0,
    avg_duration_ms: 0,
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
