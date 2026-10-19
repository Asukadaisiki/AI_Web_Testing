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

test("redacts arbitrary upstream error details", () => {
  const message = _internal.redactedError(new Error("token=must-not-appear"))
  assert.equal(message, "Trae ByteDance request failed")
  assert.doesNotMatch(message, /must-not-appear/)
})
