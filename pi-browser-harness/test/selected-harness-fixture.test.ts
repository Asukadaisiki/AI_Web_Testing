import { existsSync } from "node:fs";
import { rm } from "node:fs/promises";
import { Writable } from "node:stream";
import { ModelRuntime } from "@earendil-works/pi-coding-agent";
import { afterEach, expect, it, vi } from "vitest";
import { HarnessEventWriter, type RunRequest } from "../src/run-protocol.js";
import { SessionCleanupError } from "../src/server.js";
import {
  browserExecution, cleanupSelectedHarnessFixtures, createSelectedBrowserSession, environmentLease,
} from "./fixtures/selected-harness.js";

const request: RunRequest = {
  execution: browserExecution, ...environmentLease,
  protocol_version: "pi-browser-harness.v1", run_id: "fixture-cleanup", attempt_id: "fixture-cleanup",
  goal: "test fixture cleanup", worker_url: "http://unused.invalid", artifact_scope: "fixture-cleanup",
  model: { provider: "scripted", name: "fixture", reasoning_effort: "off" },
};
const directories: string[] = [];
afterEach(async () => {
  // These tests inspect retained uncertain resources before removing their own doubles' directories.
  await Promise.all(directories.splice(0).map((cwd) => rm(cwd, { recursive: true, force: true })));
});

async function fixture(options: {
  bindFailure?: Error;
  shutdown?: () => Promise<void>;
  disposeFailure?: Error;
  expectedCleanupFailure?: (error: unknown) => boolean;
} = {}) {
  let cwd = "";
  const creating = createSelectedBrowserSession(request, {
    modelRuntime: await ModelRuntime.create({ refreshOnCreate: false }),
    eventWriter: new HarnessEventWriter(request, new Writable({
      write(_chunk, _encoding, done) { done(); },
    })),
    createAgentSession: async (settings) => {
      cwd = settings.cwd!;
      directories.push(cwd);
      return {
        session: {
          prompt: async () => {}, abort: async () => {},
          dispose() { if (options.disposeFailure) throw options.disposeFailure; },
          bindExtensions: async () => { if (options.bindFailure) throw options.bindFailure; },
          subscribe: () => () => {}, getActiveToolNames: () => [],
          extensionRunner: {
            emit: options.shutdown ?? (async () => {}), createToolContext: () => ({}),
            getToolDefinition: () => ({
              execute: async () => ({ structuredContent: { structuredContent: { closed: true } } }),
            }),
          },
        } as never,
        extensionsResult: {} as never,
      };
    },
  }, options.expectedCleanupFailure ? { expectedCleanupFailure: options.expectedCleanupFailure } : {});
  return { creating, get cwd() { return cwd; } };
}

it("collects ordinary and fatal cleanup failures while awaiting healthy sibling cleanup", async () => {
  const ordinaryFailure = new Error("SDK disposal failed");
  const first = await fixture({ disposeFailure: ordinaryFailure });
  await first.creating;
  const second = await fixture({ shutdown: async () => { throw new Error("shutdown uncertain"); } });
  await second.creating;
  let finish!: () => void;
  const pending = new Promise<void>((resolve) => { finish = resolve; });
  const sibling = await fixture({ shutdown: () => pending });
  await sibling.creating;
  vi.stubEnv("SELECTED_FIXTURE_CLEANUP_TEST", "stubbed");
  let settled = false;
  const cleanup = cleanupSelectedHarnessFixtures().catch((error: unknown) => {
    settled = true;
    return error;
  });
  try {
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(settled).toBe(false);
    expect(existsSync(sibling.cwd)).toBe(true);
  } finally {
    finish();
  }
  const failure = await cleanup;
  expect(failure).toBeInstanceOf(AggregateError);
  expect((failure as AggregateError).errors).toEqual([
    ordinaryFailure, expect.objectContaining({ message: expect.stringContaining("shutdown uncertain") }),
  ]);
  expect((failure as AggregateError).errors[1]).toBeInstanceOf(SessionCleanupError);
  expect(existsSync(first.cwd)).toBe(true);
  expect(existsSync(second.cwd)).toBe(true);
  expect(existsSync(sibling.cwd)).toBe(false);
  expect(process.env.SELECTED_FIXTURE_CLEANUP_TEST).toBeUndefined();
  await expect(cleanupSelectedHarnessFixtures()).resolves.toBeUndefined();
});

