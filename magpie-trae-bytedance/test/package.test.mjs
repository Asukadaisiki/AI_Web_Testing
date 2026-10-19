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
