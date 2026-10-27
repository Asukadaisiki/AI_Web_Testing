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

## Fix Round 1/5

### Changes

- Added the schema-required exact `intent` values to the scripted canonical
  and `DoesNotExist` `assert_text` calls, and wrapped each canonical target in
  the registered `target.object` shape. Focused tests validate every canonical
  payload through the registered MCP Zod schemas.
- Replaced Pi runtime's historical any-pass state with the latest
  `browser.case.completed` verdict, so a later failed case controls outcome.
- Added a local import-path bootstrap to preserve both root-level and
  `integration/v3` unittest invocations.

### RED

```text
$ npm test --cache /tmp/agenttrace-npm-cache -- --run test/pi-session.test.ts
Test Files  1 failed (1)
Tests       2 failed | 10 passed (12)

The canonical assert_text payload failed the registered MCP Zod schema because
intent was missing; the DoesNotExist assertion was missing its expected intent.

$ go test ./internal/runtime -run TestPiRunnerFailsWhenLatestCaseVerdictFailsAfterEarlierPass -count=1
--- FAIL: TestPiRunnerFailsWhenLatestCaseVerdictFailsAfterEarlierPass
    pi_runner_test.go:71: error = <nil>, want terminalRunError
FAIL

$ python3 -m unittest integration/v3/test_pi_harness_scenario.py
ModuleNotFoundError: No module named 'pi_harness_scenario'

$ npm test --cache /tmp/agenttrace-npm-cache -- --run test/pi-session.test.ts
Test Files  1 failed (1)
Tests       1 failed | 11 passed (12)

The expanded canonical MCP schema check rejected target: {text: ...}; the
registered target schema requires target: {object: {text: ...}}.
```

### GREEN

```text
$ npm test --cache /tmp/agenttrace-npm-cache -- --run test/pi-session.test.ts
Test Files  1 passed (1)
Tests       12 passed (12)

$ go test ./internal/runtime -count=1
ok  	github.com/Asukadaisiki/AI_Web_Testing/backend/internal/runtime

$ python3 -m unittest integration/v3/test_pi_harness_scenario.py
Ran 11 tests
OK

$ cd integration/v3 && python3 -m unittest test_pi_harness_scenario.py
Ran 11 tests
OK

$ npm test --cache /tmp/agenttrace-npm-cache -- --run
Test Files  14 passed (14)
Tests       146 passed (146)

$ npm run typecheck --cache /tmp/agenttrace-npm-cache
tsc --noEmit

$ npm run build --cache /tmp/agenttrace-npm-cache
tsc -p tsconfig.build.json
```
