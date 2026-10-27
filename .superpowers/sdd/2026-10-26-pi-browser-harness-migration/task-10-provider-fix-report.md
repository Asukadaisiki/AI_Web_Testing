# Task 10 Supplemental Provider Fix Report

## Changes

- Preserved the existing Docker production-dependency copy and its integration
  assertion.
- Registered the exact requested model ID for scripted and OpenAI-compatible
  providers with the required model metadata.
- Made the scripted, network-free fixture emit the canonical browser MCP tool
  sequence from prior `toolResult` messages, including the `DoesNotExist`
  failure path without `finish_case`.
- Added focused provider-resolution and real `streamSimple` regression tests.

## RED

```text
$ npm test --cache /tmp/agenttrace-npm-cache -- --run test/pi-session.test.ts
Test Files  1 failed (1)
Tests       4 failed | 8 passed (12)

model scripted/browser-eval-fixture is not available
model openai-compatible/gpt-test is not available
```

The four newly added tests failed before the production TypeScript change
because neither requested model was registered.

## GREEN

```text
$ npm test --cache /tmp/agenttrace-npm-cache -- --run test/pi-session.test.ts
Test Files  1 passed (1)
Tests       12 passed (12)

$ npm test --cache /tmp/agenttrace-npm-cache -- --run
Test Files  14 passed (14)
Tests       146 passed (146)

$ npm --cache /tmp/agenttrace-npm-cache run typecheck
tsc --noEmit

$ npm --cache /tmp/agenttrace-npm-cache run build
tsc -p tsconfig.build.json

$ cd integration/v3 && python3 -m unittest test_pi_harness_scenario.py
Ran 11 tests in 0.319s
OK
```

## Self-Review

- `git diff --check` passed.
- Only the required Docker/integration changes plus the provider source and
  focused test changes are included.
- The registered models use the requested ID for both `id` and `name`, zero
  costs, text input, context window `128_000`, and `maxTokens` `4096`.
- Scripted tool calls use `mcp__browser_harness__*`, deterministic
  `scripted-tool-<step>` IDs, `toolUse` stop reasons, and the dynamically
  requested model ID.

## Concern

The exact root-level command requested by the brief,
`python3 -m unittest integration/v3/test_pi_harness_scenario.py`, fails before
test collection because the existing test uses sibling absolute imports
(`pi_harness_scenario`). The repository-supported invocation above passes; no
unrelated Python import/package restructuring was added.
