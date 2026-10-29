import { createServer, type IncomingMessage } from "node:http";
import { AgentTraceClient } from "@/src/features/browser-runs/server/agenttrace-client";
import type { BridgeConfig } from "@/src/features/browser-runs/contracts";

const harness = {
  id: "har_example",
  version: 3,
  harness_id: "harness-1",
  project_id: "at-project",
  content_digest: "digest",
  name: "Generic",
  system_prompt: "",
  skill_version_ids: [],
  mcp_binding_ids: [],
  runtime_ids: ["pi", "claude-code", "codex"],
};
const runtime = {
  id: "codex",
  name: "Codex",
  model: { provider: "openai-responses", name: "configured-model" },
  available: true,
};
const run = {
  id: "run_1",
  project_id: "at-project",
  conversation_id: "conv-fixed",
  status: "failed",
  checkpoint_version: 1,
  created_at: "2026-10-28T00:00:00Z",
  updated_at: "2026-10-28T00:00:00Z",
  runtime_id: "codex",
  harness_id: "har_example",
  harness_revision: 3,
};
const trace = {
  spans: [],
  attempts: [],
  usage: [],
  operation_evidence: [],
  derived_nodes: [],
};
const input = {
  projectId: "lf-project",
  conversationId: "conv-fixed",
  goal: "Use the acceptance skill.",
  runtimeId: "codex" as const,
  harnessId: "har_example",
  idempotencyKey: "draft-key",
};

