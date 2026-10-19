# Trae ByteDance Magpie Plugin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and locally verify an internal-only Magpie provider that signs ByteDance employees in through Trae CN SSO and exposes employee Trae models as `trae-bytedance/<model>`.

**Architecture:** Fork the MIT-licensed community Trae provider implementation as the protocol baseline, but export only a new `trae-bytedance` provider. Keep employee OAuth, token refresh, and host validation explicit and isolated; reuse the proven Trae message, stream, tool-call, and model-list conversion code only after requests are constrained to the employee hosts.

**Tech Stack:** ECMAScript modules, Node.js built-in test runner, Bun/Magpie plugin host, loopback HTTP OAuth callback, native `fetch`.

**Spec:** `docs/superpowers/specs/2026-10-19-trae-bytedance-magpie-plugin-design.md`

## Global Constraints

- Provider id is exactly `trae-bytedance`; the plugin must never claim `trae-cn`.
- Runtime dependencies are limited to Node/Bun built-ins.
- Credentials may be sent only to `solo-web.bytedance.net` and `copilot-cn.bytedance.net`.
- SSO navigation may use only `solo-web.bytedance.net`, `sso.bytedance.com`, and `accounts.feishu.cn`.
- OAuth callback listens only on `127.0.0.1`, is one-shot, and times out.
- No real token, cookie, callback value, credential body, or employee identity may enter source files, logs, fixtures, screenshots, or commits.
- The public `@magpie-community/opencode-trae-auth` installation and `trae-cn` account remain untouched.
- Live tests use a temporary `HOME` before any normal Magpie installation.
- New behavior is written test-first and each red test must fail for the intended missing behavior.

---

### Task 1: Create The Standalone Plugin Baseline

**Files:**
- Create: `magpie-trae-bytedance/package.json`
- Create: `magpie-trae-bytedance/LICENSE`
- Create: `magpie-trae-bytedance/NOTICE`
- Create: `magpie-trae-bytedance/index.mjs`
- Create: `magpie-trae-bytedance/test/plugin.test.mjs`

**Interfaces:**
- Consumes: community source at `/tmp/magpie-plugins-analysis/packages/trae/index.mjs`.
- Produces: `TraeByteDanceAuthPlugin(input) -> Promise<hooks>` with provider id `trae-bytedance`.

- [ ] **Step 1: Write the failing provider identity test**

```js
import test from "node:test"
import assert from "node:assert/strict"
import { TraeByteDanceAuthPlugin } from "../index.mjs"

test("exports only the ByteDance employee provider", async () => {
  const hooks = await TraeByteDanceAuthPlugin({ client: {} })
  assert.equal(hooks.auth.provider, "trae-bytedance")
  assert.equal(hooks.auth.methods.length, 1)
  assert.match(hooks.auth.methods[0].label, /ByteDance employee/)
})
```

- [ ] **Step 2: Run the test and verify the module is missing**

Run:

```bash
node --test magpie-trae-bytedance/test/plugin.test.mjs
```

Expected: FAIL with `ERR_MODULE_NOT_FOUND`.

- [ ] **Step 3: Copy the MIT baseline and restrict exports**

Mechanically copy the community Trae source to
`magpie-trae-bytedance/index.mjs`:

```bash
mkdir -p magpie-trae-bytedance/test
cp /tmp/magpie-plugins-analysis/packages/trae/index.mjs \
  magpie-trae-bytedance/index.mjs
cp /tmp/magpie-plugins-analysis/LICENSE magpie-trae-bytedance/LICENSE
```

Create `NOTICE` with:

```text
This package contains code adapted from magpie-community/plugins,
packages/trae, licensed under the MIT License.
Source: https://github.com/magpie-community/plugins
```

Retain the upstream copyright/license and add this package metadata:

```json
{
  "name": "opencode-trae-bytedance-auth",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "main": "./index.mjs",
  "files": ["index.mjs", "README.md", "LICENSE", "NOTICE"],
  "license": "MIT"
}
```

Remove the exported public provider functions and add an employee site entry
plus one exported function:

```js
const PROVIDER = "trae-bytedance"

export const TraeByteDanceAuthPlugin = makePlugin(SITES[PROVIDER])
```

The package entry file must export no other function because Magpie invokes
every exported function as a plugin. `_internal` remains an exported object.

