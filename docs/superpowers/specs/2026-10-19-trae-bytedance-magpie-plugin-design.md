# Trae ByteDance Magpie Plugin Design

## Summary

Build an internal-only Magpie provider plugin named
`opencode-trae-bytedance-auth`, with provider id `trae-bytedance`.
It authenticates ByteDance employees through the Trae CN employee SSO flow,
adapts the internal Trae model service to Magpie's provider interface, and
works only while the caller can reach the required ByteDance network domains.

The plugin is separate from `@magpie-community/opencode-trae-auth`. It does
not replace or modify the public `trae-cn` provider.

## Goals

- Let a ByteDance employee sign in through the existing Trae CN employee
  OAuth/SSO flow.
- Expose employee-available models to Magpie as
  `trae-bytedance/<model-id>`.
- Serve Magpie and OpenCode requests through the employee model service.
- Refresh short-lived employee credentials using the same documented client
  flow observed in the locally installed Trae CN application.
- Reject use outside the fixed internal service allowlist.
- Test the plugin locally without changing or reading unrelated Magpie
  accounts.

## Non-Goals

- Bypassing ByteDance identity, network, device, entitlement, or model-access
  controls.
- Supporting non-employees.
- Exporting an employee token as a reusable public API key.
- Sending employee credentials to public Trae endpoints.
- Modifying the installed Trae CN application.
- Publishing the plugin to npm or the Magpie community registry in the first
  implementation.
- Implementing usage cards, check-in, account rotation, or remote sharing in
  the first version.

## Evidence And Current Behavior

The design is based on:

- Magpie `0.1.1290`, whose plugin host supports OpenCode provider hooks.
- `@magpie-community/opencode-trae-auth` `0.2.17`.
- The locally installed Trae CN build dated 2026-10-16.

The public community plugin:

- starts a loopback callback server;
- opens `www.trae.cn/authorization`;
- receives Trae OAuth credentials;
- refreshes a `Cloud-IDE-JWT`;
- sends translated requests to the public Trae model host.

The installed Trae CN application contains a separate `bytedance` account
scope:

- employee login host: `https://solo-web.bytedance.net`;
- recognized SSO pages: `sso.bytedance.com` and `accounts.feishu.cn`;
- employee model host in CN: `https://copilot-cn.bytedance.net`;
- employee token refresh:
  `POST https://solo-web.bytedance.net/trae/api/v3/UpgradeBytecloudToken`;
- employee model SDK token type: `Byte-Cloud-JWT`;
- internal model APIs include `api/ide/v1/chat`,
  `api/ide/v1/get_model_list`, and `api/ide/v1/get_detail_param`.

These differences mean the employee implementation cannot be a public Trae
host substitution. Authentication, refresh, model discovery, request signing,
and possibly request/stream shapes must be implemented as an explicit
employee branch.

## Alternatives

### Extend The Community `trae-cn` Provider

This would add internal hosts and employee token handling to the existing
provider.

Rejected because it would mix credentials and host policies from two trust
zones. It would also collide with the installed community package and make
upstream updates difficult to consume.

### Reuse The Trae CN Desktop Sign-In

This would read the installed application's current employee credentials on
each request.

Deferred because the requested experience is a standalone employee OAuth
flow. It also couples the plugin to the desktop application's storage format.
It remains a possible fallback if the SSO callback cannot be completed
outside Trae CN.

### Separate Employee Provider

Recommended. The plugin owns one provider, one host allowlist, one credential
shape, and one request adapter. Public Trae remains untouched.

## Package Layout

The implementation will live under:

```text
magpie-trae-bytedance/
  package.json
  README.md
  index.mjs
  test/
    employee-auth.test.mjs
    employee-refresh.test.mjs
    employee-request.test.mjs
    employee-models.test.mjs
    hosts.test.mjs
    plugin.test.mjs
    fake-upstream.mjs
```

Responsibilities:

- `index.mjs`: a narrow fork of the community Trae plugin, retaining its
  upstream single-file layout so future protocol fixes remain comparable. It
  exports only the employee provider and keeps employee authentication and
  host-policy functions in clearly marked sections.
- `test/*.test.mjs`: tests split by authentication, refresh, host policy,
  model discovery, request conversion, and package contract.
- `test/fake-upstream.mjs`: local HTTP fixtures for deterministic tests.

The single-file production layout is deliberate compatibility with the
upstream package. Authentication and protocol behavior remain independently
testable even though they are colocated.

## Provider Contract

The plugin exports one provider:

