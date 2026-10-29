import { TRPCError } from "@trpc/server";
import {
  getUpstreamEventCursor,
  getProxyErrorStatus,
  isAllowedArtifactPath,
  wouldExceedArtifactByteLimit,
} from "@/src/features/browser-runs/server/proxy-utils";

describe("browser-run proxy boundaries", () => {
  it("allows only an exact canonical artifact path", () => {
    expect(
      isAllowedArtifactPath("run_1/final.png", [
        "run_1/final.png",
        "run_1/step-1.png",
      ]),
    ).toBe(true);
    expect(
      isAllowedArtifactPath("../run_1/final.png", ["run_1/final.png"]),
    ).toBe(false);
    expect(
      isAllowedArtifactPath("run_1/final.png?other", ["run_1/final.png"]),
    ).toBe(false);
  });

  it("maps authorization errors to their HTTP status before proxy headers", () => {
    expect(getProxyErrorStatus(new TRPCError({ code: "FORBIDDEN" }))).toBe(403);
    expect(getProxyErrorStatus(new TRPCError({ code: "NOT_FOUND" }))).toBe(404);
  });

  it("forwards Last-Event-ID before an initial validated after cursor", () => {
    expect(getUpstreamEventCursor("7", 3)).toBe("7");
    expect(getUpstreamEventCursor(undefined, 3)).toBe("3");
    expect(getUpstreamEventCursor(undefined, undefined)).toBeUndefined();
  });

  it("rejects an artifact while streaming when its byte cap is crossed", () => {
    expect(wouldExceedArtifactByteLimit(10, 20)).toBe(false);
    expect(wouldExceedArtifactByteLimit(10, 11 * 1024 * 1024)).toBe(true);
  });
});
