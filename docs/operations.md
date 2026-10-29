# Operations

## Native Startup

Follow [Management Runtime Setup](management-runtime-setup.md) before operating
services. Management is mandatory; startup requires private bridge/provider
tokens, matching worker token and existing LF project binding before build,
data or service side effects. Use the existing private Langfuse deployment,
not another setup invocation against retained state.

```bash
# Load ignored application config and private existing LF destination settings.
python3 integration/local/run.py --with-exporter
```

Defaults: API `8180`, Worker `8110`, fixture `8124`, Langfuse `3300`, native Kafka
`9192`, PG database `agenttrace_local` on `5432`, Redis DB `3`.
HTTP/API overrides must agree. Real execution needs a configured official SDK;
scripted is deterministic fixture execution, not real acceptance.
Pi/Claude/Codex credentials stay in execution environment, not snapshots.
Explicit environment overrides ignored `.env`, then native defaults.

Always use the same private persistent state, encrypted profile key/root and
artifact root across restarts. Default state is the OS temp directory's
`agenttrace-local`; use a persistent path for long-lived evidence. Native setup
reuses `environment.key` (0600), rejects insecure/invalid keys and will not
generate a replacement against retained profiles. Back up the key separately.
Do not rotate/delete a key or reset uncertain identity as a startup repair.

The launcher creates four eight-partition topics, including Environment
commands, then verifies partition counts. Existing undersized topics fail;
review ordering/topology before a deliberate operator upgrade. No automatic
partition alteration or metadata/offset reset occurs. Runtime default group is
`agenttrace-runtime-local` and concurrency is four. Do not share command topics
between independent databases or launch duplicate stacks against active work.

Trusted seed runs before application children and reuses durable binding.
Only child environment receives canonical IDs. Hand its private JSON result to
the BFF/acceptance as described in setup; the BFF routes Conversations dynamically.
Nonempty exporter project filter is replaced with the canonical Go project;
empty/unset preserves all-project export. LF destination keys must match scope.

Ctrl-C joins only this launcher's child groups; an occupied port is refusal,
not permission to kill a listener. Existing PG/Redis/Kafka/LF processes stay
untouched. A launcher child failure intentionally stops its remaining children.
Before any parent-directed restart, verify actual ownership, quiescent Runs/
leases/controls/delivery and preserved quarantine; do not assume old evidence
proves present safety. LF has a separate ownership-aware launcher documented
in [native Langfuse](../integration/local/langfuse.md). A bounded shutdown
warning requires process/log inspection, not an unidentified kill.

## Auth And Evidence

Control API operations require Bearer plus Go/LF project, actor and role
headers. Never place secrets in URLs or public bundles; credential-bearing
acceptance requests refuse redirects and cross-origin forwarding.
Health is a dependency probe, not execution/export/UI acceptance.

```bash
curl -fsS http://127.0.0.1:8180/api/v1/health
bash integration/local/langfuse.sh status
```

Health exposes independent dependency availability. PG is canonical;
Redis degradation uses PostgreSQL polling for SSE. Exporter lag is separate
from LF web health. Every durable event and outbox row commits together;
Kafka consumers deduplicate delivery, terminal work does not recreate sessions.
`run.completed` is the only completion type. Live notifier wakeups are
disposable, persisted sequences determine SSE replay and Last-Event-ID.
Trace and `/artifacts` reads remain authenticated/project-scoped.

For read-only backlog inspection use private libpq environment (not DSN/
password argv); scope any evidence query to owned canonical IDs:

```bash
psql --no-psqlrc -Atq -v ON_ERROR_STOP=1 -c \
  "SELECT topic,count(*) FROM outbox_events WHERE published_at IS NULL GROUP BY topic"
kafka-consumer-groups --bootstrap-server 127.0.0.1:9192 \
  --group agenttrace-runtime-local --describe
```

## Registry And Recovery

Manage immutable Skill/Harness versions and reviewed MCP bindings through
Langfuse's project Registry. Only project-scoped routes publish/select them.
Message creation requires explicit runtime/Harness version, content and
idempotency key. Global mutable Harness and agent/model-only flows are gone.
Existing Runs and failed-Run reruns keep their pinned snapshots after
publish/revoke. Historical snapshotless facts remain inspectable but are not
executable through a substitute Harness.

