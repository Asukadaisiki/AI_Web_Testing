import type { NextApiRequest, NextApiResponse } from "next";
import { z } from "zod/v4";
import { createTRPCContext } from "@/src/server/api/trpc";
import { clientForProject } from "@/src/features/browser-runs/server/project-client";
import {
  getProxyErrorStatus,
  getUpstreamEventCursor,
} from "@/src/features/browser-runs/server/proxy-utils";

const querySchema = z.object({
  runId: z.string().min(1),
  projectId: z.string().min(1),
  after: z.coerce.number().int().nonnegative().optional(),
});

export default async function handler(
  req: NextApiRequest,
  res: NextApiResponse,
) {
  if (req.method !== "GET") {
    res.setHeader("Allow", "GET");
    res.status(405).end();
    return;
  }
  const parsed = querySchema.safeParse(req.query);
  if (!parsed.success) {
    res.status(400).json({ message: "Invalid input" });
    return;
  }

  const { runId, projectId, after } = parsed.data;
  let client;
  try {
    const context = await createTRPCContext({
      req,
      res,
      info: {} as Parameters<typeof createTRPCContext>[0]["info"],
    });
    client = await clientForProject(projectId, context.session);
    await client.authorizeEvents(runId);
  } catch (error) {
    res
      .status(getProxyErrorStatus(error))
      .json({ message: "Unable to authorize events" });
    return;
  }

  const abort = new AbortController();
  req.on("close", () => abort.abort());
  const lastEventId = req.headers["last-event-id"];
  const cursor = getUpstreamEventCursor(
    typeof lastEventId === "string" ? lastEventId : undefined,
    after,
  );
  try {
    const upstream = await fetch(
      new URL(
        `/api/v1/runs/${encodeURIComponent(runId)}/events`,
        client.apiUrl,
      ),
      {
        headers: {
          Accept: "text/event-stream",
          ...client.headers,
          ...(cursor ? { "Last-Event-ID": cursor } : {}),
        },
        signal: abort.signal,
        redirect: "error",
      },
    );
    if (!upstream.ok || !upstream.body) {
      res
        .status(upstream.status || 502)
        .json({ message: "Unable to read events" });
      return;
    }
    res.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      "X-Accel-Buffering": "no",
    });
    res.flushHeaders();
    const reader = upstream.body.getReader();
    while (true) {
      const { done, value: chunk } = await reader.read();
      if (done) break;
      if (abort.signal.aborted) break;
      res.write(chunk);
    }
    if (!abort.signal.aborted) res.end();
  } catch {
    if (!abort.signal.aborted && !res.headersSent) {
      res.status(502).json({ message: "Unable to read events" });
    } else if (!res.writableEnded) {
      res.destroy();
    }
  }
}
