"""Disposable real LiteLLM v1.103.0 Responses -> local mock-Go transport audit.

Run inside the pinned OCI image with --network none and an explicit opt-in.
No provider credentials, external network, database or live gateway.
"""
import argparse
import hashlib
import http.server
import importlib
import importlib.metadata
import json
import os
import socket
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path
from typing import ClassVar
from urllib.error import HTTPError, URLError
from urllib.request import ProxyHandler, Request, build_opener

ALIAS = "aginctus-go-pilot"
OTHER = "aginctus-unrelated"
PROXY_KEY = "sk-synthetic-proxy-only"
UPSTREAM_KEY = "sk-synthetic-upstream-only"
SESSION_A = "ses_0123456789abcdefghijklmno"
SESSION_B = "ses_0123456789abcdefghijklmnp"
MARKER = "AGINCTUS_SYNTHETIC_RESPONSES_OK"
OPENER = build_opener(ProxyHandler({}))  # Never inherit host proxy settings.
SENTINELS = {
    "x-unapproved-probe": "synthetic-unapproved",
    "x-litellm-probe": "synthetic-litellm",
    "x-api-key": "synthetic-client-key",
    "anthropic-beta": "synthetic-beta",
}


def verify_isolation():
    if os.environ.get("AGINCTUS_ISOLATED_LITELLM_PROBE") != "1":
        raise RuntimeError("explicit isolated-test opt-in absent")
    if set(os.listdir("/sys/class/net")) != {"lo"}:
        raise RuntimeError("network isolation absent: expected loopback only")
    if not importlib.metadata.version("litellm").startswith("1.103.0"):
        raise RuntimeError("expected pinned LiteLLM v1.103.0")


def verify_policy(policy):
    expected = {"model_list": [], "litellm_settings": {"model_group_settings": {
        "forward_client_headers_to_llm_api": [ALIAS]}}}
    if policy != expected:
        raise RuntimeError("stock policy is not the reviewed one-alias fixture")


def render_config(policy, upstream_port, *, selective_hook=False):
    verify_policy(policy)
    return {
        "model_list": [{
            "model_name": name,
            "litellm_params": {
                "model": "openai/gpt-4o-mini",
                "api_base": "http://127.0.0.1:" + str(upstream_port) + "/v1",
                "api_key": UPSTREAM_KEY,
            },
        } for name in (ALIAS, OTHER)],
        "litellm_settings": (
            {"callbacks": ["litellm_opencode_go_hook.proxy_handler_instance"]}
            if selective_hook else policy["litellm_settings"]
        ),
        "general_settings": {"master_key": PROXY_KEY},
    }


def load_packaged_hook(bundle):
    """Verify exported bytes and callback-only policy before importing anything."""
    bundle = Path(bundle).resolve(strict=True)
    names = {"config.yaml", "litellm_opencode_go_hook.py", "opencode_go_forwarding.py"}
    manifest = json.loads((bundle / "manifest.json").read_text(encoding="utf-8"))
    if not isinstance(manifest, dict) or set(manifest) != names:
        raise RuntimeError("packaged hook manifest must name exactly three assets")
    for name in sorted(names):
        if hashlib.sha256((bundle / name).read_bytes()).hexdigest() != manifest[name]:
            raise RuntimeError("packaged hook digest mismatch: " + name)
    import yaml

    policy = yaml.safe_load((bundle / "config.yaml").read_text(encoding="utf-8"))
    verify_packaged_policy(policy)
    sys.path.insert(0, str(bundle))
    for name in ("opencode_go_forwarding", "litellm_opencode_go_hook"):
        module = importlib.import_module(name)
        if Path(module.__file__).resolve() != bundle / (name + ".py"):
            raise RuntimeError("callback imported outside packaged bundle")
    verify_selective_hook_import()
    return policy


def verify_packaged_policy(policy):
    if policy != {"litellm_settings": {
        "callbacks": ["litellm_opencode_go_hook.proxy_handler_instance"],
    }}:
        raise RuntimeError("packaged hook policy must be callback-only")


