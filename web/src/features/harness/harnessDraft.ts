import { errorMessage, parseHarnessDefinition, type HarnessDefinition, type HarnessSnapshot } from '../../api';

export interface HarnessDraft {
  name: string;
  prompt: string;
  skillsJSON: string;
  mcpJSON: string;
}

const sensitiveEnvName = /token|secret|password|credential|api[_-]?key|authorization|access[_-]?key|private[_-]?key/i;
const envReference = /^env:[A-Za-z_][A-Za-z0-9_]*$/;

export function harnessDraft(snapshot?: HarnessSnapshot): HarnessDraft {
  const mcp = Object.fromEntries(Object.entries(snapshot?.mcp ?? {}).map(([name, server]) => [
    name, {
      ...server,
      ...(server.env === undefined ? {} : {
        env: Object.fromEntries(Object.entries(server.env).map(([key, value]) => [
          key, sensitiveEnvName.test(key) && !envReference.test(value) ? '' : value,
        ])),
      }),
    },
  ]));
  return {
    name: snapshot?.name ?? '',
    prompt: snapshot?.system_prompt ?? '',
    skillsJSON: JSON.stringify(snapshot?.skills ?? [], null, 2),
    mcpJSON: JSON.stringify(mcp, null, 2),
  };
}

function parseJSON(text: string, label: string): unknown {
  try {
    return JSON.parse(text) as unknown;
  } catch {
    throw new Error(`${label} must contain valid JSON.`);
  }
}

export function validateHarnessDraft(draft: HarnessDraft): {
  definition: HarnessDefinition | null;
  error: string | null;
} {
  try {
    const definition = parseHarnessDefinition({
      name: draft.name.trim(),
      system_prompt: draft.prompt,
      skills: parseJSON(draft.skillsJSON, 'Skills'),
      mcp: parseJSON(draft.mcpJSON, 'MCP'),
    });
    if (definition.name === '') throw new Error('Harness name is required.');
    const names = new Set<string>();
    for (const skill of definition.skills) {
      if (!/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(skill.name)) {
        throw new Error('Skill names must be simple names, not paths.');
      }
      if (names.has(skill.name)) throw new Error('Skill names must be unique.');
      names.add(skill.name);
      const paths = new Set<string>();
      for (const file of skill.files ?? []) {
        if (
          file.path.includes('\\') || /^[A-Za-z]:/.test(file.path) ||
          file.path.split('/').some((part) => part === '' || part === '.' || part === '..')
        ) {
          throw new Error('Skill file paths must be scoped relative paths without traversal.');
        }
        if (paths.has(file.path)) throw new Error('Skill file paths must be unique within each skill.');
        paths.add(file.path);
      }
    }
    for (const [name, server] of Object.entries(definition.mcp)) {
      if (name.trim() === '' || server.command.trim() === '') {
        throw new Error('Each MCP server needs a name and command.');
      }
      for (const [key, value] of Object.entries(server.env ?? {})) {
        if (sensitiveEnvName.test(key) && !envReference.test(value)) {
          throw new Error('Sensitive MCP environment values are hidden. Use env:VARIABLE_NAME references, not credentials.');
        }
      }
    }
    return { definition, error: null };
  } catch (failure) {
    return { definition: null, error: errorMessage(failure) };
  }
}
