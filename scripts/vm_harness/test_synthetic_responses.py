"""Hostless synthetic HTTP/Responses contract tests; no real gateway/provider."""
import json
import threading
import unittest
from urllib.error import HTTPError
from urllib.request import ProxyHandler, Request, build_opener

from scripts.vm_harness.synthetic_responses import (
    TOKEN,
    SyntheticServer,
    parse_request,
    sse,
)

NONCE = "vmfixture_123456789abc"
OPENER = build_opener(ProxyHandler({}))


class SyntheticResponsesTests(unittest.TestCase):
    def test_requires_synthetic_key_model_and_nonce(self):
        body = json.dumps({"model": "synthetic-test", "input": "test nonce=" + NONCE}).encode()
        self.assertEqual(parse_request(body, "Bearer " + TOKEN), (NONCE, False))
        for key in ("", "Bearer secret", "Bearer " + TOKEN + "-wrong"):
            with self.subTest(key=key), self.assertRaises(PermissionError):
                parse_request(body, key)
        with self.assertRaises(ValueError):
            parse_request(json.dumps({"model": "real-model", "input": "nonce=" + NONCE}).encode(), "Bearer " + TOKEN)
        with self.assertRaises(ValueError):
            parse_request(json.dumps({"model": "synthetic-test", "input": "ordinary"}).encode(), "Bearer " + TOKEN)

    def test_sse_has_completion_and_no_implicit_tool_execution(self):
        output = sse(NONCE).decode()
        self.assertIn("event: response.created", output)
        self.assertIn("event: response.output_item.done", output)
        self.assertIn("event: response.completed", output)
        self.assertIn('"status":"completed"', output)
        self.assertIn("synthetic-confirmed:" + NONCE, output)
        self.assertTrue(output.endswith("data: [DONE]\n\n"))
        self.assertNotIn('"function_call"', output)

    def test_http_request_auth_and_upstream_observation(self):
        server = SyntheticServer(("127.0.0.1", 0))
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        host = "http://127.0.0.1:" + str(server.server_address[1])
        try:
            with OPENER.open(host + "/health", timeout=3) as reply:
                self.assertEqual(json.load(reply), {"status": "synthetic-ready"})
            body = json.dumps({
                "model": "synthetic-test",
                "input": "hello nonce=" + NONCE,
                "stream": True,
            }).encode()
            request = Request(host + "/v1/responses", body,
                              headers={"Content-Type": "application/json",
                                       "Authorization": "Bearer " + TOKEN})
            with OPENER.open(request, timeout=3) as reply:
                self.assertEqual(reply.status, 200)
                self.assertIn(b"response.completed", reply.read())
            request = Request(host + "/_observed/" + NONCE,
                              headers={"Authorization": "Bearer " + TOKEN})
            with OPENER.open(request, timeout=3) as reply:
                self.assertEqual(json.load(reply), {"count": 1})
            with self.assertRaises(HTTPError) as cm:
                OPENER.open(host + "/_observed/" + NONCE, timeout=3)
            self.assertEqual(cm.exception.code, 403)
            with self.assertRaises(HTTPError) as cm:
                OPENER.open(Request(host + "/v1/responses", body), timeout=3)
            self.assertEqual(cm.exception.code, 403)
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=3)


if __name__ == "__main__":
    unittest.main()
