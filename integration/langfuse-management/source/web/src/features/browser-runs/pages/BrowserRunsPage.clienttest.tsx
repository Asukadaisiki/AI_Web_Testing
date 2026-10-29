import { act, fireEvent, render, screen } from "@testing-library/react";
import { RunWorkspace } from "./BrowserRunsPage";

const BrowserRunsPage = ({ projectId }: { projectId: string }) => (
  <RunWorkspace
    projectId={projectId}
    conversationId="conversation-1"
    conversationActive
    environmentReady
  />
);

const {
  cancelMutate,
  rerunMutate,
  invalidate,
  useCancelMutation,
  useRerunMutation,
} = vi.hoisted(() => {
  const cancelMutate = vi.fn();
  const rerunMutate = vi.fn();
  return {
    cancelMutate,
    rerunMutate,
    invalidate: vi.fn().mockResolvedValue(undefined),
    useCancelMutation: vi.fn(),
    useRerunMutation: vi.fn(),
  };
});

vi.mock("@/src/utils/api", () => ({
  api: {
    useUtils: () => ({
      browserRuns: {
        list: { invalidate },
        get: { invalidate },
        trace: { invalidate },
        harnesses: { invalidate },
      },
    }),
    browserRuns: {
      runtimes: { useQuery: () => ({ data: [], isPending: false }) },
      harnesses: { useQuery: () => ({ data: [], isPending: false }) },
      createHarness: {
        useMutation: () => ({ mutate: vi.fn(), reset: vi.fn() }),
      },
      updateHarness: {
        useMutation: () => ({ mutate: vi.fn(), reset: vi.fn() }),
      },
      deleteHarness: {
        useMutation: () => ({ mutate: vi.fn(), reset: vi.fn() }),
      },
      list: {
        useQuery: () => ({
          isSuccess: true,
          data: [
            {
              id: "run-failed",
              project_id: "project-1",
              conversation_id: "conversation-1",
              status: "failed",
              checkpoint_version: 1,
              created_at: "2026-10-27T00:00:00.000Z",
              updated_at: "2026-10-27T00:00:00.000Z",
            },
            {
              id: "run-queued",
              project_id: "project-1",
              conversation_id: "conversation-1",
              status: "queued",
              checkpoint_version: 1,
              created_at: "2026-10-27T00:00:00.000Z",
              updated_at: "2026-10-27T00:00:00.000Z",
            },
            {
              id: "run-rerun",
              project_id: "project-1",
              conversation_id: "conversation-1",
              retry_of_run_id: "run-failed",
              status: "queued",
              checkpoint_version: 0,
              created_at: "2026-10-27T00:00:00.000Z",
              updated_at: "2026-10-27T00:00:00.000Z",
            },
          ],
        }),
      },
      get: { useQuery: () => ({}) },
      trace: { useQuery: () => ({}) },
      create: { useMutation: () => ({ isPending: false, mutate: vi.fn() }) },
      cancel: {
        useMutation: (...args: unknown[]) => useCancelMutation(...args),
      },
      rerun: { useMutation: (...args: unknown[]) => useRerunMutation(...args) },
    },
    traces: {
      byId: {
        useQuery: () => ({
          isError: true,
          error: { data: { code: "NOT_FOUND" } },
        }),
      },
    },
  },
}));
vi.mock("@/src/features/rbac/utils/checkProjectAccess", () => ({
  useHasProjectAccess: () => true,
}));

