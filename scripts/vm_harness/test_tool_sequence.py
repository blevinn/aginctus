"""Synthetic protocol exercise; function outputs here are NOT execution evidence."""
import json
import threading
import unittest
from urllib.error import HTTPError
from urllib.request import ProxyHandler, Request, build_opener

from scripts.vm_harness.synthetic_responses import TOKEN, SyntheticServer
from scripts.vm_harness.tool_sequence import (
    SequenceError,
    ToolSequence,
    stream,
    tool_arguments,
)

NONCE = "fixture_abc123XYZ"
TOOLS = [
    {"type": "function", "name": "read",
     "parameters": {"type": "object", "properties": {"filePath": {"type": "string"}}}},
    {"type": "function", "name": "edit",
     "parameters": {"type": "object", "properties": {
         "filePath": {"type": "string"}, "oldString": {"type": "string"},
          "newString": {"type": "string"}}}},
    {"type": "function", "name": "bash",
      "parameters": {"type": "object", "properties": {"command": {"type": "string"}}}},
]
OPENER = build_opener(ProxyHandler({}))


def initial():
    return {"model": "synthetic-test", "stream": True,
            "input": "Fix the Go fixture: nonce=" + NONCE, "tools": TOOLS}


def continuation(previous_step):
    return {"model": "synthetic-test", "stream": True,
            "previous_response_id": f"resp_{NONCE}_{previous_step}",
            "input": [{
                "type": "function_call_output",
                "call_id": f"call_{NONCE}_{previous_step}",
                "output": "synthetic tool result, not independently verified",
            }]}


def inline_continuation(previous_step):
    call_id = f"call_{NONCE}_{previous_step}"
    return {
        "model": "synthetic-test", "stream": True, "tools": TOOLS,
        "input": [
            {"type": "message", "role": "user", "content": [
                {"type": "input_text", "text": "Fix the Go fixture: nonce=" + NONCE}
            ]},
            {
                "type": "function_call", "id": f"fc_{NONCE}_{previous_step}",
                "call_id": call_id,
                "name": ("read", "edit", "bash")[previous_step],
                "arguments": json.dumps(
                    tool_arguments(previous_step, "/workspace", NONCE), separators=(",", ":")
                ),
            },
            {
                "type": "function_call_output", "call_id": call_id,
                "output": "synthetic tool result, not independently verified",
            },
        ],
    }


class ToolSequenceTests(unittest.TestCase):
    def test_exact_order_of_scripted_tool_calls(self):
        seq = ToolSequence()
        payloads = [initial()] + [continuation(i) for i in range(3)]
        for step, request in enumerate(payloads):
            nonce, doc = seq.next(request)
            self.assertEqual(nonce, NONCE)
            self.assertEqual(doc["id"], f"resp_{NONCE}_{step}")
            self.assertIn(b"event: response.completed", stream(doc))
            item = doc["output"][0]
            if step < 3:
                self.assertEqual(item["name"], ("read", "edit", "bash")[step])
                self.assertEqual(item["call_id"], f"call_{NONCE}_{step}")
                arguments = json.loads(item["arguments"])
                self.assertTrue(all(value != "/pilot" for value in arguments.values()))
            else:
                self.assertEqual(item["type"], "message")
                self.assertIn(NONCE, item["content"][0]["text"])
        with self.assertRaises(SequenceError):
            seq.next(continuation(2))

    def test_pinned_client_inline_function_call_continuations(self):
        seq = ToolSequence()
        seq.next(initial())
        for previous_step in range(3):
            nonce, response = seq.next(inline_continuation(previous_step))
            self.assertEqual(nonce, NONCE)
            self.assertEqual(response["id"], f"resp_{NONCE}_{previous_step + 1}")
        self.assertEqual(seq._steps[NONCE], 3)

    def test_rejected_toolless_probe_does_not_reserve_nonce(self):
        seq = ToolSequence()
        with self.assertRaises(SequenceError):
            seq.next({**initial(), "tools": []})
        nonce, response = seq.next(initial())
        self.assertEqual(nonce, NONCE)
        self.assertEqual(response["output"][0]["name"], "read")

    def test_missing_schema_unordered_and_forged_results_fail_closed(self):
        for mutated in (
            {**initial(), "tools": []},
            {**initial(), "tools": TOOLS[:2]},
            {**initial(), "tools": TOOLS + [TOOLS[0]]},
            {**initial(), "input": "no run ID"},
            {**initial(), "stream": False},
        ):
            with self.subTest(mutated=str(mutated)[:35]), self.assertRaises(SequenceError):
                ToolSequence().next(mutated)
        seq = ToolSequence()
        seq.next(initial())
        with self.assertRaises(SequenceError):
            seq.next(continuation(2))
        forged = continuation(0)
        forged["input"][0]["call_id"] = "call_other_0"
        with self.assertRaises(SequenceError):
            seq.next(forged)
        duplicated = continuation(0)
        duplicated["input"].append(duplicated["input"][0])
        with self.assertRaises(SequenceError):
            seq.next(duplicated)
        seq.next(continuation(0))
        with self.assertRaises(SequenceError):
            seq.next(continuation(0))

    def test_http_opt_in_requires_auth_and_streams_tool_item(self):
        server = SyntheticServer(("127.0.0.1", 0), tool_mode=True)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        url = "http://127.0.0.1:" + str(server.server_address[1]) + "/v1/responses"
        try:
            body = json.dumps(initial()).encode()
            with self.assertRaises(HTTPError) as e:
                OPENER.open(Request(url, body), timeout=3)
            self.assertEqual(e.exception.code, 403)
            req = Request(url, body, headers={"Authorization": "Bearer " + TOKEN})
            with OPENER.open(req, timeout=3) as reply:
                raw = reply.read(65536)
                self.assertIn(b'"type":"function_call"', raw)
                self.assertIn(b'"name":"read"', raw)
            with self.assertRaises(HTTPError) as e:
                OPENER.open(req, timeout=3)
            self.assertEqual(e.exception.code, 400)
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=3)


if __name__ == "__main__":
    unittest.main()
