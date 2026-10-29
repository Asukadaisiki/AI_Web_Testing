import { PassThrough, Readable, Writable } from "node:stream";
import { EventEmitter } from "node:events";
import { describe, expect, it, vi } from "vitest";
import { main } from "../src/main.js";
import type { ExecutionSessionFactory } from "../src/server.js";
import type { RunRequest } from "../src/run-protocol.js";
import { genericExecution } from "./fixtures/selected-harness.js";

const request: RunRequest = {
  protocol_version: "pi-browser-harness.v1", run_id: "r", attempt_id: "a", goal: "answer",
  model: { provider: "scripted", name: "fixture", reasoning_effort: "off" },
  execution: genericExecution,
};

function transport(factory: ExecutionSessionFactory) {
  const input = new PassThrough();
  const events: { type: string; payload?: unknown }[] = [];
  const signals = new EventEmitter();
  const output = new Writable({ write(chunk, _encoding, done) {
    events.push(JSON.parse(chunk.toString()));
    done();
  } });
  const running = main({
    argv: ["--server"], input, output, process: signals as NodeJS.Process, sessionFactory: factory,
  });
  return { input, events, signals, running };
}

describe("shared Host CLI", () => {
  it("--server dispatches without constructing a session when no run is submitted", async () => {
    const events: unknown[] = [];
    expect(await main({
      argv: ["--server"], sessionFactory: async () => { throw new Error("no runs submitted"); },
      input: Readable.from(['{"type":"shutdown"}\n']),
      output: new Writable({ write(chunk, _encoding, done) {
        events.push(JSON.parse(chunk.toString())); done();
      } }),
    })).toBe(0);
    expect(events).toEqual([{ type: "harness.ready" }]);
  });

  it("writes only JSONL and acknowledges release after settlement and close", async () => {
    let closed = false;
    const t = transport(async (_run, { eventWriter }) => ({
      async prompt() {
        eventWriter.write("pi.message.completed", { text: "done" });
        eventWriter.write("harness.settled", { assistant_text: "done", verdict: "succeeded" });
      },
      async abort() {},
      async close() { closed = true; },
    }));
    t.input.write(`${JSON.stringify({ type: "run", request })}\n`);
    await vi.waitFor(() => expect(t.events.at(-1)?.type).toBe("harness.released"));
    expect(closed).toBe(true);
    t.input.end();
    expect(await t.running).toBe(0);
    expect(t.events.map((event) => event.type)).toEqual([
      "harness.ready", "harness.started", "pi.message.completed", "harness.settled", "harness.released",
    ]);
  });

  it("malformed commands produce one protocol error and nonzero exit", async () => {
    const t = transport(async () => { throw new Error("must not execute"); });
    t.input.end("not-json\n");
    expect(await t.running).toBe(1);
    expect(t.events.map((event) => event.type)).toEqual(["harness.ready", "harness.protocol_error"]);
  });

  it.each([["SIGTERM", 143], ["SIGINT", 130]] as const)(
    "%s aborts and awaits close exactly once", async (signal, code) => {
      let finish = () => {};
      const prompting = vi.fn(async () => new Promise<void>((resolve) => { finish = resolve; }));
      const abort = vi.fn(async () => finish());
      const close = vi.fn(async () => {});
      const t = transport(async () => ({ prompt: prompting, abort, close }));
      t.input.write(`${JSON.stringify({ type: "run", request })}\n`);
      await vi.waitFor(() => expect(prompting).toHaveBeenCalledTimes(1));
      t.signals.emit(signal);
      t.signals.emit(signal);
      expect(await t.running).toBe(code);
      expect(abort).toHaveBeenCalledTimes(1);
      expect(close).toHaveBeenCalledTimes(1);
      expect(t.events.map((event) => event.type)).toEqual([
        "harness.ready", "harness.started", "harness.failed", "harness.released",
      ]);
    },
  );
});
