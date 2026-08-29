import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import * as api from "../features/planning/api";
import * as projectApi from "../features/projects/api";
import { renderWithProviders } from "../test/test-utils";
import { SessionProjectPanel } from "./SessionProjectPanel";

vi.mock("../features/planning/api", async () => {
  const actual = await vi.importActual<typeof import("../features/planning/api")>("../features/planning/api");
  return {
    ...actual,
    createProjectInSession: vi.fn(),
    linkProjectToSession: vi.fn(),
    listSessionProjects: vi.fn(),
    unlinkProjectFromSession: vi.fn(),
  };
});
vi.mock("../features/projects/api", () => ({ getProjects: vi.fn() }));

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(projectApi.getProjects).mockResolvedValue([
    { id: 1, name: "Project A", description: null },
    { id: 2, name: "Project B", description: null },
  ]);
  vi.mocked(api.listSessionProjects).mockResolvedValue([
    { id: 1, name: "Project A", description: null, is_active: true },
    { id: 2, name: "Project B", description: null, is_active: false },
  ]);
  vi.mocked(api.linkProjectToSession).mockResolvedValue({
    id: 2,
    name: "Project B",
    description: null,
    is_active: true,
  });
});

test("marks the active project and switches by clicking another linked project", async () => {
  renderWithProviders(<SessionProjectPanel sessionId={5} />);

  expect(await screen.findByText("Project A（当前）")).toBeInTheDocument();
  await userEvent.click(screen.getByText("Project B"));

  await waitFor(() => {
    expect(api.linkProjectToSession).toHaveBeenCalledWith(5, { project_id: 2 });
  });
});
