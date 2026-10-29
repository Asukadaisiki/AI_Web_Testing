import { genericExecution } from "./fixtures/selected-harness.js";
import { EventEmitter } from "node:events";
import { PassThrough, Writable } from "node:stream";
import { describe, expect, it, vi } from "vitest";

import {
  serverMain, SessionCleanupError, type ExecutionSessionFactory, type HarnessControl,
} from "../src/server.js";
import type { HarnessEvent, RunRequest } from "../src/run-protocol.js";

const request: RunRequest = {
  execution: genericExecution,
  protocol_version: "pi-browser-harness.v1",
  run_id: "run",
  attempt_id: "one",
  goal: "answer",
  model: { provider: "scripted", name: "fixture", reasoning_effort: "off" },
  worker_url: "http://unused.invalid",
  artifact_scope: "run",
};

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
}

function transport(factory: ExecutionSessionFactory) {
  const input = new PassThrough();
  const events: (HarnessEvent | HarnessControl)[] = [];
  const output = new Writable({
    write(chunk, _encoding, done) {
      events.push(JSON.parse(chunk.toString()));
      done();
    },
  });
  const signals = new EventEmitter();
  const running = serverMain({
    input, output, sessionFactory: factory, process: signals,
  });
  const send = (value: unknown) => input.write(`${JSON.stringify(value)}\n`);
  return { input, events, running, send, signals };
}

