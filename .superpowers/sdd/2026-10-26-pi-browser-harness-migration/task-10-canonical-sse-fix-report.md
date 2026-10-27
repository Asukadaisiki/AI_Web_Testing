# Task 10 Supplemental Canonical SSE Fix Report

## Status

Implemented the canonical SSE completion fix with strict RED/GREEN evidence.
`consume_terminal_sse` now returns on the durable `harness.completed` event
without inferring Run success from the event payload. The existing API and
PostgreSQL assertions remain authoritative.

Starting `HEAD` was `50bc1cebc41a1393f4a050619aba90f2c684329c`.
The worktree was clean.

## Durable Fixture Evidence

The fixture was copied from the real durable completion record for
`run_7315076dac8ea9792abc259003e9dd24`. Read-only query:

```bash
uv run --with 'psycopg[binary]>=3.2,<4' python3 -c \
  'import json, psycopg; dsn="postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace?sslmode=disable"; conn=psycopg.connect(dsn); row=conn.execute("SELECT seq, payload FROM run_events WHERE run_id = %s AND event_type = %s ORDER BY seq DESC LIMIT 1", ("run_7315076dac8ea9792abc259003e9dd24", "harness.completed")).fetchone(); print(row[0]); print(json.dumps(row[1], separators=(",", ":"), sort_keys=True)); conn.close()'
```

Relevant output:

```text
45
{"aggregate_id":"run_7315076dac8ea9792abc259003e9dd24","correlation_id":"run_7315076dac8ea9792abc259003e9dd24","event_id":"event_83ab78e3db854232dc1844395c7ffc88","event_type":"harness.completed","occurred_at":"2026-10-27T08:40:50.241701Z","payload":null,"schema_version":"agenttrace.event.v1"}
```

Exit status: `0`.

Both existing terminal SSE fixtures now use this complete envelope.

## RED

The brief's literal command was attempted first:

```bash
python3 -m unittest integration.v3.test_canonical_scenario -v
```

It encountered the repository's existing top-level import-path assumption
before running tests:

```text
test_canonical_scenario (unittest.loader._FailedTest) ... ERROR

======================================================================
ERROR: test_canonical_scenario (unittest.loader._FailedTest)
----------------------------------------------------------------------
ImportError: Failed to import test module: test_canonical_scenario
Traceback (most recent call last):
  File "/Library/Developer/CommandLineTools/Library/Frameworks/Python3.framework/Versions/3.9/lib/python3.9/unittest/loader.py", line 154, in loadTestsFromName
    module = __import__(module_name)
  File "/Users/bytedance/project/AI_Web_Testing/.worktrees/pi-browser-harness-migration/integration/v3/test_canonical_scenario.py", line 9, in <module>
    import canonical_scenario
ModuleNotFoundError: No module named 'canonical_scenario'

----------------------------------------------------------------------
Ran 1 test in 0.000s

FAILED (errors=1)

EXIT_STATUS=1
```

This was a setup error, not the intended RED. The same target was rerun with
the integration directory on `PYTHONPATH`:

```bash
PYTHONPATH=integration/v3 \
  python3 -m unittest integration.v3.test_canonical_scenario -v
```

Complete output:

```text
test_accepts_linked_pi_finish_evidence_and_derived_steps (integration.v3.test_canonical_scenario.CanonicalPiTraceTest) ... ok
test_reconnects_after_idle_timeout_and_accepts_delayed_heartbeat (integration.v3.test_canonical_scenario.CanonicalSSETest) ... ERROR
test_retries_open_failure_and_resumes_with_last_durable_event_id (integration.v3.test_canonical_scenario.CanonicalSSETest) ... ERROR

======================================================================
ERROR: test_reconnects_after_idle_timeout_and_accepts_delayed_heartbeat (integration.v3.test_canonical_scenario.CanonicalSSETest)
----------------------------------------------------------------------
Traceback (most recent call last):
  File "/Users/bytedance/project/AI_Web_Testing/.worktrees/pi-browser-harness-migration/integration/v3/test_canonical_scenario.py", line 63, in test_reconnects_after_idle_timeout_and_accepts_delayed_heartbeat
    event = consume_terminal_sse(
  File "/Users/bytedance/project/AI_Web_Testing/.worktrees/pi-browser-harness-migration/integration/v3/canonical_scenario.py", line 117, in consume_terminal_sse
    and completion.get("Status", completion.get("status")) == "succeeded"
AttributeError: 'NoneType' object has no attribute 'get'

======================================================================
ERROR: test_retries_open_failure_and_resumes_with_last_durable_event_id (integration.v3.test_canonical_scenario.CanonicalSSETest)
----------------------------------------------------------------------
Traceback (most recent call last):
  File "/Users/bytedance/project/AI_Web_Testing/.worktrees/pi-browser-harness-migration/integration/v3/test_canonical_scenario.py", line 107, in test_retries_open_failure_and_resumes_with_last_durable_event_id
    event = consume_terminal_sse(
  File "/Users/bytedance/project/AI_Web_Testing/.worktrees/pi-browser-harness-migration/integration/v3/canonical_scenario.py", line 117, in consume_terminal_sse
    and completion.get("Status", completion.get("status")) == "succeeded"
AttributeError: 'NoneType' object has no attribute 'get'

----------------------------------------------------------------------
Ran 3 tests in 0.109s

FAILED (errors=2)

EXIT_STATUS=1
```

