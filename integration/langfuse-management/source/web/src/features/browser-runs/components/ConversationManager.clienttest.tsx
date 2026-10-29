import { fireEvent, render, screen } from "@testing-library/react";
import { ConversationManager } from "./ConversationManager";

const conversations = [
  { id: "c1", project_id: "p", name: "Checkout", status: "active" as const },
  { id: "c2", project_id: "p", name: "Login", status: "archived" as const },
];
describe("conversation lifecycle", () => {
  it("selects explicit conversation IDs and requires confirmation for deletion", () => {
    const onSelect = vi.fn();
    const onDelete = vi.fn();
    render(
      <ConversationManager
        conversations={conversations}
        selectedId="c1"
        canExecute
        onSelect={onSelect}
        onCreate={vi.fn()}
        onStatus={vi.fn()}
        onDelete={onDelete}
      />,
    );
    fireEvent.change(screen.getByLabelText("Conversation"), {
      target: { value: "c2" },
    });
    expect(onSelect).toHaveBeenCalledWith("c2");
    fireEvent.click(
      screen.getByRole("button", { name: "Delete conversation" }),
    );
    expect(onDelete).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Confirm delete" }));
    expect(onDelete).toHaveBeenCalledWith("c1");
  });
  it("creates by name, archives and does not advertise unsupported restoration", () => {
    const onCreate = vi.fn((_name, onSuccess) => onSuccess());
    const onStatus = vi.fn();
    const props = {
      conversations,
      selectedId: "c1",
      onSelect: vi.fn(),
      onCreate,
      onStatus,
      onDelete: vi.fn(),
    };
    const { rerender } = render(<ConversationManager {...props} canExecute />);
    fireEvent.click(screen.getByRole("button", { name: "New conversation" }));
    fireEvent.change(screen.getByLabelText("Conversation name"), {
      target: { value: " Profile " },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create conversation" }),
    );
    expect(onCreate).toHaveBeenCalledWith("Profile", expect.any(Function));
    fireEvent.click(
      screen.getByRole("button", { name: "Archive conversation" }),
    );
    expect(onStatus).toHaveBeenCalledWith("c1", "archived");
    rerender(<ConversationManager {...props} selectedId="c2" canExecute />);
    expect(
      screen.queryByRole("button", { name: "Restore conversation" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Archive conversation" }),
    ).not.toBeInTheDocument();
    rerender(<ConversationManager {...props} canExecute={false} />);
    expect(
      screen.queryByRole("button", { name: "New conversation" }),
    ).not.toBeInTheDocument();
  });
  it("shows deletion conflict recovery and blocks pending actions", () => {
    render(
      <ConversationManager
        conversations={conversations}
        selectedId="c1"
        canExecute
        pending
        error="Cancel active runs and release the environment before deleting."
        onSelect={vi.fn()}
        onCreate={vi.fn()}
        onStatus={vi.fn()}
        onDelete={vi.fn()}
      />,
    );
    expect(
      screen.getByRole("button", { name: "Archive conversation" }),
    ).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent(/Cancel active runs/);
  });
});
