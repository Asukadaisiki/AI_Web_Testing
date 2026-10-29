import type { NextApiRequest, NextApiResponse } from "next";
import { z } from "zod/v4";
import { createTRPCContext } from "@/src/server/api/trpc";
import { clientForProject } from "@/src/features/browser-runs/server/project-client";
import {
  getProxyErrorStatus,
  isAllowedArtifactPath,
  wouldExceedArtifactByteLimit,
} from "@/src/features/browser-runs/server/proxy-utils";
import { screenshotPathsForRun } from "@/src/features/browser-runs/trace-evidence";

const querySchema = z.object({
  runId: z.string().min(1),
  projectId: z.string().min(1),
  path: z.string().min(1),
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

  const { runId, projectId, path } = parsed.data;
  let trace;
  let client;
  try {
    const context = await createTRPCContext({
      req,
      res,
      info: {} as Parameters<typeof createTRPCContext>[0]["info"],
    });
    client = await clientForProject(projectId, context.session);
    trace = await client.getTrace(runId);
  } catch (error) {
    res
      .status(getProxyErrorStatus(error))
      .json({ message: "Unable to authorize artifact" });
    return;
  }
  if (!isAllowedArtifactPath(path, screenshotPathsForRun(trace, runId))) {
    res.status(404).json({ message: "Artifact not found" });
    return;
  }

  const abort = new AbortController();
  req.on("close", () => abort.abort());
  try {
    const encodedPath = path.split("/").map(encodeURIComponent).join("/");
    const upstream = await fetch(
      new URL(`/artifacts/${encodedPath}`, client.apiUrl),
      {
        headers: client.headers,
        signal: abort.signal,
        redirect: "error",
      },
    );
    const contentType = upstream.headers.get("content-type") ?? "";
    if (!upstream.ok || !upstream.body || !contentType.startsWith("image/")) {
      res
        .status(upstream.status || 502)
        .json({ message: "Artifact unavailable" });
      return;
    }
    res.writeHead(200, {
      "Content-Type": contentType,
      "Cache-Control": "private, no-store",
      "X-Content-Type-Options": "nosniff",
    });
    let bytes = 0;
    const reader = upstream.body.getReader();
    while (true) {
      const { done, value: chunk } = await reader.read();
      if (done) break;
      if (wouldExceedArtifactByteLimit(bytes, chunk.byteLength)) {
        abort.abort();
        res.destroy();
        return;
      }
      bytes += chunk.byteLength;
      if (abort.signal.aborted) break;
      res.write(chunk);
    }
    if (!abort.signal.aborted) res.end();
  } catch {
    if (!abort.signal.aborted && !res.headersSent) {
      res.status(502).json({ message: "Artifact unavailable" });
    } else if (!res.writableEnded) {
      res.destroy();
    }
  }
}
