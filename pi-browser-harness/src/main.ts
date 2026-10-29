#!/usr/bin/env node
import type { Readable, Writable } from "node:stream";
import { stdin as defaultInput, stdout as defaultOutput, stderr } from "node:process";
import { pathToFileURL } from "node:url";
import { createExecutionSessionFactory } from "./session-factory.js";
import { serverMain, type ExecutionSessionFactory } from "./server.js";

export interface MainOptions {
  sessionFactory?: ExecutionSessionFactory;
  argv?: readonly string[];
  input?: Readable;
  output?: Writable;
  errorSink?: Writable;
  process?: NodeJS.Process;
}

/** Bootstrap owns SDK/harness selection; the server owns only generic lifetimes. */
export async function main(options: MainOptions = {}): Promise<number> {
  if (!(options.argv ?? process.argv.slice(2)).includes("--server")) {
    throw new Error("shared SDK Host requires --server; one-shot execution is not supported");
  }
  const input = options.input ?? defaultInput;
  const output = options.output ?? defaultOutput;
  const errorSink = options.errorSink ?? stderr;
  const proc = options.process ?? process;
  const factory = options.sessionFactory ?? await createExecutionSessionFactory({ stderr: errorSink });
  return serverMain({ input, output, errorSink, process: proc, sessionFactory: factory });
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
