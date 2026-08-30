import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router-dom";

import { PlanningWorkspaceProvider } from "../features/planning/planningWorkspaceStore";
import { AppRouter } from "./AppRouter";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

export function AppRoot() {
  return (
    <QueryClientProvider client={queryClient}>
      <PlanningWorkspaceProvider>
        <BrowserRouter>
          <AppRouter />
        </BrowserRouter>
      </PlanningWorkspaceProvider>
    </QueryClientProvider>
  );
}
