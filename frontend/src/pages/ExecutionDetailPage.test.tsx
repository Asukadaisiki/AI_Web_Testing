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
    createCorrection: vi.fn(),
    executeCase: vi.fn(),
    getExecutionDetail: vi.fn(),
  };
});

test("澶辫触姝ラ榛樿灞曞紑锛屾敮鎸佹煡鐪嬪€欓€夎瘎鍒嗗苟鎸夐渶灞曞紑 console 涓?network 浜嬩欢", async () => {
  vi.mocked(api.getExecutionDetail).mockResolvedValue({
    id: 12,
    case_id: 1,
    case_name: "澶辫触鐢ㄤ緥",
    project_id: 1,
    triggered_by: 1,
    status: "failed",
    error_message: "按钮未找到",
    started_at: "2026-03-09T12:00:00",
    finished_at: "2026-03-09T12:00:03",
    duration_ms: 3000,
    total_steps: 2,
    failed_step_index: 0,
    failure_category: "locator",
    failure_step_action: "click",
    latest_url: "http://example.com/login",
    latest_screenshot_url: "/artifacts/executions/12/step-01.png",
    origin_suite_run: null,
    suite_context: {
      reads: [
        {
          name: "sessionToken",
          context_key: "session_token",
          value_type: "string",
          resolved: true,
          source_suite_run_id: 7,
          error_message: null,
        },
      ],
      writes: [],
      resolution_error: null,
    },
    report: {
      status: "failed",
      steps: [
        {
          step_index: 0,
          action: "click",
          target: "鐧诲綍鎸夐挳",
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
            target: "鐧诲綍鎸夐挳",
            match_strategy: null,
            selection_reason: null,
            candidates: [
              {
                strategy: "button_role",
                preview_text: "鐧诲綍",
                role: "button",
                attributes: { aria_label: "鐧诲綍鎸夐挳", placeholder: null, data_testid: null },
                score: 108,
                matched_rules: ["exact-button-role-match", "visible", "enabled", "has-preview-text"],
                rejected_reasons: [],
                visible: true,
                enabled: true,
              },
              {
                strategy: "text",
                preview_text: "鐧诲綍",
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
            { level: "warning", text: "Console warning 1", source_url: "http://example.com/app.js", line_number: 9 },
            { level: "warning", text: "Console warning 2", source_url: "http://example.com/app.js", line_number: 10 },
            { level: "error", text: "Console warning 3", source_url: "http://example.com/app.js", line_number: 11 },
          ],
          network_events: [
            { url: "http://example.com/api/login-1", method: "POST", status: 500, resource_type: "xhr", failure_text: null },
            { url: "http://example.com/api/login-2", method: "POST", status: 500, resource_type: "xhr", failure_text: null },
            { url: "http://example.com/api/login-3", method: "POST", status: 500, resource_type: "xhr", failure_text: null },
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

  expect(await screen.findByRole("heading", { name: "澶辫触鐢ㄤ緥" })).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "返回执行中心" })).toHaveAttribute("href", "/executions");
  expect(screen.getByRole("link", { name: "返回用例" })).toHaveAttribute("href", "/cases/1/edit");
  expect(screen.getByText("Suite Context")).toBeInTheDocument();
  expect(screen.getByText(/session_token/)).toBeInTheDocument();
  expect(screen.getByText("No locator candidates matched target.")).toBeInTheDocument();
  expect(screen.getByText(/score=108/)).toBeInTheDocument();
  expect(screen.getByText(/rejected=element-not-enabled/)).toBeInTheDocument();
  const screenshot = screen.getByRole("img", { name: "step-1" });
  expect(screenshot).toHaveAttribute("src", "/artifacts/executions/12/step-01.png");
  expect(screenshot).toHaveClass("step-screenshot-image");
  expect(screen.getByRole("link", { name: "打开原图" })).toHaveAttribute("href", "/artifacts/executions/12/step-01.png");

  const runtimeCard = screen.getByText("运行信息").closest(".ant-card");
  expect(runtimeCard).not.toBeNull();
  expect(screen.queryByText("Console warning 3")).not.toBeInTheDocument();
  await userEvent.click(within(runtimeCard as HTMLElement).getAllByText("显示全部 3 条")[0]);
  expect(await screen.findByText(/Console warning 3/)).toBeInTheDocument();

  await userEvent.click(within(runtimeCard as HTMLElement).getAllByText("显示全部 3 条")[0]);
  expect(await screen.findByText(/api\/login-3/)).toBeInTheDocument();

  expect(screen.queryByRole("img", { name: "step-2" })).not.toBeInTheDocument();
  await userEvent.click(screen.getAllByText("Step 2 / assert_url_contains")[1]);
  await waitFor(() => {
    expect(screen.getByRole("img", { name: "step-2" })).toBeInTheDocument();
  });
});

test("缂哄皯鐢ㄤ緥 Base URL 鐨勬墽琛屼細鏄剧ず淇鎻愮ず", async () => {
  vi.mocked(api.getExecutionDetail).mockResolvedValue({
    id: 20,
    case_id: 8,
    case_name: "Base URL 缂哄け鐢ㄤ緥",
    project_id: 1,
    triggered_by: 1,
    status: "failed",
    error_message: "Relative goto step requires case.base_url or execution request base_url.",
    started_at: "2026-03-10T12:00:00",
    finished_at: "2026-03-10T12:00:01",
    duration_ms: 1000,
    total_steps: 1,
    failed_step_index: 0,
    failure_category: "configuration",
    failure_step_action: "goto",
    latest_url: null,
    latest_screenshot_url: null,
    origin_suite_run: null,
    suite_context: null,
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

test("鎵ц璇︽儏椤典紭鍏堟樉绀鸿繑鍥?Suite 鎵规閾炬帴", async () => {
  vi.mocked(api.getExecutionDetail).mockResolvedValue({
    id: 21,
    case_id: 5,
    case_name: "鏉ユ簮鍥炴祦鐢ㄤ緥",
    project_id: 1,
    triggered_by: 1,
    status: "failed",
    error_message: "runner boom",
    started_at: "2026-03-10T12:00:00",
    finished_at: "2026-03-10T12:00:01",
    duration_ms: 1000,
    total_steps: 1,
    failed_step_index: 0,
    failure_category: "runner",
    failure_step_action: "click",
    latest_url: null,
    latest_screenshot_url: null,
    origin_suite_run: {
      suite_id: 3,
      suite_name: "璁㈠崟濂椾欢",
      suite_run_id: 14,
    },
    suite_context: null,
    report: {
      status: "failed",
      steps: [
        {
          step_index: 0,
          action: "click",
          target: "鎻愪氦鎸夐挳",
          status: "failed",
          console_events: [],
          network_events: [],
          error_message: "runner boom",
        },
      ],
    },
  });

  renderWithProviders(<ExecutionDetailPage />, {
    route: {
      pathname: "/executions/21",
      state: {
        fromExecutions: "/executions?window_days=7&status=failed&case_id=5",
      },
    },
    path: "/executions/:executionId",
  });

  expect(await screen.findByRole("heading", { name: "鏉ユ簮鍥炴祦鐢ㄤ緥" })).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "返回 Suite 批次" })).toHaveAttribute("href", "/suites/3/runs/14");
  expect(screen.getByRole("link", { name: "璁㈠崟濂椾欢 / #14" })).toHaveAttribute("href", "/suites/3/runs/14");
});

test("needs_intervention 姝ラ灞曠ず骞查闈㈡澘锛屾彁浜や慨姝ｅ悗鏄剧ず閲嶈窇鍏ュ彛", async () => {
  vi.mocked(api.getExecutionDetail).mockResolvedValue({
    id: 31,
    case_id: 9,
    case_name: "浜哄伐骞查鐢ㄤ緥",
    project_id: 1,
    triggered_by: 1,
    status: "needs_intervention",
    error_message: "All locate tiers failed for target: 鐧诲綍鎸夐挳",
    started_at: "2026-03-14T08:00:00",
    finished_at: "2026-03-14T08:00:10",
    duration_ms: 10000,
    total_steps: 1,
    failed_step_index: 0,
    failure_category: "locator",
    failure_step_action: "click",
    latest_url: "https://app.example.com/login",
    latest_screenshot_url: "/artifacts/executions/31/step-01.png",
    origin_suite_run: null,
    suite_context: null,
    report: {
      status: "failed",
      steps: [
        {
          step_index: 0,
          action: "click",
          target: "鐧诲綍鎸夐挳",
          status: "failed",
          console_events: [],
          network_events: [],
          screenshot_url: "/artifacts/executions/31/step-01.png",
          error_message: "All locate tiers failed for target: 鐧诲綍鎸夐挳",
          intervention_request: {
            screenshot_url: "/artifacts/executions/31/step-01.png",
            page_url: "https://app.example.com/login",
            target_description: "鐧诲綍鎸夐挳",
            dom_snapshot: [
              {
                tag: "button",
                text: "鐧诲綍鎸夐挳",
                role: "button",
                aria_label: "鐧诲綍鎸夐挳",
                placeholder: null,
                data_testid: "login-button",
                css_selector: "#login-btn",
                xpath: "/html/body/button[1]",
                rect: { x: 10, y: 20, width: 80, height: 24 },
                visible: true,
                enabled: true,
              },
            ],
            ai_candidate: {
              center: [50, 30],
              bbox: [10, 20, 90, 44],
              confidence: 0.7,
              raw_response: '{"bbox":[10,20,90,44]}',
            },
            locator_trace: {
              target: "鐧诲綍鎸夐挳",
              candidates: [],
              failure_reason: "No locator candidates matched target.",
            },
          },
        },
      ],
    },
  });
  vi.mocked(api.createCorrection).mockResolvedValue({
    id: 5,
    page_url_pattern: "https://app.example.com/login",
    target_description: "鐧诲綍鎸夐挳",
    correction_type: "test_id",
    correction_value: "login-button",
    verified_count: 0,
    consecutive_failures: 0,
    is_active: true,
    source_execution_id: 31,
    created_by: 1,
    created_at: "2026-03-14T08:01:00",
    updated_at: "2026-03-14T08:01:00",
  });

  renderWithProviders(<ExecutionDetailPage />, {
    route: "/executions/31",
    path: "/executions/:executionId",
  });

  expect(await screen.findByText("人工干预")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "查看同目标修正记录" }).getAttribute("href")).toContain(
    "/corrections?target_description=",
  );
  expect(screen.getByRole("link", { name: "查看同目标修正记录" }).getAttribute("href")).toContain(
    "page_url=https%3A%2F%2Fapp.example.com%2Flogin",
  );
  expect(screen.getByText("当前步骤已进入人工干预")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "使用该选择器" }));
  await userEvent.click(screen.getByRole("button", { name: "提交修正" }));

  await waitFor(() => {
    expect(api.createCorrection).toHaveBeenCalledWith({
      page_url: "https://app.example.com/login",
      target_description: "鐧诲綍鎸夐挳",
      correction_type: "test_id",
      correction_value: "login-button",
      source_execution_id: 31,
      created_by: 1,
    });
  });
  expect(await screen.findByText("修正已保存 #5")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "重新执行当前用例" })).toBeInTheDocument();
});
test("needs_intervention step shows creation error when correction submit fails", async () => {
  vi.mocked(api.getExecutionDetail).mockResolvedValue({
    id: 41,
    case_id: 9,
    case_name: "manual intervention case",
    project_id: 1,
    triggered_by: 1,
    status: "needs_intervention",
    error_message: "All locate tiers failed for target: login button",
    started_at: "2026-03-14T08:00:00",
    finished_at: "2026-03-14T08:00:10",
    duration_ms: 10000,
    total_steps: 1,
    failed_step_index: 0,
    failure_category: "locator",
    failure_step_action: "click",
    latest_url: "https://app.example.com/login",
    latest_screenshot_url: "/artifacts/executions/41/step-01.png",
    origin_suite_run: null,
    suite_context: null,
    report: {
      status: "failed",
      steps: [
        {
          step_index: 0,
          action: "click",
          target: "login button",
          status: "failed",
          console_events: [],
          network_events: [],
          screenshot_url: "/artifacts/executions/41/step-01.png",
          error_message: "All locate tiers failed for target: login button",
          intervention_request: {
            screenshot_url: "/artifacts/executions/41/step-01.png",
            page_url: "https://app.example.com/login",
            target_description: "login button",
            dom_snapshot: [
              {
                tag: "button",
                text: "Login",
                role: "button",
                aria_label: "Login button",
                placeholder: null,
                data_testid: "login-button",
                css_selector: "#login-btn",
                xpath: "/html/body/button[1]",
                rect: { x: 10, y: 20, width: 80, height: 24 },
                visible: true,
                enabled: true,
              },
            ],
            ai_candidate: null,
            locator_trace: null,
          },
        },
      ],
    },
  });
  vi.mocked(api.createCorrection).mockRejectedValue(new Error("create boom"));

  renderWithProviders(<ExecutionDetailPage />, {
    route: "/executions/41",
    path: "/executions/:executionId",
  });

  expect(await screen.findByText("人工干预")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "查看同目标修正记录" })).toHaveAttribute(
    "href",
    "/corrections?target_description=login%20button&page_url=https%3A%2F%2Fapp.example.com%2Flogin",
  );
  await userEvent.click(screen.getByRole("button", { name: "使用该选择器" }));
  await userEvent.click(screen.getByRole("button", { name: "提交修正" }));

  expect(await screen.findByText("create boom")).toBeInTheDocument();
});

