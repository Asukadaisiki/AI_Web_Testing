import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { SuiteRunDetailPage } from "./SuiteRunDetailPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    getSuiteRunDetail: vi.fn(),
    rerunFailedSuiteRun: vi.fn(),
  };
});

test("批次详情页渲染摘要和子执行链接", async () => {
  vi.mocked(api.getSuiteRunDetail).mockResolvedValue({
    id: 7,
    suite_id: 2,
    suite_name: "订单回归套件",
    triggered_by: 1,
    source: "manual",
    source_suite_run_id: null,
    status: "failed",
    total_cases: 2,
    passed_cases: 1,
    failed_cases: 1,
    base_url_override: null,
    context_source: "empty",
    context_source_suite_run_id: null,
    rerun_context_mode: "not_applicable",
    context_snapshot: { session_token: "https://example.com/session-007" },
    started_at: "2026-03-11T20:01:00",
    finished_at: "2026-03-11T20:01:03",
    items: [
      {
        id: 1,
        case_id: 11,
        case_name_snapshot: "提交订单",
        order_index: 1,
        execution_id: 101,
        status: "failed",
        context_reads: [],
        context_writes: [
          {
            name: "sessionToken",
            context_key: "session_token",
            value_type: "string",
            source: "latest_url",
            status: "skipped",
            error_message: "runner boom",
          },
        ],
        context_resolution_error: "runner boom",
      },
      {
        id: 2,
        case_id: 12,
        case_name_snapshot: "取消订单",
        order_index: 2,
        execution_id: 102,
        status: "passed",
        context_reads: [
          {
            name: "sessionToken",
            context_key: "session_token",
            value_type: "string",
            resolved: true,
            source_suite_run_id: 7,
            error_message: null,
          },
        ],
        context_writes: [],
        context_resolution_error: null,
      },
    ],
  });

  renderWithProviders(<SuiteRunDetailPage />, {
    route: "/suites/2/runs/7",
    path: "/suites/:suiteId/runs/:runId",
  });

  expect(await screen.findByRole("heading", { name: "订单回归套件" })).toBeInTheDocument();
  expect(screen.getByText("#7")).toBeInTheDocument();
  expect(screen.getByText("empty")).toBeInTheDocument();
  expect(screen.getAllByText(/session_token/).length).toBeGreaterThan(0);
  expect(screen.getByRole("link", { name: "#101" })).toHaveAttribute("href", "/executions/101");
  expect(screen.getByRole("button", { name: "重跑失败项" })).toBeEnabled();
});

test("批次详情页支持失败重跑并跳转到新批次", async () => {
  vi.mocked(api.getSuiteRunDetail).mockImplementation(async (_suiteId, runId) =>
    runId === 7
      ? {
          id: 7,
          suite_id: 2,
          suite_name: "订单回归套件",
          triggered_by: 1,
          source: "manual",
          source_suite_run_id: null,
          status: "failed",
          total_cases: 2,
          passed_cases: 1,
          failed_cases: 1,
          base_url_override: null,
          context_source: "empty",
          context_source_suite_run_id: null,
          rerun_context_mode: "not_applicable",
          context_snapshot: { session_token: "https://example.com/session-007" },
          started_at: "2026-03-11T20:01:00",
          finished_at: "2026-03-11T20:01:03",
          items: [
            {
              id: 1,
              case_id: 11,
              case_name_snapshot: "提交订单",
              order_index: 1,
              execution_id: 101,
              status: "failed",
              context_reads: [],
              context_writes: [],
              context_resolution_error: null,
            },
          ],
        }
      : {
          id: 8,
          suite_id: 2,
          suite_name: "订单回归套件",
          triggered_by: 1,
          source: "rerun_failed",
          source_suite_run_id: 7,
          status: "passed",
          total_cases: 1,
          passed_cases: 1,
          failed_cases: 0,
          base_url_override: null,
          context_source: "suite_run_snapshot",
          context_source_suite_run_id: 7,
          rerun_context_mode: "reuse_source_context",
          context_snapshot: { session_token: "https://example.com/session-007" },
          started_at: "2026-03-11T20:05:00",
          finished_at: "2026-03-11T20:05:02",
          items: [
            {
              id: 3,
              case_id: 11,
              case_name_snapshot: "提交订单",
              order_index: 1,
              execution_id: 103,
              status: "passed",
              context_reads: [],
              context_writes: [],
              context_resolution_error: null,
            },
          ],
        },
  );
  vi.mocked(api.rerunFailedSuiteRun).mockResolvedValue({
    id: 8,
    suite_id: 2,
    suite_name: "订单回归套件",
    triggered_by: 1,
    source: "rerun_failed",
    source_suite_run_id: 7,
    status: "passed",
    total_cases: 1,
    passed_cases: 1,
    failed_cases: 0,
    base_url_override: null,
    context_source: "suite_run_snapshot",
    context_source_suite_run_id: 7,
    rerun_context_mode: "reuse_source_context",
    context_snapshot: { session_token: "https://example.com/session-007" },
    started_at: "2026-03-11T20:05:00",
    finished_at: "2026-03-11T20:05:02",
    items: [
      {
        id: 3,
        case_id: 11,
        case_name_snapshot: "提交订单",
        order_index: 1,
        execution_id: 103,
        status: "passed",
        context_reads: [],
        context_writes: [],
        context_resolution_error: null,
      },
    ],
    executions: [{ execution_id: 103, case_id: 11, case_name: "提交订单", status: "passed" }],
  });

  renderWithProviders(<SuiteRunDetailPage />, {
    route: "/suites/2/runs/7",
    path: "/suites/:suiteId/runs/:runId",
  });

  await screen.findByRole("heading", { name: "订单回归套件" });
  await userEvent.click(screen.getByRole("button", { name: "重跑失败项" }));

  await waitFor(() => {
    expect(api.rerunFailedSuiteRun).toHaveBeenCalledWith(2, 7, { actor_user_id: 1 });
  });
  expect(await screen.findByText("#8")).toBeInTheDocument();
  expect(screen.getByText("rerun_failed")).toBeInTheDocument();
});
