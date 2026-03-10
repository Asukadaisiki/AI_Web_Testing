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
    getExecutions: vi.fn(),
  };
});

test("按状态和用例筛选执行列表，并为失败项生成失败步骤跳转链接", async () => {
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
  vi.mocked(api.getExecutions).mockImplementation(async (params) => {
    if (params.status === "failed" && params.case_id === 2) {
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
  expect(api.getExecutions).toHaveBeenCalledWith({
    project_id: 1,
    case_id: undefined,
    status: undefined,
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
      limit: 10,
      offset: 0,
    });
  });

  const failureLink = await screen.findByRole("link", { name: "异常场景" });
  expect(failureLink).toHaveAttribute("href", "/executions/4#step-2");
  expect(screen.getByText("失败步骤：Step 2")).toBeInTheDocument();
  expect(screen.getByRole("img", { name: "execution-4-latest" })).toHaveAttribute(
    "src",
    "/artifacts/executions/4/step-02.png",
  );
});

test("执行中心支持翻页并按 offset 继续查询", async () => {
  vi.mocked(api.getCases).mockResolvedValue([]);
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
      limit: 10,
      offset: 10,
    });
  });

  expect(await screen.findByText("执行 11")).toBeInTheDocument();
});
