import test from "node:test"
import assert from "node:assert/strict"
import * as plugin from "../index.mjs"

const { TraeByteDanceAuthPlugin } = plugin

test("exports only the ByteDance employee provider", async () => {
  const hooks = await TraeByteDanceAuthPlugin({ client: {} })
  assert.equal(hooks.auth.provider, "trae-bytedance")
  assert.equal(hooks.auth.methods.length, 1)
  assert.match(hooks.auth.methods[0].label, /ByteDance employee/)
  assert.deepEqual(
    Object.entries(plugin)
      .filter(([, value]) => typeof value === "function")
      .map(([name]) => name),
    ["TraeByteDanceAuthPlugin"],
  )
  assert.equal("usage" in hooks.auth, false)
  assert.equal("checkin" in hooks.auth, false)
})
