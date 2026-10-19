import test from "node:test"
import assert from "node:assert/strict"
import { TraeByteDanceAuthPlugin, _internal } from "../index.mjs"

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

test("redacts arbitrary upstream error details", () => {
  const message = _internal.redactedError(new Error("token=must-not-appear"))
  assert.equal(message, "Trae ByteDance request failed")
  assert.doesNotMatch(message, /must-not-appear/)
})

test("employee browser login uses a one-shot loopback callback", async () => {
  const hooks = await TraeByteDanceAuthPlugin({ client: {} })
  const flow = await hooks.auth.methods[0].authorize()
  const login = new URL(flow.url)
  const callback = new URL(login.searchParams.get("auth_callback_url"))

  assert.equal(callback.protocol, "http:")
  assert.equal(callback.hostname, "127.0.0.1")
  assert.equal(callback.pathname, "/authorize")
  assert.equal(login.hostname, "solo-web.bytedance.net")
  assert.equal(login.pathname, "/authorization")
  assert.equal(login.searchParams.get("product_name"), "TRAE CN")

  const wrong = new URL("/wrong", callback)
  assert.equal((await fetch(wrong)).status, 404)
  assert.equal((await fetch(wrong, { method: "OPTIONS" })).status, 404)
  assert.equal((await fetch(callback, { method: "POST" })).status, 405)
  assert.equal((await fetch(callback, { method: "OPTIONS" })).status, 204)

  callback.searchParams.set("IDEUserToken", "synthetic-callback-token")
  callback.searchParams.set("ExpiredAt", "1893456000000")
  callback.searchParams.set("userInfo", JSON.stringify({ username: "employee.test" }))
  const accepted = await fetch(callback)
  assert.equal(accepted.status, 200)
  assert.equal(accepted.headers.get("cache-control"), "no-store")

  assert.deepEqual(await flow.callback(), {
    type: "success",
    access: "synthetic-callback-token",
    refresh: "",
    expires: 1893456000000,
    accountId: "employee.test",
    scope: "bytedance",
  })
  await assert.rejects(fetch(callback))
})