def verify_packaged_startup_failures(bundle, config_content):
    """Exercise missing deliveries with the actual pinned proxy executable."""
    bundle = Path(bundle)
    for missing in ("config.yaml", "litellm_opencode_go_hook.py", "opencode_go_forwarding.py"):
        with tempfile.TemporaryDirectory() as tmp:
            for name in ("config.yaml", "litellm_opencode_go_hook.py", "opencode_go_forwarding.py"):
                if name != missing:
                    (Path(tmp) / name).write_bytes(
                        config_content if name == "config.yaml" else (bundle / name).read_bytes())
            try:
                result = subprocess.run([
                    "/app/.venv/bin/litellm", "--config", str(Path(tmp) / "config.yaml"),
                    "--host", "127.0.0.1", "--port", str(free_port()),
                ], env={**os.environ, "PYTHONPATH": tmp}, cwd=tmp,
                    stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL, timeout=30, check=False)
            except subprocess.TimeoutExpired:
                raise RuntimeError("missing packaged asset did not stop startup: " + missing) from None
            if result.returncode == 0:
                raise RuntimeError("missing packaged asset exited successfully: " + missing)
    print("PASS: actual pinned proxy fails startup for missing config or either callback module")


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def response_events():
    complete = {
        "id": "resp_mock", "object": "response", "created_at": 1730000000,
        "model": "gpt-4o-mini", "status": "completed", "parallel_tool_calls": False,
        "output": [{
            "id": "msg_mock", "type": "message", "status": "completed",
            "role": "assistant", "content": [{
                "type": "output_text", "text": MARKER, "annotations": [],
            }],
        }],
        "usage": {"input_tokens": 10, "output_tokens": 8, "total_tokens": 18},
    }
    item = complete["output"][0]
    part = item["content"][0]
    events = [
        ("response.created", {"response": {**complete, "status": "in_progress", "output": []}}),
        ("response.output_item.added", {"output_index": 0, "item": {
            **item, "status": "in_progress", "content": [],
        }}),
        ("response.content_part.added", {
            "output_index": 0, "content_index": 0, "item_id": "msg_mock",
            "part": {"type": "output_text", "text": "", "annotations": []},
        }),
        ("response.output_text.delta", {
            "output_index": 0, "content_index": 0, "item_id": "msg_mock", "delta": MARKER,
        }),
        ("response.output_text.done", {
            "output_index": 0, "content_index": 0, "item_id": "msg_mock", "text": MARKER,
        }),
        ("response.content_part.done", {
            "output_index": 0, "content_index": 0, "item_id": "msg_mock", "part": part,
        }),
        ("response.output_item.done", {"output_index": 0, "item": item}),
        ("response.completed", {"response": complete}),
    ]
    return "".join(
        "event: " + kind + "\ndata: " + json.dumps({"type": kind, **body}) + "\n\n"
        for kind, body in events
    ).encode() + b"data: [DONE]\n\n"


class MockGo(http.server.BaseHTTPRequestHandler):
    calls: ClassVar[list] = []
    lock: ClassVar[threading.Lock] = threading.Lock()

    def do_POST(self):
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length > 65536:
                self.send_error(413)
                return
            request = json.loads(self.rfile.read(length))
        except (ValueError, UnicodeDecodeError):
            self.send_error(400)
            return
        with self.lock:
            self.calls.append({
                "path": self.path,
                "headers": {k.lower(): v for k, v in self.headers.items()},
                "model": request.get("model"),
            })
        if (self.path != "/v1/responses"
                or self.headers.get("Authorization") != "Bearer " + UPSTREAM_KEY):
            self.send_error(403)
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        self.wfile.write(response_events())

    def log_message(self, *_args):
        pass


