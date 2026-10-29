# Management Runtime Setup

This is the runnable seed, native process launcher and API acceptance contract
for the Phase 0-4 management design. Langfuse is the sole product UI.
Management is mandatory and requires an authenticated server-side bridge.
Deprecated management flags cannot select another stack.
These instructions do not authorize stopping any existing process.

## Prerequisites

- Native PostgreSQL, Kafka, Redis, `psql`, `kafka-topics`, Go, Node/npm and `uv`.
- Built SDK host: `npm --prefix pi-browser-harness run build`.
- Installed Python worker dependencies and Playwright Chromium.
- A running adapted Langfuse, normally `http://127.0.0.1:3300`, and an existing
  Langfuse project ID. The seed does not create users, memberships or LF projects.
- Configure the real Pi, Claude Code and Codex provider variables in `.env`.
  Catalog availability is only local readiness, not proof of provider access.
- Use a distinct application DB, Kafka runtime/exporter groups, Redis DB,
  worker IDs and private state directory when isolating acceptance. Database
  names for tests must begin `agenttrace_test_`.

Never use a second consumer stack against the same command topics/database
while an old runtime can claim that work. Ports 8180, 8110 and 3300 may already
be owned. Identify process ownership before any parent-directed shutdown.

## Server Environment

Keep credentials in the ignored `.env` or process environment, not command
arguments or committed files. Native startup honors explicit environment
variables first, then `.env`, then native defaults. Docker addresses in an
existing `.env` must be replaced with valid native addresses.

```dotenv
AGENTTRACE_HTTP_ADDR=127.0.0.1:8181
AGENTTRACE_API_URL=http://127.0.0.1:8181
AGENTTRACE_WORKER_URL=http://127.0.0.1:8111
AGENTTRACE_LANGFUSE_BASE_URL=http://127.0.0.1:3300
AGENTTRACE_LANGFUSE_PROJECT_ID=<existing-lf-project-id>
AGENTTRACE_BRIDGE_TOKEN=<private-random-service-token>
AGENTTRACE_BROWSER_PROVIDER_TOKEN=<private-random-worker-token>
AGENTTRACE_BROWSER_ALLOWED_DOMAINS=127.0.0.1,localhost
AGENTTRACE_LOCAL_STATE=<absolute-private-persistent-directory>
```

Also set `AGENTTRACE_POSTGRES_DSN`, `AGENTTRACE_KAFKA_BROKERS`,
`AGENTTRACE_REDIS_ADDR`, `AGENTTRACE_REDIS_DB`, `AGENTTRACE_ARTIFACT_DIR`,
`AGENTTRACE_KAFKA_GROUP_ID`, and real SDK credentials. Choose unique runtime
worker IDs using the current runtime registration configuration.

`AGENTTRACE_BROWSER_ALLOWED_DOMAINS` is an exact lowercase host allowlist
(no schemes, ports or wildcards). Native `run.py` defaults to
`127.0.0.1,localhost` when unset; direct Go startup defaults to deny-all.
Set explicit reviewed real domains before creating their Conversations.
The API and seed both set the Store's deployment policy once; uploaded
Harnesses and Conversation metadata cannot grant network access.
Existing Environment policies are preserved, not silently rewritten by seed.

Native startup maps `AGENTTRACE_BROWSER_PROVIDER_TOKEN` to
`LOOP_WORKER_INTERNAL_TOKEN`; mismatched nonempty values fail closed.
Both tokens are server-only and must never enter the Langfuse browser bundle.

`LOOP_ENVIRONMENTS_DIR` identifies encrypted profiles. When unset, native
startup uses `<AGENTTRACE_LOCAL_STATE>/environments`. An unset
`LOOP_ENVIRONMENT_ENCRYPTION_KEY` produces a Fernet key once in
`<AGENTTRACE_LOCAL_STATE>/environment.key` with mode 0600. Subsequent starts
reuse it. Never delete/rotate the key while retaining profiles; decryption
failure is not grounds for silently creating a replacement identity. For
production set a stable key through secret provisioning and persist/back up
the encrypted root and key under separate access controls.

