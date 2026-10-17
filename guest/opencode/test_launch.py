#!/usr/bin/env python3
"""Isolated end-to-end contract tests using a synthetic OpenAI-compatible gateway."""
import http.server
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest

LAUNCHER = Path(__file__).with_name("launch.py")


class Gateway(http.server.BaseHTTPRequestHandler):
    calls = []

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.calls.append((self.path, self.headers.get("Authorization"), body))
        payload = {"id": "resp_mock", "object": "response", "status": "completed",
                   "output": [{"type": "function_call", "name": "shell",
                               "call_id": "call_1", "arguments": "{}"}]}
        result = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(result)))
        self.end_headers()
        self.wfile.write(result)

    def log_message(self, *args):
        pass


MOCK_CLIENT = """#!/usr/bin/env python3
import json, os, urllib.request
cfg = json.loads(os.environ['OPENCODE_CONFIG_CONTENT'])
assert cfg['model'] == 'aginctus/test-model'
p = cfg['provider']['aginctus']
assert p['npm'] == '@ai-sdk/openai'
assert p['options']['apiKey'] == '{env:AGINCTUS_LITELLM_KEY}'
request = urllib.request.Request(
    p['options']['baseURL'] + '/responses',
    data=json.dumps({'model': 'test-model', 'input': 'hello'}).encode(),
    headers={'Authorization': 'Bearer ' + os.environ['AGINCTUS_LITELLM_KEY'],
             'Content-Type': 'application/json'},
)
response = json.load(urllib.request.urlopen(request))
assert response['status'] == 'completed'
assert response['output'][0]['type'] == 'function_call'
"""


class LauncherTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.client = self.directory / "fake-opencode"
        self.client.write_text(MOCK_CLIENT)
        self.client.chmod(0o700)
        self.key = self.directory / "workload-key"
        self.key.write_text("shared-test-key\n")
        self.key.chmod(0o600)
        self.settings = self.directory / "gateway.json"
        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Gateway)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)
        Gateway.calls.clear()
        self.configure()

    def configure(self, **kwargs):
        values = {"endpoint": f"http://127.0.0.1:{self.server.server_port}/v1",
                  "model": "test-model", "protocol": "responses"}
        values.update(kwargs)
        self.settings.write_text(json.dumps(values))

    def launch(self):
        env = dict(os.environ, AGINCTUS_OPENCODE_DIR=str(self.directory))
        return subprocess.run([sys.executable, str(LAUNCHER), str(self.client)],
                              env=env, capture_output=True, text=True)

    def test_authenticated_responses_and_tool_output(self):
        result = self.launch()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(Gateway.calls, [("/v1/responses", "Bearer shared-test-key",
                                         {"model": "test-model", "input": "hello"})])

    def test_fail_closed_missing_key(self):
        self.key.unlink()
        self.assertNotEqual(self.launch().returncode, 0)
        self.assertFalse(Gateway.calls)

    def test_fail_closed_unsafe_key_permissions(self):
        self.key.chmod(0o644)
        self.assertNotEqual(self.launch().returncode, 0)
        self.assertFalse(Gateway.calls)

    def test_fail_closed_invalid_endpoint(self):
        self.configure(endpoint="https://example.com/v1")
        self.assertNotEqual(self.launch().returncode, 0)
        self.assertFalse(Gateway.calls)

    def test_fail_closed_invalid_protocol(self):
        self.configure(protocol="untrusted")
        self.assertNotEqual(self.launch().returncode, 0)
        self.assertFalse(Gateway.calls)

    def test_fail_closed_missing_model(self):
        self.configure(model="")
        self.assertNotEqual(self.launch().returncode, 0)
        self.assertFalse(Gateway.calls)

    def test_secret_not_logged(self):
        self.key.chmod(0o644)
        result = self.launch()
        self.assertNotIn("shared-test-key", result.stderr)


if __name__ == "__main__":
    unittest.main()