Runtime holds execution capacity until `harness.released` confirms joined
SDK/MCP/browser cleanup. `AGENTTRACE_PI_HARNESS_TIMEOUT` bounds Attempts;
`AGENTTRACE_PI_HARNESS_TERMINATION_GRACE` bounds cleanup. Failure can terminate
the shared host and affect siblings. Missing cleanup acknowledgement retains
private Attempt files. Inspect failed Run and private logs before file cleanup;
independently detached descendants need explicit ownership inspection.
First evidence-write failure cancels the session; external effects cannot be
automatically undone.

Environment execution requires current Lease, project, generation and fencing.
Browser provider affinity must remain stable; horizontal provider failover needs
the shared encrypted root/key and reliable locking or a shared provider service.
Expired uncertain execution stays lost/needs_review for explicit review/reset.
Control takeover requires one-time capability, role authorization, expiry and
audit. Never expose provider tokens or worker URLs as browser capabilities.

Use the authenticated UI or `Management.request` for cancellation, reset and
failed-Run rerun. Rerun uses source-scoped idempotency and can repeat effects;
repeated managed requests return original response identity before readiness
checks. Conversation deletion returns accepted managed cleanup or conflict
while Run/delivery/lease work remains; wait for the tombstone and Environment
delete acknowledgement. It retains processed-command tombstones. Do not
delete/reset shared databases, whole projects or profile roots as cleanup.

## Export

Export is asynchronous/idempotent and cannot rewrite terminal Run status.
Inspect exporter private logs, retry/DLQ facts and independent group progress
when a Trace is missing. Actual goal/context, reported usage and model
evidence are canonical; unknown cost/timing/input stays unknown.
Claude cumulative query/model totals belong to the Run root and LF metadata,
not an individual GENERATION. Codex `flat-functions` is an explicit provider
option preserving actual official SDK tools/results; native is the default.
Provider failure is not retried through a different SDK.

## Verification

```bash
python3 -m unittest discover -s integration/local -p '*_test.py'
node --test integration/local/langfuse.test.mjs
python3 integration/local/management.py accept --env-file <private-management-env> \
  --report <private-evidence.json>
```

The first two are offline (Node 24); only test-owned temporary resources/children
may be created. The third is parent-authorized live acceptance against existing
services, performs tracked-resource writes and remains provisional without
external UI/RBAC/fault evidence. `support.py` supplies env/PG/SSE/poll and tracked
cleanup, no HTTP fallback or unbound run creator. Management supplies auth;
SQL evidence is read-only by caller convention. Managed faults enforce read-only
transactions/global deadline; owned infrastructure admin is intentionally separate.

Successful managed SDK cases compare scoped PG counts/single completion/
published outbox, two authenticated SSE reads/reconnect and optional actual LF
terminal correlation/available model inputs/usage. Detailed Redis TTL,
command/inbox, fixed-model timing/name/goal and generic composition coverage is
not all ported from retired suites. Parent final smoke checks password absence
and real UI; do not claim identical old/new scope.

Finite `management_faults.py` requires isolated `agenttrace_test_*`, unique
runtime owners, explicit provider capacity and finite budgets. Optional crash
requires fresh owned binary PID mapping and signal authorization; never use
the all-children native supervisor as a survivor fault stack. Quarantine is
retained, not automatically reset. Cleanup restores only tracked drain state
under a separate 30-second total budget.
`infrastructure_faults.py` owns isolated test DB/processes and refuses shared
service injection, exclusive-create collisions or unowned signaling. Source
capture includes its imported support module.

Full Go/SDK Host/Worker, isolated PostgreSQL, deployed identity/restart and live
acceptance are parent integration gates. Dated evidence retains historical
scope. Dockerfiles are portable component builds with explicit commands;
direct startup still needs trusted seed, managed secrets, reviewed resources
and stable profile/artifact mounts, not another product deployment.