- [ ] **Step 4: Run the identity test**

Run:

```bash
node --test magpie-trae-bytedance/test/plugin.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit the standalone baseline**

```bash
git add magpie-trae-bytedance
git commit -m "feat: add standalone Trae employee plugin"
```

---

### Task 2: Enforce The Employee Host Boundary

**Files:**
- Modify: `magpie-trae-bytedance/index.mjs`
- Create: `magpie-trae-bytedance/test/hosts.test.mjs`

**Interfaces:**
- Produces:
  - `employeeURL(raw, purpose) -> URL`
  - `isEmployeeURL(raw, purpose) -> boolean`
  - `purpose` is one of `"login" | "refresh" | "model"`.

- [ ] **Step 1: Write host validation tests**

```js
import test from "node:test"
import assert from "node:assert/strict"
import { _internal } from "../index.mjs"

test("accepts only the fixed host for each credential-bearing purpose", () => {
  assert.equal(
    _internal.employeeURL(
      "https://solo-web.bytedance.net/trae/api/v3/UpgradeBytecloudToken",
      "refresh",
    ).hostname,
    "solo-web.bytedance.net",
  )
  assert.equal(
    _internal.employeeURL(
      "https://copilot-cn.bytedance.net/api/agent/v3/llm_utils_chat",
      "model",
    ).hostname,
    "copilot-cn.bytedance.net",
  )
})

for (const raw of [
  "http://copilot-cn.bytedance.net/api/agent/v3/llm_utils_chat",
  "https://copilot-cn.bytedance.net.evil.example/x",
  "https://user@copilot-cn.bytedance.net/x",
  "https://copilot-cn.bytedance.net:8443/x",
]) {
  test(`rejects unsafe employee URL ${raw}`, () => {
    assert.throws(() => _internal.employeeURL(raw, "model"), /not allowed/)
  })
}
```

- [ ] **Step 2: Verify the host tests fail**

Run:

```bash
node --test magpie-trae-bytedance/test/hosts.test.mjs
```

Expected: FAIL because `_internal.employeeURL` does not exist.

- [ ] **Step 3: Implement the closed allowlist**

```js
const EMPLOYEE_HOSTS = Object.freeze({
  login: new Set(["solo-web.bytedance.net", "sso.bytedance.com", "accounts.feishu.cn"]),
  refresh: new Set(["solo-web.bytedance.net"]),
  model: new Set(["copilot-cn.bytedance.net"]),
})

function employeeURL(raw, purpose) {
  const url = new URL(raw)
  const allowed = EMPLOYEE_HOSTS[purpose]
  if (
    !allowed ||
    url.protocol !== "https:" ||
    url.username ||
    url.password ||
    url.port ||
    !allowed.has(url.hostname.toLowerCase())
  ) {
    throw new Error(`Trae ByteDance ${purpose} URL is not allowed`)
  }
  return url
}

function isEmployeeURL(raw, purpose) {
  try {
    employeeURL(raw, purpose)
    return true
  } catch {
    return false
  }
}
```

Export `employeeURL` and `isEmployeeURL` through `_internal`. Configure the
employee site with:

```js
hosts: {
  web: "https://solo-web.bytedance.net",
  auth: "https://solo-web.bytedance.net",
  api: "https://copilot-cn.bytedance.net",
}
```

- [ ] **Step 4: Run identity and host tests**

Run:

```bash
node --test magpie-trae-bytedance/test/plugin.test.mjs \
  magpie-trae-bytedance/test/hosts.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit host isolation**

```bash
git add magpie-trae-bytedance/index.mjs magpie-trae-bytedance/test/hosts.test.mjs
git commit -m "feat: constrain Trae employee hosts"
```

---

### Task 3: Parse Employee OAuth Credentials Without Leaking Values

**Files:**
- Modify: `magpie-trae-bytedance/index.mjs`
- Create: `magpie-trae-bytedance/test/employee-auth.test.mjs`

**Interfaces:**
- Produces:
  - `parseEmployeeCredential(searchParams) -> OAuthAuth`
  - `redactedError(error) -> string`

- [ ] **Step 1: Write callback parsing and redaction tests**

