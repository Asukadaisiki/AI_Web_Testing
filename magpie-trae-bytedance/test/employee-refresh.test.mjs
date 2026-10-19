import test, { afterEach } from "node:test"
import assert from "node:assert/strict"
import { _internal } from "../index.mjs"
import { json, startFakeUpstream } from "./fake-upstream.mjs"

const nativeFetch = globalThis.fetch
let upstream

afterEach(async () => {
  globalThis.fetch = nativeFetch
  await upstream?.close()
  upstream = undefined
})

async function routeRefresh(handler) {
  upstream = await startFakeUpstream(handler)
  globalThis.fetch = (input, init) => {
    const target = new URL(input)
    return nativeFetch(new URL(target.pathname, upstream.origin), init)
  }
}

test("refreshes once per old employee token", async () => {
  await routeRefresh(async (_request, res) => {
    await new Promise((resolve) => setTimeout(resolve, 20))
    json(res, 200, {
      Result: {
        Token: "employee-token-2",
        ExpiredAt: 1893456000000,
      },
    })
  })
  const auth = {
    type: "oauth",
    access: "employee-token-1",
    expires: 1890000000000,
  }

  const [first, second] = await Promise.all([
    _internal.refreshEmployee(auth),
    _internal.refreshEmployee(auth),
  ])

  assert.deepEqual(first, {
    access: "employee-token-2",
    expires: 1893456000000,
  })
  assert.deepEqual(second, first)
  assert.equal(upstream.requests.length, 1)
  assert.equal(upstream.requests[0].method, "POST")
  assert.equal(upstream.requests[0].path, "/trae/api/v3/UpgradeBytecloudToken")
  assert.equal(upstream.requests[0].headers["x-cloudide-token"], "<redacted>")
  assert.deepEqual(upstream.requests[0].body, {})
})

test("a temporary refresh failure does not modify existing auth", async () => {
  await routeRefresh((_request, res) => json(res, 503, { message: "unavailable" }))
  const auth = {
    type: "oauth",
    access: "employee-token-old",
    expires: Date.now() + 60_000,
  }
  const before = structuredClone(auth)

  await assert.rejects(_internal.refreshEmployee(auth), /renewing the sign-in/)
  assert.deepEqual(auth, before)
})

test("an unauthorized refresh marks the sign-in expired", async () => {
  await routeRefresh((_request, res) => json(res, 401, { message: "invalid" }))
  const error = await _internal.refreshEmployee({
    type: "oauth",
    access: "employee-token-old",
    expires: 1,
  }).catch((caught) => caught)

  assert.equal(error.signIn, "expired")
  assert.doesNotMatch(String(error), /employee-token-old/)
})