Lifecycle provision/reset/delete currently target `AGENTTRACE_WORKER_URL`.
An Environment is bound to that registered browser provider before provisioning;
runtime scheduling preserves its browser-worker affinity. Runtime owners may
move without migrating the browser profile. Do not advertise a different local
browser store as a replacement for a bound provider. Horizontal browser-service
failover additionally requires the same encrypted profile root, Fernet key and
compatible shared backing-store locking, or a shared provider service.
Independent local directories are not horizontal browser identity storage.

## Trusted Seed CLI

From `backend`:

```bash
go run ./cmd/agenttrace-management-seed \
  --langfuse-project-id "$AGENTTRACE_LANGFUSE_PROJECT_ID" \
  --resources-dir ../pi-browser-harness/resources
```

Optional flags:

- `--project-id`: existing Go project, default `AGENTTRACE_PROJECT_ID`.
- `--conversation-id`: existing Conversation, default
  `AGENTTRACE_CONVERSATION_ID`; it must belong to the selected project.
- `--actor-id`: trusted deployment audit actor, default `management-seed`.
- `--resources-dir`: reviewed deployed content only; default
  `AGENTTRACE_BUILTIN_HARNESS_DIR`.
- `--timeout`: positive bounded duration, default `60s`.

`AGENTTRACE_POSTGRES_DSN` stays in environment; the CLI intentionally has no
DSN/password flag. It migrates the additive schema, sets management policy,
starts no service, and writes one JSON result to stdout:

```json
{
  "project_id": "prj_...",
  "langfuse_project_id": "...",
  "conversation_id": "conv_...",
  "environment_id": "env_...",
  "mcp_version_id": "mv_...",
  "mcp_binding_id": "binding_...",
  "skill_version_ids": ["sv_..."],
  "harness_id": "ph_...",
  "harness_version_id": "hv_...",
  "runtime_ids": ["pi", "claude-code", "codex"]
}
```

Binding is server-owned and one-to-one. A mismatched LF ID is rejected,
not rebound. Empty project ID resolves the existing LF binding or creates
one bound project. Empty Conversation ID reuses the durable selected seed
Conversation, an existing active Conversation, or creates one.
Explicit existing Conversation IDs preserve their title, metadata, messages
and runs. Missing legacy Environments are added atomically with a
provisioning command/outbox only when execution/delivery is quiescent.
New Environments start provisioning with generation 1 and positive fencing;
they become ready only after the worker's real acknowledgement.

A PostgreSQL session advisory lock serializes seed processes; the connection
pool must allow at least two sessions. Existing PublishMCP, UploadSkill,
PublishSkill and PublishProjectHarness APIs remain the mutation boundary.
Interrupted bootstrap resumes from committed resources. One deployed
`browser-harness` MCP is shared; each project gets its own binding, uploaded
and published builtin Skills, and immutable `Browser Harness (Builtin)`
version supporting all three runtime IDs. No MCP command, host env, provider
secret or plaintext browser storage is persisted by the seed.

Repeated seed does not republish changed disk content, edit bindings or
reset browser state. Revocation/disabled bindings are never silently undone.
Changed reviewed resources require explicit Registry publication, not seed.
The returned Harness ID is not a claim that every SDK is configured.

## Native Launcher

```bash
python3 integration/local/run.py --with-exporter
```

`--skip-build` reuses an already-built SDK host. The launcher creates Kafka
run command/event/DLQ topics plus `agenttrace.environment.commands.v1`,
runs the trusted seed before application processes, and uses its canonical
IDs for child configuration. It starts fixture, worker, API, runtime and the
optional exporter, using configured addresses and checking their ports
before launching. It never starts a separate console and does not start or
replace Langfuse. Bridge/provider tokens, matching worker token and an existing
LF project binding are required before state creation or build/data/service
side effects. Explicit API and HTTP addresses must agree.

Kafka topics target eight partitions. Existing metadata and offsets are retained.
The launcher describes every topic after create-if-absent and refuses an
existing topic with fewer than eight partitions (or unreadable partition count).
It never automatically alters partitions: an operator must review key ordering,
consumers and topology before upgrading an undersized topic.

