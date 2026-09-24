# Execution Log

## 2026-09-24

- Switched local branch to `v2`, configured it to track `origin/v2`, backed up the previous local `v2` pointer as `backup/v2-before-origin-align-20260924-112258`, and aligned local `v2` to `origin/v2` at `1ad31cf`.

### Task 8: bounded ecommerce planner baseline

- Documented the one-input autonomous goal contract, direct test credentials, committed-step lifecycle,
  expectation-aware authoring, backend-owned tail repair, rolling PageView context, and raw/cached/fresh usage.
- Recorded exact runtime defaults: 25 model calls, 1,500,000 raw total tokens, 300,000 fresh total tokens,
  30,000 prompt tokens per completed call, and 98,304 serialized request bytes.
- Added the canonical `https://www.automationexercise.com/` quantity-`3` goal template and an appendable live
  evidence table to `plan/10-baseline.md`.
- Preserved the pre-existing baseline history. This documentation continuation did not run the live canary or access
  credentials. The controller's attempt exposed `committed_prefix_replay_failed`; no live success is recorded here.

Offline evidence:

```bash
cd backend && test -z "$(gofmt -l .)"
cd worker && uv run python -m compileall -q loop_worker tests
python3 run_tests.py
cd backend && go test ./internal/agentruntime -run 'Context|Committed|Replay|Fresh|Repeated'
cd backend && go test ./internal/planner -run 'PageView|Fingerprint|Replay'
cd worker && uv run python -m unittest tests.test_api tests.test_ecommerce_baseline
```

- Both formatting commands exited 0; `gofmt -l` listed no files.
- The first full regression reported `RESULT: OK（6 层全通过）`: 10 Python contract tests, all Go packages,
  94 Python/Playwright tests, a 12-step complete ecommerce loop with served cart evidence, and the web build.
- Both focused Go commands returned `ok`; the focused Python command passed 13 tests.
- Code review exposed a lifecycle defect: page assertions were committed before current-observation validation, while
  text detection omitted visible element and structure `FullText`.
- The final gate is `git diff --check`, `git status --short`, then `python3 run_tests.py`; exact exit codes and the
  status snapshot are retained in the ignored Task 8 report.

Task 8 fix round 1:

- Page assertions now return `condition_unmet` without changing committed steps unless the current full Observation
  satisfies them; successful assertions remain committed and are revalidated by the fresh dry run.
- Text presence checks normalized Name/Text/FullText on each visible element and FullText on each visible structure
  independently. Replay coverage includes structure-only aggregate text.
- The credentialed live canary was not run. Its metrics remain pending for the controller rerun.
