import type { NextApiRequest, NextApiResponse } from "next";
import { z } from "zod/v4";
import { createTRPCContext } from "@/src/server/api/trpc";
import { clientForProject } from "@/src/features/browser-runs/server/project-client";
import { getProxyErrorStatus } from "@/src/features/browser-runs/server/proxy-utils";
import { ControlInputSchema } from "@/src/features/browser-runs/control-contracts";
import { throwIfNoProjectAccess } from "@/src/features/rbac/utils/checkProjectAccess";

const query = z.object({
  projectId: z.string().min(1),
  controlSessionId: z.string().min(1),
});
export const config = { api: { bodyParser: { sizeLimit: "16kb" } } };

export default async function handler(
  req: NextApiRequest,
  res: NextApiResponse,
) {
  res.setHeader("Cache-Control", "private, no-store");
  res.setHeader("X-Content-Type-Options", "nosniff");
  if (!["GET", "POST"].includes(req.method ?? "")) {
    res.setHeader("Allow", "GET, POST");
    res.status(405).end();
    return;
  }
  const parsed = query.safeParse(req.query);
  const input =
    req.method === "POST" ? ControlInputSchema.safeParse(req.body) : undefined;
  if (!parsed.success || (input && !input.success)) {
    res.status(400).json({ message: "Invalid input" });
    return;
  }
  // Cookie-authenticated writes must originate from this Langfuse origin.
  if (req.method === "POST") {
    let originHost;
    try {
      originHost = new URL(req.headers.origin ?? "").host;
    } catch {
      /* invalid origin */
    }
    if (!originHost || originHost !== req.headers.host) {
      res.status(403).json({ message: "Invalid origin" });
      return;
    }
  }
  try {
    const context = await createTRPCContext({
      req,
      res,
      info: {} as Parameters<typeof createTRPCContext>[0]["info"],
    });
    const { projectId, controlSessionId } = parsed.data;
    throwIfNoProjectAccess({
      session: context.session,
      projectId,
      scope: "playground:execute",
    });
    const client = await clientForProject(projectId, context.session);
    const upstream = await fetch(
      new URL(
        `/api/v1/control-sessions/${encodeURIComponent(controlSessionId)}/${req.method === "GET" ? "screenshot" : "input"}`,
        client.apiUrl,
      ),
      {
        method: req.method,
        redirect: "error",
        signal: AbortSignal.timeout(15000),
        headers: {
          Accept: req.method === "GET" ? "image/png" : "application/json",
          ...client.headers,
          ...(input ? { "Content-Type": "application/json" } : {}),
        },
        ...(input?.success ? { body: JSON.stringify(input.data) } : {}),
      },
    );
    if (!upstream.ok) {
      res
        .status(
          [400, 401, 403, 404, 409, 410, 429].includes(upstream.status)
            ? upstream.status
            : 502,
        )
        .json({ message: "Control session unavailable" });
      return;
    }
    if (req.method === "GET") {
      // Bounded read: do not buffer an untrusted provider response without a cap.
      if (upstream.headers.get("Content-Type")?.split(";")[0] !== "image/png")
        throw new Error("Invalid frame type");
      if (!upstream.body) throw new Error("Missing frame");
      const reader = upstream.body.getReader();
      const chunks: Uint8Array[] = [];
      let size = 0;
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        size += value.byteLength;
        if (size > 8 * 1024 * 1024) {
          await reader.cancel();
          throw new Error("Frame too large");
        }
        chunks.push(value);
      }
      const frame = Buffer.concat(chunks);
      if (
        !frame
          .subarray(0, 8)
          .equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]))
      )
        throw new Error("Invalid PNG signature");
      res.setHeader("Content-Type", "image/png");
      res.status(200).send(frame);
    } else {
      res.status(200).json({ accepted: true });
    }
  } catch (error) {
    res
      .status(getProxyErrorStatus(error))
      .json({ message: "Control session unavailable" });
  }
}