```js
import test from "node:test"
import assert from "node:assert/strict"
import { _internal } from "../index.mjs"

test("parses the current employee callback envelope", () => {
  const q = new URLSearchParams({
    IDEUserToken: "synthetic-secret-token",
    ExpiredAt: "1893456000000",
    userInfo: JSON.stringify({ username: "employee.test", ai_region: "CN" }),
  })
  assert.deepEqual(_internal.parseEmployeeCredential(q), {
    type: "oauth",
    access: "synthetic-secret-token",
    refresh: "",
    expires: 1893456000000,
    accountId: "employee.test",
    scope: "bytedance",
    region: "CN",
  })
})

test("parses an encoded credential envelope", () => {
  const credential = encodeURIComponent(JSON.stringify({
    IDEUserToken: "synthetic-secret-token",
    ExpiredAt: 1893456000000,
    userInfo: JSON.stringify({ username: "employee.test" }),
  }))
  assert.equal(
    _internal.parseEmployeeCredential(
      new URLSearchParams({ credential }),
    ).accountId,
    "employee.test",
  )
})

test("rejects malformed employee credentials without echoing secrets", () => {
  const secret = "must-not-appear"
  assert.throws(
    () => _internal.parseEmployeeCredential(
      new URLSearchParams({ IDEUserToken: secret, userInfo: "{}" }),
    ),
    (error) => !String(error).includes(secret),
  )
})
```

- [ ] **Step 2: Verify the parser tests fail**

Run:

```bash
node --test magpie-trae-bytedance/test/employee-auth.test.mjs
```

Expected: FAIL because `parseEmployeeCredential` does not exist.

- [ ] **Step 3: Implement strict parsing**

Implement these rules:

```js
function parseEmployeeCredential(input) {
  const outer = Object.fromEntries(input.entries())
  const nested = parseCredentialValue(outer.credential)
  const value = { ...outer, ...nested }
  const access = String(value.IDEUserToken ?? "")
  const info = parseJSON(String(value.userInfo ?? ""))
  const accountId = String(info.username ?? "")
  const expires = whenOf(value.ExpiredAt)

  if (!access || !accountId || !expires) {
    throw new Error("Trae ByteDance returned an incomplete employee credential")
  }
  return {
    type: "oauth",
    access,
    refresh: "",
    expires,
    accountId,
    scope: "bytedance",
    ...(info.ai_region ? { region: String(info.ai_region) } : {}),
  }
}
```

`parseCredentialValue` accepts JSON or URL-encoded query syntax and returns
only recognized fields. It must not include callback values in errors.

- [ ] **Step 4: Run the parser tests**

Run:

