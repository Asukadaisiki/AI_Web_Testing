import { randomUUID } from "node:crypto";
import { createServer, type Server as HttpServer } from "node:http";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import {
  CallToolRequestSchema, ListToolsRequestSchema, McpError, ErrorCode,
  type CallToolResult, type Tool,
} from "@modelcontextprotocol/sdk/types.js";
import { normalizeName, type HarnessResources } from "./harness-resources.js";
import type { HarnessMcpServer, JsonObject, ObservedEvent } from "./harness-definition.js";
import type { HarnessEventWriter } from "./run-protocol.js";
import { validateBrowserEventPayload } from "./browser-protocol.js";
import { SessionCleanupError } from "./server.js";
import type { BrowserEvidenceGuard } from "./browser/evidence-guard.js";

export interface RuntimeMcp {
  mcpServers: Record<string, { type: "http"; url: string }>;
  /** Only the latest event of each type selected by the completion policy is retained. */
  events: ObservedEvent[];
  close(): Promise<void>;
}

interface Upstream {
  descriptor: HarnessMcpServer;
  name: string;
  client: Client;
  transport: JoinedStdioClientTransport;
  connected: boolean;
  tools: Map<string, Tool>;
  tail: Promise<unknown>;
}

/** MCP 1.29's close() escalates to SIGKILL without joining the real child.close. */
class JoinedStdioClientTransport extends StdioClientTransport {
  private startAttempted = false;
  private closing: Promise<void> | undefined;
  private acknowledgeExit!: () => void;
  private readonly childClosed = new Promise<void>((resolve) => { this.acknowledgeExit = resolve; });

  override async start(): Promise<void> {
    if (!this.startAttempted) {
      this.startAttempted = true;
      // Client.connect has installed its callback; retain it even if initialization fails.
      const onclose = this.onclose;
      this.onclose = () => {
        this.acknowledgeExit();
        onclose?.();
      };
    }
    await super.start();
  }

  override close(): Promise<void> {
    this.closing ??= (async () => {
      try {
        await super.close();
      } finally {
        if (this.startAttempted) {
          let timeout: ReturnType<typeof setTimeout> | undefined;
          try {
            await Promise.race([
              this.childClosed,
              new Promise<never>((_resolve, reject) => {
                timeout = setTimeout(() => reject(new SessionCleanupError(
                  "MCP child shutdown was not acknowledged after escalation",
                )), 2000);
              }),
            ]);
          } finally {
            clearTimeout(timeout);
          }
        }
      }
    })();
    return this.closing;
  }
}

