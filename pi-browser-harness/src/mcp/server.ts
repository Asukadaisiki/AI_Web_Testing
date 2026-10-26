import { fileURLToPath } from "node:url";

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";

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
  let builderClosed = false;
  let serverClosed = false;

  for (const schema of BROWSER_TOOL_SCHEMAS) {
    mcp.tool(
      schema.name,
      schema.description,
      schema.inputSchema,
      async (args) => {
        try {
          const outcome = await queue.enqueue(() =>
            builder.call(schema.name, args as Record<string, unknown>),
          );
          return encodeToolOutcome(outcome);
        } catch (error) {
          if (error instanceof FatalCaseBuilderError) {
            // Close only the builder; leave the MCP transport open so the SDK
            // can write the error response to the client. The production
            // onFatal callback terminates the child afterwards.
            await closeBuilderOnce();
            options.onFatal?.(error);
          }
          throw error;
        }
      },
    );
  }

  /** Idempotently closes the authoring browser session (builder) only. */
  async function closeBuilderOnce(): Promise<void> {
    if (builderClosed) return;
    builderClosed = true;
    try {
      await builder.close();
    } catch {
      // best-effort cleanup
    }
  }

  /** Idempotently closes the builder and the MCP transport. */
  async function closeOnce(): Promise<void> {
    if (serverClosed) return;
    serverClosed = true;
    await closeBuilderOnce();
    try {
      await mcp.close();
    } catch {
      // transport already gone
    }
  }

  return {
    async connect(transport: Transport): Promise<void> {
      // Chain any pre-existing onclose handler (e.g. the stdio entry's
      // process.exit(0)) rather than overwriting it, so transport teardown and
      // process exit both run.
      const previousOnClose = transport.onclose;
      transport.onclose = () => {
        void closeOnce().finally(() => previousOnClose?.());
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
export async function runStdioServer(): Promise<void> {
  const workerUrl = requiredEnv("AGENTTRACE_HARNESS_WORKER_URL");
  const artifactScope = requiredEnv("AGENTTRACE_HARNESS_ARTIFACT_SCOPE");
  const goal = requiredEnv("AGENTTRACE_HARNESS_GOAL");

  const worker = new WorkerClient(workerUrl);
  const builder = await CaseBuilder.create({ worker, artifactScope, goal });

  const server = createBrowserMcpServer(builder, {
    onFatal: () => {
      // Give the error response a chance to flush, then terminate the child.
      setTimeout(() => process.exit(1), 0);
    },
  });

  const transport = new StdioServerTransport();
  transport.onclose = () => {
    void server.close().then(() => process.exit(0));
  };

  const onSignal = (): void => {
    void server.close().then(() => process.exit(0));
  };
  process.on("SIGTERM", onSignal);
  process.on("SIGINT", onSignal);

  await server.connect(transport);
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
