#!/usr/bin/env node
import { createInterface } from "node:readline/promises";
import type { Readable, Writable } from "node:stream";
import { stdin as defaultInput, stdout as defaultOutput, stderr } from "node:process";
import { pathToFileURL } from "node:url";
import { HarnessEventWriter, parseRunRequest, type RunRequest } from "./run-protocol.js";
import type { PiSessionDeps } from "./pi-session.js";
import { createExecutionSessionFactory, loadHarnessDefinition } from "./session-factory.js";
import { serverMain, type ExecutionSession, type ExecutionSessionFactory } from "./server.js";

export interface MainOptions {
  deps?: Pick<PiSessionDeps, "createAgentSession" | "modelRuntime">;
  sessionFactory?: ExecutionSessionFactory;
  argv?: readonly string[];
  input?: Readable;
  output?: Writable;
  errorSink?: Writable;
  process?: NodeJS.Process;
}

/** Bootstrap owns SDK/harness selection; the server owns only generic lifetimes. */
export async function main(options: MainOptions = {}): Promise<number> {
  const input = options.input ?? defaultInput;
  const output = options.output ?? defaultOutput;
  const errorSink = options.errorSink ?? stderr;
  const proc = options.process ?? process;
  const serverMode = (options.argv ?? process.argv.slice(2)).includes("--server");
  const factory = options.sessionFactory ?? (options.deps
    ? await injectedFactory(options.deps, errorSink, serverMode)
    : await createExecutionSessionFactory({ stderr: errorSink }));
  if (serverMode) {
    return serverMain({ input, output, errorSink, process: proc, sessionFactory: factory });
  }
  const request = await readRequest(input, output, proc);
  const eventWriter = new HarnessEventWriter(request, output);
  const controller = new AbortController();
  let session: ExecutionSession | undefined;
  let aborting: Promise<void> | undefined;
  let signalCode: number | undefined;
  const stop = (code: number) => {
    if (signalCode !== undefined) return;
    signalCode = code;
    controller.abort();
    if (session) {
      aborting = Promise.resolve().then(() => session!.abort());
      void aborting.catch(() => {});
    }
  };
  const onInt = () => stop(130);
  const onTerm = () => stop(143);
  proc.on("SIGINT", onInt);
  proc.on("SIGTERM", onTerm);
  eventWriter.write("harness.started", {});
  try {
    session = await factory(request, { eventWriter, signal: controller.signal });
    if (controller.signal.aborted) {
      await session.abort();
      throw new Error("attempt cancelled");
    }
    await session.prompt(request.goal);
    if (!eventWriter.hasTerminalEvent) throw new Error("session completed without settlement");
  } catch (error) {
    eventWriter.write("harness.failed", {
      error: error instanceof Error ? error.message : String(error),
    });
  } finally {
    try {
      try { await aborting; } finally { await session?.close(); }
    } finally {
      proc.off("SIGINT", onInt);
      proc.off("SIGTERM", onTerm);
    }
  }
  if (signalCode !== undefined) {
    proc.exit(signalCode);
    return signalCode;
  }
  return eventWriter.terminalVerdict === "succeeded" ? 0 : 1;
}

async function injectedFactory(
  deps: Pick<PiSessionDeps, "createAgentSession" | "modelRuntime">, errorSink: Writable,
  serverMode: boolean,
): Promise<ExecutionSessionFactory> {
  const definition = await loadHarnessDefinition();
  if (serverMode) {
    return createExecutionSessionFactory({
      definition, stderr: errorSink, createAgentSession: deps.createAgentSession,
    });
  }
  const { createPiSession } = await import("./pi-session.js");
  return (request, { eventWriter, signal }) => createPiSession(request, {
    ...deps, eventWriter, signal, stderr: errorSink,
  }, definition);
}

async function readRequest(
  input: Readable, output: Writable, proc: NodeJS.Process,
): Promise<RunRequest> {
  const reader = createInterface({ input });
  const raw = (await reader[Symbol.asyncIterator]().next()).value as string | undefined;
  reader.close();
  if (raw === undefined) throw new Error("no run request on stdin");
  try {
    return parseRunRequest(raw);
  } catch (error) {
    const writer = new HarnessEventWriter({ run_id: "unknown", attempt_id: "unknown" }, output);
    writer.write("harness.failed", {
      error: error instanceof Error ? error.message : String(error),
    });
    proc.exit(1);
    throw error;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().then(
    (code) => process.exit(code),
    (error) => {
      stderr.write(error instanceof Error ? error.stack ?? error.message : String(error));
      process.exit(1);
    },
  );
}
