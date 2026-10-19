import test, { afterEach } from "node:test"
import assert from "node:assert/strict"
import { TraeByteDanceAuthPlugin } from "../index.mjs"
import { json, startFakeUpstream } from "./fake-upstream.mjs"

const nativeFetch = globalThis.fetch
let upstream

afterEach(async () => {
  globalThis.fetch = nativeFetch
  await upstream?.close()
  upstream = undefined
})

test("normalizes the employee model catalog", async () => {
  const requestedOrigins = []
  upstream = await startFakeUpstream((request, res) => {
    assert.equal(request.path, "/api/ide/v1/batch_get_detail_param")
    json(res, 200, {
      function_configs: [{
        function: "chat_v3",
        config_info_list: [{
          config_name: "employee-model",
          usage: "chat_completion",
          multimodal: true,
          display_config: { display_name: "Employee Model" },
          context_window_tokens: { dev: 200000 },
          model_detail_list: [{
            model_name: "employee-model__dev",
            max_tokens: 32000,
          }],
        }],
      }],
    })
  })
  globalThis.fetch = (input, init) => {
    const target = new URL(input)
    requestedOrigins.push(target.origin)
    return nativeFetch(new URL(target.pathname + target.search, upstream.origin), init)
  }

  const hooks = await TraeByteDanceAuthPlugin({ client: {} })
  const models = await hooks.provider.models({ models: {} }, {
    auth: {
      type: "oauth",
      access: "employee-model-token",
      refresh: "",
      expires: Date.now() + 3_600_000,
      accountId: "employee.test",
      scope: "bytedance",
    },
  })
  const model = models["employee-model"]

  assert.equal(model.id, "employee-model")
  assert.equal(model.name, "Employee Model")
  assert.deepEqual(model.limit, { context: 200000, output: 32000 })
  assert.equal(model.capabilities.input.image, true)
  assert.equal(model.capabilities.reasoning, true)
  assert.equal(model.capabilities.toolcall, true)
  assert.equal(model.api.url, "https://copilot-cn.bytedance.net")
  assert.deepEqual(requestedOrigins, ["https://copilot-cn.bytedance.net"])
})
