import { createServer } from "node:http"

export async function startFakeUpstream(handler) {
  const requests = []
  const server = createServer(async (req, res) => {
    const chunks = []
    for await (const chunk of req) chunks.push(chunk)
    const text = Buffer.concat(chunks).toString("utf8")
    const headers = Object.fromEntries(
      Object.entries(req.headers).map(([name, value]) => [
        name,
        /authorization|token/i.test(name) ? "<redacted>" : value,
      ]),
    )
    const request = {
      method: req.method,
      path: req.url,
      headers,
      body: text ? JSON.parse(text) : undefined,
    }
    requests.push(request)
    await handler(request, res)
  })

  await new Promise((resolve, reject) => {
    server.once("error", reject)
    server.listen(0, "127.0.0.1", resolve)
  })

  return {
    origin: `http://127.0.0.1:${server.address().port}`,
    requests,
    close: () => new Promise((resolve) => server.close(resolve)),
  }
}

export function json(res, status, body) {
  res.writeHead(status, { "content-type": "application/json" })
  res.end(JSON.stringify(body))
}
