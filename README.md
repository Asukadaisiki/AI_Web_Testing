# AgentTrace Eval Hub v3

AgentTrace Eval Hub turns a conversation request into a durable, browser-backed
evaluation run. PostgreSQL is the system of record, Kafka carries commands and
events, Redis provides best-effort live updates, and the Python worker executes
the local storefront fixture with Playwright. The production agent loop runs
inside one shared **session host** per Go runtime process. Each execution
attempt selects Pi, Claude Code or Codex through its official SDK adapter.
A separately managed Harness supplies system prompt, skills and MCP servers.
The default browser Harness uses deterministic TypeScript logic and the worker
HTTP API. The Go runtime consumes normalized events and writes durable traces;
the Langfuse exporter observes those traces independently.

## Langfuse runtime management

The Phase 0–4 management implementation uses Langfuse as the sole product UI.
Enable `AGENTTRACE_MANAGEMENT_ENABLED=true` and follow
[Management Runtime Setup](docs/management-runtime-setup.md) for the authenticated
project binding, registry seed and native deployment. Open
<http://127.0.0.1:3300> to manage Conversations, encrypted browser identities,
Skill/MCP/Harness versions and worker drain. The native launcher does not start
the legacy React console in management mode.

Each Conversation owns one Browser Environment. Runs pin immutable reviewed
capabilities and versions; expired execution with possible external effects is
quarantined as `lost/needs_review` for explicit inspection or reset.
See the [implementation plan](docs/superpowers/plans/2026-10-28-langfuse-agent-runtime-management.md)
for phase requirements and acceptance evidence.

The final UI artifact is `.next-management-20261028-04`: shared 128-character
Skill slug validation, accessible upload feedback, and measured Services
DLQ/audit/health/Kafka data with explicit unavailable reasons. Build and private
bundle/source scan passed; 87 client tests and typecheck passed. The broader
server suite has one preserved independent snapshot-provenance failure.
See [final source/build validation](integration/langfuse-management/VALIDATION.md)
for exact counts and the parent-owned restart/read-only retest.

## Clean start

The following commands describe the retained compatibility deployment.
Use the management setup above for the Langfuse management product.

The local runner uses native PostgreSQL and Redis, starts an isolated native
Kafka broker, and runs the API, Runtime, browser Worker and React console.
The default provider is the real `glm-5.3-flash` model. Keep its credentials
in the ignored `.env`; execution mode is explicitly selected by
`AGENTTRACE_MODEL_PROVIDER=openai-compatible` or `scripted`.

```bash
# Copy only when .env does not already exist, then set the model key locally.
cp -n .env.example .env
brew install postgresql@16 redis kafka
brew services start postgresql@16
brew services start redis
cd pi-browser-harness && npm ci && npm run build && cd ..
cd worker && uv sync && uv run playwright install chromium && cd ..
cd web && npm ci && cd ..
export AGENTTRACE_LOCAL_STATE="$HOME/Library/Caches/agenttrace-native"
export LANGFUSE_NATIVE_ROOT="$AGENTTRACE_LOCAL_STATE/langfuse"
bash integration/local/langfuse.sh setup
bash integration/local/langfuse.sh up
set -a
source "$LANGFUSE_NATIVE_ROOT/agenttrace.env"
set +a
python3 integration/local/run.py --with-exporter
```

Open the workspace at <http://127.0.0.1:5184>, inspect API health at
<http://127.0.0.1:8180/api/v1/health>, and use the fixture site at
<http://127.0.0.1:8124>. Langfuse is <http://localhost:3300>.
The application database is `agenttrace_local` on native PostgreSQL `5432`;
Kafka uses `9192`, and Redis notifications use database `3`. The runner creates
the application database and Kafka topics. Ctrl-C stops only the processes it
started. Existing PostgreSQL, Redis and other project processes stay running.

Native Langfuse prerequisites and initialization are documented in
[`integration/local/langfuse.md`](integration/local/langfuse.md).
Never commit `.env` or provider credentials. Langfuse export remains asynchronous;
its outage does not change execution results.

## Runtime and Harness composition

Choose Runtime and Harness in the Langfuse composer. Registry uploads and
previews project Skills, publishes immutable Harness versions and binds
system-reviewed MCP capabilities. Every Run stores a complete immutable Harness
snapshot, including its revision. A failed-Run rerun uses that snapshot even
after a newer version is published or its source version is revoked.

| Component | Responsibility |
| --- | --- |
| Go control plane | Durable Runs, command delivery, concurrency, cancellation and evidence |
| SDK adapters | Pi, Claude Code `query`, Codex `startThread/runStreamed` |
| Registry | Immutable Skill/Harness versions and reviewed MCP capability bindings |
| Langfuse | Product UI, project RBAC and asynchronous native trace projection |

The host uses Strategy to select an Adapter. Harness snapshots contain no SDK
types; changing Runtime does not require another Harness. Generic Harnesses
need no browser Worker. Selecting the browser MCP adds objective browser proof
and awaited session cleanup.

Configure the additional SDKs in ignored `.env`:

```dotenv
AGENTTRACE_CLAUDE_API_KEY=
AGENTTRACE_CLAUDE_BASE_URL=https://api.unself.cn/v1
AGENTTRACE_CLAUDE_MODEL=glm-5.3-flash
AGENTTRACE_CODEX_API_KEY=
AGENTTRACE_CODEX_BASE_URL=https://api.unself.cn/v1
AGENTTRACE_CODEX_MODEL=glm-5.3-flash
AGENTTRACE_CODEX_HTTP_HEADERS={}
AGENTTRACE_CODEX_TOOL_FORMAT=flat-functions
```

