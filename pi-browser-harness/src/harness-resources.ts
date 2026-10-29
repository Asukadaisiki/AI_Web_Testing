import { mkdir, mkdtemp, readFile, readdir, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { parseExecutionSpec, normalizeMcpName } from "./execution-spec.js";
import { resolveMcpCapabilities, type McpResolutionOptions } from "./mcp-capability-resolver.js";
import type { HarnessDefinition, HarnessMcpServer } from "./harness-definition.js";
import type { RunRequest } from "./run-protocol.js";

export interface HarnessResources {
  cwd: string;
  systemPrompt: string;
  skillPaths: string[];
  servers: HarnessMcpServer[];
  definition: HarnessDefinition;
  dispose(): Promise<void>;
}

export async function prepareHarnessResources(
  request: RunRequest, options: McpResolutionOptions = {},
): Promise<HarnessResources> {
  const rawExecution = (request as RunRequest & { execution?: unknown }).execution;
  const execution = rawExecution === undefined ? undefined : parseExecutionSpec(rawExecution);
  const snapshot = execution?.harness;
  const hasBrowser = !snapshot || Object.values(snapshot.mcp).some((server) => server.capability === "browser-harness");
  const browser = hasBrowser ? (await import("./browser-harness.js")).createBrowserHarnessDefinition() : undefined;
  const servers = snapshot ? await resolveMcpCapabilities(snapshot.mcp, request, options, browser) : [
    ...(typeof browser!.mcpServers === "function" ? browser!.mcpServers(request) : browser!.mcpServers),
  ];
  const repository = await realpath(fileURLToPath(new URL("../../", import.meta.url)));
  const preferredTemp = await realpath(tmpdir());
  const outside = (path: string): boolean => {
    const distance = relative(repository, path);
    return isAbsolute(distance) || distance === ".." || distance.startsWith(`..${sep}`);
  };
  const tempBase = outside(preferredTemp) ? preferredTemp : await realpath("/tmp");
  if (!outside(tempBase)) throw new Error("attempt temporary resources must be outside the repository");
  const cwd = await mkdtemp(join(tempBase, "agenttrace-attempt-"));
  let disposing: Promise<void> | undefined;
  const dispose = (): Promise<void> => {
    disposing ??= rm(cwd, { recursive: true, force: true });
    return disposing;
  };
  try {
    const skillRoot = join(cwd, "skills");
    await mkdir(skillRoot, { mode: 0o700 });
    if (snapshot) {
      for (const skill of snapshot.skills) {
        await privateFile(join(skillRoot, skill.name, "SKILL.md"), skill.content);
        for (const file of skill.files ?? []) {
          await privateFile(join(skillRoot, skill.name, file.path), file.content);
        }
      }
    } else {
      for (const source of browser!.skillPaths) await copySkills(source, skillRoot);
    }
    const skillPaths = [skillRoot];
    const systemPrompt = snapshot?.system_prompt ?? browser!.systemPrompt;
    const browserServer = servers.find((server) => normalizeName(server.name) === "browser_harness");
    const definition: HarnessDefinition = snapshot ? {
      systemPrompt, skillPaths, mcpServers: servers,
      toolAllowlist: servers.map((server) => `mcp__${normalizeName(server.name)}__*`),
      completionPolicy: browserServer ? browser!.completionPolicy : ({ assistant_settled, aborted }) => ({
        verdict: assistant_settled && !aborted ? "succeeded" : "failed",
      }),
      formatPrompt: (goal) => [
        ...snapshot.skills.flatMap((skill) => [
          `Selected Harness skill: ${skill.name}\n${skill.content}`,
          ...(skill.files ?? []).map((file) =>
            `Selected Harness skill file: ${skill.name}/${file.path}\n${file.content}`),
        ]),
        `Goal:\n${goal}`,
      ].join("\n\n"),
      ...(browserServer ? {
        completionEventTypes: browser!.completionEventTypes!,
        createEventMapper: browser!.createEventMapper!,
        cleanup: browser!.cleanup!,
      } : {}),
    } : { ...browser!, systemPrompt, skillPaths, mcpServers: servers };
    return { cwd, systemPrompt, skillPaths, servers, definition, dispose };
  } catch (error) {
    await dispose();
    throw error;
  }
}

/** Identical server/tool identities across SDK MCP naming conventions. */
export const normalizeName = normalizeMcpName;

async function privateFile(path: string, content: string | Uint8Array): Promise<void> {
  await mkdir(resolve(path, ".."), { recursive: true, mode: 0o700 });
  await writeFile(path, content, { flag: "wx", mode: 0o600 });
}

async function copySkills(source: string, destination: string): Promise<void> {
  for (const entry of await readdir(source, { withFileTypes: true })) {
    const target = join(destination, entry.name);
    if (entry.isDirectory()) {
      await mkdir(target, { mode: 0o700 });
      await copySkills(join(source, entry.name), target);
    } else if (entry.isFile()) {
      await privateFile(target, await readFile(join(source, entry.name)));
    } else {
      throw new Error("unsupported harness skill resource path");
    }
  }
}
