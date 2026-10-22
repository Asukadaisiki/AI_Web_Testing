# Task 11 Report: Canonical AgentTrace Browser Evaluation

## Result

Completed and committed after this report is written. The final canonical run was:

`run_b55c7934377f8eed53a5f4a33eda526e`

It completed with the deterministic scripted model only. No real model call, API key, or
`/tmp/agenttrace-v3.env` access was used.

## Files Changed

- `fixtures/scripts/agenttrace_browser_eval.json`: deterministic plan, grounded
  `browser_execute_case`, report, and final response fixture.
- `integration/v3/canonical_scenario.py`: bounded API/SSE orchestration, API/trace/artifact
  assertions, rerunnable scoped IDs, and failure output containing the run ID.
- `integration/v3/assert_postgres.py`: parameterized Psycopg assertions scoped to the created
  project, conversation, and run.
- `integration/v3/wait_for_stack.py`: bounded health polling with dependency diagnostics.
- `integration/v3/requirements.txt`: the integration-only `psycopg[binary]` requirement.
- `compose.yaml`: Kafka topic initializer, fixture nginx service, Playwright worker image,
  shared artifact volume, deterministic fixture wiring, and host PostgreSQL port `5433`.
- `worker/Dockerfile`: Chromium-capable worker image.
- `backend/internal/model/{model.go,scripted.go,scripted_test.go}` and
  `backend/internal/runtime/engine.go`: checkpoint-indexed scripted replay so a new run does
  not consume another run's fixture steps.
- `backend/internal/runtime/{checkpoint.go,postgres.go}` and
  `backend/internal/postgres/{runtime.go,traces.go,store_integration_test.go}`: a run root
  Trace span and exactly-once terminal assistant message persistence.
- `backend/internal/api/v3/{server.go,server_test.go}` and
  `backend/cmd/agenttrace-api/main.go`: serve the shared `/artifacts/` volume through v3.
- `backend/cmd/agenttrace-runtime/main_test.go`: asserts the checkpointed fixture contract.
- `test.config.json`: preserves `LOOP_MAX_MODEL_CALLS=40`, useful v3 limits, and
  `LARK_API_KEY`; removes the legacy ecommerce layer and adds the v3 Compose layer/ports.

## Architecture Exercised

`canonical_scenario.py` created its own project, conversation, and idempotent user message
through the API. The API persisted the run/outbox command in PostgreSQL; the API relay
published to Kafka; the runtime consumer processed the command with Redis live state; the
scripted model emitted the server-owned `browser_execute_case`; the real Python Playwright
worker executed it against `fixture-site`; the worker wrote screenshots to the shared
`artifacts` volume; runtime persisted operation evidence, Trace spans, usage, assistant final,
and completion; the API served the browser screenshot and SSE replay.

The scenario's structured PostgreSQL assertions verified: one scoped project/conversation,
exactly one user and assistant message, one succeeded run, one root Trace span with model and
operation children, one successful operation with screenshots, final zero-token scripted usage,
published Kafka command with runtime inbox processing, and exactly one published completion
event with zero unpublished completions.

## RED Evidence

The prescribed initial command was:

```sh
docker compose up -d --build
python integration/v3/canonical_scenario.py
```

Observed successive real boundaries, fixed one at a time:

1. Kafka had no required v3 topics, leaving a created run queued. Added `kafka-init`.
2. A process-global scripted fixture exhausted after a prior run. Added checkpoint-indexed
   fixture replay keyed to durable model-call count.
3. The completed run lacked a Trace root and assistant conversation message. Added durable root
   span and terminal assistant persistence.
4. Run `run_aa703af4b0248c41076825815d28e7ce` completed browser execution but `/artifacts/...`
   returned the API's plaintext 404. Added the v3 shared-volume artifact route and its RED/GREEN
   handler test.
5. Host `5432` was a separate PostgreSQL instance (`role "agenttrace" does not exist`).
   Compose now publishes its isolated PostgreSQL service on `5433`.

## GREEN Evidence

All commands exited `0`:

```sh
cd backend && go test ./...
cd backend && AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace_task11?sslmode=disable' go test -p 1 ./...
cd worker && uv run python -m unittest discover -s tests -t .
cd web && npm_config_cache=/tmp/agenttrace-npm-cache npm test -- --run
cd web && npm_config_cache=/tmp/agenttrace-npm-cache npm run build
uv run --with 'psycopg[binary]>=3.2,<4' python integration/v3/canonical_scenario.py
python integration/v3/canonical_scenario.py
docker compose config --quiet
```

- Backend default suite: passed.
- Backend isolated PostgreSQL suite: passed serially; serial package execution is necessary
  because existing integration packages intentionally reset the same dedicated test database.
- Worker: `Ran 105 tests ... OK`.
- Web: `6 passed` files, `16 passed` tests; production build succeeded.
- Config-driven canonical layer passed with
  `run_f93bac9fe7c2e3b3ca370d354d0736c5`.
- Final root canonical command passed with
  `run_b55c7934377f8eed53a5f4a33eda526e`.

Final `docker compose ps -a` showed PostgreSQL and Kafka healthy; API, runtime, worker,
fixture-site, Redis, and web running; and `kafka-init` exited `0`. Final API health returned
`status: ok` with PostgreSQL, Kafka, Redis, and worker all `ok` and `pending_outbox: 0`.

## Self-Review And Limits

- Deterministic/rerunnable: every scenario creates and scopes its own records; no database is
  truncated. Assertions contain the run ID on failure.
- Safety: no secrets were read, printed, or used. The fixture is local and scripted.
- Bounded waits: readiness, SSE, and PostgreSQL publication polling all have finite timeouts and
  diagnostics. This scenario starts no local subprocesses.
- Interoperability: the successful run traversed Kafka publish/consume and the real worker HTTP
  callback, rather than mocks or in-process substitutes.
- Checkpoint behavior: fixture calls use durable `model_calls` so independent runs replay the
  correct sequence. No intentional runtime crash/restart was performed, so crash/resume is not
  claimed.
- Remaining concern: the canonical PostgreSQL check requires
  `psycopg[binary]`; it is declared in `integration/v3/requirements.txt` and the test config
  installs it through `uv --with`.