def send(port, alias, session, authenticated=True, *, user_agent="opencode/1.15.11"):
    headers = {
        "Content-Type": "application/json",
        "User-Agent": user_agent,
        **(SENTINELS if authenticated else {}),
    }
    if session is not None:
        headers["x-opencode-session"] = session
    # x-api-key is an alternative authentication mechanism in pinned LiteLLM.
    # An anonymous negative control must NOT include a second auth header;
    # the authenticated cases separately audit the unapproved x-api-key value.
    if authenticated:
        headers["Authorization"] = "Bearer " + PROXY_KEY
    req = Request(
        "http://127.0.0.1:" + str(port) + "/v1/responses",
        data=json.dumps({
            "model": alias, "input": "Synthetic local transport check",
            "stream": True,
        }).encode(),
        headers=headers,
    )
    try:
        with OPENER.open(req, timeout=30) as res:
            body = res.read(262145)
            if len(body) > 262144:
                raise RuntimeError("proxy response exceeded byte bound")
            return res.status, body
    except HTTPError as error:
        error.close()  # Never print proxy error body.
        return error.code, b""


def wait_ready(port, process):
    url = "http://127.0.0.1:" + str(port) + "/health/liveliness"
    for _ in range(100):
        if process.poll() is not None:
            raise RuntimeError("isolated LiteLLM startup failed")
        try:
            with OPENER.open(url, timeout=1) as response:
                if response.status == 200:
                    return
        except (URLError, HTTPError, TimeoutError):
            pass
        time.sleep(0.5)
    raise RuntimeError("isolated LiteLLM readiness timed out")


def evaluate(calls, *, report=True):
    """Only report predetermined classifications, never raw header values."""

    def emit(message):
        if report:
            print(message)
    if len(calls) != 4 or any(c["path"] != "/v1/responses" for c in calls):
        raise RuntimeError("expected four native provider-bound Responses calls")
    if any(c["headers"].get("authorization") != "Bearer " + UPSTREAM_KEY for c in calls):
        raise RuntimeError("upstream authorization did not use isolated synthetic key")
    upstream_sessions = [c["headers"].get("x-opencode-session") for c in calls]
    if upstream_sessions[:3] != [SESSION_A, SESSION_A, SESSION_B]:
        raise RuntimeError("Go session missing, rewritten, or cross-contaminated")
    if upstream_sessions[3] is not None:
        raise RuntimeError("non-Go model received client Go session")
    if any(calls[3]["headers"].get(header) == marker
           for header, marker in SENTINELS.items()):
        raise RuntimeError("non-Go model forwarded client metadata")
    emit("PASS: actual pinned proxy sent native Responses to local fake upstream")
    emit("PASS: session unchanged for repeat and distinct between conversations")
    emit("PASS: non-Go model did not receive the client Go session")
    risk = False
    for header, marker in SENTINELS.items():
        copied = any(c["headers"].get(header) == marker for c in calls[:3])
        emit(("RISK: " if copied else "PASS: ") + header
              + (" forwarded" if copied else " blocked"))
        risk = risk or copied
    ua = all(c["headers"].get("user-agent") == "opencode/1.15.11"
             for c in calls[:3])
    emit(("PASS: " if ua else "RISK: ") + "original OpenCode User-Agent "
          + ("preserved" if ua else "not preserved"))
    return not risk and ua


def require_unauthenticated_rejection(status, upstream_calls):
    # A 400/404/500 must not be misreported as proof of authentication.
    # Only an actual 401/403 and zero upstream calls establish this gate.
    if status not in (401, 403) or upstream_calls != 0:
        raise RuntimeError(
            "anonymous request authentication gate failed "
            f"(http_status={status}, upstream_calls={upstream_calls})"
        )


def verify_selective_hook_import():
    # No Python stubs or substitutes: confirm the actual LiteLLM CustomLogger
    # instance and its two mounted production callback modules can be imported.
    from litellm.integrations.custom_logger import CustomLogger

    hook = importlib.import_module("litellm_opencode_go_hook")
    if not isinstance(hook.proxy_handler_instance, CustomLogger):
        raise RuntimeError("selective callback is not a pinned LiteLLM CustomLogger")


