import { createInterface } from "node:readline";
import type { EventEmitter } from "node:events";
import type { Readable, Writable } from "node:stream";
import {
  HarnessEventWriter,
  parseRunRequest,
  type RunRequest,
} from "./run-protocol.js";

export interface ExecutionSession {
  prompt(goal: string): Promise<void>;
  abort(): Promise<void>;
  close(): Promise<void>;
}

/** Factories may reject normally only after cleanup; this error makes uncertain release fatal. */
export class SessionCleanupError extends Error {
  constructor(message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = "SessionCleanupError";
  }
}

export interface ExecutionSessionContext {
  eventWriter: HarnessEventWriter;
  signal: AbortSignal;
}

export type ExecutionSessionFactory = (
  request: RunRequest,
  context: ExecutionSessionContext,
) => Promise<ExecutionSession>;

export type HarnessCommand =
  | { type: "run"; request: RunRequest }
  | { type: "cancel"; run_id: string; attempt_id: string }
  | { type: "shutdown" };

export type HarnessControl =
  | { type: "harness.ready" }
  | { type: "harness.released"; run_id: string; attempt_id: string }
  | { type: "harness.protocol_error"; error: string };

export interface ServerOptions {
  input?: Readable;
  output?: Pick<Writable, "write">;
  errorSink?: Pick<Writable, "write">;
  sessionFactory: ExecutionSessionFactory;
  process?: Pick<EventEmitter, "on" | "off">;
}

interface Attempt {
  controller: AbortController;
  session?: ExecutionSession;
  aborting?: Promise<void>;
  done: Promise<void>;
}

export function parseHarnessCommand(raw: string): HarnessCommand {
  const value: unknown = JSON.parse(raw);
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error("invalid harness command");
  }
  const command = value as Record<string, unknown>;
  const exact = (keys: string[]) =>
    Object.keys(command).length === keys.length &&
    keys.every((key) => Object.hasOwn(command, key));
  if (command.type === "run" && exact(["type", "request"])) {
    return { type: "run", request: parseRunRequest(JSON.stringify(command.request)) };
  }
  if (command.type === "shutdown" && exact(["type"])) return { type: "shutdown" };
  if (command.type === "cancel" && exact(["type", "run_id", "attempt_id"]) &&
    typeof command.run_id === "string" && command.run_id.trim() !== "" &&
    typeof command.attempt_id === "string" && command.attempt_id.trim() !== "") {
    return { type: "cancel", run_id: command.run_id, attempt_id: command.attempt_id };
  }
  throw new Error("invalid harness command");
}

const errorText = (error: unknown): string =>
  error instanceof Error ? error.message : String(error);
const attemptKey = (run: Pick<RunRequest, "run_id" | "attempt_id">): string =>
  JSON.stringify([run.run_id, run.attempt_id]);

/** The transport owns lifetimes only; sessions own execution and verdicts. */
export async function serverMain(options: ServerOptions): Promise<number> {
  const input = options.input ?? process.stdin;
  const output = options.output ?? process.stdout;
  const proc = options.process ?? process;
  const factory = options.sessionFactory;
  const attempts = new Map<string, Attempt>();
  let stopped = false;
  let exitCode = 0;
  let protocolFailed = false;
  const control = (event: HarnessControl) => output.write(`${JSON.stringify(event)}\n`);
  const reader = createInterface({ input, crlfDelay: Infinity });
  let finishReading!: () => void;
  const reading = new Promise<void>((resolve) => { finishReading = resolve; });

  const abort = (attempt: Attempt): Promise<void> => {
    attempt.controller.abort();
    if (attempt.session && !attempt.aborting) {
      attempt.aborting = Promise.resolve().then(() => attempt.session!.abort());
      // Retain the rejection for the execution task, without an unhandled rejection.
      void attempt.aborting.catch(() => {});
    }
    return attempt.aborting ?? Promise.resolve();
  };
  const stop = (code = 0): void => {
    if (stopped) return;
    stopped = true;
    exitCode = code;
    for (const attempt of attempts.values()) void abort(attempt);
    reader.close();
    finishReading();
  };
  const fatal = (error: unknown): void => {
    if (!protocolFailed) {
      protocolFailed = true;
      control({ type: "harness.protocol_error", error: errorText(error) });
    }
    stop(1);
    exitCode = 1;
  };
  const execute = async (request: RunRequest, attempt: Attempt): Promise<void> => {
    const eventWriter = new HarnessEventWriter(request, output);
    let initializationCleanedUp = true;
    eventWriter.write("harness.started", {});
    try {
      attempt.session = await factory(request, {
        eventWriter, signal: attempt.controller.signal,
      });
      if (attempt.controller.signal.aborted) {
        await abort(attempt);
        throw new Error("attempt cancelled");
      }
      await attempt.session.prompt(request.goal);
      if (!eventWriter.hasTerminalEvent) {
        throw new Error(attempt.controller.signal.aborted
          ? "attempt cancelled" : "session completed without settlement");
      }
    } catch (error) {
      eventWriter.write("harness.failed", { error: errorText(error) });
      if (error instanceof SessionCleanupError) {
        initializationCleanedUp = false;
        fatal(error);
      }
    } finally {
      try {
        try {
          await attempt.aborting;
        } finally {
          await attempt.session?.close();
        }
        if (initializationCleanedUp) {
          control({
            type: "harness.released", run_id: request.run_id, attempt_id: request.attempt_id,
          });
        }
      } catch (error) {
        fatal(error);
      }
      attempts.delete(attemptKey(request));
    }
  };

  reader.on("line", (raw) => {
    if (stopped) return;
    try {
      const command = parseHarnessCommand(raw);
      if (command.type === "shutdown") { stop(); return; }
      const key = attemptKey(command.type === "run" ? command.request : command);
      if (command.type === "cancel") {
        const attempt = attempts.get(key);
        if (attempt) void abort(attempt);
        return;
      }
      if (attempts.has(key)) throw new Error("duplicate active attempt");
      const attempt: Attempt = { controller: new AbortController(), done: Promise.resolve() };
      // Registration precedes session creation, including its first await.
      attempts.set(key, attempt);
      attempt.done = execute(command.request, attempt);
    } catch (error) {
      fatal(error);
    }
  });
  reader.on("close", () => stop());
  input.on("error", fatal);
  const onInt = () => stop(130);
  const onTerm = () => stop(143);
  proc.on("SIGINT", onInt);
  proc.on("SIGTERM", onTerm);
  control({ type: "harness.ready" });
  try {
    await reading;
    await Promise.all([...attempts.values()].map((attempt) => attempt.done));
  } finally {
    proc.off("SIGINT", onInt);
    proc.off("SIGTERM", onTerm);
    input.off("error", fatal);
    reader.close();
  }
  return exitCode;
}
