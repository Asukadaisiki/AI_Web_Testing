import { TRPCError } from "@trpc/server";
import { getHTTPStatusCodeFromError } from "@trpc/server/http";

export const MAX_ARTIFACT_BYTES = 10 * 1024 * 1024;

export const isAllowedArtifactPath = (
  path: string,
  artifactPaths: readonly string[],
) => artifactPaths.includes(path);

export const getUpstreamEventCursor = (
  lastEventId: string | undefined,
  after: number | undefined,
) => lastEventId ?? after?.toString();

export const wouldExceedArtifactByteLimit = (
  receivedBytes: number,
  incomingBytes: number,
) => receivedBytes + incomingBytes > MAX_ARTIFACT_BYTES;

export const getProxyErrorStatus = (error: unknown) =>
  error instanceof TRPCError ? getHTTPStatusCodeFromError(error) : 500;