```bash
node --test magpie-trae-bytedance/test/employee-auth.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit credential parsing**

```bash
git add magpie-trae-bytedance/index.mjs magpie-trae-bytedance/test/employee-auth.test.mjs
git commit -m "feat: parse Trae employee OAuth callback"
```

---

### Task 4: Implement The One-Shot Loopback Login

**Files:**
- Modify: `magpie-trae-bytedance/index.mjs`
- Modify: `magpie-trae-bytedance/test/employee-auth.test.mjs`

**Interfaces:**
- Produces:
  - `employeeBrowserSignIn(site) -> {url, instructions, method, callback}`

- [ ] **Step 1: Add failing listener behavior tests**

Add tests that call the returned `authorize`, extract `auth_callback_url`,
and assert:

```js
assert.equal(callback.protocol, "http:")
assert.equal(callback.hostname, "127.0.0.1")
assert.equal(callback.pathname, "/authorize")
assert.equal(login.hostname, "solo-web.bytedance.net")
assert.equal(login.pathname, "/authorization")
assert.equal(login.searchParams.get("product_name"), "TRAE CN")
```

Send a synthetic callback and assert `await flow.callback()` returns the
parsed OAuth record. Send a second callback and assert it cannot replace the
first result. Send `/wrong` and assert HTTP 404.

- [ ] **Step 2: Verify listener tests fail**

Run:

```bash
node --test magpie-trae-bytedance/test/employee-auth.test.mjs
```

Expected: FAIL because the employee method still uses public Trae OAuth.

- [ ] **Step 3: Implement the employee authorization method**

Use `createServer`, bind with:

```js
server.listen(0, "127.0.0.1", resolve)
```

Build the URL as:

```js
const login = new URL("/authorization", site.hosts.web)
login.searchParams.set("auth_callback_url", callbackURL)
login.searchParams.set("product_name", "TRAE CN")
```

Accept only `OPTIONS` and `GET /authorize`, return `Cache-Control: no-store`,
settle once, and close all sockets on completion or five-minute timeout.
Expose this as the only auth method:

```js
{
  type: "oauth",
  label: "ByteDance employee (browser)",
  authorize: () => employeeBrowserSignIn(site),
}
```

- [ ] **Step 4: Run all authentication tests**

Run:

```bash
node --test magpie-trae-bytedance/test/employee-auth.test.mjs
```

Expected: PASS with no secret values in output.

- [ ] **Step 5: Commit loopback OAuth**

```bash
git add magpie-trae-bytedance/index.mjs magpie-trae-bytedance/test/employee-auth.test.mjs
git commit -m "feat: add Trae employee browser login"
```

---

### Task 5: Refresh Employee Tokens Once

**Files:**
- Modify: `magpie-trae-bytedance/index.mjs`
- Create: `magpie-trae-bytedance/test/employee-refresh.test.mjs`
- Create: `magpie-trae-bytedance/test/fake-upstream.mjs`

**Interfaces:**
- Produces:
  - `refreshEmployee(auth) -> Promise<Partial<OAuthAuth> | undefined>`
  - one in-flight refresh per old access token.

- [ ] **Step 1: Write refresh tests against a local fixture**

The fixture captures method, path, and header names while replacing values
with `"<redacted>"`. Tests assert:

```js
assert.equal(request.method, "POST")
assert.equal(request.path, "/trae/api/v3/UpgradeBytecloudToken")
assert.equal(request.headers["x-cloudide-token"], "<redacted>")
assert.deepEqual(result, {
  access: "employee-token-2",
  expires: 1893456000000,
})
```

Call refresh twice concurrently and assert the fixture received one request.
Return HTTP 503 and assert the existing auth record is not modified. Return
401 and assert the thrown error carries `signIn: "expired"`.

- [ ] **Step 2: Verify refresh tests fail**

Run:

```bash
node --test magpie-trae-bytedance/test/employee-refresh.test.mjs
```

Expected: FAIL because employee refresh is not implemented.

- [ ] **Step 3: Implement refresh**

The production URL is fixed:

```js
const EMPLOYEE_REFRESH =
  "https://solo-web.bytedance.net/trae/api/v3/UpgradeBytecloudToken"
```

Send `{}` with `X-Cloudide-Token: auth.access`. Parse
`Result.Token` and `Result.ExpiredAt`. Deduplicate with:

```js
const refreshing = new Map()
```

Delete the map entry in `finally`. Return only changed fields to Magpie.

- [ ] **Step 4: Run refresh and authentication tests**

Run:

```bash
node --test magpie-trae-bytedance/test/employee-refresh.test.mjs \
  magpie-trae-bytedance/test/employee-auth.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit refresh behavior**

```bash
git add magpie-trae-bytedance/index.mjs magpie-trae-bytedance/test
git commit -m "feat: refresh Trae employee tokens"
```

---

### Task 6: Route Models Through The Employee Host

**Files:**
- Modify: `magpie-trae-bytedance/index.mjs`
- Create: `magpie-trae-bytedance/test/employee-request.test.mjs`
- Create: `magpie-trae-bytedance/test/employee-models.test.mjs`

**Interfaces:**
- Consumes: the baseline Trae `chatBody`, SSE parser, OpenAI response encoder,
  and live model-list mapper.
- Produces: employee model discovery and `loader.fetch`.

- [ ] **Step 1: Write failing routing tests**

Against a local fixture, assert a chat request is transformed and sent to:

```text
POST /api/agent/v3/llm_utils_chat
```

Assert these properties:

```js
assert.equal(upstream.headers.authorization, "Cloud-IDE-JWT <redacted>")
assert.equal(upstream.headers["x-cloudide-token"], "<redacted>")
assert.equal(upstream.body.config_name, "glm-5")
assert.equal(upstream.body.messages.at(-1).role, "user")
```

Assert the plugin rejects any rewritten upstream URL outside
`copilot-cn.bytedance.net` before `fetch` runs.

- [ ] **Step 2: Write failing model-list tests**