`--with-exporter` requires LF base URL/public/secret keys. Configure the
adapted Langfuse BFF with the same private bridge token and returned Go/LF
binding using its server environment. Current BFF project routing must use
the canonical Conversation selected by the UI, not a fixed startup
Conversation. Without the exporter, LF trace/session correlation is not
verified merely because management APIs work.

The launcher changes only its child environment, not the parent shell.
For a private canonical-ID handoff, run the trusted seed CLI with the same
deployment environment, redirect its JSON result to an operator-owned 0600
file outside the checkout, and use returned `project_id`/`langfuse_project_id`
to configure BFF and later acceptance. Repeated seed reuses the binding; optional
project/Conversation inputs are seed reuse, not fixed product routing.
The BFF supports `AGENTTRACE_PROJECT_BINDINGS` JSON for explicit LF-to-Go
project mappings. It does not require a startup Conversation ID.
An explicitly nonempty `AGENTTRACE_EXPORT_PROJECT_ID` selects single-project
export; native seed replaces it with the canonical Go project in children.
Unset/empty retains all-project export, so use credentials appropriate to that
deployment scope.

Shutdown handles only children launched by this runner. An occupied port
causes refusal; it is never a reason to kill an unrelated listener.

## Acceptance

Parent runs this only after management workers and LF are ready. Acceptance
does not launch a duplicate stack or kill workers. It creates two new
independent Conversations and isolated acceptance Skill/Harness versions
inside the seeded project. It never resets or deletes the seed Conversation.
It intentionally performs writes, cancellation, reset, revoke and deletion
on its own acceptance resources, and retains audit/evidence IDs.

```bash
python3 integration/local/management.py accept \
  --env-file <private-management-env> \
  --timeout 360 \
  --report <management-evidence.json>
```

Required environment: canonical `AGENTTRACE_PROJECT_ID`,
`AGENTTRACE_LANGFUSE_PROJECT_ID`, `AGENTTRACE_BRIDGE_TOKEN`,
`AGENTTRACE_API_URL`, and `AGENTTRACE_E2E_POSTGRES_DSN` for read-only evidence.
Set `AGENTTRACE_E2E_GOAL_URL` to the existing catalog fixture
(default `http://127.0.0.1:8124/index.html`).
`--env-file` supplements `.env`; exported process variables win.

Every ControlAPI request uses Bearer plus `X-Agenttrace-Project-Id`,
`X-Langfuse-Project-Id`, `X-Agenttrace-Actor-Id` and `X-Agenttrace-Role`.
Credential-bearing requests refuse all HTTP redirects.
Default role is `project_admin`. SQL is read-only and scoped by canonical IDs;
it is not used to fake provisioning, claim leases, or rewrite snapshots.

The scenario records actual catalog, ready Environments, SDK completions,
nonzero model usage, browser tool success, fetched PNG hashes, Run snapshot
digests, explicit negative/rerun lineage, active cancellation, one-time
takeover replay refusal, real screenshot/input/close, generation advance,
and acknowledged delete tombstones. Publishing/revoking newer Skill/Harness
versions must not mutate earlier Run snapshots or rerun snapshots.

Successful SDK cases also compare project-scoped PostgreSQL trace counts,
single durable completion and drained Run outbox, then perform two authenticated
SSE reads with Last-Event-ID reconnect against canonical ordered events.
When private LF keys are supplied, they check real terminal export correlation,
available model inputs and per-span generation usage. This is not identical to
the retired suite: Redis TTL/stream assertions, explicit command/inbox counts,
fixed-model timing/name/current-goal comparisons and generic Harness composition
are not all ported. Missing SDK per-call data stays unknown. Parent-run final
smoke separately checks password absence and real Langfuse UI/RBAC.

Drain selects an idle project-dedicated worker and restores its previous
setting. Shared deployment worker drain is skipped unless the operator
explicitly supplies `AGENTTRACE_ACCEPTANCE_ROLE=system_admin`. A failed
restore is recorded as incomplete cleanup, never hidden.

### Persistent Login Fixture

The ecommerce fixture uses sessionStorage and cannot prove profile identity
across contexts. Parent may explicitly run this tiny localStorage fixture
on a free port; it is not another application stack:

