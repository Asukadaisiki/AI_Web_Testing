# Operations

## Native local start

Follow the dependency installation in `README.md`. PostgreSQL and Redis run as
native services. Initialize the real Langfuse stack once, then start it:

```bash
export AGENTTRACE_LOCAL_STATE="$HOME/Library/Caches/agenttrace-native"
export LANGFUSE_NATIVE_ROOT="$AGENTTRACE_LOCAL_STATE/langfuse"
bash integration/local/langfuse.sh setup
bash integration/local/langfuse.sh up
set -a
source "$LANGFUSE_NATIVE_ROOT/agenttrace.env"
set +a
python3 integration/local/run.py --with-exporter
```

The task console uses `http://127.0.0.1:5184`, API `8180`, Worker `8110`,
fixture `8124`, and Langfuse `3300`. Native Kafka uses an isolated broker on
`9192`; the runner creates eight-partition command, event and DLQ topics.
PostgreSQL database `agenttrace_local` is created if absent. Redis notifications
use database `3`. Override public configuration through environment variables;
model credentials remain in ignored `.env`.
Restarting with the same state directory preserves Kafka's cluster metadata
and delivery offsets. Set `AGENTTRACE_LOCAL_STATE` consistently across restarts.

The model provider is explicit: `openai-compatible` selects real
`glm-5.3-flash`, and `scripted` selects a fixture. A missing real model key
fails clearly. API mode selection does not require that key. Runtime owns the
provider credentials; request snapshots carry only public model identity and
reasoning effort.

Ctrl-C joins the runner's children and stops only the process groups it started.
PostgreSQL, Redis, pre-existing Kafka and Langfuse services remain untouched.
Langfuse has its separate stop command:

```bash
bash integration/local/langfuse.sh down
```

The pinned upstream Langfuse worker can retain an open handle after logging
`Shutdown complete, exiting process...`. The launcher reports this after its
bounded stop wait and refuses to kill an unidentified process. If this occurs,
inspect its private log and saved PID identity before releasing that owned
process; existing PostgreSQL, Redis and application services stay running.

Runtime logs and artifacts live outside the repository in
`$AGENTTRACE_LOCAL_STATE`, defaulting to the OS temporary directory's
`agenttrace-local`. Set a persistent location for long-term evidence.
Langfuse defaults to `/tmp/agenttrace-native/langfuse`; see
`integration/local/langfuse.md` for persistent storage and owner login.

## Health and evidence

```bash
curl -fsS http://127.0.0.1:8180/api/v1/health
bash integration/local/langfuse.sh status
```

Health returns `ok`, `degraded` or `down` and independent PostgreSQL, Kafka,
Redis, Worker and Langfuse checks. PostgreSQL failure returns HTTP 503.
Redis failure marks `mode: postgres_poll`; SSE continues from PostgreSQL.
Langfuse health probes its actual web endpoint when configured. Exporter lag is
checked separately, not inferred from web health.

PostgreSQL is the authority for execution and evidence. Every durable event and
outbox message is committed together. Kafka provides delivery and independent
consumer progress. The `agenttrace-live-notifier` group writes expiring Redis
wakeups; SSE fetches PostgreSQL events and uses their sequences as replay IDs.
Redis does not supply a second execution history. Cancellation reads durable
PostgreSQL state every 250 ms; no unused Redis leases or cancel hints remain.

Inspect pending delivery without changing state:

```bash
psql "postgresql://$(whoami)@127.0.0.1:5432/agenttrace_local" -c \
  "SELECT topic, count(*) FROM outbox_events
   WHERE published_at IS NULL GROUP BY topic;"
kafka-consumer-groups --bootstrap-server 127.0.0.1:9192 \
  --group agenttrace-runtime-local --describe
kafka-consumer-groups --bootstrap-server 127.0.0.1:9192 \
  --group agenttrace-live-notifier --describe
```

Restart a stopped runtime through the same runner/configuration. Kafka redelivery
uses the durable inbox; terminal Runs do not create another session. A host
crash fails its active sessions; a later request can start a fresh host.
Do not share one Kafka command topic between independently owned databases.

## Session boundaries and failures

Each Go runtime process owns one shared host, with isolated selected SDK
sessions per Attempt. Pi, Claude Code and Codex adapters share the lifecycle
contract and immutable Harness snapshots.
Cancellation addresses both Run and Attempt, and the execution slot is released
after awaited session/MCP cleanup. `AGENTTRACE_PI_HARNESS_TIMEOUT` bounds an
Attempt; `AGENTTRACE_PI_HARNESS_TERMINATION_GRACE` bounds cleanup and shutdown.
Cleanup timeout may terminate the shared host and affect sibling sessions.

On the first failed evidence write, Runtime cancels the affected session,
waits for cleanup and fails that Run. It does not keep performing unrecorded
actions. Already completed external effects cannot be undone automatically;
inspect the evidence and external state before a user rerun.