Feed a synthetic employee `batch_get_detail_param` response and assert the
provider hook returns the normalized model id, name, context, output, image,
reasoning, and tool capabilities.

- [ ] **Step 3: Verify request and model tests fail**

Run:

```bash
node --test magpie-trae-bytedance/test/employee-request.test.mjs \
  magpie-trae-bytedance/test/employee-models.test.mjs
```

Expected: FAIL because the copied public site configuration does not route the
employee provider.

- [ ] **Step 4: Configure the employee site**

Add an explicit site entry:

```js
"trae-bytedance": {
  id: "trae-bytedance",
  realm: "Trae ByteDance",
  name: "Trae ByteDance",
  employee: true,
  webHost: "bytedance.net",
  hosts: {
    web: "https://solo-web.bytedance.net",
    auth: "https://solo-web.bytedance.net",
    api: "https://copilot-cn.bytedance.net",
  },
  clientId: "ono9krqynydwx5",
  appId: "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8",
  ideVersion: "3.3.65",
  ideVersionCode: "20260401",
  clientVersion: "0.1.69",
  clientVersionCode: "20260917",
  pluginVersion: "2.3.24254",
  brand: "TRAE",
  functions: ["chat_v3", "solo_work_lite", "solo_agent", "solo_agent_lite"],
  models: { ...SITES["trae-cn"].models },
  apiOf: () => "https://copilot-cn.bytedance.net",
  modelHost: (raw) => isEmployeeURL(raw, "model"),
  ownPage: () => false,
  regionOf: () => "CN",
}
```

Use the public Trae CN function catalog and request translation, but override:

- OAuth method with `employeeBrowserSignIn`;
- refresh with `refreshEmployee`;
- `fresh` renews an employee account from `access`, not `refresh`;
- `auth.refresh` is exposed for employee accounts even though `refresh` is
  empty;
- `usage` and `checkin` properties are not added to the employee auth hook;
- all upstream model URLs passed through `employeeURL(..., "model")`.

- [ ] **Step 5: Run routing, model, and stream regression tests**

Run:

```bash
node --test magpie-trae-bytedance/test/*.test.mjs
```

Expected: PASS.

- [ ] **Step 6: Commit employee model routing**

```bash
git add magpie-trae-bytedance/index.mjs magpie-trae-bytedance/test
git commit -m "feat: route Trae employee model requests"
```

---

### Task 7: Document Installation And Security Boundaries

**Files:**
- Create: `magpie-trae-bytedance/README.md`
- Modify: `magpie-trae-bytedance/package.json`
- Create: `magpie-trae-bytedance/test/package.test.mjs`

**Interfaces:**
- Produces: a locally installable package with no publish side effects.

- [ ] **Step 1: Write package policy tests**

```js
import test from "node:test"
import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"

test("package is private and exposes one entry", async () => {
  const pkg = JSON.parse(await readFile(
    new URL("../package.json", import.meta.url),
    "utf8",
  ))
  assert.equal(pkg.private, true)
  assert.equal(pkg.main, "./index.mjs")
  assert.equal(pkg.type, "module")
  assert.equal(pkg.scripts?.test, "node --test test/*.test.mjs")
})
```

- [ ] **Step 2: Verify the policy test fails for the missing script**

Run:

```bash
node --test magpie-trae-bytedance/test/package.test.mjs
```

Expected: FAIL because `pkg.scripts.test` is undefined.

- [ ] **Step 3: Add scripts and README**

Add:

```json
"scripts": {
  "test": "node --test test/*.test.mjs"
}
```

README must include:

- internal-network and employee-only prerequisites;
- local Magpie install command;
- `trae-bytedance/<model>` naming;
- exact allowed hosts;
- token storage location and `0600` expectation;
- uninstall command;
- statement that the plugin does not bypass entitlement or network controls;
- live-test procedure using a non-sensitive prompt.

- [ ] **Step 4: Run package tests**

Run:

```bash
cd magpie-trae-bytedance
npm test
```

Expected: all tests PASS and output contains no synthetic token value.

- [ ] **Step 5: Commit package documentation**

```bash
git add magpie-trae-bytedance
git commit -m "docs: explain Trae employee plugin usage"
```

---

### Task 8: Verify In An Isolated Magpie Home

**Files:**
- Create outside repository: temporary Magpie home and logs.
- Do not modify: `~/.config/magpie`.

