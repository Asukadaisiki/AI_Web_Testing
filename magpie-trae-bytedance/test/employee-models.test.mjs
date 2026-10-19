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

test("model discovery does not follow credential-bearing redirects", async (t) => {
  const attacker = await startFakeUpstream((_request, res) => {
    json(res, 200, { function_configs: [] })
  })
  t.after(() => attacker.close())
  upstream = await startFakeUpstream((_request, res) => {
    res.writeHead(302, { location: `${attacker.origin}/collect` })
    res.end()
  })
  globalThis.fetch = (input, init) => {
    const target = new URL(input)
    return nativeFetch(new URL(target.pathname + target.search, upstream.origin), init)
  }

  const hooks = await TraeByteDanceAuthPlugin({ client: {} })
  const fallback = { "glm-5": { id: "glm-5" } }
  const models = await hooks.provider.models({ models: fallback }, {
    auth: {
      type: "oauth",
      access: "employee-model-redirect-token",
      refresh: "",
      expires: Date.now() + 3_600_000,
      accountId: "employee.test",
      scope: "bytedance",
    },
  })

  assert.deepEqual(models, fallback)
  assert.equal(attacker.requests.length, 0)
})

test("an empty discovery result leaves fallback models usable", async () => {
  let batchRequests = 0
  upstream = await startFakeUpstream((request, res) => {
    if (request.path.includes("batch_get_detail_param")) {
      batchRequests += 1
      json(res, 200, batchRequests === 1 ? {
        function_configs: [{
          function: "chat_v3",
          config_info_list: [{
            config_name: "employee-model",
            usage: "chat_completion",
          }],
        }],
      } : { function_configs: [] })
      return
    }
    if (request.path.includes("get_detail_param")) {
      json(res, 200, { config_info_list: [] })
      return
    }
    res.writeHead(200, { "content-type": "text/event-stream" })
    res.end('event: output\ndata: {"response":"OK"}\n\nevent: done\ndata: {}\n\n')
  })
  globalThis.fetch = (input, init) => {
    const target = new URL(input)
    return nativeFetch(new URL(target.pathname + target.search, upstream.origin), init)
  }

  const hooks = await TraeByteDanceAuthPlugin({ client: {} })
  const fallback = { "glm-5": { id: "glm-5" } }
  const context = {
    auth: {
      type: "oauth",
      access: "employee-empty-model-token",
      refresh: "",
      expires: Date.now() + 3_600_000,
      accountId: "employee.test",
      scope: "bytedance",
    },
  }
  assert.deepEqual(
    Object.keys(await hooks.provider.models({ models: fallback }, context)),
    ["employee-model"],
  )
  assert.deepEqual(await hooks.provider.models({ models: fallback }, {
    auth: context.auth,
  }), fallback)

  const loader = await hooks.auth.loader(async () => context.auth)
  const response = await loader.fetch(`${loader.baseURL}/chat/completions`, {
    method: "POST",
    body: JSON.stringify({
      model: "glm-5",
      messages: [{ role: "user", content: "Reply with OK" }],
    }),
  })

  assert.equal(response.status, 200)
})
