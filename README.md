# AgentTrace Eval Hub v3

One product and execution path: **Langfuse UI/BFF -> authenticated Go Control
API -> project Registry snapshots -> fenced Runtime/Browser Workers**.
PostgreSQL owns durable facts, Kafka delivers commands/events, Redis provides
best-effort live wakeups, and the optional exporter projects actual traces to
Langfuse. Each Run explicitly selects Pi, Claude Code or Codex through its
official SDK in a shared session host.

Management is mandatory. Follow [Management Runtime Setup](docs/management-runtime-setup.md)
for private bridge/provider settings, existing Langfuse project binding,
trusted bootstrap and canonical seed-ID handoff. Open <http://127.0.0.1:3300>
for Conversations, encrypted browser identities, immutable Skill/Harness
versions, reviewed MCP bindings and Services. Each Conversation owns one
Environment; uncertain expired execution remains quarantined as
`lost/needs_review`, not automatically replayed or reset.

## Native Startup

Use existing private configuration and persistent roots. Do not overwrite an
existing `.env`, initialize another Langfuse deployment or launch duplicate
consumers against an active stack.

```bash
# Install dependencies only when needed; Node 24 for native Langfuse.
brew install postgresql@16 redis kafka
npm --prefix pi-browser-harness ci
npm --prefix pi-browser-harness run build
uv sync --project worker
uv run --project worker playwright install chromium
# Privately load application configuration and existing Langfuse destination keys.
python3 integration/local/run.py --with-exporter
```

Prerequisites: existing PostgreSQL/Redis, existing adapted private Langfuse and
nonblank `AGENTTRACE_BRIDGE_TOKEN`, `AGENTTRACE_BROWSER_PROVIDER_TOKEN`,
`AGENTTRACE_LANGFUSE_PROJECT_ID`. `--with-exporter` additionally requires LF
base/public/secret keys. Configure real SDK credentials in ignored `.env` or
process environment; `scripted` is only deterministic fixture execution.
Explicit process variables override `.env`, which overrides native defaults.

Defaults: PG `agenttrace_local` at `5432` with OS user; Kafka `127.0.0.1:9192`;
Redis `127.0.0.1:6379` DB `3`; HTTP/API `8180`; worker `8110`; fixture `8124`;
Langfuse `3300`; runtime concurrency `4`, group `agenttrace-runtime-local`.
The launcher starts fixture, browser Worker, API, Runtime and optional Exporter.
It always seeds Management before application children; it never starts or
replaces Langfuse. Deprecated management flags cannot change the plan.
The four Kafka topics, including Environment commands, require at least eight
partitions. Existing undersized topics fail clearly and require an operator
review, not an automatic partition/offset reset.

`AGENTTRACE_LOCAL_STATE` defaults to the OS temporary directory's
`agenttrace-local`. Preserve existing profile/artifact roots and private
`environment.key` (0600); never regenerate a key against retained profiles.
Native startup maps the provider token to `LOOP_WORKER_INTERNAL_TOKEN` and
refuses a conflicting worker token. The default browser allowlist is exactly
`127.0.0.1,localhost` when unset; explicitly empty denies network.
Ctrl-C stops only launcher-owned children; an occupied port causes refusal.
See [Operations](docs/operations.md) and [native Langfuse ownership/setup](integration/local/langfuse.md).

The seed installs canonical project/Conversation IDs only in child environment.
Use its private JSON-result handoff for the BFF and later acceptance, not an
assumption that another shell inherits them. BFF selects Conversations
dynamically from the bound project; a startup Conversation is not required.
Unset exporter project filter exports all Go projects; a nonempty filter is
rebound to the canonical seeded Go project for single-project export.

## Runtime And Registry

Message submission requires `content`, `runtime_id`, `harness_id` and
`idempotency_key`. Project Registry resolves immutable versions and an owned
ready Environment atomically. Existing managed idempotency serialization is
preserved; replay precedes readiness. Explicit failed-Run rerun copies its
immutable snapshot, preserving lineage even after publish/revoke.
Historical snapshotless Runs remain inspectable, not silently executable.

The Node host supports only shared session-host execution with an explicit
supported SDK and validated snapshot. The reviewed browser MCP supplies actual
tool results, grounded browser progress and awaited cleanup. Generic reviewed
Harnesses need not select browser tools, but all Runs retain managed ownership.
There is no disk-Harness or startup-module execution fallback.