describe("shared execution server", () => {
  it("overlaps runs, routes cancellation by run and attempt, and releases only after close", async () => {
    const finishOne = deferred();
    const finishTwo = deferred();
    const closeOne = deferred();
    const order: string[] = [];
    const t = transport(async (run, { eventWriter }) => ({
      async prompt() {
        order.push(`prompt:${run.run_id}`);
        await (run.run_id === "run" ? finishOne : finishTwo).promise;
        eventWriter.write("pi.message.completed", { text: run.run_id });
        eventWriter.write("harness.settled", {
          assistant_text: run.run_id, verdict: "succeeded",
        });
      },
      async abort() {
        order.push(`abort:${run.run_id}`);
        (run.run_id === "run" ? finishOne : finishTwo).resolve();
      },
      async close() {
        order.push(`close:${run.run_id}`);
        if (run.run_id === "run") await closeOne.promise;
      },
    }));
    t.send({ type: "run", request });
    t.send({ type: "run", request: { ...request, run_id: "other" } });
    await vi.waitFor(() => expect(order).toEqual(["prompt:run", "prompt:other"]));
    t.send({ type: "cancel", run_id: "run", attempt_id: "one" });
    t.send({ type: "cancel", run_id: "run", attempt_id: "one" });
    await vi.waitFor(() => expect(order).toContain("close:run"));
    expect(order.filter((entry) => entry === "abort:run")).toHaveLength(1);
    expect(order).not.toContain("abort:other");
    expect(t.events.filter((event) => event.type === "harness.released")).toEqual([]);
    finishTwo.resolve();
    await vi.waitFor(() => expect(t.events.filter(
      (event) => event.type === "harness.released",
    )).toEqual([{ type: "harness.released", run_id: "other", attempt_id: "one" }]));
    closeOne.resolve();
    await vi.waitFor(() => expect(t.events.filter(
      (event) => event.type === "harness.released",
    )).toHaveLength(2));
    t.send({ type: "shutdown" });
    expect(await t.running).toBe(0);
    expect(t.events[0]).toEqual({ type: "harness.ready" });
    for (const runId of ["run", "other"]) {
      const trace = t.events.filter((event): event is HarnessEvent =>
        "sequence" in event && event.run_id === runId);
      expect(trace.map((event) => event.sequence)).toEqual([1, 2, 3]);
      expect(trace.filter((event) => ["harness.failed", "harness.settled"].includes(event.type)))
        .toHaveLength(1);
    }
  });

  it("retains targeted cancellation during initialization and skips prompt", async () => {
    const initialized = deferred();
    const closed = deferred();
    const order: string[] = [];
    const t = transport(async (_run, { signal }) => {
      order.push("initializing");
      await initialized.promise;
      expect(signal.aborted).toBe(true);
      return {
        async prompt() { order.push("prompt"); },
        async abort() { order.push("abort"); },
        async close() { await closed.promise; order.push("closed"); },
      };
    });
    t.send({ type: "run", request });
    t.send({ type: "cancel", run_id: "run", attempt_id: "one" });
    t.send({ type: "cancel", run_id: "run", attempt_id: "one" });
    await vi.waitFor(() => expect(order).toEqual(["initializing"]));
    initialized.resolve();
    await vi.waitFor(() => expect(order).toEqual(["initializing", "abort"]));
    expect(t.events.some((event) => event.type === "harness.released")).toBe(false);
    closed.resolve();
    await vi.waitFor(() => expect(t.events.at(-1)?.type).toBe("harness.released"));
    t.input.end();
    expect(await t.running).toBe(0);
    expect(t.events.filter((event) => event.type === "harness.failed")).toHaveLength(1);
    expect(order).toEqual(["initializing", "abort", "closed"]);
  });

  it.each(["EOF", "shutdown", "SIGINT", "SIGTERM"])("drains active work on %s", async (stop) => {
    const prompted = deferred();
    const aborted = deferred();
    const close = deferred();
    let returned = false;
    const t = transport(async () => ({
      async prompt() { prompted.resolve(); await aborted.promise; },
      async abort() { aborted.resolve(); },
      async close() { await close.promise; },
    }));
    t.send({ type: "run", request });
    await prompted.promise;
    if (stop === "EOF") t.input.end();
    else if (stop === "shutdown") t.send({ type: "shutdown" });
    else t.signals.emit(stop);
    void t.running.then(() => { returned = true; });
    await aborted.promise;
    expect(returned).toBe(false);
    expect(t.events.some((event) => event.type === "harness.released")).toBe(false);
    close.resolve();
    expect(await t.running).toBe(stop === "SIGINT" ? 130 : stop === "SIGTERM" ? 143 : 0);
    expect(t.events.at(-1)?.type).toBe("harness.released");
    expect(t.signals.listenerCount("SIGINT")).toBe(0);
  });

  it("isolates initialization failures and suppresses traces after the first terminal", async () => {
    const t = transport(async (run, { eventWriter }) => {
      if (run.attempt_id === "one") throw new Error("initialization failed");
      return {
        async prompt() {
          eventWriter.write("harness.settled", { assistant_text: "done", verdict: "succeeded" });
          eventWriter.write("harness.settled", { assistant_text: "duplicate", verdict: "succeeded" });
          throw new Error("late failure");
        },
        async abort() {},
        async close() {},
      };
    });
    t.send({ type: "run", request });
    t.send({ type: "run", request: { ...request, attempt_id: "two" } });
    await vi.waitFor(() => expect(t.events.filter((event) => event.type === "harness.released")).toHaveLength(2));
    t.input.end();
    expect(await t.running).toBe(0);
    expect(t.events.filter((event) => event.type === "harness.failed")).toHaveLength(1);
    expect(t.events.filter((event) => event.type === "harness.settled")).toHaveLength(1);
  });

  it.each([
    "not-json",
    JSON.stringify({ type: "run", request: {} }),
    JSON.stringify({ type: "cancel", run_id: "run" }),
    JSON.stringify({ type: "shutdown", extra: true }),
    JSON.stringify({ type: "unknown" }),
  ])("treats malformed commands as fatal transport errors: %s", async (raw) => {
    const t = transport(async () => { throw new Error("must not create session"); });
    t.input.write(`${raw}\n`);
    expect(await t.running).toBe(1);
    expect(t.events.map((event) => event.type)).toEqual(["harness.ready", "harness.protocol_error"]);
  });

  it("fails the transport if cleanup fails instead of falsely acknowledging release", async () => {
    const t = transport(async (_run, { eventWriter }) => ({
      async prompt() { eventWriter.write("harness.settled", { assistant_text: "done", verdict: "succeeded" }); },
      async abort() {},
      async close() { throw new Error("cleanup failed"); },
    }));
    t.send({ type: "run", request });
    expect(await t.running).toBe(1);
    expect(t.events.at(-1)).toEqual({ type: "harness.protocol_error", error: "cleanup failed" });
    expect(t.events.some((event) => event.type === "harness.released")).toBe(false);
  });

  it("does not acknowledge release when a rejected factory reports incomplete cleanup", async () => {
    const t = transport(async () => { throw new SessionCleanupError("initialization cleanup failed"); });
    t.send({ type: "run", request });
    expect(await t.running).toBe(1);
    expect(t.events.at(-1)).toEqual({
      type: "harness.protocol_error", error: "initialization cleanup failed",
    });
    expect(t.events.some((event) => event.type === "harness.released")).toBe(false);
  });
});
