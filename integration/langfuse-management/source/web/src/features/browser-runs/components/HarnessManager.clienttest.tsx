import { fireEvent, render, screen } from "@testing-library/react";
import { HarnessManager } from "./HarnessManager";

const harness = {
  id: "har_1",
  version: 1,
  harness_id: "harness-1",
  project_id: "p",
  content_digest: "digest",
  name: "Generic",
  system_prompt: "Be concise",
  skill_version_ids: [],
  mcp_binding_ids: [],
  runtime_ids: ["pi", "claude-code", "codex"] as (
    | "pi"
    | "claude-code"
    | "codex"
  )[],
};

describe("Harness manager", () => {
  it("publishes version references, defaults runtimes and never exposes raw MCP command/env", () => {
    const onSave = vi.fn();
    const { rerender } = render(
      <HarnessManager harnesses={[harness]} canManage onSave={onSave} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Manage Harnesses" }));
    fireEvent.click(screen.getByRole("button", { name: "Edit Generic" }));
    expect(screen.getByLabelText("System prompt")).toHaveValue("Be concise");
    expect(screen.queryByLabelText("MCP JSON")).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/command|env/i)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Save Harness" }));
    expect(onSave.mock.calls[0]?.slice(0, 2)).toEqual([
      harness.harness_id,
      {
        name: "Generic",
        system_prompt: "Be concise",
        skill_version_ids: [],
        mcp_binding_ids: [],
        runtime_ids: ["pi", "claude-code", "codex"],
      },
    ]);
    rerender(
      <HarnessManager
        harnesses={[{ ...harness, version: 2, system_prompt: "Updated" }]}
        canManage
        onSave={onSave}
      />,
    );
    expect(screen.getByLabelText("System prompt")).toHaveValue("Updated");
  });

  it("gates configuration and blocks pending writes with visible errors", () => {
    const props = { harnesses: [harness], onSave: vi.fn(), onDelete: vi.fn() };
    const { rerender } = render(
      <HarnessManager {...props} canManage={false} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Manage Harnesses" }));
    expect(
      screen.queryByRole("button", { name: "Edit Generic" }),
    ).not.toBeInTheDocument();
    rerender(
      <HarnessManager {...props} canManage pending error="Save unavailable" />,
    );
    expect(screen.getByRole("button", { name: "Edit Generic" })).toBeDisabled();
    expect(screen.getByText("Save unavailable")).toBeInTheDocument();
  });
});