```text
id: trae-bytedance
name: Trae ByteDance
api: internal placeholder base handled by loader.fetch
npm: @ai-sdk/openai-compatible
```

The OpenCode-compatible hooks are:

- `config`: declares the provider and a minimal fallback model catalog.
- `auth.methods`: exposes one employee browser OAuth method.
- `auth.loader`: returns a `fetch` implementation that signs and translates
  each request.
- `auth.refresh`: refreshes the employee token before expiry.
- `provider.models`: reads the employee-visible model list.

The plugin must not claim the `trae-cn` provider id.

## Authentication Flow

1. Verify that the internal login host is reachable. Failure is reported as
   "ByteDance network unavailable"; it is not treated as invalid credentials.
2. Start an HTTP server on a random `127.0.0.1` port.
3. Use the random loopback port and a one-shot callback as the local
   correlation boundary. The observed employee authorization URL does not
   define a `state` round trip, so the plugin must not invent one and assume
   the server will preserve it.
4. Open:

   ```text
   https://solo-web.bytedance.net/authorization
     ?auth_callback_url=http://127.0.0.1:<port>/authorize
     &product_name=TRAE%20CN
   ```

5. The employee completes SSO in the browser.
6. The loopback server accepts only `GET`/`OPTIONS` on `/authorize`, once.
7. Parse the returned credential envelope. Static analysis indicates the
   current employee flow returns:

   ```text
   IDEUserToken
   ExpiredAt
   userInfo
   ```

   A compatibility parser may also accept an encoded `credential` envelope,
   but only the explicitly recognized fields are retained.
8. Parse `userInfo` and require a non-empty employee `username`.
9. Store the Magpie OAuth record:

   ```text
   type: oauth
   access: employee token
   expires: normalized epoch milliseconds
   accountId: employee username
   scope: bytedance
   ```

10. Close the listener immediately after success, refusal, or timeout.

No cookie extraction or automated SSO form interaction is used. The employee
authenticates in the normal browser and approves the flow personally.

## Credential Refresh

Before expiry, call:

```http
POST https://solo-web.bytedance.net/trae/api/v3/UpgradeBytecloudToken
X-Cloudide-Token: <current employee token>
Content-Type: application/json

{}
```

Accept only a successful response containing a replacement token and expiry.
The new values are returned through Magpie's `auth.refresh` hook so Magpie
performs the durable update.

Only one refresh may run per old token at a time. Requests arriving during a
refresh wait for the same promise. A failed refresh does not erase a token
that is still valid. A 401/403 or explicit invalid-token response marks the
sign-in as expired.

## Internal Network And Host Policy

The first version uses a closed allowlist:

```text
solo-web.bytedance.net
copilot-cn.bytedance.net
sso.bytedance.com
accounts.feishu.cn
127.0.0.1
```

Rules:

- upstream APIs must use HTTPS;
- the callback must use HTTP on `127.0.0.1`;
- no URL may contain credentials;
- no non-default port is accepted for an upstream;
- redirects are accepted only when every hop remains on the login allowlist;
- credentials are never attached to redirects automatically;
- model requests can go only to `copilot-cn.bytedance.net`;
- refresh requests can go only to `solo-web.bytedance.net`;
- no server-provided host is trusted without revalidation.

This allowlist is code, not user configuration. Supporting another region or
host requires a code and test change.

## Model Discovery

The plugin first tries the employee model-list endpoint exposed by the current
Trae CN client configuration:

```text
https://copilot-cn.bytedance.net/api/ide/v1/get_model_list
```

If the current employee client instead requires its batch/detail endpoint,
the live protocol probe may select:

```text
/api/ide/v1/get_detail_param
/api/ide/v1/batch_get_detail_param
```

The final endpoint and payload are fixed only after the live SSO probe records
the response status and non-secret JSON shape. The plugin never logs model
responses containing account data.

Model ids, names, context limits, output limits, reasoning support, image
support, and tool support are mapped into Magpie's model schema. If discovery
fails, the plugin returns its last known list when Magpie provides one;
otherwise it exposes a minimal tested fallback list.

## Request Adapter

Magpie hands the plugin an OpenAI-compatible chat-completions request. The
adapter:

1. validates the requested model against the employee model list;
2. normalizes messages, tools, tool choice, reasoning settings, and images;
3. maps the request to the employee Trae endpoint;
4. signs it with the employee token type required by the live protocol;
5. supplies only the fixed Trae client headers proven necessary;
6. translates the upstream stream into OpenAI-compatible SSE;
7. preserves cancellation from the downstream request;
8. maps quota, authentication, invalid-input, and unavailable errors to
   meaningful HTTP statuses.