```bash
python3 integration/local/management.py fixture --port 8125
```

Set `AGENTTRACE_MANAGEMENT_IDENTITY_URL` to
`http://127.0.0.1:8125/management_identity.html`. The scenario logs in with a
new fixture-only identity, checks it in a later Run in Conversation A,
checks retained/isolated identity using every available real SDK, checks
Conversation B remains signed out, resets A, then verifies A is
signed out. The fixed fixture password is not a real credential.
This checks actual persisted browser identity, not an injected storage blob.
Without this URL, identity is explicitly skipped/provisional.

### Results And Limits

JSON reports are private (0600), contain IDs/counts/statuses/digests, and omit
credentials, control tokens, profile refs and provider handles. A failed
assertion remains failed. SDK unavailability/scripted mode is skipped, not
passed. Interrupted work is retained with cleanup limitations.

Exit 0 means the checks executed without failure; inspect `status`,
`checks` and `limitations`. Status stays `provisional` because native API
acceptance does not by itself prove the Langfuse browser UI/RBAC workflow or
multi-node failure/expiry recovery. Exit 1 denotes failure/incomplete cleanup;
130 denotes interruption. Parent must append actual LF browser and
controlled outage evidence before claiming full product acceptance.

## Finite Multiworker Faults

The separate Phase 3 harness uses only already-running services and an isolated
`agenttrace_test_*` database. Parent must start two runtime processes with unique
worker IDs in the same consumer group and application DB. Use the current
runtime variables `AGENTTRACE_RUNTIME_WORKER_ID` (different per process),
`AGENTTRACE_RUNTIME_CONCURRENCY`, `AGENTTRACE_BROWSER_WORKER_CAPACITY=2` or
higher (same value on both), and optionally `AGENTTRACE_WORKER_PROJECT_ID`
for dedicated workers. Leave `AGENTTRACE_BROWSER_WORKER_ID` unset to preserve
the canonical endpoint-derived ID shared with API lifecycle/control.
Both runtime owners must be idle, enabled and support the selected real SDK.
Other eligible runtime owners must be disabled
or isolated first; the harness does not drain unrelated workers.
The bound browser provider must have capacity at least two.

```bash
python3 integration/local/management_faults.py \
  --env-file <private-management-env> \
  --runtime-worker <runtime-a-id> \
  --runtime-worker <runtime-b-id> \
  --runtime-id pi --runs 6 --timeout 600 \
  --report <management-fault-evidence.json>
```

Default six initial Runs, bounded to 4-24, execute against distinct Conversations
concurrently. The script requires a sampled overlap of at least two managed
assignments, actual model tokens, browser tools, successful dry runs and fetched
PNG artifacts. It then disables runtime B, witnesses real execution on A, drains
A while its lease is active, cancels with durable acknowledgement, enables B and
requires fresh work to execute exclusively on B. Original drain settings are
restored within a separate 30-second total cleanup budget.

The global scenario deadline is at most 900 seconds. Initial Run count excludes
the small fixed drain/rebalance/survivor workload. This is a finite measured
local workload, not a long-running soak. SDK providers may take longer than the
chosen deadline; that is a recorded failure, not an excuse to fake completions.

### Optional Owned Crash

No signal is sent by default. Parent must explicitly authorize fault injection
and map both registered runtime IDs to directly launched Go binary PIDs:

```bash
python3 integration/local/management_faults.py \
  --env-file <private-management-env> \
  --runtime-worker <runtime-a-id> --runtime-worker <runtime-b-id> \
  --runtime-id pi --runs 6 --timeout 600 \
  --owner-pid <owned-supervisor-pid> \
  --runtime-executable <absolute-path-to-agenttrace-runtime-binary> \
  --runtime-process <runtime-a-id>=<runtime-a-pid> \
  --runtime-process <runtime-b-id>=<runtime-b-pid> \
  --allow-owned-signals \
  --report <management-fault-evidence.json>
```

