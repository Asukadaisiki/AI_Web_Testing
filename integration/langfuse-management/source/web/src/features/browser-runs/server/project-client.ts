import { TRPCError } from "@trpc/server";
import type { Session } from "next-auth";
import { createAgentTraceClient } from "./agenttrace-client";
import { readBridgeConfig } from "./config";

export async function clientForProject(
  projectId: string,
  session: Session | null,
) {
  const user = session?.user;
  if (!user?.id) throw new TRPCError({ code: "UNAUTHORIZED" });
  const membership = user.organizations
    .flatMap((org) => org.projects)
    .find((project) => project.id === projectId && !project.deletedAt);
  if (!membership && !user.admin) throw new TRPCError({ code: "UNAUTHORIZED" });
  const config = readBridgeConfig(projectId);
  if (!config) throw new TRPCError({ code: "PRECONDITION_FAILED" });
  if (config.langfuseProjectId !== projectId)
    throw new TRPCError({ code: "NOT_FOUND" });
  const role =
    user.admin || membership?.role === "ADMIN" || membership?.role === "OWNER"
      ? "admin"
      : "member";
  const client = createAgentTraceClient({ ...config, actorId: user.id, role });
  await client.verifyBinding();
  return client;
}
