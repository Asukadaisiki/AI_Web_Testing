# AgentTrace browser worker

`loop_worker` is the v3 Playwright execution service. It accepts runtime
operations, evaluates browser cases, and writes screenshots and evidence to the
shared artifacts volume. Its HTTP application is
`loop_worker/main.py`; the worker-side case types are
`loop_worker/contracts.py`.

Native managed startup is `python3 integration/local/run.py` from the repository
root; see `../docs/management-runtime-setup.md` for mandatory private settings.
For explicitly configured standalone worker startup:

```bash
cd worker
uv sync
# Privately inject LOOP_WORKER_INTERNAL_TOKEN, LOOP_ENVIRONMENTS_DIR,
# LOOP_ENVIRONMENT_ENCRYPTION_KEY and LOOP_ARTIFACTS_DIR first.
uv run --no-sync uvicorn loop_worker.main:app --host 127.0.0.1 --port 8110
```

Run worker tests:

```bash
cd worker
PLAYWRIGHT_BROWSERS_PATH=/tmp/agenttrace-playwright \
  uv run python -m unittest discover -s tests -t .
```

Parent-authorized end-to-end acceptance uses already-running managed services,
from the repository root:

```bash
python3 integration/local/management.py accept --env-file <private-management-env>
```

The retained browser-case fixture is
`../fixtures/contract/case_contract.json`; it remains a worker compatibility
contract. See `../CONTRACT.md` for the API, event, and operation contracts.

## Managed Uploads

`upload_file` accepts one relative name, never a host path or caller-supplied
bytes. Configure `LOOP_UPLOADS_DIR` as an absolute, link-free directory.
Environment sessions and replay read only
`$LOOP_UPLOADS_DIR/{environment_id}_g{profile_generation}/`.
Provision creates the generation directory; reset/delete remove obsolete
generation uploads without following links. Trusted staging services must put
files in these directories; the worker has no public filesystem/upload API.

Uploads fail closed when unconfigured. Absolute paths, traversal, backslashes,
symlinks (including directory components), hard links, special files and files
larger than 16 MiB are rejected as `upload_rejected`. Descriptor-based reads
prevent pathname replacement from redirecting reads; only validated managed
bytes are passed to Playwright.

## Environment Provider

Configuration, read when constructing the app:

| Variable | Purpose |
| --- | --- |
| `LOOP_WORKER_INTERNAL_TOKEN` | Explicit server-only Bearer credential |
| `LOOP_ENVIRONMENTS_DIR` | Persistent absolute, link-free profile directory |
| `LOOP_ENVIRONMENT_ENCRYPTION_KEY` | Fernet key: URL-safe base64 of 32 random bytes |
| `LOOP_UPLOADS_DIR` | Optional managed uploads root |

Generate a key with `uv run python -c 'from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())'`.
Inject keys/tokens through your secret manager, not Run snapshots, browser
responses, source control or logs. No default key or token is generated.
Back up the encryption key separately from the profile volume. Losing it makes
profiles unreadable. To rotate, drain and close sessions, re-encrypt existing
records offline with authenticated old/new keys, then switch the configured key
and restart. Simply replacing the key fails closed; automatic key rotation is
not implemented.

Profile directory permissions are `0700`; encrypted record and lock files are
`0600`. Cookies, localStorage and IndexedDB state are authenticated/encrypted
together with Environment identity, policy, generation, token high-water mark
and lifecycle state. Writes use fsync and atomic replacement. SessionStorage,
browser extensions, downloads and an entire Chromium user-data directory are
not persisted.

### Internal HTTP Contract

All browser session/replay and `/environments/*` requests require
`Authorization: Bearer <LOOP_WORKER_INTERNAL_TOKEN>`. Missing configuration is
`503 internal_auth_unconfigured`; wrong/missing credentials are `401 unauthorized`.
These routes are for the Control API, not direct browser access.

| Method/Path | Request/Response |
| --- | --- |
| `POST /environments/{id}/provision` | `{project_id, profile_generation, fencing_token, policy?: {allowed_domains: [...]}}` -> metadata |
| `GET /environments/{id}` | Metadata only, never a profile path/state or control URL |
| `POST /environments/{id}/reset` | `{profile_generation, fencing_token}` -> metadata |
| `DELETE /environments/{id}` | JSON body `{profile_generation, fencing_token}` -> deleted metadata |
| `POST /sessions` | Strict `{session_id, project_id, environment_id, profile_generation, fencing_token}` -> `{session_id: "bsess_..."}` |
| `POST /execute` | Strict `{session_id, browser_session_id, case, project_id, environment_id, profile_generation, fencing_token}` -> `ExecutionResult` |
| `GET /environments/{id}/sessions/{bsess}/screenshot` | Identity headers -> viewport `image/png`, `Cache-Control: no-store` |
| `POST /environments/{id}/sessions/{bsess}/input` | Identity and one input action in JSON -> `{accepted: true}` |

