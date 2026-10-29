import { fireEvent, render, screen } from "@testing-library/react";
import { NewRunDialog } from "./NewRunDialog";

const catalogs = {
  conversationId: "conversation-1",
  runtimes: [
    {
      id: "codex" as const,
      name: "Codex",
      model: { provider: "configured", name: "configured" },
      available: true,
    },
  ],
  harnesses: [
    {
      id: "har_1",
      version: 1,
      harness_id: "h1",
      project_id: "p",
      content_digest: "digest",
      name: "Generic",
      system_prompt: "",
      skill_version_ids: [],
      mcp_binding_ids: [],
      runtime_ids: ["pi", "claude-code", "codex"] as (
        | "pi"
        | "claude-code"
        | "codex"
      )[],
    },
  ],
};
function openComposer() {
  fireEvent.click(screen.getByRole("button", { name: "New agent run" }));
  fireEvent.change(screen.getByLabelText("Runtime"), {
    target: { value: "codex" },
  });
  fireEvent.change(screen.getByLabelText("Harness"), {
    target: { value: "har_1" },
  });
}

describe("NewRunDialog", () => {
  it("reuses a draft key for retries and rotates it when the draft changes", () => {
    const onCreate = vi.fn();

    render(
      <NewRunDialog {...catalogs} onCreate={onCreate} isLoading={false} />,
    );
    openComposer();
    fireEvent.change(screen.getByLabelText("URL"), {
      target: { value: "https://shop.test" },
    });
    fireEvent.change(screen.getByLabelText("Goal"), {
      target: { value: "Verify cart" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Start run" }));
    fireEvent.click(screen.getByRole("button", { name: "Start run" }));
    fireEvent.change(screen.getByLabelText("Goal"), {
      target: { value: "Verify checkout" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Start run" }));

    const first = onCreate.mock.calls[0]?.[0];
    const retry = onCreate.mock.calls[1]?.[0];
    const changed = onCreate.mock.calls[2]?.[0];
    expect(first).toMatchObject({
      conversationId: "conversation-1",
      url: "https://shop.test",
      goal: "Verify cart",
    });
    expect(retry).toEqual(first);
    expect(changed).toMatchObject({
      url: "https://shop.test",
      goal: "Verify checkout",
    });
    expect(changed?.idempotencyKey).not.toBe(first?.idempotencyKey);
  });

  it("closes after the current submission succeeds and uses a new key for the next draft", () => {
    const onCreate = vi.fn(
      (
        _input: { url?: string; goal: string; idempotencyKey: string },
        onSuccess: () => void,
      ) => onSuccess(),
    );

    render(
      <NewRunDialog {...catalogs} onCreate={onCreate} isLoading={false} />,
    );
    openComposer();
    fireEvent.change(screen.getByLabelText("URL"), {
      target: { value: "https://shop.test" },
    });
    fireEvent.change(screen.getByLabelText("Goal"), {
      target: { value: "Verify cart" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Start run" }));

    expect(
      screen.queryByRole("button", { name: "Start run" }),
    ).not.toBeInTheDocument();

    openComposer();
    fireEvent.change(screen.getByLabelText("URL"), {
      target: { value: "https://shop.test" },
    });
    fireEvent.change(screen.getByLabelText("Goal"), {
      target: { value: "Verify cart" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Start run" }));

    expect(onCreate.mock.calls[1]?.[0]?.idempotencyKey).not.toBe(
      onCreate.mock.calls[0]?.[0]?.idempotencyKey,
    );
  });

  it("keeps a failed draft and its key available for retry", () => {
    const onCreate = vi.fn();

    render(
      <NewRunDialog {...catalogs} onCreate={onCreate} isLoading={false} />,
    );
    openComposer();
    fireEvent.change(screen.getByLabelText("URL"), {
      target: { value: "https://shop.test" },
    });
    fireEvent.change(screen.getByLabelText("Goal"), {
      target: { value: "Verify cart" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Start run" }));
    fireEvent.click(screen.getByRole("button", { name: "Start run" }));

    expect(screen.getByLabelText("URL")).toHaveValue("https://shop.test");
    expect(screen.getByLabelText("Goal")).toHaveValue("Verify cart");
    expect(onCreate.mock.calls[1]?.[0]).toEqual(onCreate.mock.calls[0]?.[0]);
  });

  it("does not let a stale success callback close a reopened draft", () => {
    const successCallbacks: Array<() => void> = [];
    const onCreate = vi.fn(
      (
        _input: { url?: string; goal: string; idempotencyKey: string },
        onSuccess: () => void,
      ) => successCallbacks.push(onSuccess),
    );

    render(
      <NewRunDialog {...catalogs} onCreate={onCreate} isLoading={false} />,
    );
    openComposer();
    fireEvent.change(screen.getByLabelText("URL"), {
      target: { value: "https://shop.test" },
    });
    fireEvent.change(screen.getByLabelText("Goal"), {
      target: { value: "Verify cart" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Start run" }));
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    openComposer();
    fireEvent.change(screen.getByLabelText("Goal"), {
      target: { value: "Verify checkout" },
    });

    expect(successCallbacks).toHaveLength(1);
    successCallbacks[0]();

    expect(screen.getByLabelText("Goal")).toHaveValue("Verify checkout");
    expect(
      screen.getByRole("button", { name: "Start run" }),
    ).toBeInTheDocument();
  });
});
