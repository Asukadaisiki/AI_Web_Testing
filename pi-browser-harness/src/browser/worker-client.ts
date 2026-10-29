import {
  parseActResponse,
  parseExecutionResult,
  parseObservation,
  type ActRequest,
  type ActResponse,
  type Case,
  type ExecutionResult,
  type Observation,
} from "./contract.js";

const DEFAULT_TIMEOUT_MS = 5 * 60 * 1_000;
const DEFAULT_MAX_RESPONSE_BYTES = 4 * 1024 * 1024;

export interface WorkerClientOptions {
  timeoutMs?: number;
  maxResponseBytes?: number;
  environmentLease?: {
    project_id: string;
    environment_id: string;
    profile_generation: number;
    fencing_token: number;
  };
  providerToken?: string;
}

export class WorkerTransportError extends Error {
  constructor(
    public readonly statusCode: number,
    public readonly code: string,
    public readonly detail: string,
  ) {
    super(`${code}: ${detail}`);
    this.name = "WorkerTransportError";
  }
}

interface WorkerErrorBody {
  error: string;
  detail: string;
}

export class WorkerClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly environmentLease: WorkerClientOptions["environmentLease"];
  private readonly headers: Record<string, string>;
  private browserSessionId: string | undefined;

  constructor(baseUrl: string, options: WorkerClientOptions = {}) {
    const parsed = new URL(baseUrl);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      throw new TypeError("worker base URL must use HTTP or HTTPS");
    }
    this.baseUrl = parsed.toString().replace(/\/$/, "");
    this.timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    this.maxResponseBytes =
      options.maxResponseBytes ?? DEFAULT_MAX_RESPONSE_BYTES;
    const lease = options.environmentLease;
    if (!lease?.project_id?.trim() || !lease.environment_id?.trim() ||
      !Number.isSafeInteger(lease.profile_generation) || lease.profile_generation < 1 ||
      !Number.isSafeInteger(lease.fencing_token) || lease.fencing_token < 1 || !options.providerToken?.trim()) {
      throw new TypeError("managed browser requires complete lease metadata and host provider token");
    }
    this.environmentLease = { ...lease };
    this.headers = {
      authorization: `Bearer ${options.providerToken}`,
      "x-project-id": lease.project_id,
      "x-environment-id": lease.environment_id,
      "x-profile-generation": String(lease.profile_generation),
      "x-fencing-token": String(lease.fencing_token),
    };
    if (!Number.isInteger(this.timeoutMs) || this.timeoutMs <= 0) {
      throw new RangeError("timeoutMs must be a positive integer");
    }
    if (
      !Number.isInteger(this.maxResponseBytes) ||
      this.maxResponseBytes <= 0
    ) {
      throw new RangeError("maxResponseBytes must be a positive integer");
    }
  }

  async openSession(
    artifactScope: string,
    signal?: AbortSignal,
  ): Promise<string> {
    const payload = await this.request(
      "POST",
      "/sessions",
      { session_id: artifactScope, ...this.environmentLease },
      signal,
    );
    if (
      !isRecord(payload) ||
      typeof payload.session_id !== "string" ||
      payload.session_id.trim() === ""
    ) {
      throw malformedResponse(200, "session response requires a non-empty session_id");
    }
    this.browserSessionId = payload.session_id;
    return payload.session_id;
  }

  async navigate(
    browserSessionId: string,
    url: string,
    signal?: AbortSignal,
  ): Promise<Observation> {
    const payload = await this.request(
      "POST",
      `/sessions/${encodeURIComponent(browserSessionId)}/navigate`,
      { url },
      signal,
    );
    return decodeSuccess(payload, parseObservation);
  }

  async act(
    browserSessionId: string,
    request: ActRequest,
    signal?: AbortSignal,
  ): Promise<ActResponse> {
    const payload = await this.request(
      "POST",
      `/sessions/${encodeURIComponent(browserSessionId)}/act`,
      request,
      signal,
    );
    return decodeSuccess(payload, parseActResponse);
  }

  async execute(
    artifactScope: string,
    artifact: Case,
    signal?: AbortSignal,
  ): Promise<ExecutionResult> {
    if (!this.browserSessionId) {
      throw new TypeError("managed dryrun requires a held browser session");
    }
    const payload = await this.request(
      "POST",
      "/execute",
      { session_id: artifactScope, case: artifact,
        ...this.environmentLease, browser_session_id: this.browserSessionId,
      },
      signal,
    );
    return decodeSuccess(payload, parseExecutionResult);
  }

  async closeSession(
    browserSessionId: string,
    signal?: AbortSignal,
  ): Promise<void> {
    const payload = await this.request(
      "DELETE",
      `/sessions/${encodeURIComponent(browserSessionId)}`,
      undefined,
      signal,
    );
    if (!isRecord(payload) || payload.closed !== true) {
      throw malformedResponse(200, "close response requires closed=true");
    }
    if (this.browserSessionId === browserSessionId) this.browserSessionId = undefined;
  }

  private async request(
    method: "POST" | "DELETE",
    path: string,
    body: unknown,
    externalSignal?: AbortSignal,
  ): Promise<unknown> {
    const controller = new AbortController();
    let timedOut = false;
    const onExternalAbort = (): void => {
      controller.abort(externalSignal?.reason);
    };
    if (externalSignal?.aborted === true) {
      onExternalAbort();
    } else {
      externalSignal?.addEventListener("abort", onExternalAbort, { once: true });
    }
    const timer = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, this.timeoutMs);

    try {
      const response = await fetch(`${this.baseUrl}${path}`, {
        method,
        headers: { ...this.headers, ...(body === undefined ? {} : { "content-type": "application/json" }) },
        ...(body === undefined
          ? {}
          : {
              body: JSON.stringify(body),
            }),
        signal: controller.signal,
        redirect: "error",
      });

      const payload = await this.readJson(response);
      if (!response.ok) {
        const workerError = parseWorkerError(payload);
        if (workerError === null) {
          throw new WorkerTransportError(
            response.status,
            "worker_error",
            "worker returned a malformed error response",
          );
        }
        throw new WorkerTransportError(
          response.status,
          workerError.error,
          workerError.detail,
        );
      }
      return payload;
    } catch (error) {
      if (externalSignal?.aborted === true) {
        throw externalSignal.reason instanceof Error
          ? externalSignal.reason
          : error;
      }
      if (timedOut) {
        throw new WorkerTransportError(
          0,
          "worker_error",
          `worker request timed out after ${this.timeoutMs}ms`,
        );
      }
      if (error instanceof WorkerTransportError) {
        throw error;
      }
      throw new WorkerTransportError(
        0,
        "worker_error",
        `worker request failed: ${errorMessage(error)}`,
      );
    } finally {
      clearTimeout(timer);
      externalSignal?.removeEventListener("abort", onExternalAbort);
    }
  }

  private async readJson(response: Response): Promise<unknown> {
    const contentLength = response.headers.get("content-length");
    if (
      contentLength !== null &&
      Number.parseInt(contentLength, 10) > this.maxResponseBytes
    ) {
      await response.body?.cancel();
      throw oversizedResponse(response.status, this.maxResponseBytes);
    }
    if (response.body === null) {
      throw malformedResponse(response.status, "worker returned an empty response");
    }

    const reader = response.body.getReader();
    const chunks: Uint8Array[] = [];
    let total = 0;
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) {
          break;
        }
        total += value.byteLength;
        if (total > this.maxResponseBytes) {
          await reader.cancel();
          throw oversizedResponse(response.status, this.maxResponseBytes);
        }
        chunks.push(value);
      }
    } catch (error) {
      if (error instanceof WorkerTransportError) {
        throw error;
      }
      throw malformedResponse(
        response.status,
        `failed to read worker response: ${errorMessage(error)}`,
      );
    }

    const bytes = new Uint8Array(total);
    let offset = 0;
    for (const chunk of chunks) {
      bytes.set(chunk, offset);
      offset += chunk.byteLength;
    }
    try {
      return JSON.parse(new TextDecoder().decode(bytes)) as unknown;
    } catch (error) {
      throw malformedResponse(
        response.status,
        `worker returned malformed JSON: ${errorMessage(error)}`,
      );
    }
  }
}

function decodeSuccess<T>(
  payload: unknown,
  decode: (value: unknown) => T,
): T {
  try {
    return decode(payload);
  } catch (error) {
    throw malformedResponse(
      200,
      `worker returned an invalid success payload: ${errorMessage(error)}`,
    );
  }
}

function malformedResponse(
  statusCode: number,
  detail: string,
): WorkerTransportError {
  return new WorkerTransportError(statusCode, "worker_error", detail);
}

function oversizedResponse(
  statusCode: number,
  maxBytes: number,
): WorkerTransportError {
  return malformedResponse(
    statusCode,
    `worker response exceeds the ${maxBytes}-byte limit`,
  );
}

function parseWorkerError(value: unknown): WorkerErrorBody | null {
  if (
    !isRecord(value) ||
    typeof value.error !== "string" ||
    typeof value.detail !== "string"
  ) {
    return null;
  }
  return { error: value.error, detail: value.detail };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
