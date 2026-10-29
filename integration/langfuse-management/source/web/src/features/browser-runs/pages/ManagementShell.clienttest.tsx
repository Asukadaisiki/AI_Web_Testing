import { fireEvent, render, screen } from "@testing-library/react";
import { ManagementShell } from "./ManagementShell";

const mocks = vi.hoisted(() => ({
  admin: true,
  list: vi.fn(() => ({
    data: [
      { id: "c1", project_id: "p", name: "First", status: "active" },
      { id: "c2", project_id: "p", name: "Second", status: "archived" },
    ],
    isSuccess: true,
  })),
}));
vi.mock("@/src/features/rbac/utils/checkProjectAccess", () => ({
  useHasProjectAccess: ({ scope }: { scope: string }) =>
    scope === "project:update" ? mocks.admin : true,
}));
vi.mock("./BrowserRunsPage", () => ({
  RunWorkspace: (props: {
    conversationId: string;
    conversationActive: boolean;
    environmentReady: boolean;
  }) => (
    <p>
      Workspace {props.conversationId}{" "}
      {props.conversationActive && props.environmentReady ? "ready" : "blocked"}
    </p>
  ),
}));
vi.mock("@/src/utils/api", () => ({
  api: {
    useUtils: () => ({
      browserRuns: {
        conversations: { invalidate: vi.fn() },
        environment: { invalidate: vi.fn() },
      },
    }),
    browserRuns: {
      conversations: { useQuery: mocks.list },
      environment: {
        useQuery: ({ conversationId }: { conversationId: string }) => ({
          data: {
            id: `e-${conversationId}`,
            conversation_id: conversationId,
            state: "ready",
            profile_generation: 1,
          },
          isSuccess: true,
        }),
      },
      ...Object.fromEntries(
        [
          "createConversation",
          "updateConversation",
          "deleteConversation",
          "resetEnvironment",
          "reviewEnvironment",
          "startControl",
          "stopControl",
        ].map((key) => [
          key,
          { useMutation: () => ({ mutate: vi.fn(), reset: vi.fn() }) },
        ]),
      ),
    },
  },
}));
it("routes the explicit conversation and hides administration from non-admins", () => {
  const { rerender } = render(<ManagementShell projectId="p" />);
  expect(screen.getByText("Workspace c1 ready")).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Conversation"), {
    target: { value: "c2" },
  });
  expect(screen.getByText("Workspace c2 blocked")).toBeInTheDocument();
  expect(screen.getByRole("tab", { name: "Registry" })).toBeInTheDocument();
  mocks.admin = false;
  rerender(<ManagementShell projectId="p" />);
  expect(
    screen.queryByRole("tab", { name: "Registry" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("tab", { name: "Services" }),
  ).not.toBeInTheDocument();
});