Static evidence shows employee endpoints under `api/ide/v1`, but it does not
prove that the public plugin's `llm_utils_chat` body is accepted there.
Therefore protocol work is split into two gates:

- Gate A: login, refresh, and model discovery.
- Gate B: one non-tool streaming chat, followed by tools and reasoning.

The implementation does not claim full public-plugin parity until Gate B
passes.

## Secret Handling

- Tests use synthetic tokens only.
- Live diagnostics report booleans, field names, response status, content
  type, and host.
- Tokens, cookies, authorization codes, callback query values, and complete
  response bodies are never logged.
- Errors redact values of keys matching
  `token|secret|key|cookie|authorization|credential|session|jwt`.
- OAuth credentials are stored only through Magpie's `plugin-auth.json`
  mechanism, whose file mode is `0600`.
- The plugin does not read Trae CN's credential store in the first version.
- No employee credential is written into this repository or test artifacts.

## Testing Strategy

### Unit Tests

Run without network access:

- callback accepts the expected employee envelope;
- callback rejects wrong path, duplicate completion, missing username, and
  malformed credential JSON;
- errors and logs contain no synthetic secret values;
- expiry values are normalized correctly;
- host validation rejects HTTP, ports, credentials, suffix attacks, and
  unlisted domains;
- refresh deduplicates concurrent calls;
- refresh preserves a still-valid token on transient failure;
- model-list responses map to Magpie model metadata;
- basic text and tool-call streams map to OpenAI-compatible SSE;
- cancellation aborts the upstream request.

### Plugin Contract Test

Load the package as Magpie does and verify:

- one provider named `trae-bytedance`;
- one OAuth method;
- `loader.fetch` is returned only for a valid OAuth record;
- no hook or model claims `trae-cn`;
- all tests pass under a temporary `HOME`.

### Isolated Magpie Test

Use a temporary home and the installed Magpie binary:

```text
HOME=<temp> /Volumes/magpie/magpie.app/Contents/MacOS/magpie \
  plugin add <absolute-plugin-path>
```

Verify plugin listing and synthetic provider behavior before any live login.
This must not read or modify `~/.config/magpie`.

### Live Employee Test

This step requires the user's presence:

1. Connect to the ByteDance network.
2. Start employee login from the sandbox Magpie.
3. Hand browser control to the user for SSO.
4. Record only callback field names and success/failure metadata.
5. Verify token refresh if the token is close enough to expiry, otherwise
   exercise the refresh function against a synthetic fixture.
6. Fetch and list employee-visible models.
7. Send a minimal non-sensitive prompt such as `Reply with OK`.
8. Verify a streaming response.
9. Test one harmless tool-call round using a local echo tool.
10. Confirm no live token appears in logs, shell history, repository files,
    screenshots, or reports.

Only after the sandbox passes may the user choose to install the local plugin
into the normal Magpie configuration. Existing `trae-cn` remains installed.

## Error Behavior

- No internal network: actionable network/VPN error, account remains signed
  in.
- SSO rejected or cancelled: login fails without storing credentials.
- Callback timeout: listener closes and login fails.
- Malformed callback: generic failure; values are not echoed.
- Token expired or revoked: HTTP 401 and sign-in-required state.
- Model not entitled: HTTP 403.
- Quota exhausted or rate limited: HTTP 429 with upstream reset metadata when
  available.
- Unsupported request feature: HTTP 400, never silently dropped.
- Internal upstream unavailable: HTTP 502/503, without marking credentials
  invalid.

## Delivery And Integration

The implementation is a local package in this workspace. It is installed into
the Magpie sandbox by absolute path, so edits are immediately testable.

No upstream fork, npm publication, or community pull request is part of this
scope. Once behavior is verified, those can be considered separately after
removing internal-only details and obtaining the required organizational
approval.

## Acceptance Criteria

- The package loads in Magpie `0.1.1290`.
- It exposes `trae-bytedance` and does not alter `trae-cn`.
- Employee SSO completes through a one-time loopback callback.
- Only ByteDance employee credentials are accepted.
- All credential-bearing requests are constrained to the fixed allowlist.
- Employee model discovery succeeds on the internal network.
- A minimal streaming chat succeeds through Magpie.
- A harmless tool-call round succeeds, or is explicitly documented as a
  protocol gap if the upstream does not expose it.
- Unit and contract tests pass with the network disabled.
- No real credential is present in repository files or test output.
