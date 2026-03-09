import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { ExecutionsPage } from "./ExecutionsPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    getExecutions: vi.fn(),
  };
});

test("按状态筛选执行列表并展示状态", async () => {
  vi.mocked(api.getExecutions)
    .mockResolvedValueOnce([
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
      },
    ])
    .mockResolvedValueOnce([
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
      },
    ]);

  renderWithProviders(<ExecutionsPage />, {
    route: "/executions",
    path: "/executions",
  });

  expect(await screen.findByText("登录冒烟")).toBeInTheDocument();
  expect(screen.getByText("通过")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("combobox"));
  await userEvent.click(await screen.findByText("失败"));

  await waitFor(() => {
    expect(api.getExecutions).toHaveBeenLastCalledWith({
      project_id: 1,
      status: "failed",
      limit: 20,
    });
  });
  expect(await screen.findByText("异常场景")).toBeInTheDocument();
});
