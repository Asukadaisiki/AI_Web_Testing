import test, { afterEach } from "node:test"
import assert from "node:assert/strict"
import { TraeByteDanceAuthPlugin, _internal } from "../index.mjs"
import { startFakeUpstream } from "./fake-upstream.mjs"

const nativeFetch = globalThis.fetch
let upstream

afterEach(async () => {
  globalThis.fetch = nativeFetch
  await upstream?.close()
  upstream = undefined
})

const auth = {
  type: "oauth",
  access: "employee-chat-token",
  refresh: "",
  expires: Date.now() + 3_600_000,
  accountId: "employee.test",
  scope: "bytedance",
}

test("routes translated chat requests through the fixed employee host", async () => {
  upstream = await startFakeUpstream((_request, res) => {
    res.writeHead(200, { "content-type": "text/event-stream" })
    res.end('event: output\ndata: {"response":"OK"}\n\nevent: done\ndata: {}\n\n')
  })
  globalThis.fetch = (input, init) => {
    const target = new URL(input)
    return nativeFetch(new URL(target.pathname + target.search, upstream.origin), init)
  }

  const hooks = await TraeByteDanceAuthPlugin({ client: {} })
  const loader = await hooks.auth.loader(async () => auth)
  assert.equal(loader.baseURL, "https://copilot-cn.bytedance.net/v1")

  const response = await loader.fetch(`${loader.baseURL}/chat/completions`, {
    method: "POST",
    body: JSON.stringify({
      model: "glm-5",
      messages: [{ role: "user", content: "Reply with OK" }],
      stream: false,
    }),
  })

  assert.equal(response.status, 200)
  assert.equal((await response.json()).choices[0].message.content, "OK")
  assert.equal(upstream.requests.length, 1)
  const request = upstream.requests[0]
  assert.equal(request.method, "POST")
  assert.equal(request.path, "/api/agent/v3/llm_utils_chat")
  assert.equal(request.headers.authorization, "Cloud-IDE-JWT <redacted>")
  assert.equal(request.headers["x-cloudide-token"], "<redacted>")
  assert.equal(request.body.config_name, "glm-5")
  assert.equal(request.body.messages.at(-1).role, "user")
})

test("translates employee text and tool-call events to OpenAI SSE", async () => {
  upstream = await startFakeUpstream((_request, res) => {
    res.writeHead(200, { "content-type": "text/event-stream" })
    res.end([
      'event: output\ndata: {"response":"Checking. "}\n\n',
      'event: output\ndata: {"tool_calls":[{"id":"call-1","function":{"name":"echo","arguments":{"text":"hello"}}}]}\n\n',
      'event: done\ndata: {}\n\n',
    ].join(""))
  })
  globalThis.fetch = (input, init) => {
    const target = new URL(input)
    return nativeFetch(new URL(target.pathname + target.search, upstream.origin), init)
  }

  const hooks = await TraeByteDanceAuthPlugin({ client: {} })
  const loader = await hooks.auth.loader(async () => auth)
  const response = await loader.fetch(`${loader.baseURL}/chat/completions`, {
    method: "POST",
    body: JSON.stringify({
      model: "glm-5",
      stream: true,
      tools: [{
        type: "function",
        function: {
          name: "echo",
          parameters: {
            type: "object",
            properties: { text: { type: "string" } },
            required: ["text"],
          },
        },
      }],
      messages: [{ role: "user", content: "Call echo with hello" }],
    }),
  })
  const events = (await response.text())
    .split("\n")
    .filter((line) => line.startsWith("data: "))
    .map((line) => line.slice(6))

  assert.equal(response.status, 200)
  assert.equal(response.headers.get("content-type"), "text/event-stream")
  assert.equal(events.at(-1), "[DONE]")
  const chunks = events.slice(0, -1).map(JSON.parse)
  assert.equal(
    chunks.map((chunk) => chunk.choices?.[0]?.delta?.content ?? "").join(""),
    "Checking. ",
  )
  const call = chunks.flatMap(
    (chunk) => chunk.choices?.[0]?.delta?.tool_calls ?? [],
  )[0]
  assert.equal(call.function.name, "echo")
  assert.deepEqual(JSON.parse(call.function.arguments), { text: "hello" })
})

