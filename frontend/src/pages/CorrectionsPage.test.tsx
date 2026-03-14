import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { CorrectionsPage } from "./CorrectionsPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    getCorrections: vi.fn(),
    updateCorrectionState: vi.fn(),
  };
});

test("渲染修正记录列表并支持筛选与启停", async () => {
  vi.mocked(api.getCorrections).mockResolvedValue([
    {
      id: 12,
      page_url_pattern: "https://app.example.com/login?session=*",
      target_description: "登录按钮",
      correction_type: "test_id",
      correction_value: "login-button",
      verified_count: 3,
      consecutive_failures: 0,
      is_active: true,
      source_execution_id: 41,
      created_by: 1,
      created_at: "2026-03-14T20:00:00",
      updated_at: "2026-03-14T20:10:00",
    },
  ]);
  vi.mocked(api.updateCorrectionState).mockResolvedValue({
    id: 12,
    page_url_pattern: "https://app.example.com/login?session=*",
    target_description: "登录按钮",
    correction_type: "test_id",
    correction_value: "login-button",
    verified_count: 3,
    consecutive_failures: 0,
    is_active: false,
    source_execution_id: 41,
    created_by: 1,
    created_at: "2026-03-14T20:00:00",
    updated_at: "2026-03-14T20:12:00",
  });

  renderWithProviders(<CorrectionsPage />, {
    route: "/corrections?target_description=%E7%99%BB%E5%BD%95%E6%8C%89%E9%92%AE&page_url=https%3A%2F%2Fapp.example.com%2Flogin",
    path: "/corrections",
  });

  expect(await screen.findByText("登录按钮")).toBeInTheDocument();
  expect(api.getCorrections).toHaveBeenCalledWith({
    target_description: "登录按钮",
    page_url: "https://app.example.com/login",
    is_active: undefined,
    limit: 10,
    offset: 0,
  });
  expect(screen.getByRole("link", { name: "#41" })).toHaveAttribute("href", "/executions/41");
  expect(screen.getByText("Test ID")).toBeInTheDocument();

  const row = screen.getByText("登录按钮").closest("tr");
  expect(row).not.toBeNull();
  await userEvent.click(within(row as HTMLElement).getByRole("button", { name: /停\s*用/ }));

  await waitFor(() => {
    expect(api.updateCorrectionState).toHaveBeenCalledWith(12, { is_active: false });
  });
});

test("应用筛选时会带上 page_url 和状态参数重新查询", async () => {
  vi.mocked(api.getCorrections).mockResolvedValue([]);

  renderWithProviders(<CorrectionsPage />, {
    route: "/corrections",
    path: "/corrections",
  });

  await screen.findByText("当前筛选条件下没有修正记录。");
  await userEvent.type(screen.getByPlaceholderText("按目标描述筛选"), "订单按钮");
  await userEvent.type(screen.getByPlaceholderText("按页面 URL 筛选"), "https://app.example.com/orders/123");
  await userEvent.click(screen.getByRole("combobox"));
  await userEvent.click(screen.getByText("仅已停用"));
  await userEvent.click(screen.getByRole("button", { name: "应用筛选" }));

  await waitFor(() => {
    expect(api.getCorrections).toHaveBeenLastCalledWith({
      target_description: "订单按钮",
      page_url: "https://app.example.com/orders/123",
      is_active: false,
      limit: 10,
      offset: 0,
    });
  });
});
