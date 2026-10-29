import { browserExecution, environmentLease, workerOptions } from "./fixtures/selected-harness.js";
import { randomBytes } from "node:crypto";
import { spawn, type ChildProcess } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, realpath, rm } from "node:fs/promises";
import { createServer, type Server } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Writable } from "node:stream";
import { fileURLToPath } from "node:url";
import { createAgentSession, ModelRuntime, type AgentSession } from "@earendil-works/pi-coding-agent";
import { createAssistantMessageEventStream } from "@earendil-works/pi-ai";
import { expect, it } from "vitest";
import { prepareHarnessResources, type HarnessResources } from "../src/harness-resources.js";
import { validateBrowserEventPayload } from "../src/browser-protocol.js";
import { createPiSession, type PiSession } from "../src/pi-session.js";
import { HarnessEventWriter, type HarnessEvent, type RunRequest } from "../src/run-protocol.js";

const workerRoot = fileURLToPath(new URL("../../worker", import.meta.url));
const childPath = fileURLToPath(new URL("../dist/mcp/server.js", import.meta.url));

async function listen(server: Server): Promise<number> {
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("no listening port");
  return address.port;
}

async function closeServer(server: Server): Promise<void> {
  if (server.listening) await new Promise<void>((resolve, reject) =>
    server.close((error) => error ? reject(error) : resolve()));
}

async function stop(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return;
  const exited = once(child, "exit");
  child.kill("SIGTERM");
  const timer = setTimeout(() => child.kill("SIGKILL"), 5000);
  try { await exited; } finally { clearTimeout(timer); }
}

