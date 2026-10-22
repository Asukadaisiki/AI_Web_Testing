# AgentTrace browser worker

`loop_worker` is the v3 Playwright execution service. It accepts runtime
operations, evaluates browser cases, and writes screenshots and evidence to the
shared artifacts volume. Its HTTP application is
`loop_worker/main.py`; the worker-side case types are
`loop_worker/contracts.py`.

Run locally:

```bash
cd worker
uv sync
PLAYWRIGHT_BROWSERS_PATH=/tmp/agenttrace-playwright \
  uv run uvicorn loop_worker.main:app --host 0.0.0.0 --port 8100
```

Run worker tests:

```bash
cd worker
PLAYWRIGHT_BROWSERS_PATH=/tmp/agenttrace-playwright \
  uv run python -m unittest discover -s tests -t .
```

The canonical end-to-end test runs through Compose:

```bash
uv run --with 'psycopg[binary]>=3.2,<4' python integration/v3/canonical_scenario.py
```

The retained browser-case fixture is
`../fixtures/contract/case_contract.json`; it remains a worker compatibility
contract. See `../CONTRACT.md` for the API, event, and operation contracts.
