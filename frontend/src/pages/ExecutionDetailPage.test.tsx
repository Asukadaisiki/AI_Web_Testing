import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { ExecutionDetailPage } from "./ExecutionDetailPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    getExecutionDetail: vi.fn(),
  };
});

test("失败步骤默认展开，支持查看候选评分并按需展开 console 与 network 事件", async () => {
  vi.mocked(api.getExecutionDetail).mockResolvedValue({
    id: 12,
    case_id: 1,
    case_name: "失败用例",
    project_id: 1,
    triggered_by: 1,
    status: "failed",
    error_message: "按钮未找到",
    started_at: "2026-03-09T12:00:00",
    finished_at: "2026-03-09T12:00:03",
    duration_ms: 3000,
    total_steps: 2,
    failed_step_index: 0,
    latest_screenshot_url: "/artifacts/executions/12/step-01.png",
    report: {
      status: "failed",
      steps: [
        {
          step_index: 0,
          action: "click",
          target: "登录按钮",
          status: "failed",
          duration_ms: 31,
          page_title: "登录页",
          viewport: { width: 1280, height: 720 },
          dom_summary: {
            text_preview: "请登录系统",
            button_count: 1,
            input_count: 2,
            link_count: 1,
          },
          locator_trace: {
            target: "登录按钮",
            match_strategy: null,
            selection_reason: null,
            candidates: [
              {
                strategy: "button_role",
                preview_text: "登录",
                role: "button",
                attributes: { aria_label: "登录按钮", placeholder: null, data_testid: null },
                score: 108,
                matched_rules: ["exact-button-role-match", "visible", "enabled", "has-preview-text"],
                rejected_reasons: [],
                visible: true,
                enabled: true,
              },
              {
                strategy: "text",
                preview_text: "登录",
                role: "span",
                attributes: { aria_label: null, placeholder: null, data_testid: null },
                score: 83,
                matched_rules: ["exact-text-match", "visible", "enabled", "has-preview-text"],
                rejected_reasons: ["element-not-enabled"],
                visible: true,
                enabled: false,
              },
            ],
            selected_candidate: null,
            failure_reason: "No locator candidates matched target.",
          },
          console_events: [
            {
              level: "warning",
              text: "Console warning 1",
              source_url: "http://example.com/app.js",
              line_number: 9,
            },
            {
              level: "warning",
              text: "Console warning 2",
              source_url: "http://example.com/app.js",
              line_number: 10,
            },
            {
              level: "error",
              text: "Console warning 3",
              source_url: "http://example.com/app.js",
              line_number: 11,
            },
          ],
          network_events: [
            {
              url: "http://example.com/api/login-1",
              method: "POST",
              status: 500,
              resource_type: "xhr",
              failure_text: null,
            },
            {
              url: "http://example.com/api/login-2",
              method: "POST",
              status: 500,
              resource_type: "xhr",
              failure_text: null,
            },
            {
              url: "http://example.com/api/login-3",
              method: "POST",
              status: 500,
              resource_type: "xhr",
              failure_text: null,
            },
          ],
          url: "http://example.com/login",
          screenshot_url: "/artifacts/executions/12/step-01.png",
          error_message: "按钮未找到",
        },
        {
          step_index: 1,
          action: "assert_url_contains",
          value: "/dashboard",
          status: "passed",
          duration_ms: 10,
          console_events: [],
          network_events: [],
          screenshot_url: "/artifacts/executions/12/step-02.png",
        },
      ],
    },
  });

  renderWithProviders(<ExecutionDetailPage />, {
    route: "/executions/12",
    path: "/executions/:executionId",
  });

  expect(await screen.findByRole("heading", { name: "失败用例" })).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "返回执行中心" })).toHaveAttribute("href", "/executions");
  expect(screen.getByRole("link", { name: "返回用例" })).toHaveAttribute("href", "/cases/1/edit");
  expect(screen.getByText("No locator candidates matched target.")).toBeInTheDocument();
  expect(screen.getByText(/score=108/)).toBeInTheDocument();
  expect(screen.getByText(/rejected=element-not-enabled/)).toBeInTheDocument();
  expect(screen.getByRole("img", { name: "step-1" })).toHaveAttribute(
    "src",
    "/artifacts/executions/12/step-01.png",
  );

  const runtimeCard = screen.getByText("运行信息").closest(".ant-card");
  expect(runtimeCard).not.toBeNull();
  expect(screen.queryByText("Console warning 3")).not.toBeInTheDocument();
  await userEvent.click(within(runtimeCard as HTMLElement).getAllByText("显示全部 3 条")[0]);
  expect(await screen.findByText(/Console warning 3/)).toBeInTheDocument();

  await userEvent.click(within(runtimeCard as HTMLElement).getAllByText("显示全部 3 条")[0]);
  expect(await screen.findByText(/api\/login-3/)).toBeInTheDocument();

  expect(screen.queryByRole("img", { name: "step-2" })).not.toBeInTheDocument();
  await userEvent.click(screen.getAllByText("Step 2 · assert_url_contains")[1]);
  await waitFor(() => {
    expect(screen.getByRole("img", { name: "step-2" })).toBeInTheDocument();
  });
});

test("缺少用例 Base URL 的执行会显示修复提示", async () => {
  vi.mocked(api.getExecutionDetail).mockResolvedValue({
    id: 20,
    case_id: 8,
    case_name: "Base URL 缺失用例",
    project_id: 1,
    triggered_by: 1,
    status: "failed",
    error_message: "Relative goto step requires case.base_url or execution request base_url.",
    started_at: "2026-03-10T12:00:00",
    finished_at: "2026-03-10T12:00:01",
    duration_ms: 1000,
    total_steps: 1,
    failed_step_index: 0,
    latest_screenshot_url: null,
    report: {
      status: "failed",
      steps: [
        {
          step_index: 0,
          action: "goto",
          value: "/",
          status: "failed",
          console_events: [],
          network_events: [],
          error_message: "Relative goto step requires case.base_url or execution request base_url.",
        },
      ],
    },
  });

  renderWithProviders(<ExecutionDetailPage />, {
    route: "/executions/20",
    path: "/executions/:executionId",
  });

  expect(await screen.findByText("该用例的相对路径 goto 缺少用例 Base URL")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "返回用例" })).toHaveAttribute("href", "/cases/8/edit");
});
