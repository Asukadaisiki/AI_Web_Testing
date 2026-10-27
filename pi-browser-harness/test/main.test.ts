import { Readable, Writable } from "node:stream";
import { EventEmitter } from "node:events";

import { describe, expect, it, vi } from "vitest";

import { ModelRuntime } from "@earendil-works/pi-coding-agent";

import { main } from "../src/main.js";
import type { RunRequest } from "../src/run-protocol.js";

const validRequest: RunRequest = {
  protocol_version: "pi-browser-harness.v1",
  run_id: "run_1",
  attempt_id: "attempt_1",
  goal: "verify the cart",
  model: {
    provider: "scripted",
    name: "browser-fixture",
    reasoning_effort: "high",
  },
  worker_url: "http://worker:8100",
  artifact_scope: "run_1",
};

function streamInput(lines: string[]): Readable {
  return Readable.from(lines.map((l) => `${l}\n`));
}

function collectingOutput(): { stream: Writable; chunks: () => string } {
  const buffer: string[] = [];
  const stream = new Writable({
    write(chunk, _encoding, callback) {
      buffer.push(chunk.toString());
      callback();
    },
  });
  return { stream, chunks: () => buffer.join("") };
}

/** A fake session that records calls and can hang on prompt. */
function makeFakeSession(opts: { hang?: boolean } = {}) {
  const calls = { abort: 0, dispose: 0, prompt: 0 };
  let listener: ((event: unknown) => void) | undefined;
  let releaseHang: (() => void) | undefined;
  const session = {
    prompt: vi.fn(async () => {
      calls.prompt += 1;
      if (opts.hang) {
        await new Promise<void>((resolve) => {
          releaseHang = resolve;
        });
      }
    }),
    abort: vi.fn(async () => {
      calls.abort += 1;
      releaseHang?.();
    }),
    dispose: vi.fn(() => {
      calls.dispose += 1;
    }),
    bindExtensions: vi.fn().mockResolvedValue(undefined),
    subscribe: vi.fn((fn: (event: unknown) => void) => {
      listener = fn;
      return () => {};
    }),
    emit(event: unknown) {
      listener?.(event);
    },
    getActiveToolNames: vi.fn(() => []),
    getVerdict: vi.fn((): "succeeded" | "failed" => "failed"),
  };
  return { session, calls };
}

function makeFakeProcess() {
  const emitter = new EventEmitter();
  let exitCode: number | undefined;
  const fakeProc = {
    on: (event: string, handler: (...args: unknown[]) => void) =>
      emitter.on(event, handler),
    off: (event: string, handler: (...args: unknown[]) => void) =>
      emitter.off(event, handler),
    exit: (code?: number) => {
      exitCode = code ?? 0;
      // In tests we do not actually terminate; the caller observes exitCode.
    },
  } as unknown as NodeJS.Process;
  return { fakeProc, emitter, getExitCode: () => exitCode };
}

async function makeDeps(
  session: ReturnType<typeof makeFakeSession>["session"],
) {
  const runtime = await ModelRuntime.create({ refreshOnCreate: false });
  return {
    createAgentSession: vi.fn().mockResolvedValue({
      session,
      extensionsResult: {},
    }),
    modelRuntime: runtime,
  };
}