Manage Harnesses in the workspace or `/api/v1/harnesses`. Definitions contain
system prompt, Skills JSON and MCP JSON. Updates increment revisions; existing
Runs and failed-Run reruns keep the previous snapshots. Environment references
resolve in the execution host. Configure Claude and Codex credentials/base URL/
model using `AGENTTRACE_CLAUDE_*` and `AGENTTRACE_CODEX_*`; consult `.env.example`.
For a Responses provider without namespace tools, explicitly select
`AGENTTRACE_CODEX_TOOL_FORMAT=flat-functions`; the default is `native`. The
attempt-local provider adapter closes after the Codex process exits. Provider
errors remain failed Runs and are not retried through a different SDK.
Missing SDK or MCP shutdown acknowledgements retain the Attempt's private
working files and prevent `harness.released`. Diagnose the failed Run and private
runtime log before removing retained files. Direct SDK children and MCP
transports are joined; independently detached descendants have no separate
join acknowledgement.

Legacy Harness modules supply tools, prompts, skills and completion policy. Set
`AGENTTRACE_HARNESS_MODULE` to a trusted local `.mjs` module or `file:` URL
implementing `pi-browser-harness/src/harness-definition.ts`.
Restart Runtime to select it. Generic requests need no browser address;
the browser Harness validates its own Worker URL and artifact scope.
The Runtime cannot infer success from a browser case or assistant prose.

## Langfuse export

The shared `run.completed` contract carries Run ID, terminal status and optional
error for success, failure and cancellation. Export is asynchronous and
idempotent. Failure retries or durable DLQ handoff do not rewrite Run status.
Inspect the exporter's log, `consumer_retry_counts`, DLQ outbox and independent
Kafka group progress if a Trace is missing.

Model observations use captured model input and measured call intervals.
Token details preserve provider totals and cache tokens; unknown billed cost
is omitted. Trace input is the current Run's actual goal, not future messages
in its Conversation.
The Pi SDK does not distinguish absent usage from an all-zero report.
All-zero counts are therefore omitted; positive counts, including cache-only
usage, remain available. This avoids presenting unavailable data as measured zero.
Claude reports assistant-message output and cumulative query/model usage;
Codex reports completed-turn output, usage and host-measured turn intervals.
Missing per-call details remain unknown instead of fabricated.
Claude's cumulative query usage is associated with the Run root and keyed by
SDK result, provider and model. Langfuse preserves it in
`agenttrace_sdk_query_usage` metadata on the Trace and root observation; it does
not assign pipeline totals to an individual assistant GENERATION.

## Rerun and deletion

A user rerun creates a fresh Run from the failed Run's immutable specification,
retaining source-message identity and recording its parent. It can repeat
external effects. Repeating a rerun request with the same UUID key returns the
same new Run:

```bash
curl -X POST http://127.0.0.1:8180/api/v1/runs/<failed-run-id>/reruns \
  -H 'Content-Type: application/json' -d '{"idempotency_key":"<uuid>"}'
```

Conversation/project DELETE returns HTTP 204 idempotently, or HTTP 409 while
there is active work, pending delivery or an unprocessed command.
Deletion retains processed command tombstones so Kafka replay cannot recreate
deleted work. Delete scoped projects through the API instead of resetting
shared services.

## Verification

```bash
AGENTTRACE_E2E_POSTGRES_DSN="postgresql://$(whoami)@127.0.0.1:5432/agenttrace_local" \
python3 integration/local/acceptance.py \
  --env-file "$LANGFUSE_NATIVE_ROOT/agenttrace.env"
```

The separate `integration/local/composition.py` verifies the three official SDKs
with common browser and generic Harnesses, failure, cancellation and snapshots.
Use the Worker audit proxy instructions in README to capture actual release
acknowledgements. `--resume-report` rechecks existing acceptance Runs and reruns
only failed initial combinations, preserving their original evidence.
If an adapter changed after a saved report, `--refresh-runtime <runtime-id>`
with `--resume-report` creates fresh checks for that adapter and retains the
previous execution IDs under `prior_runs`. Deleted Harness reruns still use
their original snapshots.

Acceptance creates its own project and retains sanitized evidence.
It also downloads successful browser screenshots through the API and validates
their PNG signatures and Run scope.

For fault drills, start the test-only Worker audit proxy in a second terminal,
then start the local runner with `AGENTTRACE_WORKER_URL=http://127.0.0.1:8111`.
Keep the same application state and Langfuse environment:

```bash
python3 integration/local/worker_audit.py \
  --log /tmp/agenttrace-native/worker-audit.jsonl
# In the runner terminal:
AGENTTRACE_WORKER_URL=http://127.0.0.1:8111 \
python3 integration/local/run.py --with-exporter
# Once the stack is ready:
AGENTTRACE_WORKER_URL=http://127.0.0.1:8111 \
python3 integration/local/faults.py \
  --env-file /tmp/agenttrace-native/langfuse/agenttrace.env \
  --worker-audit /tmp/agenttrace-native/worker-audit.jsonl \
  --report /tmp/agenttrace-native/faults.json
```

The proxy records request metadata only. Drills create isolated Runs, inject
a scoped event-write failure, start a separate API with unavailable Redis,
and pause the identified native Kafka and Langfuse web processes temporarily.
Finally blocks restore each fault. Restart the application with Worker URL
`http://127.0.0.1:8110` after the drills and stop the audit proxy.

Go destructive integration tests require a dedicated database named
`agenttrace_test_*` and Redis database `15`; loopback Redis is accepted, while
default Redis DB `0` and remote addresses are rejected. Run affected packages
serially (`go test -p 1`) because several test helpers reset their own schema.
Legacy `integration/v3` container scenarios remain available but are not the
native acceptance path.
