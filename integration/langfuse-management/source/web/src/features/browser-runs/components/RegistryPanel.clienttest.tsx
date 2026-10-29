import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { RegistryPanel } from "./RegistryPanel";

const props = {
  canManage: true,
  skills: [
    {
      id: "s",
      project_id: "p",
      name: "Login",
      versions: [
        {
          id: "v",
          skill_id: "s",
          name: "Login",
          version: 1,
          content_digest: "digest",
          status: "draft" as const,
          files: [{ path: "SKILL.md", content: "# Login" }],
        },
      ],
    },
  ],
  definitions: [
    {
      id: "m",
      name: "browser-harness",
      version: 1,
      config_schema: { properties: {}, required: [] },
      secret_schema: { properties: {}, required: [] },
    },
  ],
  bindings: [],
  harnesses: [],
  onUpload: vi.fn(),
  onPublish: vi.fn(),
  onRevoke: vi.fn(),
  onBinding: vi.fn(),
  onHarness: vi.fn(),
};
it("previews uploaded skill files and only admins publish or configure bindings", () => {
  const { rerender } = render(<RegistryPanel {...props} />);
  fireEvent.click(screen.getByRole("button", { name: "Preview Login v1" }));
  expect(screen.getByText("# Login")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Publish Login v1" }));
  expect(props.onPublish).toHaveBeenCalledWith("s", "v");
  fireEvent.mouseDown(screen.getByRole("tab", { name: "MCP bindings" }), {
    button: 0,
    ctrlKey: false,
  });
  fireEvent.change(screen.getByLabelText("MCP version"), {
    target: { value: "m" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save binding" }));
  expect(props.onBinding).toHaveBeenCalledWith(
    undefined,
    { mcp_version_id: "m", config: {}, secret_refs: {}, enabled: true },
    expect.any(Function),
  );
  expect(screen.queryByLabelText(/command|env/i)).not.toBeInTheDocument();
  rerender(<RegistryPanel {...props} canManage={false} />);
  expect(
    screen.queryByRole("button", { name: "Save binding" }),
  ).not.toBeInTheDocument();
});

async function selectSkillPackage() {
  const file = new File(["# Test\nInstructions"], "SKILL.md");
  Object.defineProperty(file, "text", {
    value: async () => "# Test\nInstructions",
  });
  fireEvent.change(screen.getByLabelText("Skill folder"), {
    target: { files: [file] },
  });
  await waitFor(() =>
    expect(screen.getByLabelText("Skill name")).not.toBeDisabled(),
  );
}

it("describes the slug rules and blocks invalid names even with a ready package", async () => {
  const onUpload = vi.fn();
  render(<RegistryPanel {...props} onUpload={onUpload} />);
  const input = screen.getByLabelText("Skill name");
  const button = screen.getByRole("button", { name: "Upload draft" });
  expect(input).toHaveAttribute("maxlength", "128");
  expect(input).toHaveAccessibleDescription(/128|letters|digits/i);
  expect(button).toBeDisabled();
  await selectSkillPackage();
  for (const name of ["bad name", " padded ", "-leading", "a".repeat(129)]) {
    fireEvent.change(input, { target: { value: name } });
    expect(input).toHaveValue(name);
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveAccessibleDescription(/start|128|letters|digits/i);
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(button).toBeDisabled();
    fireEvent.click(button);
  }
  expect(onUpload).not.toHaveBeenCalled();
  fireEvent.change(input, { target: { value: "Login.v2_test-1" } });
  expect(input).toHaveAttribute("aria-invalid", "false");
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(button).toBeEnabled();
  fireEvent.click(button);
  expect(onUpload).toHaveBeenCalledWith(
    {
      name: "Login.v2_test-1",
      files: [{ path: "SKILL.md", content: "# Test\nInstructions" }],
    },
    expect.any(Function),
  );
});

it("enables a 128-character name without truncating its upload payload", async () => {
  const onUpload = vi.fn();
  render(<RegistryPanel {...props} onUpload={onUpload} />);
  await selectSkillPackage();
  const name = "A".repeat(128);
  fireEvent.change(screen.getByLabelText("Skill name"), {
    target: { value: name },
  });
  fireEvent.click(screen.getByRole("button", { name: "Upload draft" }));
  expect(onUpload).toHaveBeenCalledWith(
    expect.objectContaining({ name }),
    expect.any(Function),
  );
});
