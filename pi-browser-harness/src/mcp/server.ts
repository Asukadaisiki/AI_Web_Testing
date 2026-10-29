import { fileURLToPath } from "node:url";
import type { EventEmitter } from "node:events";
import type { Readable, Writable } from "node:stream";

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import { z } from "zod";

import { BROWSER_TOOL_SCHEMAS } from "./schemas.js";
import { encodeToolOutcome } from "./tool-result.js";
import {
  CaseBuilder,
  FatalCaseBuilderError,
  type ToolOutcome,
} from "../browser/case-builder.js";
import { WorkerClient } from "../browser/worker-client.js";

/**
 * The subset of `CaseBuilder` consumed by the MCP server. The real
 * `CaseBuilder` satisfies this structurally; tests substitute a fake.
 */
export interface BrowserMcpBuilder {
  call(
    toolName: string,
    args: Record<string, unknown>,
    signal?: AbortSignal,
  ): Promise<ToolOutcome>;
  close(): Promise<void>;
}

export interface BrowserMcpServer {
  /** Attaches the server to a transport (stdio or in-memory) and starts it. */
  connect(transport: Transport): Promise<void>;
  /** Idempotently closes the builder and the MCP transport. */
  close(): Promise<void>;
}

export interface BrowserMcpServerOptions {
  /**
   * Invoked when a fatal builder error (e.g. committed-prefix replay failure)
   * escapes a tool call. The production stdio entry uses this to terminate the
   * child after cleanup.
   */
  onFatal?: (error: FatalCaseBuilderError) => void;
}

/**
 * Creates a run-scoped MCP server that exposes the sixteen stable browser
 * authoring tools. Tool calls are serialized through a promise queue owned by
 * this server so the authoring browser is never driven concurrently.
 */
export function createBrowserMcpServer(
  builder: BrowserMcpBuilder,
  options: BrowserMcpServerOptions = {},
): BrowserMcpServer {
  const mcp = new McpServer(
    { name: "agenttrace-browser-authoring", version: "1.0.0" },
    {
      capabilities: { tools: {} },
    },
  );

  const queue = new PromiseQueue();
  let builderClosing: Promise<void> | undefined;
  let serverClosing: Promise<void> | undefined;

  // SDK stdio close ignores child exit status. Hosts must await this acknowledgement
  // before transport teardown to distinguish release from uncertain cleanup.
  mcp.server.setRequestHandler(z.object({ method: z.literal("browser/close") }), async () => {
    await closeBuilderOnce();
    return { closed: true };
  });

  // Pi exposes managed MCP tools, but not arbitrary client requests. The harness
  // hides this lifecycle alias from the model and calls it before SDK shutdown.
  mcp.tool("close_session", "Acknowledge authoring browser cleanup", {}, async () => {
    await closeBuilderOnce();
    return {
      content: [],
      structuredContent: { closed: true },
    };
  });

  for (const schema of BROWSER_TOOL_SCHEMAS) {
    mcp.tool(
      schema.name,
      schema.description,
      schema.inputSchema,
      async (args) => {
        try {
          if (builderClosing) throw new Error("browser session is closing");
          const outcome = await queue.enqueue(() =>
            builder.call(schema.name, args as Record<string, unknown>),
          );
          return encodeToolOutcome(outcome);
        } catch (error) {
          if (error instanceof FatalCaseBuilderError) {
            // Close only the builder; leave the MCP transport open so the SDK
            // can write the error response to the client. The production
            // onFatal callback terminates the child afterwards.
            try {
              await closeBuilderOnce();
            } finally {
              options.onFatal?.(error);
            }
            if (error.result?.redacted_args?.length) {
              return {
                ...encodeToolOutcome({ result: {
                  ...error.result, ok: false, error: error.code, detail: error.message,
                } }),
                isError: true,
              };
            }
          }
          throw error;
        }
      },
    );
  }

  /** Idempotently closes the authoring browser session (builder) only. */
  function closeBuilderOnce(): Promise<void> {
    builderClosing ??= queue.enqueue(() => builder.close());
    return builderClosing;
  }

  /** Idempotently closes the builder and the MCP transport. */
  function closeOnce(): Promise<void> {
    serverClosing ??= (async () => {
      try {
        await closeBuilderOnce();
      } finally {
        try {
          await mcp.close();
        } catch {
          // transport already gone; builder cleanup rejection still propagates
        }
      }
    })();
    return serverClosing;
  }

  return {
    async connect(transport: Transport): Promise<void> {
      // Chain any pre-existing onclose handler (e.g. the stdio entry's
      // process.exit(0)) rather than overwriting it, so transport teardown and
      // process exit both run.
      const previousOnClose = transport.onclose;
      transport.onclose = () => {
        void closeOnce().then(
          () => previousOnClose?.(),
          () => previousOnClose?.(),
        );
      };
      await mcp.connect(transport);
    },
    close: closeOnce,
  };
}

