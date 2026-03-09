import { screen } from "@testing-library/react";
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

test("展示步骤时间线、截图和失败原因", async () => {
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
    report: {
      status: "failed",
      steps: [
        {
          step_index: 0,
          action: "click",
          target: "登录按钮",
          status: "failed",
          url: "http://example.com/login",
          screenshot_url: "/artifacts/executions/12/step-01.png",
          error_message: "按钮未找到",
        },
      ],
    },
  });

  renderWithProviders(<ExecutionDetailPage />, {
    route: "/executions/12",
    path: "/executions/:executionId",
  });

  expect(await screen.findByRole("heading", { name: "失败用例" })).toBeInTheDocument();
  expect(screen.getAllByText("Step 1 · click")).toHaveLength(2);
  expect(screen.getAllByText("按钮未找到").length).toBeGreaterThan(0);
  expect(screen.getByRole("img", { name: "step-1" })).toHaveAttribute(
    "src",
    "/artifacts/executions/12/step-01.png",
  );
});
