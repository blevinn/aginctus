"""Synthetic Responses upstream for disposable NixOS host E2E tests ONLY.

Standard-library HTTP server. Never sends requests to external providers.
The *real* LiteLLM is tested separately; this server must not be substituted
for the LiteLLM gateway or counted as proof that OpenCode ran tools.
"""
from __future__ import annotations

import argparse
import ipaddress
import json
import re
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MAX_BODY = 16384
TOKEN = "synthetic-fixture-only"
NONCE = re.compile(r"\bnonce=([a-zA-Z0-9_-]{8,64})\b")


def parse_request(raw: bytes, authorization: str) -> tuple[str, bool]:
    if authorization != "Bearer " + TOKEN:
        raise PermissionError("synthetic upstream authentication denied")
    if len(raw) > MAX_BODY:
        raise ValueError("oversized synthetic request")
    payload = json.loads(raw)
    if not isinstance(payload, dict) or payload.get("model") not in (
        "synthetic-test", "openai/synthetic-test"
    ):
        raise ValueError("unsupported synthetic upstream model")
    prompt = payload.get("input")
    if isinstance(prompt, list):
        # OpenAI Responses clients may use typed message content.
        prompt = json.dumps(prompt, separators=(",", ":"), ensure_ascii=True)
    if not isinstance(prompt, str) or len(prompt) > MAX_BODY:
        raise ValueError("invalid synthetic prompt")
    match = NONCE.search(prompt)
    if not match:
        raise ValueError("missing synthetic run nonce")
    return match.group(1), payload.get("stream") is True


def response_document(nonce: str) -> dict:
    value = "synthetic-confirmed:" + nonce
    item = {
        "id": "msg_synthetic",
        "type": "message",
        "status": "completed",
        "role": "assistant",
        "content": [{"type": "output_text", "text": value, "annotations": []}],
    }
    return {
        "id": "resp_synthetic",
        "object": "response",
        "created_at": int(time.time()),
        "status": "completed",
        "model": "synthetic-test",
        "output": [item],
        "usage": {"input_tokens": 12, "output_tokens": 8, "total_tokens": 20},
    }


def sse(nonce: str) -> bytes:
    doc = response_document(nonce)
    events = [
        ("response.created", {"type": "response.created",
                              "response": {**doc, "status": "in_progress", "output": []}}),
        ("response.output_item.done", {"type": "response.output_item.done",
                                      "item": doc["output"][0], "output_index": 0}),
        ("response.completed", {"type": "response.completed", "response": doc}),
    ]
    output = b""
    for name, event in events:
        output += ("event: " + name + "\ndata: " + json.dumps(
            event, separators=(",", ":")) + "\n\n").encode()
    return output + b"data: [DONE]\n\n"


class SyntheticServer(ThreadingHTTPServer):
    daemon_threads = True
    allow_reuse_address = True

    def __init__(self, address):
        super().__init__(address, Handler)
        self._lock = threading.Lock()
        self._observed: dict[str, int] = {}

    def record(self, nonce: str):
        with self._lock:
            self._observed[nonce] = self._observed.get(nonce, 0) + 1

    def count(self, nonce: str) -> int:
        with self._lock:
            return self._observed.get(nonce, 0)


class Handler(BaseHTTPRequestHandler):
    server: SyntheticServer

    def log_message(self, _format, *_args):
        # Never log headers, tokens, provider payloads or prompt text.
        return

    def _send(self, status: int, body: bytes, mime="application/json"):
        self.send_response(status)
        self.send_header("Content-Type", mime)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            return self._send(200, b'{"status":"synthetic-ready"}')
        if self.path.startswith("/_observed/"):
            # This route only exposes an aggregate count for a nonce; it is
            # upstream fixture evidence, NOT independent gateway audit.
            nonce = self.path.removeprefix("/_observed/")
            if not re.fullmatch(r"[a-zA-Z0-9_-]{8,64}", nonce):
                return self._send(404, b"{}")
            if self.headers.get("Authorization") != "Bearer " + TOKEN:
                return self._send(403, b"{}")
            return self._send(200, json.dumps({"count": self.server.count(nonce)}).encode())
        return self._send(404, b"{}")

    def do_POST(self):
        if self.path not in ("/v1/responses", "/responses"):
            return self._send(404, b"{}")
        if self.headers.get("Content-Length", "").isdigit():
            size = int(self.headers["Content-Length"])
        else:
            return self._send(400, b"{}")
        if size <= 0 or size > MAX_BODY:
            return self._send(413, b"{}")
        try:
            nonce, streaming = parse_request(
                self.rfile.read(size), self.headers.get("Authorization", ""))
        except (ValueError, TypeError, UnicodeDecodeError, json.JSONDecodeError):
            return self._send(400, b"{}")
        except PermissionError:
            return self._send(403, b"{}")
        self.server.record(nonce)
        if streaming:
            return self._send(200, sse(nonce), "text/event-stream")
        return self._send(200, json.dumps(response_document(nonce)).encode())


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--bind", required=True)
    parser.add_argument("--port", type=int, default=9080)
    args = parser.parse_args()
    address = ipaddress.ip_address(args.bind)
    if address.version != 4 or not (address.is_private or address.is_loopback):
        parser.error("synthetic listener must bind only to private IPv4")
    if not 1024 <= args.port <= 65535:
        parser.error("invalid synthetic port")
    server = SyntheticServer((str(address), args.port))
    try:
        server.serve_forever()
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
