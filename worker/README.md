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

## Managed Uploads

`upload_file` accepts one relative name, never a host path or caller-supplied
bytes. Configure `LOOP_UPLOADS_DIR` as an absolute, link-free directory.
Legacy `/sessions` and `/execute` read only
`$LOOP_UPLOADS_DIR/{session_id}/`; Environment sessions read only
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

All `/environments/*` requests require
`Authorization: Bearer <LOOP_WORKER_INTERNAL_TOKEN>`. Missing configuration is
`503 internal_auth_unconfigured`; wrong/missing credentials are `401 unauthorized`.
These routes are for the Control API, not direct browser access.

| Method/Path | Request/Response |
| --- | --- |
| `POST /environments/{id}/provision` | `{profile_generation, fencing_token, policy?: {allowed_domains: [...]}}` -> metadata |
| `GET /environments/{id}` | Metadata only, never a profile path/state or control URL |
| `POST /environments/{id}/reset` | `{profile_generation, fencing_token}` -> metadata |
| `DELETE /environments/{id}` | JSON body `{profile_generation, fencing_token}` -> deleted metadata |
| `POST /sessions` | `{session_id, environment_id, profile_generation, fencing_token}` -> `{session_id: "bsess_..."}` |
| `POST /execute` | `{session_id, case, environment_id, profile_generation, fencing_token}` -> `ExecutionResult` |
| `GET /environments/{id}/sessions/{bsess}/screenshot` | Identity headers -> viewport `image/png`, `Cache-Control: no-store` |
| `POST /environments/{id}/sessions/{bsess}/input` | Identity and one input action in JSON -> `{accepted: true}` |

Metadata fields: `environment_id`, `profile_generation`, `fencing_token`,
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
Screenshot uses these same headers. Input instead carries identity in JSON:

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
identity and its domain `session_id`. Its guard spans the complete runner and
context cleanup, so reset/close cannot race execution. Dry runs use a fresh
context with no persistent profile loaded or saved, the Environment's domain
policy, disabled downloads/service workers and the same generation upload
namespace. Disallowed goto is a failed step with `domain_rejected`; intercepted
redirects fail navigation. Omitting identity for an active managed domain
session is rejected. Node `finish_case` must forward all three identity fields.

Legacy `/sessions` requests with just `session_id` and unbound `/execute` remain
ephemeral/compatible; uploads are constrained even there. Keep the worker
network-private: compatibility routes are not a public authentication surface.
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