it("allows only the matched per-session failure without exempting a failing sibling", async () => {
  const expected = await fixture({
    shutdown: async () => { throw new Error("asserted shutdown failure"); },
    expectedCleanupFailure: (error) => error instanceof SessionCleanupError &&
      error.message === "asserted shutdown failure",
  });
  await expect((await expected.creating).close()).rejects.toThrow("asserted shutdown failure");
  const unexpected = await fixture({ shutdown: async () => { throw new Error("unexpected sibling failure"); } });
  await unexpected.creating;
  await expect(cleanupSelectedHarnessFixtures()).rejects.toMatchObject({
    errors: [expect.objectContaining({ message: "unexpected sibling failure" })],
  });
  expect(existsSync(expected.cwd)).toBe(false);
  expect(existsSync(unexpected.cwd)).toBe(true);
});

it("rejects a different failure from an opted-in session", async () => {
  const owned = await fixture({
    shutdown: async () => { throw new Error("wrong failure"); },
    expectedCleanupFailure: (error) => error instanceof SessionCleanupError &&
      error.message === "asserted shutdown failure",
  });
  await owned.creating;
  await expect(cleanupSelectedHarnessFixtures()).rejects.toMatchObject({
    errors: [expect.objectContaining({ message: "wrong failure" })],
  });
  expect(existsSync(owned.cwd)).toBe(true);
});

it("collects a throwing failure matcher and still cleans other sessions", async () => {
  const assertion = new Error("cleanup expectation failed");
  const owned = await fixture({
    shutdown: async () => { throw new Error("shutdown failed"); },
    expectedCleanupFailure: () => { throw assertion; },
  });
  await owned.creating;
  const sibling = await fixture();
  await sibling.creating;
  await expect(cleanupSelectedHarnessFixtures()).rejects.toMatchObject({ errors: [assertion] });
  expect(existsSync(owned.cwd)).toBe(true);
  expect(existsSync(sibling.cwd)).toBe(false);
});

it("reports initialization cleanup uncertainty even when creation rejection was caught", async () => {
  const owned = await fixture({
    bindFailure: new Error("bind failed"),
    shutdown: async () => { throw new Error("initialization shutdown failed"); },
  });
  const failure = await owned.creating.catch((error: unknown) => error);
  expect(failure).toBeInstanceOf(SessionCleanupError);
  await expect(cleanupSelectedHarnessFixtures()).rejects.toMatchObject({ errors: [failure] });
  expect(existsSync(owned.cwd)).toBe(true);
});

it("allows explicitly matched initialization cleanup failure without changing its fatal rejection", async () => {
  const owned = await fixture({
    bindFailure: new Error("bind failed"),
    shutdown: async () => { throw new Error("asserted initialization shutdown failed"); },
    expectedCleanupFailure: (error) => error instanceof SessionCleanupError &&
      error.message === "asserted initialization shutdown failed",
  });
  await expect(owned.creating).rejects.toBeInstanceOf(SessionCleanupError);
  await expect(cleanupSelectedHarnessFixtures()).resolves.toBeUndefined();
  expect(existsSync(owned.cwd)).toBe(false);
});

it("removes resources after ordinary initialization failure with acknowledged cleanup", async () => {
  const owned = await fixture({ bindFailure: new Error("bind failed") });
  await expect(owned.creating).rejects.toThrow("bind failed");
  await expect(cleanupSelectedHarnessFixtures()).resolves.toBeUndefined();
  expect(existsSync(owned.cwd)).toBe(false);
});
