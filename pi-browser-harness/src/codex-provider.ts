import { createHash, randomUUID } from "node:crypto";
import { createServer, type ServerResponse } from "node:http";
import { once } from "node:events";
import { SessionCleanupError } from "./server.js";

export interface CodexProvider {
  baseUrl: string;
  apiKey: string;
  close(): Promise<void>;
}

type ObjectValue = Record<string, unknown>;
function object(value: unknown): value is ObjectValue {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** Only declared selected MCP functions can acquire a provider-facing alias. */
class ToolMapping {
  private readonly aliases = new Map<string, { namespace: string; name: string }>();
  private readonly names = new Map<string, string>();
  private readonly reserved = new Set<string>();
  constructor(private readonly namespaces: Set<string>) {}

  private alias(namespace: string, name: string): string {
    const key = JSON.stringify([namespace, name]);
    const existing = this.names.get(key);
    if (existing) return existing;
    const raw = `${namespace}__${name}`;
    let alias = raw;
    let salt = 0;
    while (alias.length > 64 || !/^[A-Za-z0-9_-]+$/.test(alias) ||
      this.reserved.has(alias) || this.aliases.has(alias)) {
      const hash = createHash("sha256").update(`${key}:${salt++}`).digest("hex").slice(0, 16);
      alias = `${raw.replace(/[^A-Za-z0-9_-]/g, "_").slice(0, 47)}_${hash}`;
    }
    this.names.set(key, alias);
    this.aliases.set(alias, { namespace, name });
    return alias;
  }

  request(body: ObjectValue): ObjectValue {
    const tools = Array.isArray(body.tools) ? body.tools : [];
    for (const tool of tools) {
      if (object(tool) && tool.type !== "namespace" && typeof tool.name === "string") {
        if (this.aliases.has(tool.name)) throw new Error("Provider tool declaration collides with an existing alias");
        this.reserved.add(tool.name);
      }
    }
    const declarations = tools.flatMap((tool) => {
      if (!object(tool) || tool.type !== "namespace" || typeof tool.name !== "string" ||
        !this.namespaces.has(tool.name) || !Array.isArray(tool.tools)) return [tool];
      const namespace = tool.name;
      return tool.tools.map((child) => {
        if (!object(child) || child.type !== "function" || typeof child.name !== "string") {
          throw new Error("Unsupported selected MCP provider declaration");
        }
        return { ...child, name: this.alias(namespace, child.name) };
      });
    });
    return { ...body, ...(Array.isArray(body.tools) ? { tools: declarations } : {}),
      ...(body.input !== undefined ? { input: this.protocol(body.input, "request") } : {}),
      ...(body.tool_choice !== undefined ? { tool_choice: this.protocol(body.tool_choice, "request") } : {}),
    };
  }

  protocol(value: unknown, direction: "request" | "response"): unknown {
    if (Array.isArray(value)) return value.map((item) => this.protocol(item, direction));
    if (!object(value)) return value;
    const result = { ...value };
    const callable = value.type === "function" || value.type === "function_call" ||
      value.type === "function_call_output" ||
      (typeof value.type === "string" && value.type.startsWith("response.function_call_arguments."));
    if (callable && typeof value.name === "string") {
      if (direction === "response" && !value.namespace) {
        const mapped = this.aliases.get(value.name);
        if (mapped) Object.assign(result, mapped);
      } else if (direction === "request" && typeof value.namespace === "string") {
        const alias = this.names.get(JSON.stringify([value.namespace, value.name]));
        if (alias) { result.name = alias; delete result.namespace; }
      }
    }
    // Never descend into arguments, tool results, model text, usage or opaque provider fields.
    for (const key of ["input", "response", "item", "tool_choice", "tools", "function"]) {
      if (value[key] !== undefined) result[key] = this.protocol(value[key], direction);
    }
    if (value.type !== "function_call_output" && Array.isArray(value.output)) {
      result.output = this.protocol(value.output, direction);
    }
    return result;
  }
}

const hopHeaders = new Set([
  "host", "connection", "keep-alive", "transfer-encoding", "content-length",
  "upgrade", "proxy-authenticate", "proxy-authorization", "te", "trailer",
]);

/** An attempt-owned Responses wire adapter, never an execution or evidence source. */
export async function openCodexProvider(options: {
  baseUrl: string; apiKey: string; headers: Record<string, string>;
  servers: readonly string[]; signal: AbortSignal;
}): Promise<CodexProvider> {
  const route = `/codex-provider/${randomUUID()}/v1`;
  const token = randomUUID();
  const mapping = new ToolMapping(new Set(options.servers.map((name) => `mcp__${name}`)));
  const upstreamUrl = new URL(options.baseUrl);
  upstreamUrl.pathname = `${upstreamUrl.pathname.replace(/\/$/, "")}/responses`;
  const pending = new Set<Promise<void>>();
  const controllers = new Set<AbortController>();
  let stopping = false;
  let closing: Promise<void> | undefined;
  const cancel = () => { for (const controller of controllers) controller.abort(); };
  options.signal.addEventListener("abort", cancel);

  const listener = createServer((request, response) => {
    const address = listener.address();
    const host = address && typeof address === "object" ? `127.0.0.1:${address.port}` : "";
    const deny = (status: number) => { request.resume(); response.writeHead(status).end(); };
    if (request.headers.origin || request.headers.host !== host) { deny(403); return; }
    if (request.url !== `${route}/responses`) { deny(404); return; }
    if (request.headers.authorization !== `Bearer ${token}`) { deny(401); return; }
    if (request.method !== "POST") { deny(405); return; }
    if (stopping || options.signal.aborted) { deny(503); return; }
    const controller = new AbortController();
    controllers.add(controller);
    const abort = () => controller.abort();
    const stopRequest = () => {
      request.resume();
      response.destroy();
    };
    controller.signal.addEventListener("abort", stopRequest, { once: true });
    request.once("aborted", abort);
    response.once("close", abort);
    const operation = (async () => {
      let upstream: Response | undefined;
      try {
        const chunks: Buffer[] = [];
        let size = 0;
        for await (const chunk of request) {
          size += chunk.length;
          if (size > 16 * 1024 * 1024) { deny(413); return; }
          chunks.push(Buffer.from(chunk));
        }
        const body: unknown = JSON.parse(Buffer.concat(chunks).toString("utf8"));
        if (!object(body)) { deny(400); return; }
        const translated = mapping.request(body);
        const headers = new Headers();
        for (const [key, value] of Object.entries(request.headers)) {
          if (!hopHeaders.has(key) && !["authorization", "content-encoding"].includes(key) && typeof value === "string") {
            headers.set(key, value);
          }
        }
        headers.set("content-type", "application/json");
        headers.set("authorization", `Bearer ${options.apiKey}`);
        for (const [key, value] of Object.entries(options.headers)) headers.set(key, value);
        controller.signal.throwIfAborted();
        upstream = await fetch(upstreamUrl, {
          method: "POST", headers, body: JSON.stringify(translated),
          signal: controller.signal, redirect: "manual",
        });
        const responseHeaders: Record<string, string> = {};
        upstream.headers.forEach((value, key) => {
          if (!hopHeaders.has(key) && key !== "content-encoding") responseHeaders[key] = value;
        });
        response.writeHead(upstream.status, responseHeaders);
        if (!upstream.body) { response.end(); return; }
        if (upstream.ok && upstream.headers.get("content-type")?.includes("text/event-stream")) {
          await streamEvents(upstream.body, response, (event) => mapping.protocol(event, "response"), controller.signal);
        } else if (upstream.ok && upstream.headers.get("content-type")?.includes("application/json")) {
          response.end(JSON.stringify(mapping.protocol(await upstream.json(), "response")));
        } else {
          for await (const chunk of upstream.body) await write(response, chunk, controller.signal);
        }
        response.end();
      } catch {
        // No URLs, credentials, provider bodies or custom header values enter local diagnostics.
        if (!response.headersSent && !response.destroyed) {
          response.writeHead(controller.signal.aborted ? 503 : 502, { "content-type": "application/json" });
          response.end('{"error":{"message":"Codex provider bridge request failed"}}');
        } else if (!response.destroyed) response.destroy();
        request.resume();
      } finally {
        if (upstream?.body && !upstream.body.locked) await upstream.body.cancel().catch(() => {});
        request.off("aborted", abort);
        response.off("close", abort);
        controller.signal.removeEventListener("abort", stopRequest);
        controllers.delete(controller);
      }
    })();
    pending.add(operation);
    void operation.then(() => pending.delete(operation), () => pending.delete(operation));
  });
  const close = (): Promise<void> => {
    stopping = true;
    cancel();
    closing ??= (async () => {
      options.signal.removeEventListener("abort", cancel);
      try {
        const stopped = listener.listening ? new Promise<void>((resolve, reject) => {
          listener.close((error) => error ? reject(error) : resolve());
          listener.closeAllConnections();
        }) : Promise.resolve();
        await Promise.all([stopped, Promise.allSettled([...pending])]);
      } catch {
        throw new SessionCleanupError("Codex provider bridge shutdown was not acknowledged");
      }
    })();
    return closing;
  };
  try {
    options.signal.throwIfAborted();
    await new Promise<void>((resolve, reject) => {
      listener.once("error", reject);
      listener.listen(0, "127.0.0.1", resolve);
    });
    options.signal.throwIfAborted();
    const address = listener.address();
    if (!address || typeof address === "string") throw new Error("Codex provider bridge startup failed");
    return { baseUrl: `http://127.0.0.1:${address.port}${route}`, apiKey: token, close };
  } catch (error) {
    await close();
    if (error instanceof SessionCleanupError) throw error;
    throw new Error("Codex provider bridge startup failed");
  }
}

async function write(response: ServerResponse, data: string | Uint8Array, signal: AbortSignal): Promise<void> {
  signal.throwIfAborted();
  if (!response.write(data)) await once(response, "drain", { signal });
}

async function streamEvents(
  body: ReadableStream<Uint8Array>, response: ServerResponse,
  translate: (event: unknown) => unknown, signal: AbortSignal,
): Promise<void> {
  const decoder = new TextDecoder();
  let buffered = "";
  const frame = async (raw: string) => {
    const lines = raw.split(/\r?\n/);
    const data = lines.filter((line) => line.startsWith("data:")).map((line) => line.slice(5).replace(/^ /, "")).join("\n");
    let serialized: string | undefined;
    if (data && data !== "[DONE]") {
      try { serialized = JSON.stringify(translate(JSON.parse(data))); } catch { /* Preserve non-JSON SSE data. */ }
    }
    if (serialized !== undefined) {
      const metadata = lines.filter((line) => !line.startsWith("data:"));
      await write(response, [...metadata, `data: ${serialized}`].join("\n") + "\n\n", signal);
    } else await write(response, raw + "\n\n", signal);
  };
  for await (const chunk of body) {
    buffered += decoder.decode(chunk, { stream: true });
    let boundary: RegExpExecArray | null;
    while ((boundary = /\r?\n\r?\n/.exec(buffered))) {
      await frame(buffered.slice(0, boundary.index));
      buffered = buffered.slice(boundary.index + boundary[0].length);
    }
    if (buffered.length > 16 * 1024 * 1024) throw new Error("Provider SSE frame too large");
  }
  buffered += decoder.decode();
  if (buffered) await frame(buffered);
}