it.skipIf(process.env.RUN_PASSWORD_WORKER_TEST !== "1")(
  "real Pi/MCP/Chromium keeps password authoring and dry-run evidence private",
  async () => {
    const root = await realpath(await mkdtemp(join(tmpdir(), "password-worker-")));
    const canary = "real-chromium-password-canary-7429";
    const witnessed: string[] = [];
    const publicOutputs: unknown[] = [];
    const contexts: unknown[] = [];
    const events: HarnessEvent[] = [];
    let worker: ChildProcess | undefined;
    let session: PiSession | undefined;
    let resources: HarnessResources | undefined;
    let sdk: AgentSession | undefined;
    let workerLog = "";
    const fixture = createServer((req, res) => {
      if (req.method === "POST" && req.url === "/witness") {
        const chunks: Buffer[] = [];
        req.on("data", (chunk) => chunks.push(chunk));
        req.on("end", () => {
          witnessed.push(JSON.parse(Buffer.concat(chunks).toString()).value);
          res.end("ok");
        });
        return;
      }
      res.setHeader("content-type", "text/html");
      res.end(`<!doctype html><title>Login evidence</title>
        <label for="username">Username</label><input id="username">
        <label for="password">Password</label><input id="password" type="password">
        <script>
          document.querySelector('#password').addEventListener('input', event => {
            fetch('/witness', { method: 'POST', body: JSON.stringify({ value: event.target.value }) });
          });
        </script>`);
    });
    try {
      const fixtureUrl = `http://127.0.0.1:${await listen(fixture)}/login`;
      const reservation = createServer();
      const port = await listen(reservation);
      await closeServer(reservation);
      const workerUrl = `http://127.0.0.1:${port}`;
      // Spawn only this test's worker. Never reconfigure or stop the live parent stack.
      worker = spawn(join(workerRoot, ".venv/bin/python"), [
        "-m", "uvicorn", "loop_worker.main:app", "--host", "127.0.0.1", "--port", String(port),
      ], { cwd: workerRoot, env: { ...process.env,
        LOOP_ARTIFACTS_DIR: join(root, "artifacts"),
        LOOP_ENVIRONMENTS_DIR: join(root, "profiles"),
        LOOP_UPLOADS_DIR: join(root, "uploads"),
        LOOP_ENVIRONMENT_ENCRYPTION_KEY: randomBytes(32).toString("base64"),
        LOOP_WORKER_INTERNAL_TOKEN: workerOptions.providerToken,
      },
        stdio: ["ignore", "pipe", "pipe"] });
      worker.stdout!.on("data", (chunk) => { workerLog += chunk; });
      worker.stderr!.on("data", (chunk) => { workerLog += chunk; });
      let healthy = false;
      for (let attempt = 0; attempt < 100; attempt++) {
        healthy = await fetch(`${workerUrl}/health`).then((response) => response.ok, () => false);
        if (healthy) break;
        if (worker.exitCode !== null) throw new Error(`test worker exited: ${workerLog}`);
        await new Promise((resolve) => setTimeout(resolve, 100));
      }
      expect(healthy, workerLog).toBe(true);
      const provision = await fetch(`${workerUrl}/environments/${environmentLease.environment_id}/provision`, {
        method: "POST",
        headers: { authorization: `Bearer ${workerOptions.providerToken}`,
          "x-project-id": environmentLease.project_id, "content-type": "application/json" },
        body: JSON.stringify({
          project_id: environmentLease.project_id, profile_generation: environmentLease.profile_generation,
          fencing_token: 1, policy: { allowed_domains: ["127.0.0.1"] },
        }),
      });
      expect(provision.status, await provision.text()).toBe(200);
      const request: RunRequest = {
        execution: browserExecution, ...environmentLease,
        protocol_version: "pi-browser-harness.v1", run_id: "password-evidence",
        attempt_id: "password-evidence-attempt", goal: `Verify private password entry using ${canary}`,
        model: { provider: "scripted", name: "password-fixture", reasoning_effort: "off" },
        worker_url: workerUrl, artifact_scope: "password-evidence",
      };
      const runtime = await ModelRuntime.create({ refreshOnCreate: false });
      const original = runtime.streamSimple;
      runtime.streamSimple = function(model, context, options) {
        contexts.push(JSON.parse(JSON.stringify(context)));
        const privateStream = original.call(this, model, context, options);
        const stream = createAssistantMessageEventStream();
        void privateStream.result().then((message) => {
          if (message.content.some((block) => block.type === "toolCall" && block.arguments.value === canary)) {
            // A real SDK message with text preceding password tool classification.
            message.content.unshift({ type: "text", text: `I will enter ${canary}` });
          }
          stream.end(message);
        });
        return stream;
      };
      const tools = [
        { name: "open_page", arguments: { url: fixtureUrl, intent: "open login" } },
        { name: "input", arguments: { target: { object: { text: "Username" } },
          value: "public-user", expect_value: "public-user", intent: "enter username" } },
        { name: "input", arguments: { target: { object: { text: "Password" } },
          value: canary, expect_value: canary, intent: "enter password" } },
        { name: "finish_case", arguments: { name: "private password case" } },
      ];
      resources = await prepareHarnessResources(request);
      resources.servers[0]!.args = [childPath];
      session = await createPiSession(request, {
        cwd: resources.cwd, modelRuntime: runtime,
        eventWriter: new HarnessEventWriter(request, new Writable({
          write(chunk, _encoding, done) { events.push(JSON.parse(chunk.toString())); done(); },
        })),
        createAgentSession: async (options) => {
          const result = await createAgentSession(options);
          sdk = result.session;
          sdk.subscribe((event) => {
            if (event.type === "tool_execution_end") {
              publicOutputs.push(event.result);
              expect(JSON.stringify(events)).not.toContain(canary);
            }
          });
          return result;
        },
      }, {
        ...resources.definition,
        scriptedResponse: (_goal, prior) => tools[prior] ? {
          tool: { name: `mcp__browser_harness__${tools[prior]!.name}`,
            arguments: tools[prior]!.arguments as never },
        } : { text: `Password ${canary} verified privately` },
      });
      await session.prompt(request.goal);
      expect(session.getVerdict()).toBe("succeeded");
      expect(witnessed.filter((value) => value === canary).length).toBeGreaterThanOrEqual(2);
      expect(JSON.stringify(publicOutputs)).not.toContain(canary);
      const messages = contexts.flatMap((context) => (context as { messages: { role: string; content: unknown }[] }).messages);
      expect(messages.some((message) => message.role === "user" && JSON.stringify(message.content).includes(canary))).toBe(true);
      expect(JSON.stringify(messages.filter((message) => message.role !== "user"))).not.toContain(canary);
      expect(JSON.stringify(events)).not.toContain(canary);
      expect(workerLog).not.toContain(canary);
      const completed = events.find((event) => event.type === "browser.case.completed");
      expect(completed?.payload.case.steps[1]?.value).toBe("public-user");
      expect(completed?.payload.case.steps[2]?.value).toBe("[redacted]");
      expect(completed?.payload.case.steps[2]?.postconditions[0]?.value).toBe("[redacted]");
      expect(completed?.payload.dry_run.steps.map((step) => [step.index, step.action, step.status]))
        .toEqual([[0, "goto", "passed"], [1, "input", "passed"], [2, "input", "passed"]]);
      expect(() => validateBrowserEventPayload(completed!.type, completed!.payload)).not.toThrow();
      expect(JSON.stringify(contexts)).toContain("public-user");
      const modelEvents = events.filter((event) => event.type === "pi.message.completed");
      expect(modelEvents.some((event) => event.payload.text === "I will enter [redacted]")).toBe(true);
      expect(modelEvents.at(-1)?.payload.text).toBe("Password [redacted] verified privately");
      expect(events.findIndex((event) => event.type === "browser.observation")).toBeGreaterThanOrEqual(0);
      expect(events.findIndex((event) => event.type === "browser.observation"))
        .toBeLessThan(events.findIndex((event) => event.type === "pi.message.completed"));
    } finally {
      try { await session?.close(); } finally {
        try { if (worker) await stop(worker); } finally {
          await closeServer(fixture);
          await resources?.dispose();
          await rm(root, { recursive: true, force: true });
        }
      }
    }
  }, 60_000,
);