/** One retained stdio client per selected server, owned by this Attempt rather than its SDK. */
export async function openRuntimeMcp(
  resources: HarnessResources, eventWriter: Pick<HarnessEventWriter, "write">,
  evidenceGuard?: BrowserEvidenceGuard,
): Promise<RuntimeMcp> {
  const upstreams: Upstream[] = [];
  const events: ObservedEvent[] = [];
  const pending = new Set<Promise<unknown>>();
  const wrappers = new Set<Server>();
  const hostCalls = new Map<string, Promise<CallToolResult>>();
  let listener: HttpServer | undefined;
  let closing: Promise<void> | undefined;
  let stopping = false;

  const emit = (event: ObservedEvent): void => {
    eventWriter.write(event.type, event.payload);
    if (!resources.definition.completionEventTypes?.includes(event.type)) return;
    const index = events.findIndex((prior) => prior.type === event.type);
    if (index < 0) events.push(event);
    else events[index] = event;
  };
  const track = <T>(operation: Promise<T>): Promise<T> => {
    pending.add(operation);
    void operation.then(() => pending.delete(operation), () => pending.delete(operation));
    return operation;
  };
  const callHost = (name: string, arguments_: JsonObject): Promise<CallToolResult> => {
    const prior = hostCalls.get(name);
    if (prior) return prior;
    const operation = (async () => {
      const upstream = upstreams.find((item) => [...item.tools.keys()]
        .some((tool) => `mcp__${item.name}__${tool}` === name));
      const tool = upstream?.tools.get(name.slice(`mcp__${upstream.name}__`.length));
      if (!upstream?.connected || !tool) throw new Error("MCP cleanup tool is unavailable");
      const result = await upstream.client.callTool({ name: tool.name, arguments: arguments_ });
      if (result.isError) throw new Error("MCP resource cleanup failed");
      return result as CallToolResult;
    })();
    hostCalls.set(name, operation);
    return operation;
  };

  const close = (): Promise<void> => {
    stopping = true;
    closing ??= (async () => {
      const failures: unknown[] = [];
      await Promise.allSettled([...pending]);
      try {
        if (resources.definition.cleanup) {
          try { await resources.definition.cleanup({ callMcpTool: callHost }); }
          catch (error) { failures.push(error); }
        }
        for (const upstream of upstreams.filter((item) => item.connected)) {
          const wasCleaned = [...upstream.tools.keys()].some((tool) =>
            hostCalls.has(`mcp__${upstream.name}__${tool}`));
          if (wasCleaned || !upstream.tools.has("close_session")) continue;
          try {
            const result = await callHost(`mcp__${upstream.name}__close_session`, {});
            if (result.structuredContent?.closed !== true) {
              throw new Error("MCP cleanup acknowledgement is missing");
            }
          } catch (error) { failures.push(error); }
        }
      } catch (error) {
        failures.push(error);
      } finally {
        // An HTTP client closing its own connection never releases the retained stdio owner.
        for (const upstream of upstreams) {
          try { await upstream.client.close(); } catch (error) { failures.push(error); }
          // A failed connect or early onclose may have detached the transport from Client.
          try { await upstream.transport.close(); } catch (error) { failures.push(error); }
        }
        for (const wrapper of wrappers) {
          try { await wrapper.close(); } catch (error) { failures.push(error); }
        }
        wrappers.clear();
        if (listener) {
          try {
            await new Promise<void>((resolve, reject) => {
              listener!.close((error) => error ? reject(error) : resolve());
              listener!.closeAllConnections();
            });
          } catch (error) { failures.push(error); }
        }
      }
      if (failures.length) throw new SessionCleanupError("MCP resource cleanup or shutdown was not acknowledged");
    })();
    return closing;
  };

  try {
    const seen = new Set<string>();
    for (const descriptor of resources.servers) {
      const name = normalizeName(descriptor.name);
      if (seen.has(name.toLowerCase())) throw new Error("duplicate MCP server name");
      seen.add(name.toLowerCase());
      const client = new Client({ name: "agenttrace-runtime", version: "1.0.0" });
      const transport = new JoinedStdioClientTransport({
        command: descriptor.command, ...(descriptor.args ? { args: descriptor.args } : {}),
        ...(descriptor.env ? { env: descriptor.env } : {}), cwd: resources.cwd, stderr: "ignore",
      });
      const upstream: Upstream = {
        descriptor, name, client, transport, connected: false, tools: new Map(), tail: Promise.resolve(),
      };
      upstreams.push(upstream);
      await client.connect(transport as Transport);
      upstream.connected = true;
      let cursor: string | undefined;
      const cursors = new Set<string>();
      do {
        const listed = await client.listTools(cursor ? { cursor } : {});
        for (const tool of listed.tools) {
          const normalized = normalizeName(tool.name);
          if (upstream.tools.has(normalized)) throw new Error("duplicate MCP tool name");
          upstream.tools.set(normalized, tool);
        }
        cursor = listed.nextCursor;
        if (cursor && cursors.has(cursor)) throw new Error("invalid MCP discovery cursor");
        if (cursor) cursors.add(cursor);
      } while (cursor);
    }

    const mcpServers: RuntimeMcp["mcpServers"] = {};
    if (upstreams.length) {
      const routes = new Map(upstreams.map((upstream) => [`/mcp/${randomUUID()}`, upstream]));
      listener = createServer((request, response) => {
        const upstream = routes.get(request.url ?? "");
        const address = listener!.address();
        const host = typeof address === "object" && address ? `127.0.0.1:${address.port}` : "";
        if (stopping) { response.writeHead(503).end(); return; }
        if (!upstream) { response.writeHead(404).end(); return; }
        if (request.headers.origin || request.headers.host !== host) { response.writeHead(403).end(); return; }
        if (request.method !== "POST") { response.writeHead(405).end(); return; }
        // Stateless HTTP transports are request-scoped. All of them forward to the same retained client.
        const wrapper = new Server({ name: upstream.name, version: "1.0.0" }, { capabilities: { tools: {} } });
        const transport = new StreamableHTTPServerTransport({ enableJsonResponse: true });
        wrappers.add(wrapper);
        response.once("close", () => {
          wrappers.delete(wrapper);
          void wrapper.close().catch(() => {});
        });
        const hidden = new Set([...(upstream.descriptor.hostOnlyTools ?? []).map(normalizeName), "close_session"]);
        const visible = upstream.descriptor.exposure === "hidden" ? [] :
          [...upstream.tools].filter(([name]) => !hidden.has(name));
        const visibleNames = new Set(visible.map(([name]) => name));
        wrapper.setRequestHandler(ListToolsRequestSchema, () => ({
          tools: visible.map(([name, tool]) => ({ ...tool, name })),
        }));
        wrapper.setRequestHandler(CallToolRequestSchema, ({ params }, extra) => {
          if (stopping || !visibleNames.has(params.name)) {
            throw new McpError(ErrorCode.InvalidParams, "MCP tool is not model-visible");
          }
          const tool = upstream.tools.get(params.name)!;
          const execute = async (): Promise<CallToolResult> => {
            const id = randomUUID();
            const name = `mcp__${upstream.name}__${params.name}`;
            const started = new Date().toISOString();
            if (upstream.name === "browser_harness" && params.name === "finish_case") {
              const index = events.findIndex((event) => event.type === "browser.case.completed");
              if (index >= 0) events.splice(index, 1);
            }
            emit({ type: "pi.tool.started", payload: { tool_call_id: id, tool_name: name } });
            evidenceGuard?.observe({
              type: "tool_execution_start", toolCallId: id, toolName: name, args: params.arguments ?? {},
            });
            let result: CallToolResult;
            try {
              result = await upstream.client.callTool({
                name: tool.name, arguments: params.arguments ?? {},
              }, undefined, { signal: extra.signal }) as CallToolResult;
            } catch {
              result = { isError: true, content: [{ type: "text", text: "MCP tool execution failed" }] };
            }
            evidenceGuard?.observe({
              type: "tool_execution_end", toolCallId: id, toolName: name, result,
            });
            emit({ type: "pi.tool.completed", payload: {
              tool_call_id: id, tool_name: name, is_error: Boolean(result.isError),
              started_at: started, ended_at: new Date().toISOString(),
              result: {
                content: result.content,
                ...(result.structuredContent !== undefined ? { structuredContent: result.structuredContent } : {}),
                ...(result.isError !== undefined ? { isError: result.isError } : {}),
              },
              ...(result.structuredContent ? { structured_result: result.structuredContent } : {}),
            } });
            if (upstream.name === "browser_harness") {
              for (const event of browserEvents(params.name, id, result)) emit(event);
            }
            return result;
          };
          const operation = upstream.descriptor.executionMode === "sequential"
            ? upstream.tail.then(execute, execute) : execute();
          upstream.tail = operation.catch(() => {});
          return track(operation);
        });
        // MCP 1.29's concrete transports widen optional properties relative to Transport.
        void wrapper.connect(transport as Transport).then(() => transport.handleRequest(request, response)).catch(() => {
          if (!response.headersSent) response.writeHead(500).end();
          else response.end();
        });
      });
      await new Promise<void>((resolve, reject) => {
        listener!.once("error", reject);
        listener!.listen(0, "127.0.0.1", resolve);
      });
      const address = listener.address();
      if (!address || typeof address === "string") throw new Error("MCP loopback listener failed");
      for (const [path, upstream] of routes) {
        mcpServers[upstream.name] = { type: "http", url: `http://127.0.0.1:${address.port}${path}` };
      }
    }
    return { mcpServers, events, close };
  } catch {
    await close();
    throw new Error("MCP runtime startup failed");
  }
}

function browserEvents(tool: string, id: string, result: CallToolResult): ObservedEvent[] {
  const structured = result.structuredContent;
  if (!structured) return [];
  const events: ObservedEvent[] = [];
  if (structured.page !== undefined) {
    events.push({ type: "browser.observation", payload: { tool_call_id: id, observation: structured.page } });
  }
  if (tool === "finish_case" && !result.isError) {
    const dryRun = structured.dry_run as { status?: unknown } | undefined;
    const payload = {
      verdict: dryRun?.status === "passed" ? "passed" as const : "failed" as const,
      case: structured.case, dry_run: structured.dry_run,
    };
    try {
      validateBrowserEventPayload("browser.case.completed", payload);
      events.push({ type: "browser.case.completed", payload: payload as
        Extract<ObservedEvent, { type: "browser.case.completed" }>["payload"] });
    } catch {
      // Invalid or empty artifacts are not evidence, regardless of the model's success claim.
    }
  }
  return events;
}
