import { afterEach, expect, it, vi } from "vitest";
import { HarnessEventWriter, type RunRequest } from "../src/run-protocol.js";
import { createExecutionSessionFactory } from "../src/session-factory.js";

const adapters = vi.hoisted(() => ({
  claude: vi.fn(),
  codex: vi.fn(),
}));
vi.mock("@earendil-works/pi-coding-agent", () => {
  throw new Error("Pi SDK is unavailable");
});
vi.mock("../src/browser-harness.js", () => {
  throw new Error("Browser Harness is unavailable");
});
vi.mock("../src/claude-session.js", () => ({ createClaudeSession: adapters.claude }));
vi.mock("../src/codex-session.js", () => ({ createCodexSession: adapters.codex }));

afterEach(() => vi.clearAllMocks());

it.each(["claude-code", "codex"] as const)(
  "dispatches %s without loading Pi or the legacy browser Harness",
  async (runtime) => {
    const session = { prompt: vi.fn(), abort: vi.fn(), close: vi.fn() };
    const selected = runtime === "codex" ? adapters.codex : adapters.claude;
    selected.mockResolvedValueOnce(session);
    const request: RunRequest = {
      protocol_version: "pi-browser-harness.v1",
      run_id: "runtime-selection",
      attempt_id: "attempt",
      goal: "Use the selected Harness",
      model: { provider: "compatible", name: "model", reasoning_effort: "off" },
      execution: {
        runtime_id: runtime,
        harness: {
          id: "generic", name: "Generic", revision: 2,
          system_prompt: "Return the selected answer", skills: [], mcp: {},
        },
      },
    };
    const options = {
      eventWriter: new HarnessEventWriter(request, { write: () => true }),
      signal: new AbortController().signal,
    };
    const factory = await createExecutionSessionFactory();
    expect(await factory(request, options)).toBe(session);
    expect(selected).toHaveBeenCalledExactlyOnceWith(request, options);
    expect(runtime === "codex" ? adapters.claude : adapters.codex).not.toHaveBeenCalled();
  },
);

it("rejects an unknown runtime without selecting another adapter", async () => {
  const factory = await createExecutionSessionFactory();
  const request = {
    execution: { runtime_id: "unknown" },
  } as unknown as RunRequest;
  await expect(factory(request, {
    eventWriter: new HarnessEventWriter({ run_id: "r", attempt_id: "a" }, { write: () => true }),
    signal: new AbortController().signal,
  })).rejects.toThrow("execution specification");
  expect(adapters.claude).not.toHaveBeenCalled();
  expect(adapters.codex).not.toHaveBeenCalled();
});