Metadata fields include `environment_id`, `project_id`, `profile_generation`, `fencing_token`,
`state` (`ready`, `leased`, `deleted`), `policy`.
HTTP errors use `{error, detail}`. Missing resources are `404`; stale,
occupied or mismatched identities are `409 environment_conflict`; unverifiable
ciphertext is `409 environment_broken`. Invalid input is `400`.

Generations/tokens are positive integers. Provision requires a new identity;
an identical ready provision is idempotent. Session creation requires the
current generation and a strictly newer token, and holds an exclusive
filesystem lock for the lifetime of its context. A reused token is rejected,
even after close/restart. Reset requires exactly the next generation and a
strictly newer token, fences durably before closing/discarding the old context,
and clears profile state. An identical successful reset is idempotent.
Delete requires the current generation, a newer token and no active context;
it removes state/uploads but retains an encrypted fencing tombstone. Deleted
identities cannot be reprovisioned.

Managed `/sessions/{bsess}/navigate`, `/act`, and `DELETE /sessions/{bsess}`
require Bearer auth plus `X-Profile-Generation` and `X-Fencing-Token`.
Session actions also require `X-Project-Id` and `X-Environment-Id`.
Screenshot uses the provider's project/generation/fencing headers and
Environment/session route scope. Input uses `X-Project-Id` and optional matching
`X-Environment-Id` headers, with generation/fencing/action in strict JSON:

```json
{"profile_generation":1,"fencing_token":2,"action":"click","x":10,"y":20}
```

Other permitted actions: `key` with `key`, `type` with `text`, `navigate` with
`url`. Clicks are integer viewport coordinates. Text is at most 4096 characters
without control characters. Keys: Enter, Tab, Shift+Tab, Escape, Backspace,
Delete, ArrowLeft/Right/Up/Down, Home, End. Extra/mismatched action fields,
evaluation, CDP, arbitrary keyboard shortcuts and filesystem access are refused.

Domain policy uses exact lowercase hosts, HTTP(S) only, with no URL credentials;
ports may be explicit. Empty `allowed_domains` denies network access.
Subresources are also gated; service workers, WebSockets and accepted downloads
are disabled. HTTP redirects are denied because Chromium redirect chains
bypass Playwright routing. Redirect-heavy login flows need a future per-hop
provider adapter; explicit allowed navigation and script-triggered new
requests are still gated. This is not a DNS/IP firewall or a general sandbox.

Acknowledged managed close snapshots state, closes the context successfully,
then atomically persists it before returning `{closed: true}`. A failed close
does not acknowledge/persist state. Shutdown/crash discards unacknowledged
changes; metadata can remain `leased` and requires Control API reconciliation.
Reset discards old state, including in-flight writes serialized by the provider
lock. A worker crash is not permission to replay external effects.

Managed `/execute` requires Bearer authentication, the active lease's current
complete body identity, domain `session_id` and held `browser_session_id`.
Its guard spans the complete runner and
context cleanup, so reset/close cannot race execution. Dry runs use a fresh
context with no persistent profile loaded or saved, the Environment's domain
policy, disabled downloads/service workers and the same generation upload
namespace. Disallowed goto is a failed step with `domain_rejected`; intercepted
redirects fail navigation. Omitting identity for an active managed domain
session is rejected. Node `finish_case` must forward every identity field and
the held handle; extra or partial identity fields are rejected.

Unbound `/sessions` and `/execute` requests are rejected. Keep the worker
network-private; provider credentials are not a public browser capability.
PostgreSQL remains lease/Project ownership authority. The Control API owns
one-time takeover URL issuance, expiry, replay protection, authorization and
audit, and proxies screenshot/input without exposing worker addresses or
credentials. Provider locks/high-water marks apply to one shared local storage
root; cross-host placement and failover need canonical leases and managed
profile transfer. Network filesystems with unreliable flock/fsync are unsupported.

### Targeted Verification

```bash
uv run python -m unittest tests.test_upload_safety tests.test_environments \
  tests.test_api.HealthAndErrorsTest tests.test_contracts -q
uv run python -m unittest tests.test_environment_browser -q
```

The second command is a small serial real-Chromium suite, not the full browser
regression. It covers isolated/persistent identities, reset, controlled input
and screenshots, redirects, and session/runner upload enforcement.