test("renews an expired employee access token before a chat request", async () => {
  upstream = await startFakeUpstream((request, res) => {
    if (request.path === "/trae/api/v3/UpgradeBytecloudToken") {
      res.writeHead(200, { "content-type": "application/json" })
      res.end(JSON.stringify({
        Result: {
          Token: "employee-token-renewed",
          ExpiredAt: 1893456000000,
        },
      }))
      return
    }
    res.writeHead(200, { "content-type": "text/event-stream" })
    res.end('event: output\ndata: {"response":"OK"}\n\nevent: done\ndata: {}\n\n')
  })
  const sentTokens = []
  globalThis.fetch = (input, init) => {
    const target = new URL(input)
    const headers = new Headers(init?.headers)
    sentTokens.push(headers.get("x-cloudide-token"))
    return nativeFetch(new URL(target.pathname + target.search, upstream.origin), init)
  }
  const saved = []
  const hooks = await TraeByteDanceAuthPlugin({
    client: {
      auth: {
        set: async (value) => saved.push(value),
      },
    },
  })
  const expired = {
    ...auth,
    access: "employee-token-expired",
    expires: Date.now() - 1,
  }
  const loader = await hooks.auth.loader(async () => expired)

  const response = await loader.fetch(`${loader.baseURL}/chat/completions`, {
    method: "POST",
    body: JSON.stringify({
      model: "glm-5",
      messages: [{ role: "user", content: "Reply with OK" }],
    }),
  })

  assert.equal(response.status, 200)
  assert.deepEqual(
    upstream.requests.map((request) => request.path),
    [
      "/trae/api/v3/UpgradeBytecloudToken",
      "/api/agent/v3/llm_utils_chat",
    ],
  )
  assert.deepEqual(sentTokens, [
    "employee-token-expired",
    "employee-token-renewed",
  ])
  assert.deepEqual(saved[0], {
    path: { id: "trae-bytedance" },
    body: {
      ...expired,
      access: "employee-token-renewed",
      expires: 1893456000000,
    },
  })
})

test("rejects a non-employee model host before fetch runs", async () => {
  const site = _internal.SITES["trae-bytedance"]
  const original = site.apiOf
  let fetched = false
  site.apiOf = () => "https://outside.example"
  globalThis.fetch = async () => {
    fetched = true
    throw new Error("must not fetch")
  }

  try {
    const hooks = await TraeByteDanceAuthPlugin({ client: {} })
    await assert.rejects(hooks.auth.loader(async () => auth), /not allowed/)
    assert.equal(fetched, false)
  } finally {
    site.apiOf = original
  }
})

test("does not follow a credential-bearing model redirect", async (t) => {
  const attacker = await startFakeUpstream((_request, res) => {
    res.writeHead(200, { "content-type": "text/event-stream" })
    res.end('event: done\ndata: {}\n\n')
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
  const loader = await hooks.auth.loader(async () => auth)
  const response = await loader.fetch(`${loader.baseURL}/chat/completions`, {
    method: "POST",
    body: JSON.stringify({
      model: "glm-5",
      messages: [{ role: "user", content: "Reply with OK" }],
    }),
  })

  assert.equal(response.status, 502)
  assert.equal(attacker.requests.length, 0)
})

test("redacts employee upstream error details", async () => {
  upstream = await startFakeUpstream((_request, res) => {
    res.writeHead(500, { "content-type": "application/json" })
    res.end(JSON.stringify({ message: "token=must-not-appear" }))
  })
  globalThis.fetch = (input, init) => {
    const target = new URL(input)
    return nativeFetch(new URL(target.pathname + target.search, upstream.origin), init)
  }

  const hooks = await TraeByteDanceAuthPlugin({ client: {} })
  const loader = await hooks.auth.loader(async () => auth)
  const response = await loader.fetch(`${loader.baseURL}/chat/completions`, {
    method: "POST",
    body: JSON.stringify({
      model: "glm-5",
      messages: [{ role: "user", content: "Reply with OK" }],
    }),
  })
  const text = await response.text()

  assert.equal(response.status, 500)
  assert.doesNotMatch(text, /must-not-appear/)
  assert.match(text, /request failed/)
})

test("rejects unknown employee models before an upstream request", async () => {
  let fetched = false
  globalThis.fetch = async () => {
    fetched = true
    throw new Error("must not fetch")
  }

  const hooks = await TraeByteDanceAuthPlugin({ client: {} })
  const loader = await hooks.auth.loader(async () => auth)
  const response = await loader.fetch(`${loader.baseURL}/chat/completions`, {
    method: "POST",
    body: JSON.stringify({
      model: "not-in-employee-catalog",
      messages: [{ role: "user", content: "Reply with OK" }],
    }),
  })

  assert.equal(response.status, 400)
  assert.equal(fetched, false)
})

test("rejects unsupported OpenAI request options", async () => {
  let fetched = false
  globalThis.fetch = async () => {
    fetched = true
    throw new Error("must not fetch")
  }

  const hooks = await TraeByteDanceAuthPlugin({ client: {} })
  const loader = await hooks.auth.loader(async () => auth)
  const response = await loader.fetch(`${loader.baseURL}/chat/completions`, {
    method: "POST",
    body: JSON.stringify({
      model: "glm-5",
      messages: [{ role: "user", content: "Return JSON" }],
      response_format: { type: "json_object" },
    }),
  })

  assert.equal(response.status, 400)
  assert.equal(fetched, false)
})

test("rejects unknown OpenAI request fields", async () => {
  let fetched = false
  globalThis.fetch = async () => {
    fetched = true
    throw new Error("must not fetch")
  }

  const hooks = await TraeByteDanceAuthPlugin({ client: {} })
  const loader = await hooks.auth.loader(async () => auth)
  const response = await loader.fetch(`${loader.baseURL}/chat/completions`, {
    method: "POST",
    body: JSON.stringify({
      model: "glm-5",
      messages: [{ role: "user", content: "Reply with OK" }],
      reasoning_effort: "high",
    }),
  })

  assert.equal(response.status, 400)
  assert.equal(fetched, false)
})