**Interfaces:**
- Consumes: `/Volumes/magpie/magpie.app/Contents/MacOS/magpie`.
- Produces: proof that Magpie loads `trae-bytedance` without provider clashes.

- [ ] **Step 1: Run the complete automated suite**

```bash
node --test magpie-trae-bytedance/test/*.test.mjs
```

Expected: all tests PASS.

- [ ] **Step 2: Create a temporary home and install by path**

```bash
export TRAE_MAGPIE_TEST_HOME="$(mktemp -d)"
HOME="$TRAE_MAGPIE_TEST_HOME" \
  /Volumes/magpie/magpie.app/Contents/MacOS/magpie \
  plugin add "$PWD/magpie-trae-bytedance"
```

- [ ] **Step 3: Inspect provider metadata**

```bash
HOME="$TRAE_MAGPIE_TEST_HOME" \
  /Volumes/magpie/magpie.app/Contents/MacOS/magpie plugin
```

Expected: one `trae-bytedance` provider, no `trae-cn` claim, no load error.

- [ ] **Step 4: Check sandbox files for leaked synthetic secrets**

```bash
if rg -n 'synthetic-secret-token|must-not-appear' \
  "$TRAE_MAGPIE_TEST_HOME" magpie-trae-bytedance \
  --glob '!test/**'; then
  exit 1
fi
```

Expected: no matches.

- [ ] **Step 5: Record the sandbox verification**

Add the exact Magpie version, package version, commands, and pass/fail result
to the implementation commit message or final report. Do not commit sandbox
logs.

---

### Task 9: Complete The Live Employee Verification

**Files:**
- Modify only if a live discrepancy is proven:
  `magpie-trae-bytedance/index.mjs`
- Add a synthetic regression test for every proven discrepancy.
- Do not commit live response bodies or credentials.

**Interfaces:**
- Produces: authenticated model discovery and one streaming chat through the
  sandbox Magpie gateway.

- [ ] **Step 1: Start login in the sandbox**

```bash
HOME="$TRAE_MAGPIE_TEST_HOME" \
  /Volumes/magpie/magpie.app/Contents/MacOS/magpie \
  plugin login trae-bytedance
```

Hand browser control to the user. The user completes SSO.

- [ ] **Step 2: Verify authentication without printing credentials**

```bash
HOME="$TRAE_MAGPIE_TEST_HOME" \
  /Volumes/magpie/magpie.app/Contents/MacOS/magpie plugin
```

Expected: `trae-bytedance` is signed in and displays only the employee account
label.

- [ ] **Step 3: Fetch and test models**

```bash
HOME="$TRAE_MAGPIE_TEST_HOME" \
  /Volumes/magpie/magpie.app/Contents/MacOS/magpie \
  provider test trae-bytedance
```

Expected: model discovery succeeds and at least one employee model is listed.

- [ ] **Step 4: Run a minimal gateway chat**

Start sandbox Magpie on an unused loopback port and send an OpenAI-compatible
streaming request with:

```json
{
  "model": "trae-bytedance/<first-listed-model>",
  "messages": [{"role": "user", "content": "Reply with OK"}],
  "stream": true
}
```

Expected: HTTP 200, at least one content delta, `[DONE]`, and no credential in
the response or logs.

- [ ] **Step 5: Verify a harmless tool-call round**

Send a request containing one function named `echo` with schema:

```json
{
  "type": "object",
  "properties": {"text": {"type": "string"}},
  "required": ["text"]
}
```

Prompt the model to call `echo` with `"hello"`. Feed the local result back and
verify the final answer streams successfully.

- [ ] **Step 6: Convert any live discrepancy into a red test**

If login, model discovery, headers, stream events, or tool calls differ from
the static evidence:

1. record only field names, status, content type, and redacted shape;
2. create the smallest synthetic fixture reproducing that shape;
3. run the new test and observe the expected failure;
4. implement the minimal parser or adapter change;
5. rerun the complete suite;
6. repeat the sandbox live check.

- [ ] **Step 7: Final verification**

Run:

```bash
node --test magpie-trae-bytedance/test/*.test.mjs
git diff --check
git status --short
```

Expected: tests PASS; no whitespace errors; only intended plugin and plan/spec
changes are present in the feature worktree.
