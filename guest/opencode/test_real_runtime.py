#!/usr/bin/env python3
"""Exercise the *real pinned* OpenCode CLI against a loopback Responses mock.

Run with: nix develop -c python3 guest/opencode/test_real_runtime.py /nix/store/.../bin/opencode
The test fails unless the real CLI makes an authenticated /v1/responses call
and consumes a complete SSE text response. Never needs a live LiteLLM gateway.
"""
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
EXPECTED = "AGINCTUS_MOCK_RESPONSE_OK"


class Handler(http.server.BaseHTTPRequestHandler):
    calls = []

    def do_POST(self):
        payload = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        try:
            body = json.loads(payload)
        except (ValueError, UnicodeDecodeError):
            self.send_error(400)
            return
        type(self).calls.append((self.path, self.headers.get("Authorization"), body.get("model")))
        if self.path != "/v1/responses" or self.headers.get("Authorization") != "Bearer integration-only-key":
            self.send_error(403)
            return
        response = {
            "id": "resp_mock", "object": "response", "created_at": 1730000000,
            "model": "test-model", "status": "completed", "parallel_tool_calls": False,
            "output": [{"id": "msg_mock", "type": "message", "status": "completed",
                        "role": "assistant", "content": [{"type": "output_text",
                        "text": EXPECTED, "annotations": []}]}],
            "usage": {"input_tokens": 10, "output_tokens": 8, "total_tokens": 18},
        }
        events = [
            ("response.created", {**response, "status": "in_progress", "output": []}),
            ("response.output_item.added", {"output_index": 0, "item": {
                "id": "msg_mock", "type": "message", "role": "assistant", "status": "in_progress", "content": []}}),
            ("response.content_part.added", {"item_id": "msg_mock", "output_index": 0, "content_index": 0,
                                              "part": {"type": "output_text", "text": "", "annotations": []}}),
            ("response.output_text.delta", {"item_id": "msg_mock", "output_index": 0, "content_index": 0,
                                            "delta": EXPECTED}),
            ("response.output_text.done", {"item_id": "msg_mock", "output_index": 0, "content_index": 0,
                                           "text": EXPECTED}),
            ("response.content_part.done", {"item_id": "msg_mock", "output_index": 0, "content_index": 0,
                                            "part": {"type": "output_text", "text": EXPECTED, "annotations": []}}),
            ("response.output_item.done", {"output_index": 0, "item": response["output"][0]}),
            ("response.completed", response),
        ]
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        for name, value in events:
            self.wfile.write(("event: " + name + "\ndata: " +
                             json.dumps({"type": name, **({"response": value} if name.startswith("response.created") or name == "response.completed" else value)}) +
                             "\n\n").encode())
            self.wfile.flush()
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()

    def log_message(self, *args):
        pass


def main(binary):
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        state = root / "gateway"
        state.mkdir()
        (state / "workload-key").write_text("integration-only-key\n")
        (state / "workload-key").chmod(0o600)
        Handler.calls = []
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            (state / "gateway.json").write_text(json.dumps({
                "endpoint": f"http://127.0.0.1:{server.server_port}/v1",
                "model": "test-model", "protocol": "responses",
            }))
            env = dict(os.environ, AGINCTUS_OPENCODE_DIR=str(state),
                       HOME=str(root), XDG_CONFIG_HOME=str(root / "config"),
                       XDG_DATA_HOME=str(root / "data"), XDG_CACHE_HOME=str(root / "cache"),
                       XDG_STATE_HOME=str(root / "state"),
                       OPENCODE_DISABLE_AUTOUPDATE="true")
            result = subprocess.run(
                [sys.executable, str(LAUNCHER), binary, "run", "--model", "aginctus/test-model",
                 EXPECTED + " (repeat this marker verbatim)"],
                cwd=root, env=env, capture_output=True, text=True, timeout=90)
            assert Handler.calls, "real OpenCode did not reach the mock gateway"
            assert all(p == "/v1/responses" and auth == "Bearer integration-only-key"
                       and model == "test-model" for p, auth, model in Handler.calls), (
                           "unexpected endpoint, authentication or model selection")
            assert result.returncode == 0, (
                "OpenCode exited unsuccessfully (stdout/stderr suppressed to avoid credential leaks)")
            assert EXPECTED in result.stdout, "OpenCode did not consume the SSE response"
            print("PASS: real OpenCode received authenticated streaming Responses output")
        finally:
            server.shutdown()
            server.server_close()


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("Usage: test_real_runtime.py /absolute/path/to/pinned/opencode")
    main(sys.argv[1])
