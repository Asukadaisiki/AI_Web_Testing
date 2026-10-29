import { fireEvent, render, screen } from "@testing-library/react";
import { NewRunDialog } from "./NewRunDialog";
import { RunActions } from "./RunActions";
import { RunList } from "./RunList";

const runtimes = [
  {
    id: "pi" as const,
    name: "Pi",
    model: { provider: "configured", name: "configured" },
    available: true,
  },
  {
    id: "codex" as const,
    name: "Codex",
    model: { provider: "configured", name: "configured" },
    available: true,
  },
  {
    id: "claude-code" as const,
    name: "Claude Code",
    model: { provider: "configured", name: "configured" },
    available: false,
    unavailable_reason: "Executable unavailable",
  },
];
const harnesses = [
  {
    id: "har_example",
    name: "Generic",
    version: 3,
    harness_id: "h1",
    project_id: "p",
    content_digest: "digest",
    system_prompt: "",
    skill_version_ids: [],
    mcp_binding_ids: [],
    runtime_ids: ["pi", "claude-code", "codex"] as (
      | "pi"
      | "claude-code"
      | "codex"
    )[],
  },
];

describe("Runtime/Harness composer regressions", () => {
  it("requires independent ready selections, supports no URL and uses revision-aware retry keys", () => {
    const onCreate = vi.fn();
    const { rerender } = render(
      <NewRunDialog
        conversationId="c1"
        onCreate={onCreate}
        isLoading={false}
        runtimes={runtimes}
        harnesses={harnesses}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: /New (browser task|agent run)/ }),
    );
    fireEvent.change(screen.getByLabelText("Goal"), {
      target: { value: " Use the acceptance skill. " },
    });
    const start = screen.getByRole("button", { name: /Start (task|run)/ });
    expect(start).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Runtime"), {
      target: { value: "codex" },
    });
    fireEvent.change(screen.getByLabelText("Harness"), {
      target: { value: "har_example" },
    });
    expect(start).toBeEnabled();
    fireEvent.click(start);
    fireEvent.click(start);
    expect(onCreate.mock.calls[0]?.[0]).toMatchObject({
      goal: "Use the acceptance skill.",
      runtimeId: "codex",
      harnessId: "har_example",
    });
    expect(onCreate.mock.calls[0]?.[0]?.url).toBeUndefined();
    expect(onCreate.mock.calls[1]?.[0]).toEqual(onCreate.mock.calls[0]?.[0]);
    rerender(
      <NewRunDialog
        conversationId="c1"
        onCreate={onCreate}
        isLoading={false}
        runtimes={runtimes}
        harnesses={[{ ...harnesses[0]!, version: 4 }]}
      />,
    );
    fireEvent.click(start);
    expect(onCreate.mock.calls[2]?.[0]?.idempotencyKey).not.toEqual(
      onCreate.mock.calls[0]?.[0]?.idempotencyKey,
    );
    fireEvent.change(screen.getByLabelText("Runtime"), {
      target: { value: "pi" },
    });
    fireEvent.click(start);
    expect(onCreate.mock.calls[3]?.[0]?.idempotencyKey).not.toEqual(
      onCreate.mock.calls[2]?.[0]?.idempotencyKey,
    );
  });

  it("displays catalog readiness and action errors and blocks pending submission", () => {
    render(
      <NewRunDialog
        conversationId="c1"
        onCreate={vi.fn()}
        isLoading
        runtimes={runtimes}
        harnesses={harnesses}
        error="Submission unavailable"
      />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: /New (browser task|agent run)/ }),
    );
    expect(screen.getByText(/Executable unavailable/)).toBeInTheDocument();
    expect(screen.getByText("Submission unavailable")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Start (task|run)/ }),
    ).toBeDisabled();
  });
});

describe("Durable action regressions", () => {
  it("delegates rerun retries after errors and blocks pending or cancelling clicks", () => {
    const onRerun = vi.fn();
    const props = {
      status: "failed" as const,
      onCancel: vi.fn(),
      onRerun,
    };
    const { rerender } = render(<RunActions {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "Rerun" }));
    rerender(<RunActions {...props} error="Response lost" />);
    fireEvent.click(screen.getByRole("button", { name: "Rerun" }));
    expect(onRerun.mock.calls).toEqual([[], []]);
    rerender(<RunActions {...props} rerunPending error="Rerun unavailable" />);
    expect(screen.getByRole("button", { name: /Rerun/ })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: /Rerun/ }));
    expect(onRerun).toHaveBeenCalledTimes(2);
    expect(screen.getByText("Rerun unavailable")).toBeInTheDocument();
    rerender(<RunActions {...props} status="cancelling" />);
    expect(screen.getByRole("button", { name: /Cancel/ })).toBeDisabled();
  });

  it("keeps immutable run identities inside a horizontally scrollable table", () => {
    const runId =
      "run_01JBCQ9W6NDK4H5T7M8P2R3S4V5X6Y7Z8A9B0C1D2E3F4G5H6J7K8M9N";
    const harnessId =
      "har_deleted_01JBCQ9W6NDK4H5T7M8P2R3S4V5X6Y7Z8A9B0C1D2E3F4G5H";

    render(
      <RunList
        selectedRunId={runId}
        onSelect={vi.fn()}
        runs={[
          {
            id: runId,
            project_id: "p",
            conversation_id: "c",
            status: "failed",
            checkpoint_version: 1,
            created_at: "2026-10-28T00:00:00Z",
            updated_at: "2026-10-28T00:00:00Z",
            runtime_id: "codex",
            harness_id: harnessId,
            harness_revision: 3,
          },
        ]}
      />,
    );

    const viewport = screen.getByRole("region", { name: "Run table" });
    expect(viewport).toHaveClass("overflow-x-auto");
    expect(screen.getByRole("table")).toHaveClass("min-w-[56rem]");
    expect(screen.getByText(runId).closest("td")).toHaveClass("break-all");
    expect(
      screen.getByText(`${harnessId} (revision 3)`).closest("td"),
    ).toHaveClass("break-all");
    expect(screen.getByText("codex")).toBeInTheDocument();
  });
});