Keep reviewed `AGENTTRACE_PI_HARNESS_ENTRY`, cwd, builtin resource/MCP paths and
Pi/Claude/Codex credentials. `AGENTTRACE_CODEX_TOOL_FORMAT=native` is the default;
explicit `flat-functions` adapts Responses providers that ignore namespace
tools without replacing official SDK execution. Provider errors remain failed
Runs. Missing per-call inputs, timings or billed costs stay unknown.
MCP config accepts deployed reviewed capabilities, not arbitrary commands,
host paths or environment-secret references.

Browser open/replay uses internal auth and complete project/Environment/
generation/fencing identity with a held browser handle. Trace and root
`/artifacts` reads remain authenticated and project-scoped.
See [Architecture](docs/architecture.md) and [Contract](CONTRACT.md).

## Verify

Offline native regression (does not operate the deployed stack):

```bash
python3 -m unittest discover -s integration/local -p '*_test.py'
node --test integration/local/langfuse.test.mjs
cd backend
go test . -run '^Test(OperatorFilesDescribePortableManagedStartup|NativeManagedStartupEnablesParallelRuntimeDelivery|RetiredProductPathsAreAbsent|RetiredV2PathsAreAbsent)$' -count=1
```

Python discovery includes `support_test.py`: shared environment, PG credential
isolation, SSE parsing, bounded polling and authenticated tracked cleanup.
Infrastructure administration and its owned-process/isolated-DB guards remain
separate. Tests may create temporary files and test-owned children.

Parent-authorized live acceptance against already-running managed services:

```bash
python3 integration/local/management.py accept --env-file <private-management-env> \
  --timeout 360 --report <private-management-evidence.json>
```

It creates only its tracked acceptance Conversations/resources in the bound
project; it never resets/deletes the seed Conversation. Actual SDK/browser,
Registry/rerun, identity/control/reset/delete and optional drain are measured.
Successful SDK cases check scoped PG counts, single completion, published
outbox, two authenticated SSE reads/reconnect and optional actual LF export
correlation/available generation input/usage. Unavailable SDKs are skipped;
exit zero/provisional is not full UI/RBAC or fault acceptance.
See setup for finite `management_faults.py` and owned isolated
`infrastructure_faults.py` workflows.

Legacy suites were retired, not declared equivalent: Redis TTL, detailed
command/inbox counts, fixed-model timing/name/current-goal export checks and
generic composition matrix are not all migrated. Parent owns combined Go/Host/
Worker regression, isolated PG checks, live SDK/UI/password evidence, controlled
restart and final cleanup. Langfuse feature tests run in the sibling checkout:
see [current source/build contract](integration/langfuse-management/README.md).
Those `pnpm --filter web` commands target Langfuse, not a root console.

Dated [remediation](docs/remediation-acceptance-2026-10-28.md),
[composition](docs/runtime-harness-acceptance-2026-10-28.md) and
[Langfuse validation](integration/langfuse-management/VALIDATION.md) retain
their original tested scope/build identity. They are not proof of current
deployment parity. [Cleanup integration report](docs/management-only-cleanup-2026-10-28.md)
is parent-owned and records fresh final gates.

## Source Layout

| Path | Responsibility |
| --- | --- |
| `backend/cmd/agenttrace-api` | Authenticated Control API and outbox relay |
| `backend/cmd/agenttrace-management-seed` | Trusted Registry/binding bootstrap |
| `backend/cmd/agenttrace-runtime` | Kafka delivery, leases, fenced execution |
| `backend/cmd/agenttrace-exporter` | Optional asynchronous Langfuse projection |
| `backend/internal/api/v3`, `postgres` | Scoped API, immutable facts, additive migrations |
| `backend/internal/piharness` | Shared JSONL host transport and supervision |
| `pi-browser-harness` | Explicit SDK adapters, snapshot materialization, MCP/browser tools |
| `worker/loop_worker` | Authenticated managed browser provider and replay |
| `fixtures/contract/case_contract.json` | Retained browser interoperability contract |
| `integration/local` | Mandatory native launcher, focused support, managed/owned acceptance |
| `integration/langfuse-management` | Retained Langfuse source capture and feature contracts |

Dockerfiles remain portable component build definitions, with explicit Runtime
and Worker commands. They are not a second deployment orchestrator. Direct
image startup requires trusted bootstrap, native/reachable infrastructure,
managed secrets, stable encrypted-profile key/root and shared artifact mounts;
the Runtime image includes reviewed host resources and MCP entry paths.
