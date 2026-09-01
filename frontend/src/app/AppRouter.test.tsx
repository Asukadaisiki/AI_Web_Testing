import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp, ConfigProvider } from "antd";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";

import { AppRouter } from "./AppRouter";

vi.mock("../pages/SessionListPage", () => ({
  SessionListPage: () => <div>Session List Mock</div>,
}));
vi.mock("../pages/PlanningPage", () => ({
  PlanningPage: () => <div>Planning Mock</div>,
}));
vi.mock("../pages/CasesPage", () => ({
  CasesPage: () => <div>Cases Mock</div>,
}));
vi.mock("../pages/CaseEditPage", () => ({
  CaseEditPage: () => <div>Case Editor Mock</div>,
}));
vi.mock("../pages/ExecutionDetailPage", () => ({
  ExecutionDetailPage: () => <div>Execution Detail Mock</div>,
}));
vi.mock("../pages/ReportPage", () => ({
  ReportPage: () => <div>Report Mock</div>,
}));

function renderRouter(initialEntries: string[]) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return render(
    <ConfigProvider>
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter
            initialEntries={initialEntries}
          >
            <AppRouter />
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>
    </ConfigProvider>,
  );
}

test("root route renders session list without authentication", async () => {
  renderRouter(["/"]);
  expect(await screen.findByText("Session List Mock")).toBeInTheDocument();
});

test("routes render without loading the current user", async () => {
  renderRouter(["/cases"]);
  expect(await screen.findByText("Cases Mock")).toBeInTheDocument();
});

test("/cases renders cases page", async () => {
  renderRouter(["/cases"]);
  expect(await screen.findByText("Cases Mock")).toBeInTheDocument();
});

test("/cases/new renders case create page", async () => {
  renderRouter(["/cases/new?project_id=1"]);
  expect(await screen.findByText("Case Editor Mock")).toBeInTheDocument();
});

test("/cases/:id/edit renders case edit page", async () => {
  renderRouter(["/cases/12/edit"]);
  expect(await screen.findByText("Case Editor Mock")).toBeInTheDocument();
});

test("/run/:id renders execution detail page", async () => {
  renderRouter(["/run/12"]);
  expect(await screen.findByText("Execution Detail Mock")).toBeInTheDocument();
});

test("legacy execution detail path redirects to /run/:id", async () => {
  renderRouter(["/executions/12"]);
  expect(await screen.findByText("Execution Detail Mock")).toBeInTheDocument();
});

test("/dashboard redirects to root", async () => {
  renderRouter(["/dashboard"]);
  expect(await screen.findByText("Session List Mock")).toBeInTheDocument();
});

test("/login redirects to the planning workspace", async () => {
  renderRouter(["/login"]);
  expect(await screen.findByText("Session List Mock")).toBeInTheDocument();
});

test("/executions redirects to /cases", async () => {
  renderRouter(["/executions"]);
  expect(await screen.findByText("Cases Mock")).toBeInTheDocument();
});
