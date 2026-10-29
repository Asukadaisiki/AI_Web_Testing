# Langfuse Runtime Management

Working source: `/Users/bytedance/project/langfuse-agenttrace`.
Product route: `/project/{langfuseProjectId}/browser-runs`.
Langfuse remains the sole product UI and authenticated BFF.

## Source And Build

`source/` is a full-source overlay limited to browser-runs, its tests, AgentTrace
API routes and the project page. It includes the existing first-loop evidence
feature needed by management. `wiring.patch` changes only the sidebar label and
server env schema. It intentionally excludes the sibling's unrelated dirty work,
Next build directories and private environment values.

Capture again after further sibling edits:

```bash
bash integration/langfuse-management/capture.sh /Users/bytedance/project/langfuse-agenttrace
```

Apply to a checkout that already has the first-loop integration:

```bash
bash integration/langfuse-management/bootstrap.sh /absolute/path/to/langfuse-agenttrace
```

The prerequisite root router has the import
`@/src/features/browser-runs/server/router` and `browserRuns: browserRunsRouter`.
Do not overlay over unreviewed feature-local changes; bootstrap deliberately
does not reset or commit anything.

Final artifact: `.next-management-20261028-05`, built under Node 24 with the
parent's private matching acceptance env. Native build/client soundness scan
and the separate private-value static bundle/source scan passed. Use `-05`;
earlier builds are historical. Parent updated local services after quiescence.
The completed original UI session has 33 screenshots. Final05 adds three
read-only screenshots verifying Skill validation and Services telemetry.

Final source includes shared 128-character Skill-name validation, accessible
help/error and invalid upload gating without trimming. Services displays
measured DLQ/audit/health/Kafka data with scopes and reasons, preserving null as
unavailable and real zero as zero. See `VALIDATION.md` for exact outcomes:
87 client tests, 86 server tests and typecheck passed. Parent reproduced and
fixed the foreign snapshot-project guard; historical failure remains recorded.
Additive heartbeat/lease telemetry is projected into `observability` and its
deployed rendering was verified.

Verified native root: `/Users/bytedance/Library/Caches/agenttrace-native/langfuse`.
Private credentials: that directory's `credentials.json` (0600).
Exporter settings: that directory's `agenttrace.env`. The old bridge settings
are separate under `langfuse-first-loop/bridge.private.env`; use only the new
management stack's matching bridge token and project mapping.

Node 24 executable used: `/usr/local/bin/node` (`v24.18.0`).
Exact build invocation from the sibling, with Node 24 and pnpm 11.10.0
(already completed; the wrapper will not reuse an existing target):

```bash
env PATH="/usr/local/bin:$PATH" \
LANGFUSE_NATIVE_SOURCE=/Users/bytedance/project/langfuse-agenttrace \
LANGFUSE_NATIVE_ROOT=/Users/bytedance/Library/Caches/agenttrace-native/langfuse \
LANGFUSE_NATIVE_NEXT_DIST_DIR=.next-management-20261028-05 \
pnpm --filter web exec dotenv \
  -e /Users/bytedance/Library/Caches/agenttrace-management-acceptance-20261028/acceptance.env \
  -- bash /Users/bytedance/project/AI_Web_Testing/integration/local/langfuse.sh build
```

The native wrapper maps `LANGFUSE_NATIVE_NEXT_DIST_DIR` to `NEXT_DIST_DIR`,
requires a fresh web-local basename other than `.next`, and runs its private
build log and client-bundle soundness scan. The separate private-value scan
checks the completed static assets and captured source against privately loaded
credentials; no credential values are printed. Select another fresh name if the
directory already exists. This command does not restart services.

Equivalent direct build from the sibling, using the parent's configured
environment:

```bash
NEXT_DIST_DIR="$LANGFUSE_NATIVE_NEXT_DIST_DIR" pnpm --filter web build
```

After explicit restart coordination, the parent may use the existing native
`up` command with the SAME dist variable. Direct Next start also needs the SAME
`NEXT_DIST_DIR` and coordinated port. Do not serve the old build for acceptance.

## Server Configuration

- `AGENTTRACE_API_URL`: trusted API origin.
- `AGENTTRACE_BRIDGE_TOKEN`: server-only bearer credential; never NEXT_PUBLIC.
- Initial mapping: `AGENTTRACE_LANGFUSE_PROJECT_ID` plus `AGENTTRACE_PROJECT_ID`.
- Multiple mappings: `AGENTTRACE_PROJECT_BINDINGS` JSON object mapping Langfuse
  project ID to AgentTrace project ID; this takes precedence for an explicit key.
- `AGENTTRACE_CONVERSATION_ID` is no longer used. Conversation selection is dynamic.

Every upstream call carries `Authorization: Bearer <server token>`,
`X-Agenttrace-Project-Id`, `X-Langfuse-Project-Id`, `X-Agenttrace-Role`
(`admin` or `member`, derived from current membership), and
`X-Agenttrace-Actor-Id`. Binding is verified before each BFF client is used.
Langfuse viewer writes are rejected locally. Registry administration and
services require project-update permission. No platform MCP write is exposed.

See `CONTRACTS.md` for wire details and `TAKEOVER-CONTRACT.md` for agent handoff.

## Validation

Run from the sibling:

```bash
pnpm --filter web run test src/__tests__/server/unit/browser-runs
pnpm --filter web run test-client src/features/browser-runs
pnpm --filter web run typecheck
```

Fresh final outcomes are recorded in `VALIDATION.md`. Tests use controlled
upstream responses, not a deployed management stack. The actual UI workflows
were already exercised in `live-browser-evidence-20261028-212222/VALIDATION.md`;
do not replay their mutations. Parent should only verify invalid Skill-name
feedback/disabled upload, valid-name enabled upload and read-only Services
display after the coordinated final05 restart; those checks have completed.

Real API list/create/archive verification, from the sibling:

```bash
pnpm --filter web exec tsx --tsconfig tsconfig.json \
  /Users/bytedance/project/AI_Web_Testing/integration/langfuse-management/live-management-check.mts \
  --env-file /absolute/path/to/new-management-bridge.env
```

This calls the actual BFF client and records only sanitized endpoint/status,
non-secret conversation inputs, returned IDs and assertions in
`live-evidence.json`. It deliberately creates an acceptance conversation and
leaves it archived. No bridge values are logged.
