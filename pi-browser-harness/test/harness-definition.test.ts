import { Readable, Writable } from "node:stream";
import { createAgentSession, ModelRuntime } from "@earendil-works/pi-coding-agent";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { HarnessDefinition } from "../src/harness-definition.js";
import { createPiSession } from "../src/pi-session.js";
import { serverMain, type HarnessControl } from "../src/server.js";
import { HarnessEventWriter, type HarnessEvent, type RunRequest } from "../src/run-protocol.js";
import { createExecutionSessionFactory, loadHarnessDefinition } from "../src/session-factory.js";
import { main } from "../src/main.js";

const request: RunRequest = {
  protocol_version: "pi-browser-harness.v1",
  run_id: "lightweight",
  attempt_id: "lightweight-attempt",
  goal: "Reply with a short answer",
  model: { provider: "scripted", name: "lightweight", reasoning_effort: "off" },
};

const lightweight: HarnessDefinition = {
  systemPrompt: "Reply directly. Do not use tools.",
  skillPaths: [],
  toolAllowlist: [],
  mcpServers: [],
  completionPolicy: ({ assistant_settled, aborted }) => ({
    verdict: assistant_settled && !aborted ? "succeeded" : "failed",
  }),
};

describe("switchable harness", () => {
  afterEach(() => vi.unstubAllEnvs());

  it("selects a trusted startup module for server mode and one-shot mode", async () => {
    vi.stubEnv("AGENTTRACE_HARNESS_MODULE",
      new URL("./fixtures/lightweight-harness.mjs", import.meta.url).pathname);
    for (const argv of [[], ["--server"]]) {
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
      const running = main({ argv, input, output });
      input.push(`${JSON.stringify(argv.length ? { type: "run", request } : request)}\n`);
      if (!argv.length) input.push(null);
      expect(await running).toBe(0);
      expect(events.find((event) => event.type === "harness.settled")?.payload.verdict)
        .toBe("succeeded");
      expect(events.some((event) => event.type.startsWith("browser."))).toBe(false);
    }
  });

  it("rejects a startup module without a completion policy", async () => {
    await expect(loadHarnessDefinition(
      new URL("./fixtures/invalid-harness.mjs", import.meta.url).href,
    )).rejects.toThrow("default export a HarnessDefinition");
  });

  it("does not re-register a sibling attempt's model in the same mutable runtime", async () => {
    const received: Parameters<typeof createAgentSession>[0][] = [];
    const factory = await createExecutionSessionFactory({
      definition: lightweight,
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
      sessionFactory: async (run, { eventWriter }) => createPiSession(run, {
        createAgentSession,
        modelRuntime: await ModelRuntime.create({ refreshOnCreate: false }),
        eventWriter,
      }, lightweight),
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
      const session = await createPiSession(run, {
        modelRuntime: await ModelRuntime.create({ refreshOnCreate: false }),
        eventWriter: new HarnessEventWriter(run, new Writable({
          write(_chunk, _encoding, done) { done(); },
        })),
        createAgentSession: async (received) => { options = received; return createAgentSession(received); },
      }, lightweight);
      return { session, options };
    }));
    try {
      expect(sessions.map(({ options }) => options?.sessionManager?.getSessionId())).toEqual(["one", "two"]);
      expect(sessions[0]!.options?.settingsManager).not.toBe(sessions[1]!.options?.settingsManager);
      expect(sessions[0]!.options?.resourceLoader!.getExtensions().runtime)
        .not.toBe(sessions[1]!.options?.resourceLoader!.getExtensions().runtime);
      expect(sessions[0]!.session.getActiveToolNames()).toEqual([]);
    } finally {
      await Promise.all(sessions.map(({ session }) => session.close()));
    }
  });
});