describe("BrowserRunsPage lifecycle actions", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "EventSource",
      class {
        close() {}
        addEventListener() {}
      },
    );
    invalidate.mockClear();
    cancelMutate.mockReset();
    rerunMutate.mockReset();
    useCancelMutation.mockReturnValue({ mutate: cancelMutate, reset: vi.fn() });
    useRerunMutation.mockReturnValue({ mutate: rerunMutate, reset: vi.fn() });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("reruns a failed Run with a new key and selects the returned Run", () => {
    rerunMutate.mockImplementation(
      (input: { projectId: string; runId: string; idempotencyKey: string }) => {
        expect(input).toMatchObject({
          projectId: "project-1",
          runId: "run-failed",
        });
        expect(input.idempotencyKey).not.toBe("");
        useRerunMutation.mock.calls
          .at(-1)?.[0]
          ?.onSuccess({ run_id: "run-rerun" }, input);
      },
    );

    render(<BrowserRunsPage projectId="project-1" />);
    fireEvent.click(screen.getByText("run-failed"));
    fireEvent.click(screen.getByRole("button", { name: "Rerun" }));

    expect(rerunMutate).toHaveBeenCalledTimes(1);
    expect(screen.getByText("Run run-rerun")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Cancel run" }),
    ).toBeInTheDocument();
  });

  it("retries the original key after a lost response and selection away/back, then rotates on confirmed acceptance", () => {
    const { rerender } = render(<BrowserRunsPage projectId="project-1" />);
    fireEvent.click(screen.getByText("run-failed"));
    fireEvent.click(screen.getByRole("button", { name: "Rerun" }));
    const original = rerunMutate.mock.calls[0]![0];
    useRerunMutation.mockReturnValue({
      mutate: rerunMutate,
      reset: vi.fn(),
      isError: true,
    });
    rerender(<BrowserRunsPage projectId="project-1" />);
    expect(screen.getByText(/Rerun failed/)).toBeInTheDocument();
    fireEvent.click(screen.getByText("run-queued"));
    fireEvent.click(screen.getByText("run-failed"));
    fireEvent.click(screen.getByRole("button", { name: "Rerun" }));
    expect(rerunMutate.mock.calls[1]![0]).toEqual(original);

    act(() =>
      useRerunMutation.mock.calls
        .at(-1)?.[0]
        ?.onSuccess({ run_id: "run-rerun" }, original),
    );
    fireEvent.click(screen.getByText("run-failed"));
    fireEvent.click(screen.getByRole("button", { name: "Rerun" }));
    const newIntent = rerunMutate.mock.calls[2]![0];
    expect(newIntent.idempotencyKey).not.toBe(original.idempotencyKey);
    // A late duplicate acceptance must not clear the newer unresolved intent.
    act(() =>
      useRerunMutation.mock.calls
        .at(-1)?.[0]
        ?.onSuccess({ run_id: "run-rerun" }, original),
    );
    fireEvent.click(screen.getByText("run-failed"));
    fireEvent.click(screen.getByRole("button", { name: "Rerun" }));
    expect(rerunMutate.mock.calls[3]![0]).toEqual(newIntent);
  });

  it("offers recovery after active polling exhausts while a terminal Run is selected", () => {
    vi.useFakeTimers();
    try {
      const { unmount } = render(<BrowserRunsPage projectId="project-1" />);
      fireEvent.click(screen.getByText("run-failed"));
      act(() => vi.advanceTimersByTime(300000));
      expect(screen.getByText("Live updates paused")).toBeInTheDocument();
      expect(invalidate).toHaveBeenCalledTimes(180);
      fireEvent.click(
        screen.getByRole("button", { name: "Reconnect updates" }),
      );
      expect(screen.queryByText("Live updates paused")).not.toBeInTheDocument();
      expect(invalidate).toHaveBeenCalledTimes(183);
      act(() => vi.advanceTimersByTime(5000));
      expect(invalidate).toHaveBeenCalledTimes(186);
      expect(screen.getByText("Durable status: failed")).toBeInTheDocument();
      expect(invalidate).toHaveBeenCalledWith({
        projectId: "project-1",
        runId: "run-failed",
      });
      unmount();
      act(() => vi.advanceTimersByTime(60000));
      expect(invalidate).toHaveBeenCalledTimes(186);
    } finally {
      vi.useRealTimers();
    }
  });

  it("cancels a queued Run with its selected project and Run IDs", () => {
    render(<BrowserRunsPage projectId="project-1" />);
    fireEvent.click(screen.getByText("run-queued"));
    fireEvent.click(screen.getByRole("button", { name: "Cancel run" }));

    expect(cancelMutate).toHaveBeenCalledWith({
      projectId: "project-1",
      runId: "run-queued",
    });
  });
  it("keeps failed execution status when native export has not arrived", () => {
    render(<BrowserRunsPage projectId="project-1" />);
    fireEvent.click(screen.getByText("run-failed"));
    expect(screen.getByText("Durable status: failed")).toBeInTheDocument();
    expect(
      screen.getByText(/Native Trace export is syncing or unavailable/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "View Langfuse Trace" }),
    ).toHaveAttribute(
      "href",
      "/project/project-1/traces/agenttrace-trace-run-failed",
    );
    expect(screen.getByRole("link", { name: "View Langfuse Session" })).toHaveAttribute(
      "href", "/project/project-1/sessions/agenttrace-session-conversation-1",
    );
  });
  it("disables lifecycle buttons and surfaces action errors during pending operations", () => {
    useRerunMutation.mockReturnValue({
      mutate: rerunMutate,
      reset: vi.fn(),
      isPending: true,
      isError: true,
    });
    render(<BrowserRunsPage projectId="project-1" />);
    fireEvent.click(screen.getByText("run-failed"));
    expect(screen.getByRole("button", { name: "Rerun" })).toBeDisabled();
    expect(screen.getByText(/Rerun failed/)).toBeInTheDocument();
  });
});