/**
 * Runs the production stdio MCP server. Reads the worker URL, artifact scope,
 * and goal from the environment, builds the worker client and case builder,
 * and serves MCP on stdin/stdout.
 */
export interface StdioServerOptions {
  input?: Readable;
  output?: Writable;
  process?: Pick<EventEmitter, "on" | "off"> & { exit(code: number): void };
  builder?: BrowserMcpBuilder;
}

export async function runStdioServer(options: StdioServerOptions = {}): Promise<void> {
  const input = options.input ?? process.stdin;
  const output = options.output ?? process.stdout;
  const proc = options.process ?? process;
  const builder = options.builder ?? await CaseBuilder.create({
    worker: new WorkerClient(requiredEnv("AGENTTRACE_HARNESS_WORKER_URL"), {
      ...(process.env.AGENTTRACE_BROWSER_PROVIDER_TOKEN ? {
        providerToken: process.env.AGENTTRACE_BROWSER_PROVIDER_TOKEN,
      } : {}),
      ...(process.env.AGENTTRACE_HARNESS_ENVIRONMENT_ID ? {
        environmentLease: {
          project_id: requiredEnv("AGENTTRACE_HARNESS_PROJECT_ID"),
          environment_id: requiredEnv("AGENTTRACE_HARNESS_ENVIRONMENT_ID"),
          profile_generation: Number(requiredEnv("AGENTTRACE_HARNESS_PROFILE_GENERATION")),
          fencing_token: Number(requiredEnv("AGENTTRACE_HARNESS_FENCING_TOKEN")),
        },
      } : {}),
    }),
    artifactScope: requiredEnv("AGENTTRACE_HARNESS_ARTIFACT_SCOPE"),
    goal: requiredEnv("AGENTTRACE_HARNESS_GOAL"),
  });

  const server = createBrowserMcpServer(builder, {
    onFatal: () => {
      // Give the error response a chance to flush, then terminate the child.
      setTimeout(() => void shutdown(1), 0);
    },
  });

  let shuttingDown: Promise<void> | undefined;
  const shutdown = (code = 0): Promise<void> => {
    shuttingDown ??= server.close().then(() => code, (error: unknown) => {
      console.error(error instanceof Error ? error.message : String(error));
      return 1;
    }).then((exitCode) => {
      input.off("end", onShutdown);
      proc.off("SIGTERM", onShutdown);
      proc.off("SIGINT", onShutdown);
      proc.exit(exitCode);
    });
    return shuttingDown;
  };
  const onShutdown = (): void => {
    void shutdown();
  };
  const transport = new StdioServerTransport(input, output);
  transport.onclose = onShutdown;
  // MCP SDK 1.20's server transport does not emit onclose on stdin EOF.
  input.once("end", onShutdown);
  proc.on("SIGTERM", onShutdown);
  proc.on("SIGINT", onShutdown);

  try {
    await server.connect(transport);
  } catch (error) {
    await shutdown(1);
    throw error;
  }
}

function requiredEnv(name: string): string {
  const value = process.env[name];
  if (value === undefined || value.trim() === "") {
    throw new Error(`environment variable ${name} is required`);
  }
  return value;
}

class PromiseQueue {
  #tail: Promise<unknown> = Promise.resolve();

  enqueue<T>(fn: () => Promise<T>): Promise<T> {
    const result = this.#tail.then(fn);
    this.#tail = result.then(
      () => undefined,
      () => undefined,
    );
    return result;
  }
}

// Run as the stdio entry point when this module is executed directly.
if (process.argv[1] !== undefined && isMainModule()) {
  runStdioServer().catch((error) => {
    console.error(
      error instanceof Error ? error.message : String(error),
    );
    process.exit(1);
  });
}

function isMainModule(): boolean {
  try {
    return fileURLToPath(import.meta.url) === process.argv[1];
  } catch {
    return false;
  }
}
