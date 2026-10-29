import { fireEvent, render, screen } from "@testing-library/react";
import { RunActions } from "./RunActions";

describe("RunActions", () => {
  it("blocks reruns during environment recovery without blocking cancellation", () => {
    const props = { onCancel: vi.fn(), onRerun: vi.fn(), canRerun: false };
    const { rerender } = render(<RunActions {...props} status="failed" />);
    expect(screen.getByRole("button", { name: "Rerun" })).toBeDisabled();
    rerender(<RunActions {...props} status="running" />);
    expect(screen.getByRole("button", { name: "Cancel run" })).toBeEnabled();
  });
  it("only offers cancellation for non-terminal runs", () => {
    const onCancel = vi.fn();

    const { rerender } = render(
      <RunActions status="running" onCancel={onCancel} onRerun={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Cancel run" }));
    expect(onCancel).toHaveBeenCalledTimes(1);

    rerender(
      <RunActions status="succeeded" onCancel={onCancel} onRerun={vi.fn()} />,
    );
    expect(
      screen.queryByRole("button", { name: "Cancel run" }),
    ).not.toBeInTheDocument();
  });

  it("only offers rerun for failed runs and delegates intent to the page owner", () => {
    const onRerun = vi.fn();

    const { rerender } = render(
      <RunActions status="failed" onCancel={vi.fn()} onRerun={onRerun} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Rerun" }));
    expect(onRerun).toHaveBeenCalledWith();

    rerender(
      <RunActions status="cancelled" onCancel={vi.fn()} onRerun={onRerun} />,
    );
    expect(
      screen.queryByRole("button", { name: "Rerun" }),
    ).not.toBeInTheDocument();
  });
  it("delegates retries without component-owned keys across remounts", () => {
    const onRerun = vi.fn();
    const props = { status: "failed" as const, onCancel: vi.fn(), onRerun };
    const { rerender } = render(<RunActions key="original" {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "Rerun" }));
    rerender(<RunActions key="other" {...props} status="queued" />);
    rerender(<RunActions key="original" {...props} error="Response lost" />);
    fireEvent.click(screen.getByRole("button", { name: "Rerun" }));
    expect(onRerun.mock.calls).toEqual([[], []]);
    expect(screen.getByRole("alert")).toHaveTextContent("Response lost");
  });
});