async function boundary(
  overrides: Record<string, { status?: number; body?: unknown }> = {},
) {
  const requests: {
    path: string;
    method: string;
    body: unknown;
    authorization: string | undefined;
  }[] = [];
  const readBody = async (request: IncomingMessage) => {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const raw = Buffer.concat(chunks).toString();
    return raw ? JSON.parse(raw) : undefined;
  };
  const server = createServer(async (request, response) => {
    const pathname = request.url!;
    requests.push({
      path: pathname,
      method: request.method!,
      body: await readBody(request),
      authorization: request.headers.authorization,
    });
    const defaults: Record<string, unknown> = {
      "/api/v1/runtimes": { runtimes: [runtime] },
      "/api/v1/projects/at-project/harnesses":
        request.method === "GET" ? { harnesses: [harness] } : harness,
      "/api/v1/projects/at-project/harnesses/harness-1/versions": harness,
      "/api/v1/conversations/conv-fixed": {
        conversation: { id: "conv-fixed", project_id: "at-project" },
        messages: [],
        runs: [run],
      },
      "/api/v1/conversations/conv-fixed/messages": {
        run_id: "run_1",
        status: "queued",
      },
      "/api/v1/runs/run_1": run,
      "/api/v1/runs/run_1/trace": trace,
      "/api/v1/runs/run_1/cancel": { run_id: "run_1", status: "cancelling" },
      "/api/v1/runs/run_1/reruns": { run_id: "run_2", status: "queued" },
    };
    const result = overrides[pathname];
    response.writeHead(result?.status ?? 200, {
      "content-type": "application/json",
    });
    response.end(JSON.stringify(result?.body ?? defaults[pathname] ?? {}));
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("No TCP address");
  const config: BridgeConfig = {
    apiUrl: `http://127.0.0.1:${address.port}`,
    bridgeToken: "server-owned-token",
    langfuseProjectId: "lf-project",
    agenttraceProjectId: "at-project",
    actorId: "user-1",
    role: "admin",
  };
  return {
    client: new AgentTraceClient(config),
    requests,
    close: () => new Promise<void>((resolve) => server.close(() => resolve())),
  };
}

describe("Runtime/Harness controlled HTTP boundary", () => {
  it("submits the selected identities with no URL, agent fallback or model credentials", async () => {
    const upstream = await boundary();
    try {
      await expect(upstream.client.createRun(input)).resolves.toEqual({
        run_id: "run_1",
        status: "queued",
      });
      expect(
        upstream.requests.find((request) => request.method === "POST")?.body,
      ).toEqual({
        content: "Use the acceptance skill.",
        runtime_id: "codex",
        harness_id: "har_example",
        idempotency_key: "draft-key",
      });
      expect(
        upstream.requests.every(
          (request) => request.authorization === "Bearer server-owned-token",
        ),
      ).toBe(true);
      expect(
        upstream.requests.some((request) => request.path.includes("/agents")),
      ).toBe(false);
    } finally {
      await upstream.close();
    }
  });

  it("normalizes an optional HTTP URL into the goal without forwarding another field", async () => {
    const upstream = await boundary();
    try {
      await upstream.client.createRun({
        ...input,
        url: " https://example.test ",
      });
      expect(
        upstream.requests.find((request) => request.method === "POST")?.body,
      ).toEqual({
        content: "Open https://example.test. Use the acceptance skill.",
        runtime_id: "codex",
        harness_id: "har_example",
        idempotency_key: "draft-key",
      });
    } finally {
      await upstream.close();
    }
  });

  it("rejects an unavailable runtime without submitting", async () => {
    const upstream = await boundary({
      "/api/v1/runtimes": {
        body: {
          runtimes: [
            {
              ...runtime,
              available: false,
              unavailable_reason: "Executable unavailable",
            },
          ],
        },
      },
    });
    try {
      await expect(upstream.client.createRun(input)).rejects.toMatchObject({
        code: "BAD_REQUEST",
      });
      expect(
        upstream.requests.some((request) => request.method === "POST"),
      ).toBe(false);
    } finally {
      await upstream.close();
    }
  });

  it.each(["file:///tmp/test", "not a url"])(
    "rejects invalid optional URL %s before HTTP",
    async (url) => {
      const upstream = await boundary();
      try {
        await expect(
          upstream.client.createRun({ ...input, url }),
        ).rejects.toMatchObject({ code: "BAD_REQUEST" });
        expect(upstream.requests).toEqual([]);
      } finally {
        await upstream.close();
      }
    },
  );

  it("rejects the wrong LF project before HTTP", async () => {
    const upstream = await boundary();
    try {
      await expect(
        upstream.client.createRun({ ...input, projectId: "foreign" }),
      ).rejects.toMatchObject({ code: "NOT_FOUND" });
      expect(upstream.requests).toEqual([]);
    } finally {
      await upstream.close();
    }
  });

  it("validates named catalogs and retains historical run identity", async () => {
    const upstream = await boundary();
    try {
      expect(await upstream.client.listRuntimes()).toEqual([runtime]);
      expect(await upstream.client.listHarnesses()).toEqual([harness]);
      expect(await upstream.client.getRun("run_1")).toMatchObject({
        runtime_id: "codex",
        harness_id: "har_example",
        harness_revision: 3,
      });
    } finally {
      await upstream.close();
    }
  });

  it("rejects malformed catalog wrappers", async () => {
    const upstream = await boundary({
      "/api/v1/runtimes": { body: [runtime] },
    });
    try {
      await expect(upstream.client.listRuntimes()).rejects.toMatchObject({
        code: "INTERNAL_SERVER_ERROR",
      });
    } finally {
      await upstream.close();
    }
  });

  it("publishes Harness creation and updates through project-scoped version endpoints", async () => {
    const upstream = await boundary();
    try {
      const definition = {
        name: "Generic",
        system_prompt: "",
        skill_version_ids: [],
        mcp_binding_ids: [],
        runtime_ids: ["pi", "claude-code", "codex"] as (
          | "pi"
          | "claude-code"
          | "codex"
        )[],
      };
      expect(await upstream.client.createHarness(definition)).toEqual(harness);
      expect(
        await upstream.client.updateHarness(harness.harness_id, definition),
      ).toEqual(harness);
      expect(
        upstream.requests.map((request) => [
          request.method,
          request.path,
          request.body,
        ]),
      ).toEqual([
        ["POST", "/api/v1/projects/at-project/harnesses", definition],
        [
          "POST",
          "/api/v1/projects/at-project/harnesses/harness-1/versions",
          definition,
        ],
      ]);
    } finally {
      await upstream.close();
    }
  });

  it("reads canonical missing evidence without inventing evaluation fields", async () => {
    const upstream = await boundary();
    try {
      expect(await upstream.client.getTrace("run_1")).toEqual(trace);
      expect(upstream.requests.map((request) => request.path)).toEqual([
        "/api/v1/runs/run_1",
        "/api/v1/runs/run_1/trace",
      ]);
    } finally {
      await upstream.close();
    }
  });

  it.each(["getTrace", "cancelRun", "authorizeEvents"] as const)(
    "preflights foreign Go project for %s",
    async (method) => {
      const upstream = await boundary({
        "/api/v1/runs/run_1": { body: { ...run, project_id: "foreign" } },
      });
      try {
        await expect(upstream.client[method]("run_1")).rejects.toMatchObject({
          code: "NOT_FOUND",
        });
        expect(upstream.requests).toHaveLength(1);
      } finally {
        await upstream.close();
      }
    },
  );

  it("permits Run detail from another conversation in the same project", async () => {
    const upstream = await boundary({
      "/api/v1/runs/run_1": { body: { ...run, conversation_id: "foreign" } },
    });
    try {
      await expect(upstream.client.getRun("run_1")).resolves.toMatchObject({
        conversation_id: "foreign",
      });
    } finally {
      await upstream.close();
    }
  });
});