def check_rejected_without_upstream(port, session, *, user_agent):
    before = len(MockGo.calls)
    status, _ = send(port, ALIAS, session, user_agent=user_agent)
    after = len(MockGo.calls)
    # Do not infer rejection from a transport failure or a normal 200.
    if status not in (400, 401, 403, 422) or after != before:
        raise RuntimeError(
            "selective hook failed closed negative case "
            f"(http_status={status}, upstream_delta={after - before})"
        )


def run(fixture, require_safe, *, selective_hook=False, packaged_hook=None):
    verify_isolation()
    packaged_policy = load_packaged_hook(packaged_hook) if packaged_hook else None
    selective_hook = selective_hook or packaged_hook is not None
    if selective_hook:
        verify_selective_hook_import()
    import yaml  # pinned image dependency; deliberately optional in dev-shell tests

    policy = yaml.safe_load(Path(fixture).read_text(encoding="utf-8"))
    verify_policy(policy)
    MockGo.calls = []
    fake = http.server.ThreadingHTTPServer(("127.0.0.1", 0), MockGo)
    threading.Thread(target=fake.serve_forever, daemon=True).start()
    proxy = None
    try:
        with tempfile.TemporaryDirectory() as tmp:
            config = Path(tmp) / "test-only.yaml"
            rendered = render_config(policy, fake.server_port, selective_hook=selective_hook)
            if packaged_policy is not None:
                rendered["litellm_settings"] = packaged_policy["litellm_settings"]
            config.write_text(yaml.safe_dump(rendered), encoding="utf-8")
            if packaged_hook:
                verify_packaged_startup_failures(packaged_hook, config.read_bytes())
            port = free_port()
            proxy = subprocess.Popen([
                "/app/.venv/bin/litellm", "--config", str(config),
                "--host", "127.0.0.1", "--port", str(port),
            ], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL, env={
                    **os.environ,
                    **({"PYTHONPATH": str(Path(packaged_hook).resolve())}
                       if packaged_hook else {}),
                })
            wait_ready(port, proxy)
            status, _ = send(port, ALIAS, SESSION_A, authenticated=False)
            require_unauthenticated_rejection(status, len(MockGo.calls))
            print("PASS: anonymous request rejected without contacting upstream")
            for alias, session in [(ALIAS, SESSION_A), (ALIAS, SESSION_A),
                                   (ALIAS, SESSION_B), (OTHER, SESSION_A)]:
                status, body = send(port, alias, session)
                if status != 200 or b"response.completed" not in body:
                    raise RuntimeError("proxy did not return terminal Responses SSE")
            safe = evaluate(MockGo.calls)
            if selective_hook:
                check_rejected_without_upstream(
                    port, "invalid-session", user_agent="opencode/1.15.11")
                check_rejected_without_upstream(
                    port, None, user_agent="opencode/1.15.11")
                check_rejected_without_upstream(
                    port, SESSION_A, user_agent="python-httpx/0.28")
                print("PASS: selective hook rejects malformed/missing session and spoofed User-Agent")
            if not safe and require_safe:
                raise RuntimeError("stock policy does not satisfy strict upstream isolation")
            print("DECISION: " + ("strict policy passes" if safe else
                                 "stock forwarding has risks; evaluate selective callback"))
    finally:
        if proxy is not None:
            proxy.terminate()
            try:
                proxy.wait(timeout=8)
            except subprocess.TimeoutExpired:
                proxy.kill()
                proxy.wait(timeout=8)
        fake.shutdown()
        fake.server_close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("fixture")
    parser.add_argument("--require-safe", action="store_true")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--selective-hook", action="store_true",
                        help="enable installed #470 callback instead of broad group forwarding")
    mode.add_argument("--packaged-hook", metavar="BUNDLE_DIR",
                      help="verify and use exported Go bundle policy and modules")
    args = parser.parse_args()
    try:
        run(args.fixture, args.require_safe or args.selective_hook or bool(args.packaged_hook),
            selective_hook=args.selective_hook, packaged_hook=args.packaged_hook)
    except (RuntimeError, OSError, ValueError, KeyError, ImportError) as error:
        print("FAIL: " + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
