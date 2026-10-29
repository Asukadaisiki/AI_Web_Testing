import { Readable, Writable } from "node:stream";
import { afterEach, expect, it, vi } from "vitest";
import { main } from "../src/main.js";
import type { HarnessEvent } from "../src/run-protocol.js";
import type { HarnessControl } from "../src/server.js";
import { genericExecution } from "./fixtures/selected-harness.js";

// A replacement must boot even when the browser implementation/resources are unavailable.
vi.mock("../src/browser-harness.js", () => { throw new Error("browser resources are unavailable"); });
vi.mock("../src/browser/contract.js", () => { throw new Error("browser contracts are unavailable"); });

afterEach(() => vi.unstubAllEnvs());

it("boots a replacement harness without loading the browser implementation", async () => {
  const input = new Readable({ read() {} });
  const events: (HarnessEvent | HarnessControl)[] = [];
  const output = new Writable({
    write(chunk, _encoding, done) {
      const event: typeof events[number] = JSON.parse(chunk.toString());
      events.push(event);
      if (event.type === "harness.released") input.push(null);
      done();
    },
  });
  const running = main({ argv: ["--server"], input, output });
  input.push(`${JSON.stringify({
    type: "run",
    request: {
      protocol_version: "pi-browser-harness.v1", run_id: "replacement", attempt_id: "attempt",
      goal: "answer",
      model: { provider: "scripted", name: "replacement", reasoning_effort: "off" },
      execution: genericExecution,
    },
  })}\n`);
  expect(await running).toBe(0);
  expect(events.find((event) => event.type === "harness.settled")?.payload.verdict).toBe("succeeded");
  const message = events.find((event) => event.type === "pi.message.completed");
  expect(message?.payload).toMatchObject({
    text: "Final: request completed.",
    input: { messages: expect.arrayContaining([expect.objectContaining({ role: "user" })]) },
    started_at: expect.any(String),
    ended_at: expect.any(String),
  });
  expect(events.some((event) => event.type.startsWith("browser."))).toBe(false);
  expect(events.find((event) => event.type === "pi.usage")?.payload).not.toHaveProperty("cost");
});
