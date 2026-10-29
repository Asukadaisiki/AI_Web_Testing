import { afterEach, beforeEach, vi } from "vitest";
import type { ExecutionSpec } from "../../src/execution-spec.js";
import type { RunRequest } from "../../src/run-protocol.js";
import { prepareHarnessResources } from "../../src/harness-resources.js";
import { createPiSession, type PiSession, type PiSessionDeps } from "../../src/pi-session.js";
import { SessionCleanupError } from "../../src/server.js";
import { scriptedResponse } from "./canonical-browser-script.js";

export const genericExecution: ExecutionSpec = {
  runtime_id: "pi", harness: {
    id: "selected", name: "Selected", revision: 1,
    system_prompt: "Reply directly. Do not use tools.", skills: [], mcp: {},
  },
};

export const browserExecution: ExecutionSpec = {
  runtime_id: "pi", harness: {
    id: "browser-selected", name: "Selected browser", revision: 1,
    system_prompt: "Selected prompt: author exactly one executable browser case.",
    skills: [{ name: "selected-browser", content:
      "---\nname: selected-browser\ndescription: Selected browser instructions\n---\nFollow selected browser instructions." }],
    mcp: { browser: { capability: "browser-harness", config: {}, secret_refs: {} } },
  },
};

export const environmentLease = {
  project_id: "fixture-project", environment_id: "fixture-environment",
  profile_generation: 1, fencing_token: 2,
};
export const workerOptions = { environmentLease, providerToken: "private-fixture-token" };
export const browserChildEnv = {
  AGENTTRACE_HARNESS_PROJECT_ID: environmentLease.project_id,
  AGENTTRACE_HARNESS_ENVIRONMENT_ID: environmentLease.environment_id,
  AGENTTRACE_HARNESS_PROFILE_GENERATION: String(environmentLease.profile_generation),
  AGENTTRACE_HARNESS_FENCING_TOKEN: String(environmentLease.fencing_token),
  AGENTTRACE_BROWSER_PROVIDER_TOKEN: workerOptions.providerToken,
};

interface SelectedHarnessTestOptions {
  /** Per-fixture opt-in for a cleanup rejection explicitly asserted by its test. */
  expectedCleanupFailure?: (error: unknown) => boolean;
}

interface OwnedFixture extends SelectedHarnessTestOptions {
  dispose(): Promise<void>;
  session?: PiSession;
  initializationCleanupFailure?: SessionCleanupError;
}

const fixtures: OwnedFixture[] = [];
beforeEach(() => vi.stubEnv("AGENTTRACE_BROWSER_PROVIDER_TOKEN", workerOptions.providerToken));
export async function cleanupSelectedHarnessFixtures(): Promise<void> {
  try {
    const outcomes = await Promise.allSettled(fixtures.splice(0).map(async (fixture) => {
      try {
        if (fixture.initializationCleanupFailure) throw fixture.initializationCleanupFailure;
        await fixture.session?.close();
      } catch (error) {
        if (!fixture.expectedCleanupFailure?.(error)) throw error;
      }
      // Retain uncertain resources unless this fixture explicitly matched the failure.
      await fixture.dispose();
    }));
    const failures = outcomes.flatMap((outcome) => outcome.status === "rejected" ? [outcome.reason] : []);
    if (failures.length) throw new AggregateError(failures, "Selected Harness fixture cleanup failed");
  } finally {
    vi.unstubAllEnvs();
  }
}
afterEach(cleanupSelectedHarnessFixtures);

/** Test composition only: callbacks and local child paths never enter a wire snapshot. */
export async function createSelectedBrowserSession(
  request: RunRequest, deps: PiSessionDeps & { mcpChildPath?: string },
  options: SelectedHarnessTestOptions = {},
): Promise<PiSession> {
  const resources = await prepareHarnessResources(request);
  const fixture: OwnedFixture = { dispose: resources.dispose, ...options };
  fixtures.push(fixture);
  if (deps.mcpChildPath) resources.servers[0]!.args = [deps.mcpChildPath];
  try {
    const session = await createPiSession(request, { ...deps, cwd: deps.cwd ?? resources.cwd }, {
      ...resources.definition, scriptedResponse,
    });
    const close = session.close;
    let closing: Promise<void> | undefined;
    session.close = () => closing ??= (async () => {
      await close();
      await resources.dispose();
    })();
    fixture.session = session;
    return session;
  } catch (error) {
    if (error instanceof SessionCleanupError) fixture.initializationCleanupFailure = error;
    else await resources.dispose();
    throw error;
  }
}