Use independent runtime binaries and an owned supervisor that keeps the API,
browser provider and survivor runtime alive. Do not target the native `run.py`
stack: its supervisor intentionally stops all children when one child exits.
`go run` wrappers are not accepted as the mapped executable. The operator's
ID-to-PID mapping is explicit deployment evidence, not discoverable from the
product API; verify it before authorizing. Ownership checks reject a supervisor,
non-descendants, changed parent/start-time/command and wrong executable. Only
the selected first runtime binary receives SIGKILL, immediately after fresh
lease/Run/PID verification. No unowned PID, browser worker or SDK descendant is
signaled automatically.

Set `AGENTTRACE_MANAGEMENT_IDENTITY_URL` to the explicit localStorage fixture.
The script waits for acknowledged browser login click and an actual
`assert_text` of a unique identity, fetches a real PNG, then signals the active
runtime owner. Recovery must show `execution_lost`, lost started attempt,
expired assignment/lease, advanced fencing and `needs_review` without another
attempt. It repeats that check after real successful work on the survivor.
It does not mark the uncertain Environment ready, reset it or automatically
retry the side effect.

Healthy Conversations are deleted only after measurements are collected.
The crash Conversation and its evidence remain for operator review; encrypted
browser state may not include the just-witnessed in-memory side effect when
the host died. The killed runtime is not restarted. Parent must inspect the
recorded fault-time descendant PIDs and any later descendants for orphan
SDK/browser processes before cleanup or an explicit review decision. A report
of `needs_review` proves quarantine, not that all external effects rolled back.

### Measurements

Samples include selected DB connection counts, scenario queued/active Runs,
active assignments and runtime owners. Durable per-attempt timing includes
creation-to-assignment queue delay, creation-to-SDK-start delay, lease duration
and request-to-terminal cancellation acknowledgement. Reports retain sample
counts and p50/p95/max; missing measurements stay null rather than zero.
All queries execute inside `BEGIN READ ONLY` with statement timeout.
Connection counts include the harness's own read-only probe and all sessions
in the chosen DB; they are not individual process-pool measurements.

Memory is measured only with explicitly owned process roots:
`--owner-pid <supervisor> --measure-pid <api-binary-pid>
--measure-pid <runtime-a-pid> --measure-pid <runtime-b-pid>`.
The optional signal mappings include runtime trees automatically. RSS includes
descendants present at each sample, records missing/exited identities as null,
and never sums overlapping root trees. Without owned roots, memory is explicitly
unmeasured/provisional.

Optional operator budgets are `--max-db-connections`, `--max-queue-seconds`
(p95 assignment delay) and `--max-cancel-seconds` (durable maximum).
No production-scale performance claim follows from an unbudgeted local test.
Signals omitted, unavailable SDKs, missing memory or external LF/browser-store
checks remain explicit skips/limitations. Exit 0 is not full acceptance:
inspect each check, `status`, `measurements`, retained resources and limitations.

## Verification

Offline launcher/scenario tests:

```bash
python3 -m unittest discover -s integration/local -p '*_test.py'
node --test integration/local/langfuse.test.mjs
```

Use Node 24 for the Langfuse launcher tests. `support_test.py` is included in
Python discovery and checks the extracted environment/PG/SSE/poll/cleanup
adapters. It supplies no unauthenticated HTTP or legacy run creation.

Operator/source contracts, from `backend`:

```bash
go test . -run '^Test(OperatorFilesDescribePortableManagedStartup|NativeManagedStartupEnablesParallelRuntimeDelivery|RetiredProductPathsAreAbsent|RetiredV2PathsAreAbsent)$' -count=1
```

Focused Go tests, from `backend`:

```bash
go test ./cmd/agenttrace-management-seed ./internal/postgres \
  -run '^Test(ManagementSeed|SeedCLI)' -count=1
```

Real seed PostgreSQL tests use only
`AGENTTRACE_SEED_TEST_POSTGRES_DSN`, which must name `agenttrace_test_*`.
The test helper connects to the maintenance DB, creates its own uniquely
named `agenttrace_test_seed_*` DB, migrates it, and drops only that DB.
It never resets the DSN's DB or shares another agent's test variable.
The local PostgreSQL role therefore needs CREATE/DROP DATABASE permission.
Unset DSN skips integration tests, not proof of database behavior.
