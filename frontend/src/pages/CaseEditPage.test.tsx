import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route } from "react-router-dom";
import { vi } from "vitest";

import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";
import { CaseEditPage } from "./CaseEditPage";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    createCase: vi.fn(),
    deleteCase: vi.fn(),
    getCaseDetail: vi.fn(),
    updateCase: vi.fn(),
  };
});

beforeEach(() => {
  vi.resetAllMocks();
});

test("creates a case for the project from the new-case route", async () => {
  vi.mocked(api.createCase).mockResolvedValue({ id: 42 } as never);

  renderWithProviders(<CaseEditPage />, {
    route: "/cases/new?project_id=7",
    path: "/cases/new",
    extraRoutes: [<Route key="edit" path="/cases/:caseId/edit" element={<div>edit-view</div>} />],
  });

  await userEvent.type(screen.getByLabelText("用例名称"), "登录冒烟");
  await userEvent.type(screen.getByLabelText("Base URL"), "https://example.com");
  await userEvent.click(screen.getByRole("button", { name: /保\s*存/ }));

  await waitFor(() => {
    expect(api.createCase).toHaveBeenCalledWith(
      expect.objectContaining({
        project_id: 7,
        actor_user_id: 1,
        name: "登录冒烟",
        base_url: "https://example.com",
        steps: [],
      }),
    );
  });
  expect(api.getCaseDetail).not.toHaveBeenCalled();
});

test("rejects an invalid edit id without requesting a NaN case", async () => {
  renderWithProviders(<CaseEditPage />, {
    route: "/cases/not-a-number/edit",
    path: "/cases/:caseId/edit",
  });

  expect(await screen.findByText("用例 ID 无效")).toBeInTheDocument();
  expect(api.getCaseDetail).not.toHaveBeenCalled();
});
