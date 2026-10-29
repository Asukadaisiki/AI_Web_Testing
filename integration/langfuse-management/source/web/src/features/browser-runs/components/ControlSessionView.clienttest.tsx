import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ControlSessionView } from "./ControlSessionView";

it("uses only the token-free Langfuse gateway and submits browser actions", async () => {
  const url = "/api/agenttrace/control/c1?projectId=p1";
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockImplementation(async (_url, init) =>
        init?.method === "POST"
          ? Response.json({ accepted: true })
          : {
              ok: true,
              blob: async () => new Blob(["frame"], { type: "image/png" }),
            },
      ),
  );
  const onStop = vi.fn();
  try {
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <ControlSessionView
          session={{ id: "c1", url, expires_at: "2099-01-01T00:00:00Z" }}
          onStop={onStop}
        />
      </QueryClientProvider>,
    );
    await screen.findByRole("button", { name: "Browser frame" });
    fireEvent.change(screen.getByLabelText("Browser text"), {
      target: { value: "Login" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Type text" }));
    await waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(
        url,
        expect.objectContaining({
          method: "POST",
          body: '{"action":"type","text":"Login"}',
        }),
      ),
    );
    expect(screen.queryByRole("iframe")).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "End control session" }),
    );
    expect(onStop).toHaveBeenCalledOnce();
  } finally {
    vi.unstubAllGlobals();
  }
});