Compatible provider endpoints and models can replace these values. The runtime
catalog reports local configuration readiness; actual SDK/provider failures
remain visible as failed Runs. MCP execution accepts only deployed reviewed
capability identifiers and schema-validated configuration. Arbitrary commands,
host paths and `env:VARIABLE_NAME` references are rejected. Scoped Secret Manager
references resolve only in the host; credential values are absent from catalog
responses and execution snapshots.
`AGENTTRACE_CODEX_TOOL_FORMAT` defaults to `native`. Set `flat-functions` for
Responses providers that ignore namespace tools, including the tested GLM
endpoint. This Attempt-local provider adapter preserves official SDK execution,
actual MCP results and reported usage; it requires no separate service.

## Verify

Verify the complete real-model workflow against the local stack:

```bash
AGENTTRACE_E2E_POSTGRES_DSN="postgresql://$(whoami)@127.0.0.1:5432/agenttrace_local" \
python3 integration/local/acceptance.py \
  --env-file "$LANGFUSE_NATIVE_ROOT/agenttrace.env"
```

The acceptance retains its scoped project and verifies success, failure, active
cancellation, PostgreSQL evidence, Kafka/outbox delivery, Redis notifications,
ordered SSE replay and real Langfuse traces. Reports omit credentials and payloads.
Go integration tests must use a separate database named `agenttrace_test_*`
and a dedicated Redis test database, never the application database.

Verify the SDK composition matrix with browser cleanup acknowledgements:

```bash
# Keep this test-only proxy running in another terminal.
python3 integration/local/worker_audit.py --port 8112 \
  --log "$AGENTTRACE_LOCAL_STATE/worker-audit.jsonl"
AGENTTRACE_E2E_WORKER_URL=http://127.0.0.1:8112 \
AGENTTRACE_E2E_WORKER_AUDIT="$AGENTTRACE_LOCAL_STATE/worker-audit.jsonl" \
python3 integration/local/composition.py \
  --env-file "$LANGFUSE_NATIVE_ROOT/agenttrace.env" \
  --report "$AGENTTRACE_LOCAL_STATE/composition-acceptance.json"
```

This creates isolated Harnesses and tests all three official SDKs with the same
browser Harness and the same generic Harness, prompt/skill effects, negative
assertions, cancellation, snapshots after edit/delete, failed-Run reruns, SSE
and optional Langfuse. The running application's Worker remains on `8110`.

Verified native results and architecture corrections are recorded in
[`docs/remediation-acceptance-2026-10-28.md`](docs/remediation-acceptance-2026-10-28.md).
The three official SDKs, independent Harness snapshots and optional Langfuse
passed all 15 composition scenarios; see
[`docs/runtime-harness-acceptance-2026-10-28.md`](docs/runtime-harness-acceptance-2026-10-28.md).

Individual layers:

```bash
cd backend && go test -p 1 ./...
cd worker && uv run python -m unittest discover -s tests -t .
cd web && npm test -- --run --minWorkers=1 --maxWorkers=2 && npm run build
AGENTTRACE_API_URL=http://127.0.0.1:8180 \
AGENTTRACE_E2E_POSTGRES_DSN="postgresql://$(whoami)@127.0.0.1:5432/agenttrace_local" \
  npm run test:e2e
```

`integration/v3` retains the older Compose-specific scenarios for container
deployments. The native workflow uses `integration/local`; it needs no Docker.

The shared-host integration test can run without Compose. Build the Node
package, then point the Go test at its compiled entrypoint:

```bash
cd pi-browser-harness
npm ci --ignore-scripts --no-audit --no-fund
npm run build
cd ../backend
AGENTTRACE_TEST_HARNESS_ENTRY="$(pwd)/../pi-browser-harness/dist/main.js" \
  go test -race ./internal/piharness -run TestSharedHostWithRealPiAndReplacementHarness -count=1
```

It exercises the real Pi SDK and MCP transport with a replacement Harness and
controlled HTTP execution, including concurrent sessions, targeted cancellation,
cleanup acknowledgements and process reuse. Native acceptance separately checks
PostgreSQL, Kafka, Redis, Playwright and Langfuse.

For operations, reset procedures, backlog inspection, and failure recovery, see
[`docs/operations.md`](docs/operations.md). The architecture and canonical
browser scenario are documented in
[`docs/architecture.md`](docs/architecture.md).

## Source layout

| Path | Responsibility |
| --- | --- |
| `backend/cmd/agenttrace-api` | HTTP API, PostgreSQL writes, and outbox relay |
| `backend/cmd/agenttrace-runtime` | Kafka command consumer and durable run runtime |
| `backend/cmd/agenttrace-exporter` | Optional Langfuse event exporter |
| `backend/internal/api/v3` | REST, SSE, health, and artifact contracts |
| `backend/internal/domain`, `event`, `execution` | Run/event contracts and SDK-free execution snapshots |
| `backend/internal/piharness` | Shared session-host transport, process supervision and JSONL validation |
| `backend/internal/postgres` | PostgreSQL schema and durable repositories |
| `backend/internal/traceprojection` | Derived Trace step projection from operation evidence |
| `pi-browser-harness` | Generic session host, Pi/Claude Code/Codex SDK adapters, Harness materialization and browser tools |
| `pi-browser-harness/Dockerfile` | Multi-stage image: builds the harness with Node.js and installs it alongside the Go runtime |
| `worker/loop_worker` | Worker HTTP API and Playwright execution |
| `web/src/features/trace` | Trace parsing, tree normalization, and derived-node inspection |
| `web/src/features/workspace` | Three-column project, conversation, and trace workspace |
| `fixtures/contract/case_contract.json` | Retained browser case contract |
| `integration/local` | Native application/Langfuse launchers, real-model acceptance and fault drills |
| `integration/v3` | Canonical Compose scenario and failure drills |