describe("main CLI", () => {
  it("writes only valid JSONL to stdout for a valid request", async () => {
    const { session } = makeFakeSession();
    const { stream: stdout, chunks } = collectingOutput();
    const stderrChunks: string[] = [];
    const stderr = new Writable({
      write(chunk, _enc, cb) {
        stderrChunks.push(chunk.toString());
        cb();
      },
    });

    const code = await main({
      deps: await makeDeps(session),
      input: streamInput([JSON.stringify(validRequest)]),
      output: stdout,
      errorSink: stderr,
    });

    const lines = chunks().trimEnd().split("\n");
    for (const line of lines) {
      expect(() => JSON.parse(line)).not.toThrow();
    }
    // stdout contains the started and settled events; no stray text.
    const types = lines.map((l) => JSON.parse(l).type);
    expect(types).toContain("harness.started");
    // stderr is empty for a clean run.
    expect(stderrChunks.join("")).toBe("");
    // No passing finish evidence -> non-zero exit.
    expect(code).toBe(1);
  });

  it("emits exactly one harness.failed for malformed stdin and exits non-zero", async () => {
    const { stream: stdout, chunks } = collectingOutput();
    const { fakeProc, getExitCode } = makeFakeProcess();

    const codePromise = main({
      input: streamInput(["not-json"]),
      output: stdout,
      process: fakeProc,
    });

    // readRequest calls process.exit(1) on malformed input.
    await expect(codePromise).rejects.toThrow();
    // In tests process.exit is replaced, so the promise rejects when exit is
    // called; the failure event was already written.
    const lines = chunks().trimEnd().split("\n").filter(Boolean);
    const failedEvents = lines
      .map((l) => JSON.parse(l))
      .filter((e) => e.type === "harness.failed");
    expect(failedEvents).toHaveLength(1);
    expect(getExitCode()).toBe(1);
  });

  it("SIGTERM aborts and disposes exactly once with exit code 143", async () => {
    const { session, calls } = makeFakeSession({ hang: true });
    const { stream: stdout } = collectingOutput();
    const { fakeProc, emitter, getExitCode } = makeFakeProcess();

    const codePromise = main({
      deps: await makeDeps(session),
      input: streamInput([JSON.stringify(validRequest)]),
      output: stdout,
      process: fakeProc,
    });

    // Let the run start and hang on prompt.
    await new Promise((resolve) => setTimeout(resolve, 10));
    emitter.emit("SIGTERM");
    emitter.emit("SIGTERM");

    // The SIGTERM handler aborts and disposes; main then returns normally
    // (the fake process.exit does not terminate).
    await codePromise;
    await vi.waitFor(() => expect(getExitCode()).toBe(143));
    expect(calls.abort).toBe(1);
    expect(calls.dispose).toBe(1);
  });

  it("SIGINT aborts and disposes exactly once with exit code 130", async () => {
    const { session, calls } = makeFakeSession({ hang: true });
    const { stream: stdout } = collectingOutput();
    const { fakeProc, emitter, getExitCode } = makeFakeProcess();

    const codePromise = main({
      deps: await makeDeps(session),
      input: streamInput([JSON.stringify(validRequest)]),
      output: stdout,
      process: fakeProc,
    });

    // Let the run start and hang on prompt.
    await new Promise((resolve) => setTimeout(resolve, 10));
    emitter.emit("SIGINT");
    emitter.emit("SIGINT");

    // The Go runtime sends SIGINT on cancellation; the handler must abort and
    // dispose the session just like SIGTERM.
    await codePromise;
    await vi.waitFor(() => expect(getExitCode()).toBe(130));
    expect(calls.abort).toBe(1);
    expect(calls.dispose).toBe(1);
  });

  it("exits non-zero when the assistant settles without passing finish evidence", async () => {
    const { session } = makeFakeSession();
    // Emit settlement but no passing browser case verdict.
    session.prompt.mockImplementation(async () => {
      session.emit({ type: "agent_settled", aborted: false });
    });
    const { stream: stdout } = collectingOutput();

    const code = await main({
      deps: await makeDeps(session),
      input: streamInput([JSON.stringify(validRequest)]),
      output: stdout,
    });

    expect(code).toBe(1);
  });

  it("exits zero when the assistant settles with passing finish evidence", async () => {
    const { session } = makeFakeSession();
    session.prompt.mockImplementation(async () => {
      session.emit({
        type: "tool_execution_end",
        toolCallId: "call_1",
        toolName: "mcp__browser_harness__finish_case",
        result: {
          structuredContent: {
            ok: true,
            case: {
              case_version: "loop.case.v1",
              name: "open example",
              goal: "open the example page",
              base_url: "https://example.test",
              steps: [{
                index: 0,
                action: "goto",
                intent: "open the example page",
                value: "https://example.test",
                submit: false,
                preconditions: [],
                postconditions: [{
                  type: "url_contains",
                  value: "example.test",
                  timeout_ms: 1000,
                }],
                timeout_ms: 1000,
              }],
            },
            dry_run: {
              execution_id: "execution-1",
              status: "passed",
              started_at: "2026-10-26T12:00:00.000Z",
              finished_at: "2026-10-26T12:00:01.000Z",
              final_url: "https://example.test",
              steps: [{
                index: 0,
                action: "goto",
                status: "passed",
                started_at: "2026-10-26T12:00:00.000Z",
                duration_ms: 10,
                url_before: "about:blank",
                url_after: "https://example.test",
                conditions: [],
                evidence: { screenshot_path: null, console: [], network: [] },
                error: null,
              }],
              error: null,
            },
          },
        },
        isError: false,
      });
      session.emit({ type: "agent_settled", aborted: false });
    });
    const { stream: stdout } = collectingOutput();

    const code = await main({
      deps: await makeDeps(session),
      input: streamInput([JSON.stringify(validRequest)]),
      output: stdout,
    });

    expect(code).toBe(0);
  });
});
