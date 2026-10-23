# AgentTrace Eval Hub v3

AgentTrace Eval Hub turns a conversation request into a durable, browser-backed
evaluation run. PostgreSQL is the system of record, Kafka carries commands and
events, Redis provides best-effort live updates, and the Python worker executes
the local storefront fixture with Playwright.

## Clean start

The default Compose stack uses the checked-in scripted model fixture. It does
not need model credentials.

```bash
cp .env.example .env
docker compose up -d --build
curl -fsS http://127.0.0.1:8080/api/v1/health
```

Open the workspace at <http://127.0.0.1:5174>, inspect API health at
<http://127.0.0.1:8080/api/v1/health>, and use the fixture site at
<http://127.0.0.1:8123>. PostgreSQL is exposed only for local diagnostics on
`127.0.0.1:5433`. Compose starts PostgreSQL, Kafka, Redis, fixture site,
worker, API, runtime, and Web. `kafka-init` creates the required topics.

The worker lock uses official PyPI and Debian URLs by default. On a restricted
network, set `UV_PACKAGE_BASE`, `DEBIAN_MIRROR`, and
`DEBIAN_SECURITY_MIRROR` in `.env` to compatible mirrors before building;
frozen Python versions and hashes remain enforced.

The optional asynchronous Langfuse exporter is not part of the default stack
and requires real provider values only when explicitly enabled:

```bash
AGENTTRACE_LANGFUSE_BASE_URL=https://langfuse.example \
AGENTTRACE_LANGFUSE_PUBLIC_KEY=... \
AGENTTRACE_LANGFUSE_SECRET_KEY=... \
docker compose --profile langfuse up -d exporter
```

Never commit `.env` or provider credentials. The API reports this optional
integration as `disabled`; exporter failures never block a local run.

## Current progress

The v3-only architecture cutover is complete across the API, runtime, worker,
and Web workspace. Trace responses now project durable operation evidence into
derived step nodes, which the workspace merges into its collapsible trace tree.
Real browser acceptance has been completed for both 23-step and 35-step traces.

## Verify

Run the complete v3 sequence against an already healthy Compose stack:

```bash
python run_v3_tests.py
python integration/v3/failure_drills.py
```

On macOS hosts without a `python` alias, run:

```bash
python3 run_v3_tests.py
python3 integration/v3/failure_drills.py
```

The runner uses `/tmp/agenttrace-npm-cache`, Playwright uses
`/tmp/agenttrace-playwright`, and Go destructive integration helpers receive an
isolated `agenttrace_test_v3_runner_*` database. It neither shuts down Compose
nor removes service logs. The canonical scenario and failure drills intentionally
add scoped acceptance records to the active clean stack; inspect them as needed,
then use the documented data-reset command to remove the Compose volumes.

Individual layers:

```bash
cd backend && go test -p 1 ./...
cd worker && uv run python -m unittest discover -s tests -t .
cd web && npm test -- --run && npm run build && npm run test:e2e
uv run --with 'psycopg[binary]>=3.2,<4' python integration/v3/canonical_scenario.py
uv run --with 'psycopg[binary]>=3.2,<4' python integration/v3/concurrent_delete_scenario.py
```

The concurrent scenario runs multiple projects and conversations through
PostgreSQL, Kafka, Redis, the scripted model, and the real Playwright worker.
It also verifies message idempotency, idempotent project/conversation deletion,
and Kafka replay safety after deletion.

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
| `backend/internal/domain`, `event`, `operation` | Run, event, and operation contracts |
| `backend/internal/postgres` | PostgreSQL schema and durable repositories |
| `backend/internal/traceprojection` | Derived Trace step projection from operation evidence |
| `worker/loop_worker` | Worker HTTP API and Playwright execution |
| `web/src/features/trace` | Trace parsing, tree normalization, and derived-node inspection |
| `web/src/features/workspace` | Three-column project, conversation, and trace workspace |
| `fixtures/contract/case_contract.json` | Retained browser case contract |
| `integration/v3` | Canonical Compose scenario and failure drills |
