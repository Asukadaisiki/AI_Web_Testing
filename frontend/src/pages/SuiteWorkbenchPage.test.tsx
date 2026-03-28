import { Route } from "react-router-dom";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { SuiteWorkbenchPage } from "./SuiteWorkbenchPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    getCases: vi.fn(),
    getSuiteDetail: vi.fn(),
    getSuiteRuns: vi.fn(),
    createSuite: vi.fn(),
    updateSuite: vi.fn(),
    executeSuite: vi.fn(),
  };
});

test("suite 工作台支持创建、排序并保存", async () => {
  vi.mocked(api.getCases).mockResolvedValue([
    {
      id: 1,
      project_id: 1,
      name: "登录用例",
      description: "检查登录",
      input_contract: [],
      output_contract: [],
      steps: [{ action: "goto", value: "/login" }],
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-11T20:00:00",
      updated_at: "2026-03-11T20:00:00",
    },
    {
      id: 2,
      project_id: 1,
      name: "退出用例",
      description: "检查退出",
      input_contract: [],
      output_contract: [],
      steps: [{ action: "click", target: "退出按钮" }],
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-11T20:00:00",
      updated_at: "2026-03-11T20:00:00",
    },
  ]);
  vi.mocked(api.createSuite).mockResolvedValue({
    id: 9,
    project_id: 1,
    name: "新建回归套件",
    description: "组合验证",
    case_count: 2,
    created_by: 1,
    updated_by: 1,
    created_at: "2026-03-11T20:00:00",
    updated_at: "2026-03-11T20:00:00",
    latest_run: null,
    cases: [
      { case_id: 2, case_name: "退出用例", order_index: 1 },
      { case_id: 1, case_name: "登录用例", order_index: 2 },
    ],
  });

  renderWithProviders(<SuiteWorkbenchPage />, {
    route: "/suites/new",
    path: "/suites/new",
    extraRoutes: [<Route key="edit" path="/suites/:suiteId/edit" element={<div>suite-edit-view</div>} />],
  });

  expect(await screen.findByText("登录用例")).toBeInTheDocument();
  await userEvent.type(screen.getByLabelText("Suite 名称"), "新建回归套件");
  await userEvent.click(screen.getAllByRole("button", { name: "加入 Suite" })[0]);
  await userEvent.click(screen.getAllByRole("button", { name: "加入 Suite" })[0]);

  expect(screen.getAllByText(/Case \d/)).toHaveLength(2);
  await userEvent.click(screen.getAllByRole("button", { name: /上\s*移/ })[1]);
  await userEvent.click(screen.getByRole("button", { name: /保\s*存/ }));

  await waitFor(() => {
    expect(api.createSuite).toHaveBeenCalledWith({
      project_id: 1,
      actor_user_id: 1,
      name: "新建回归套件",
      description: null,
      cases: [{ case_id: 2 }, { case_id: 1 }],
    });
  });
  expect(await screen.findByText("suite-edit-view")).toBeInTheDocument();
}, 10000);

test("suite 工作台展示最近批次并在执行后跳转详情页", async () => {
  vi.mocked(api.getCases).mockResolvedValue([
    {
      id: 1,
      project_id: 1,
      name: "登录用例",
      description: "检查登录",
      input_contract: [],
      output_contract: [],
      steps: [{ action: "goto", value: "/login" }],
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-11T20:00:00",
      updated_at: "2026-03-11T20:00:00",
    },
  ]);
  vi.mocked(api.getSuiteDetail).mockResolvedValue({
    id: 3,
    project_id: 1,
    name: "已有 Suite",
    description: "已有描述",
    case_count: 1,
    created_by: 1,
    updated_by: 1,
    created_at: "2026-03-11T20:00:00",
    updated_at: "2026-03-11T20:00:00",
    latest_run: {
      id: 30,
      suite_id: 3,
      suite_name: "已有 Suite",
      triggered_by: 1,
      source: "manual",
      source_suite_run_id: null,
      status: "passed",
      total_cases: 1,
      passed_cases: 1,
      failed_cases: 0,
      base_url_override: null,
      context_source: "empty",
      context_source_suite_run_id: null,
      rerun_context_mode: "not_applicable",
      context_snapshot: {},
      started_at: "2026-03-11T20:05:00",
      finished_at: "2026-03-11T20:05:01",
    },
    cases: [{ case_id: 1, case_name: "登录用例", order_index: 1 }],
  });
  vi.mocked(api.getSuiteRuns).mockResolvedValue([
    {
      id: 30,
      suite_id: 3,
      suite_name: "已有 Suite",
      triggered_by: 1,
      source: "manual",
      source_suite_run_id: null,
      status: "passed",
      total_cases: 1,
      passed_cases: 1,
      failed_cases: 0,
      base_url_override: null,
      context_source: "empty",
      context_source_suite_run_id: null,
      rerun_context_mode: "not_applicable",
      context_snapshot: {},
      started_at: "2026-03-11T20:05:00",
      finished_at: "2026-03-11T20:05:01",
    },
  ]);
  vi.mocked(api.executeSuite).mockResolvedValue({
    id: 31,
    suite_id: 3,
    suite_name: "已有 Suite",
    triggered_by: 1,
    source: "manual",
    source_suite_run_id: null,
    started_at: "2026-03-11T20:10:00",
    finished_at: "2026-03-11T20:10:01",
    total_cases: 1,
    passed_cases: 1,
    failed_cases: 0,
    base_url_override: null,
    context_source: "empty",
    context_source_suite_run_id: null,
    rerun_context_mode: "not_applicable",
    context_snapshot: {},
    status: "passed",
    items: [
      {
        id: 91,
        execution_id: 31,
        case_id: 1,
        case_name_snapshot: "登录用例",
        order_index: 1,
        status: "passed",
        context_reads: [],
        context_writes: [],
        context_resolution_error: null,
      },
    ],
    executions: [{ execution_id: 31, case_id: 1, case_name: "登录用例", status: "passed" }],
  });

  renderWithProviders(<SuiteWorkbenchPage />, {
    route: "/suites/3/edit",
    path: "/suites/:suiteId/edit",
    extraRoutes: [<Route key="suite-run-detail" path="/suites/:suiteId/runs/:runId" element={<div>suite-run-detail</div>} />],
  });

  expect(await screen.findByDisplayValue("已有 Suite")).toBeInTheDocument();
  expect(screen.getByText("最近批次")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "查看详情" })).toHaveAttribute("href", "/suites/3/runs/30");

  await userEvent.click(screen.getByRole("button", { name: /执\s*行\s*Suite/ }));

  await waitFor(() => {
    expect(api.executeSuite).toHaveBeenCalledWith(3, { actor_user_id: 1 });
  });
  expect(await screen.findByText("suite-run-detail")).toBeInTheDocument();
});
