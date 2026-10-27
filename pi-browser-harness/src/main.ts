#!/usr/bin/env node
import { createInterface } from "node:readline/promises";
import type { Readable, Writable } from "node:stream";
import { stdin as defaultInput, stdout as defaultOutput, stderr } from "node:process";

import {
  HarnessEventWriter,
  parseRunRequest,
  type RunRequest,
} from "./run-protocol.js";
import {
  createBrowserSession,
  type BrowserSessionDeps,
} from "./pi-session.js";
import {
  ModelRuntime,
  createAgentSession,
} from "@earendil-works/pi-coding-agent";

export interface MainOptions {
  deps?: Pick<BrowserSessionDeps, "createAgentSession" | "modelRuntime">;
  input?: Readable;
  output?: Writable;
  errorSink?: Writable;
  /** Process-like object for signal registration (tests inject a fake). */
  process?: NodeJS.Process;
}

/**
 * Runs one harness attempt: reads a RunRequest from stdin, creates a Pi
 * browser session, prompts it with the goal, and exits with a verdict code.
 *
 * stdout carries only JSONL harness events; diagnostics go to stderr.
 */
export async function main(options: MainOptions = {}): Promise<number> {
  const input = options.input ?? defaultInput;
  const output = options.output ?? defaultOutput;
  const errorSink = options.errorSink ?? stderr;
  const proc = options.process ?? process;

  const resolved = options.deps ?? {
    createAgentSession,
    modelRuntime: await ModelRuntime.create({ refreshOnCreate: false }),
  };

  const request = await readRequest(input, output, errorSink, proc);
  const eventWriter = new HarnessEventWriter(request, output);

  eventWriter.write("harness.started", {});

  const session = await createBrowserSession(request, {
    ...resolved,
    eventWriter,
    stderr: errorSink,
  });

  let settled = false;
  let disposed = false;
  const disposeOnce = (): void => {
    if (disposed) {
      return;
    }
    disposed = true;
    session.dispose();
  };
  let shutdownStarted = false;
  const onShutdown = async (exitCode: number): Promise<void> => {
    if (shutdownStarted) {
      return;
    }
    shutdownStarted = true;
    try {
      await session.abort();
    } catch {
      // best effort
    }
    disposeOnce();
    proc.exit(exitCode);
  };
  // The Go runtime signals the process group with SIGINT on cancellation;
  // handle both SIGTERM and SIGINT so the Pi session is gracefully aborted
  // and disposed before exit.
  const onSigterm = (): void => void onShutdown(143);
  const onSigint = (): void => void onShutdown(130);
  proc.on("SIGTERM", onSigterm);
  proc.on("SIGINT", onSigint);

  try {
    await session.prompt(request.goal);
    settled = true;
  } catch (error) {
    eventWriter.write("harness.failed", {
      error: error instanceof Error ? error.message : String(error),
    });
  } finally {
    proc.off("SIGTERM", onSigterm);
    proc.off("SIGINT", onSigint);
    disposeOnce();
  }

  if (!settled) {
    return 1;
  }

  const verdict = session.getVerdict();
  return verdict === "succeeded" ? 0 : 1;
}

async function readRequest(
  input: Readable,
  output: Writable,
  errorSink: Writable,
  proc: NodeJS.Process,
): Promise<RunRequest> {
  const rl = createInterface({ input, output: undefined as never });
  const raw = (await rl[Symbol.asyncIterator]().next()).value as
    | string
    | undefined;
  rl.close();

  if (raw === undefined) {
    throw new Error("no run request on stdin");
  }

  try {
    return parseRunRequest(raw);
  } catch (error) {
    // Malformed input: emit exactly one harness.failed on stdout and exit.
    const writer = new HarnessEventWriter(
      { run_id: "unknown", attempt_id: "unknown" },
      output,
    );
    writer.write("harness.failed", {
      error: error instanceof Error ? error.message : String(error),
    });
    void errorSink;
    proc.exit(1);
  }
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main().then(
    (code) => process.exit(code),
    (error) => {
      stderr.write(
        error instanceof Error ? error.stack ?? error.message : String(error),
      );
      process.exit(1);
    },
  );
}
