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