test("needs_intervention step shows rerun error when rerun fails", async () => {
  vi.mocked(api.getExecutionDetail).mockResolvedValue({
    id: 42,
    case_id: 9,
    case_name: "manual intervention case",
    project_id: 1,
    triggered_by: 1,
    status: "needs_intervention",
    error_message: "All locate tiers failed for target: login button",
    started_at: "2026-03-14T08:00:00",
    finished_at: "2026-03-14T08:00:10",
    duration_ms: 10000,
    total_steps: 1,
    failed_step_index: 0,
    failure_category: "locator",
    failure_step_action: "click",
    latest_url: "https://app.example.com/login",
    latest_screenshot_url: "/artifacts/executions/42/step-01.png",
    origin_suite_run: null,
    suite_context: null,
    report: {
      status: "failed",
      steps: [
        {
          step_index: 0,
          action: "click",
          target: "login button",
          status: "failed",
          console_events: [],
          network_events: [],
          screenshot_url: "/artifacts/executions/42/step-01.png",
          error_message: "All locate tiers failed for target: login button",
          intervention_request: {
            screenshot_url: "/artifacts/executions/42/step-01.png",
            page_url: "https://app.example.com/login",
            target_description: "login button",
            dom_snapshot: [
              {
                tag: "button",
                text: "Login",
                role: "button",
                aria_label: "Login button",
                placeholder: null,
                data_testid: "login-button",
                css_selector: "#login-btn",
                xpath: "/html/body/button[1]",
                rect: { x: 10, y: 20, width: 80, height: 24 },
                visible: true,
                enabled: true,
              },
            ],
            ai_candidate: null,
            locator_trace: null,
          },
        },
      ],
    },
  });
  vi.mocked(api.createCorrection).mockResolvedValue({
    id: 6,
    page_url_pattern: "https://app.example.com/login",
    target_description: "login button",
    correction_type: "test_id",
    correction_value: "login-button",
    verified_count: 0,
    consecutive_failures: 0,
    is_active: true,
    source_execution_id: 42,
    created_by: 1,
    created_at: "2026-03-14T08:01:00",
    updated_at: "2026-03-14T08:01:00",
  });
  vi.mocked(api.executeCase).mockRejectedValue(new Error("rerun boom"));

  renderWithProviders(<ExecutionDetailPage />, {
    route: "/executions/42",
    path: "/executions/:executionId",
  });

  expect(await screen.findByText("人工干预")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "使用该选择器" }));
  await userEvent.click(screen.getByRole("button", { name: "提交修正" }));
  expect(await screen.findByText("修正已保存 #6")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "重新执行当前用例" }));
  expect(await screen.findByText("rerun boom")).toBeInTheDocument();
});
