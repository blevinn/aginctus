// No OpenCode runtime/network required: test the actual hook's scoping and
// propagation semantics with synthetic OpenCode-style events.
import test from "node:test"
import assert from "node:assert/strict"
import { AginctusGoSession } from "./go_session_plugin.js"

const first = "ses_0123456789abcdefghijklmno"
const second = "ses_0123456789abcdefghijklmnp"
const model = { providerID: "aginctus", id: "aginctus-go-pilot" }

async function headersFor(event) {
  const plugin = await AginctusGoSession()
  const output = { headers: {} }
  await plugin["chat.headers"](event, output)
  return output.headers
}

test("native OpenCode session passes unchanged on the Go pilot only", async () => {
  const one = await headersFor({ sessionID: first, model })
  const repeat = await headersFor({ sessionID: first, model })
  const independent = await headersFor({ sessionID: second, model })
  assert.deepEqual(one, {
    "x-opencode-session": first,
    "x-opencode-session-id": first,
  })
  assert.deepEqual(one, repeat)
  assert.notDeepEqual(one, independent)
})

test("native and spend-log session values are emitted together without fabrication", async () => {
  const plugin = await AginctusGoSession()
  const output = { headers: { "x-provider-context": "untouched" } }
  await plugin["chat.headers"]({ sessionID: second, model }, output)
  assert.equal(output.headers["x-opencode-session"], second)
  assert.equal(output.headers["x-opencode-session-id"], second)
  assert.equal(output.headers["x-provider-context"], "untouched")
  assert.equal(Object.keys(output.headers).length, 3)
})

test("unrelated models and providers never receive a Go session header", async () => {
  for (const target of [
    { providerID: "aginctus", id: "unrelated-model" },
    { providerID: "other", id: "aginctus-go-pilot" },
  ]) {
    const headers = await headersFor({ sessionID: first, model: target })
    assert.deepEqual(headers, {})
  }
})

test("Go pilot fails closed if session ID is missing or untrustworthy", async () => {
  const plugin = await AginctusGoSession()
  for (const sessionID of [
    undefined, null, "", "not-native", "ses_", "ses_abc\r\nx-api-key:bad",
    "ses_" + "z".repeat(129),
  ]) {
    const output = { headers: {} }
    await assert.rejects(
      () => plugin["chat.headers"]({ model, sessionID }, output),
      /missing or invalid native OpenCode session/,
    )
    assert.deepEqual(output.headers, {})
  }
})

test("unrelated preexisting request headers are preserved without additions", async () => {
  const plugin = await AginctusGoSession()
  const output = { headers: { "X-Expected": "value" } }
  await plugin["chat.headers"]({ model, sessionID: first }, output)
  assert.deepEqual(output.headers, {
    "X-Expected": "value",
    "x-opencode-session": first,
    "x-opencode-session-id": first,
  })
})
