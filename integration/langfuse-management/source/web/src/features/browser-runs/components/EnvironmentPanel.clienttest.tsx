import { fireEvent, render, screen } from "@testing-library/react";
import { EnvironmentPanel } from "./EnvironmentPanel";

const environment = {
  id: "e1",
  project_id: "p",
  conversation_id: "c1",
  state: "needs_review" as const,
  profile_generation: 2,
};
describe("environment recovery", () => {
  it("requires deliberate review or reset and does not offer takeover while leased", () => {
    const onReview = vi.fn();
    const onReset = vi.fn();
    const props = {
      environment,
      canExecute: true,
      onReview,
      onReset,
      onTakeover: vi.fn(),
      onStop: vi.fn(),
    };
    const { rerender } = render(<EnvironmentPanel {...props} />);
    expect(
      screen.getByText(/External effects may be uncertain/),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Confirm reviewed" }));
    expect(onReview).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Reset profile" }));
    expect(onReset).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Confirm reset" }));
    expect(onReset).toHaveBeenCalledTimes(1);
    rerender(
      <EnvironmentPanel
        {...props}
        environment={{
          ...environment,
          state: "leased",
          current_run_id: "run-1",
        }}
      />,
    );
    expect(
      screen.getByRole("button", { name: "Take over browser" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Reset profile" }),
    ).toBeDisabled();
    expect(screen.getByText("run-1")).toBeInTheDocument();
  });
  it("hides controls from viewers and surfaces transport errors", () => {
    render(
      <EnvironmentPanel
        environment={environment}
        canExecute={false}
        error="Environment unavailable"
        onReview={vi.fn()}
        onReset={vi.fn()}
        onTakeover={vi.fn()}
        onStop={vi.fn()}
      />,
    );
    expect(
      screen.queryByRole("button", { name: "Reset profile" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Environment unavailable",
    );
  });
});