This is the intended RED: JSON `payload:null` decoded to `None`, then the
consumer called `.get()` on it.

## Implementation

The production change is limited to the terminal decision:

- preserve event parsing and durable last-event-ID tracking;
- return when the parsed SSE event type is `harness.completed`;
- remove all terminal payload status inspection;
- leave Run success validation to `assert_api_outcome` and
  `assert_canonical_run`.

No reconnect, heartbeat, idle timeout, total timeout, sleep, or bounded retry
logic changed.

## GREEN

Command:

```bash
PYTHONPATH=integration/v3 \
  python3 -m unittest integration.v3.test_canonical_scenario -v
```

Complete output:

```text
test_accepts_linked_pi_finish_evidence_and_derived_steps (integration.v3.test_canonical_scenario.CanonicalPiTraceTest) ... ok
test_reconnects_after_idle_timeout_and_accepts_delayed_heartbeat (integration.v3.test_canonical_scenario.CanonicalSSETest) ... ok
test_retries_open_failure_and_resumes_with_last_durable_event_id (integration.v3.test_canonical_scenario.CanonicalSSETest) ... ok

----------------------------------------------------------------------
Ran 3 tests in 0.104s

OK

EXIT_STATUS=0
```

## Required Verification

Compile command:

```bash
python3 -m py_compile \
  integration/v3/canonical_scenario.py \
  integration/v3/test_canonical_scenario.py
```

Complete output:

```text
EXIT_STATUS=0
```

Diff check command:

```bash
git diff --check
```

Complete output:

```text
EXIT_STATUS=0
```

## Live Canonical Scenario

Command:

```bash
AGENTTRACE_API_URL='http://127.0.0.1:8080' \
AGENTTRACE_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace?sslmode=disable' \
  uv run --with 'psycopg[binary]>=3.2,<4' \
  python3 integration/v3/canonical_scenario.py
```

Complete relevant output:

```text
INFO add_decision: Id::<PubGrubPackage>(0) @ 0a0.dev0 without checking dependencies
INFO add_decision: Id::<PubGrubPackage>(1) @ 3.3.6 without checking dependencies
INFO add_decision: Id::<PubGrubPackage>(2) @ 3.3.6 without checking dependencies
INFO add_decision: Id::<PubGrubPackage>(3) @ 3.3.6 without checking dependencies
INFO add_decision: Id::<PubGrubPackage>(4) @ 3.3.6 without checking dependencies
INFO add_decision: Id::<PubGrubPackage>(5) @ 3.3.6 without checking dependencies
INFO add_decision: Id::<PubGrubPackage>(6) @ 3.3.6 without checking dependencies
canonical scenario passed; run_id=run_0a87770a8cce83c381cad18524d44133

EXIT_STATUS=0
```

The live scenario passed its final Run status, trace, linked operation
evidence, artifacts, derived steps, usage, and PostgreSQL assertions after
receiving the null-payload completion SSE.

## Files

- `integration/v3/canonical_scenario.py`
- `integration/v3/test_canonical_scenario.py`
- `.superpowers/sdd/2026-10-26-pi-browser-harness-migration/task-10-canonical-sse-fix-report.md`

## Self-Review

- The fixture is the exact durable envelope from the reported failing run,
  including `payload:null`.
- Both existing completion fixtures use the real envelope.
- The regression fails on the original production code for the reported
  `NoneType` reason and passes after the minimal production change.
- Event type, not payload content, controls SSE completion.
- API and PostgreSQL outcome checks are unchanged.
- Existing tests still assert heartbeat tolerance, reconnect after read
  timeout, retry after open failure, last-event-ID resume, bounded read
  timeout, and total deadline.
- The diff is restricted to the three files allowed by the brief.
- No Docker or Compose command was used. No service was stopped or restarted.
- No subagent or reviewer was dispatched.

## Concerns

The test module uses top-level imports from `integration/v3`, so the required
unittest target needs `PYTHONPATH=integration/v3` in this shell. This is
pre-existing and was not broadened into this narrowly scoped fix. No functional
concerns remain from the SSE change.
