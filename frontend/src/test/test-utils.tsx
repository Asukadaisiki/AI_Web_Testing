import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp, ConfigProvider } from "antd";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter, Route, Routes } from "react-router-dom";

import { PlanningWorkspaceProvider } from "../features/planning/planningWorkspaceStore";

export function renderWithProviders(
  ui: ReactElement,
  {
    route = "/",
    path = "/",
    extraRoutes = [],
  }: {
    route?: string | { pathname: string; search?: string; hash?: string; state?: unknown };
    path?: string;
    extraRoutes?: ReactElement[];
  } = {},
) {
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
          <PlanningWorkspaceProvider>
            <MemoryRouter
              initialEntries={[route]}
            >
              <Routes>
                <Route path={path} element={ui} />
                {extraRoutes}
              </Routes>
            </MemoryRouter>
          </PlanningWorkspaceProvider>
        </QueryClientProvider>
      </AntdApp>
    </ConfigProvider>,
  );
}
