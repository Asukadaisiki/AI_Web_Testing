import { genericExecution } from "./fixtures/selected-harness.js";
import { Readable, Writable } from "node:stream";
import { createAgentSession } from "@earendil-works/pi-coding-agent";
import { afterEach, describe, expect, it, vi } from "vitest";

import { serverMain, type HarnessControl } from "../src/server.js";
import { HarnessEventWriter, type HarnessEvent, type RunRequest } from "../src/run-protocol.js";
import { createExecutionSessionFactory } from "../src/session-factory.js";
import { main } from "../src/main.js";

const request: RunRequest = {
  execution: genericExecution,
  protocol_version: "pi-browser-harness.v1",
  run_id: "lightweight",
  attempt_id: "lightweight-attempt",
  goal: "Reply with a short answer",
  model: { provider: "scripted", name: "lightweight", reasoning_effort: "off" },
};

describe("switchable harness", () => {
  afterEach(() => vi.unstubAllEnvs());

  it("runs the selected snapshot through the shared server", async () => {
    const events: (HarnessEvent | HarnessControl)[] = [];
    const input = new Readable({ read() {} });
    const output = new Writable({
      write(chunk, _encoding, done) {
        const event: HarnessEvent | HarnessControl = JSON.parse(chunk.toString());
        events.push(event);
        if (event.type === "harness.released") input.push(null);
        done();
      },
    });
    const running = main({ argv: ["--server"], input, output });
    input.push(`${JSON.stringify({ type: "run", request })}\n`);
    expect(await running).toBe(0);
    expect(events.find((event) => event.type === "harness.settled")?.payload.verdict)
      .toBe("succeeded");
    expect(events.some((event) => event.type.startsWith("browser."))).toBe(false);
  });

  it("does not re-register a sibling attempt's model in the same mutable runtime", async () => {
    const received: Parameters<typeof createAgentSession>[0][] = [];
    const factory = await createExecutionSessionFactory({
      createAgentSession: async (options) => {
        received.push(options);
        return createAgentSession(options);
      },
    });
    const sessions = await Promise.all(["first-model", "second-model"].map(async (name) => {
      const run = { ...request, attempt_id: name, model: { ...request.model, name } };
      return factory(run, {
        signal: new AbortController().signal,
        eventWriter: new HarnessEventWriter(run, new Writable({
          write(_chunk, _encoding, done) { done(); },
        })),
      });
    }));
    try {
      expect(received[0]!.modelRuntime).not.toBe(received[1]!.modelRuntime);
      for (const options of received) {
        expect(options!.modelRuntime!.getModel("scripted", options!.model!.id)?.id)
          .toBe(options!.model!.id);
      }
    } finally {
      await Promise.all(sessions.map((session) => session.close()));
    }
  });

  it("runs a lightweight harness through the real Pi adapter and generic server observer", async () => {
    const events: (HarnessEvent | HarnessControl)[] = [];
    const output = new Writable({
      write(chunk, _encoding, done) { events.push(JSON.parse(chunk.toString())); done(); },
    });
    const finished = new Promise<void>((resolve) => {
      output.on("finish", resolve);
    });
    const input = new Readable({ read() {} });
    const result = serverMain({
      input, output,
      sessionFactory: await createExecutionSessionFactory(),
    });
    input.push(`${JSON.stringify({ type: "run", request })}\n`);
    const released = new Promise<void>((resolve) => {
      const timer = setInterval(() => {
        if (events.some((event) => event.type === "harness.released")) {
          clearInterval(timer);
          resolve();
        }
      }, 5);
      timer.unref();
    });
    await released;
    input.push(null);
    expect(await result).toBe(0);
    output.end();
    await finished;
    expect(events.some((event) => event.type.startsWith("browser."))).toBe(false);
    expect(events.find((event) => event.type === "harness.settled")?.payload).toMatchObject({
      verdict: "succeeded",
    });
    expect(events.map((event) => event.type)).toEqual([
      "harness.ready", "harness.started", "pi.message.completed",
      "pi.usage", "harness.settled", "harness.released",
    ]);
  }, 30_000);

  it("preserves explicit attempt IDs and creates independent settings and extension runtimes", async () => {
    const sessions = await Promise.all(["one", "two"].map(async (id) => {
      let options!: Parameters<typeof createAgentSession>[0];
      const run = { ...request, attempt_id: id };
      const factory = await createExecutionSessionFactory({
        createAgentSession: async (received) => { options = received; return createAgentSession(received); },
      });
      const session = await factory(run, {
        signal: new AbortController().signal,
        eventWriter: new HarnessEventWriter(run, new Writable({
          write(_chunk, _encoding, done) { done(); },
        })),
      });
      return { session, options };
    }));
    try {
      expect(sessions.map(({ options }) => options?.sessionManager?.getSessionId())).toEqual(["one", "two"]);
      expect(sessions[0]!.options?.settingsManager).not.toBe(sessions[1]!.options?.settingsManager);
      expect(sessions[0]!.options?.resourceLoader!.getExtensions().runtime)
        .not.toBe(sessions[1]!.options?.resourceLoader!.getExtensions().runtime);
      expect(sessions[0]!.options?.tools).toEqual([]);
    } finally {
      await Promise.all(sessions.map(({ session }) => session.close()));
    }
  });
});
