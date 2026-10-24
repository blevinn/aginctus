"""Loopback provider-bound HTTP tests: no LiteLLM, provider or Incus required."""
import http.server
import importlib.util
import json
import threading
import unittest
from pathlib import Path
from urllib.request import ProxyHandler, Request, build_opener

SPEC = importlib.util.spec_from_file_location(
    "go_forwarding", Path(__file__).with_name("opencode_go_forwarding.py"))
policy = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(policy)

A = "ses_0000000000abcdefgh"
B = "ses_1111111111abcdefgh"


class Receiver(http.server.BaseHTTPRequestHandler):
    headers_seen = []

    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", "0")))
        self.headers_seen.append({name.lower(): value for name, value in self.headers.items()})
        body = json.dumps({"status": "completed"}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


class MockGoTransportTests(unittest.TestCase):
    def setUp(self):
        Receiver.headers_seen = []
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Receiver)
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{server.server_port}/v1/responses"
        self.opener = build_opener(ProxyHandler({}))

    def send(self, session, model=policy.GO_ALIAS):
        data = {
            "model": model,
            "proxy_server_request": {"headers": {
                "x-opencode-session-id": session,
                "User-Agent": "opencode/1.0.0",
                "Authorization": "Bearer workload-never-forward",
                "x-api-key": "workload-secret-never-forward",
                "x-litellm-api-key": "proxy-credential-never-forward",
                "x-attack-header": "evil-never-forward",
            }},
            "extra_headers": {"X-Leak": "body-injected"},
            "headers": {"X-Leak-Again": "previously-forwarded"},
        }
        output = policy.sanitize_go_headers(data, "aresponses")
        assert model == policy.GO_ALIAS
        self.assertNotIn("extra_headers", output)
        headers = {**output["headers"], "Authorization": "Bearer synthetic-upstream-only",
                   "Content-Type": "application/json"}
        req = Request(self.url, data=b'{"stream":false}', headers=headers)
        with self.opener.open(req, timeout=5) as response:
            self.assertEqual(response.status, 200)

    def test_provider_http_receives_only_session_ua_and_distinct_provider_auth(self):
        for session in (A, A, B):
            self.send(session)
        observed = Receiver.headers_seen
        self.assertEqual(len(observed), 3)
        self.assertEqual([o["x-opencode-session"] for o in observed], [A, A, B])
        for record in observed:
            self.assertEqual(record["user-agent"], "opencode/1.0.0")
            self.assertEqual(record["authorization"], "Bearer synthetic-upstream-only")
            self.assertFalse(any(name in record for name in (
                "x-api-key", "x-litellm-api-key", "x-attack-header",
                "x-leak", "x-leak-again")))

    def test_invalid_identity_sends_no_network_request(self):
        for session in ("", "bad", "ses_" + "x" * 130):
            with self.assertRaises(ValueError):
                self.send(session)
        self.assertEqual(Receiver.headers_seen, [])


if __name__ == "__main__":
    unittest.main()
