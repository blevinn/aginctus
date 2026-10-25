"""Disposable real LiteLLM v1.103.0 Responses -> local mock-Go transport audit.

Run inside the pinned OCI image with --network none and an explicit opt-in.
No provider credentials, external network or live gateway. Production mode uses
an owned disposable PostgreSQL database inside the same isolated namespace.
"""
import argparse
import contextlib
import hashlib
import http.server
import importlib
import importlib.metadata
import json
import os
import re
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
        "error": None, "incomplete_details": None, "instructions": None,
        "tools": [], "tool_choice": "auto", "temperature": 1, "top_p": 1,
        "usage": {"input_tokens": 10, "output_tokens": 8, "total_tokens": 18,
                  "input_tokens_details": {"cached_tokens": 0},
                  "output_tokens_details": {"reasoning_tokens": 0}},
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
        "event: " + kind + "\ndata: " + json.dumps({
            "type": kind, "sequence_number": sequence, **body}) + "\n\n"
        for sequence, (kind, body) in enumerate(events)
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
        # Incremental delivery exercises streaming instead of a single buffered
        # response, which can outrun the CLI's event subscription at startup.
        for event in response_events().split(b"\n\n"):
            if event:
                self.wfile.write(event + b"\n\n")
                self.wfile.flush()
                time.sleep(0.02)

    def log_message(self, *_args):
        pass


def send(port, alias, session, authenticated=True, *, user_agent="opencode/1.15.11",
         key=PROXY_KEY, extra_body=None, extra_headers=None):
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
        headers["Authorization"] = "Bearer " + key
    headers.update(extra_headers or {})
    req = Request(
        "http://127.0.0.1:" + str(port) + "/v1/responses",
        data=json.dumps({
            "model": alias, "input": "Synthetic local transport check",
            "stream": True,
            **(extra_body or {}),
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


@contextlib.contextmanager
def disposable_postgres(binary_dir, root):
    """No host service, socket, credentials, or data directory is reused."""
    binary_dir = Path(binary_dir).resolve(strict=True)
    if not str(binary_dir).startswith("/nix/store/"):
        raise RuntimeError("PostgreSQL must be an existing Nix store binary")
    data = root / "postgres"
    data.mkdir(mode=0o700)
    identity = {}
    if os.geteuid() == 0:
        # Rootless OCI root may initialize LiteLLM's generated Prisma client;
        # PostgreSQL itself always runs as an unprivileged namespace identity.
        root.chmod(0o755)
        os.chown(data, 65534, 65534)
        identity = {"user": 65534, "group": 65534}
    port = free_port()

    def command(name, *args):
        result = subprocess.run([str(binary_dir / name), *args],
                                stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                                stderr=subprocess.DEVNULL, timeout=60, check=False, **identity)
        if result.returncode:
            raise RuntimeError("disposable PostgreSQL command failed: " + name)

    command("initdb", "-D", str(data), "--no-locale", "--encoding=UTF8",
            "--auth=trust", "--username=litellm")
    process = subprocess.Popen([
        str(binary_dir / "postgres"), "-D", str(data), "-h", "127.0.0.1",
        "-p", str(port), "-k", str(data),
    ], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        **identity)
    try:
        for _ in range(100):
            if process.poll() is not None:
                raise RuntimeError("disposable PostgreSQL startup failed")
            ready = subprocess.run([
                str(binary_dir / "pg_isready"), "-h", "127.0.0.1", "-p", str(port),
            ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False, timeout=5)
            if ready.returncode == 0:
                break
            time.sleep(0.1)
        else:
            raise RuntimeError("disposable PostgreSQL readiness timed out")
        command("createdb", "-h", "127.0.0.1", "-p", str(port), "-U", "litellm", "litellm")
        yield f"postgresql://litellm:synthetic-only@127.0.0.1:{port}/litellm"
    finally:
        stop_process(process)


def stop_process(process):
    process.terminate()
    try:
        process.wait(timeout=8)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=8)


def admin_request(port, path, payload):
    request = Request(f"http://127.0.0.1:{port}" + path,
                      headers={"Authorization": "Bearer " + PROXY_KEY,
                               "Content-Type": "application/json"},
                      data=json.dumps(payload).encode())
    try:
        with OPENER.open(request, timeout=30) as response:
            body = response.read(1048577)
            if response.status not in (200, 201) or len(body) > 1048576:
                raise RuntimeError("synthetic administration response invalid")
            return json.loads(body)
    except HTTPError as error:
        status = error.code
        error.close()
        raise RuntimeError(f"synthetic administration failed: {path} HTTP {status}") from None


def real_opencode(launcher, binary, root, port, key, session=None):
    state = root / "gateway"
    state.mkdir(exist_ok=True, mode=0o700)
    (state / "workload-key").write_text(key, encoding="utf-8")
    (state / "workload-key").chmod(0o600)
    (state / "gateway.json").write_text(json.dumps({
        "endpoint": f"http://127.0.0.1:{port}/v1", "model": ALIAS,
        "protocol": "responses",
    }), encoding="utf-8")
    environment = {
        **os.environ, "AGINCTUS_OPENCODE_DIR": str(state), "HOME": str(root),
        **{"XDG_" + name.upper() + "_HOME": str(root / name)
           for name in ("config", "data", "cache", "state")},
        "OPENCODE_DISABLE_AUTOUPDATE": "1",
        "OPENCODE_DISABLE_DEFAULT_PLUGINS": "1",
        "OPENCODE_DISABLE_MODELS_FETCH": "1",
    }
    try:
        result = subprocess.run([
            sys.executable, str(launcher), str(binary), "run", "--format", "json",
            "--title", "Synthetic offline acceptance",
            *(["--session", session] if session else []), MARKER,
        ], cwd=root, env=environment, stdin=subprocess.DEVNULL,
            capture_output=True, text=True, timeout=120, check=False)
    except subprocess.TimeoutExpired:
        raise RuntimeError("real OpenCode timed out (output suppressed)") from None
    # Never print CLI output: even errors may contain request metadata.
    if result.returncode or len(result.stdout) > 1048576:
        raise RuntimeError("real OpenCode failed (output suppressed)")
    return completed_native_session(result.stdout, session)


def completed_native_session(output, session=None):
    """An exit-zero or a partial stream is not proof of a completed response."""
    try:
        events = [json.loads(line) for line in output.splitlines() if line.strip()]
        sessions = {event["sessionID"] for event in events if "sessionID" in event}
        text = "".join(event.get("part", {}).get("text", "")
                       for event in events if event.get("type") == "text")
    except (ValueError, TypeError, AttributeError):
        raise RuntimeError("real OpenCode returned invalid JSON events") from None
    if (len(sessions) != 1 or MARKER not in text
            or any(event.get("type") == "error" for event in events)
            or not any(event.get("type") == "step_finish"
                       and event.get("part", {}).get("reason") == "stop" for event in events)):
        raise RuntimeError("real OpenCode did not finish and consume terminal SSE text")
    native = sessions.pop()
    if (not isinstance(native, str) or not re.fullmatch(r"ses_[A-Za-z0-9_-]{8,128}", native)
            or native in (SESSION_A, SESSION_B) or (session is not None and native != session)):
        raise RuntimeError("real OpenCode session identity invalid")
    return native


def run_production(bundle, launcher, binary, postgres_bin):
    """Extend the pinned probe with the production launcher and DB registry."""
    verify_isolation()
    load_packaged_hook(bundle)
    bundle = Path(bundle).resolve()
    deployment = json.loads((bundle / "deployment.json").read_text(encoding="utf-8"))
    environment = deployment["environment"]
    if (environment.get("STORE_MODEL_IN_DB") != "True"
            or environment.get("CONFIG_FILE_PATH") != "/app/aginctus-go/config.yaml"
            or environment.get("PYTHONPATH") != "/app/aginctus-go"
            or "@sha256:" not in deployment["image"]):
        raise RuntimeError("unexpected rendered production environment")
    with tempfile.TemporaryDirectory() as version_home:
        version = subprocess.run([str(binary), "--version"], capture_output=True,
                                 env={**os.environ, "HOME": version_home},
                                 text=True, timeout=30, check=False)
    if version.returncode or not version.stdout.strip():
        raise RuntimeError("pinned OpenCode version unavailable")
    MockGo.calls = []
    fake = http.server.ThreadingHTTPServer(("127.0.0.1", 0), MockGo)
    threading.Thread(target=fake.serve_forever, daemon=True).start()
    try:
        with tempfile.TemporaryDirectory() as tmp, contextlib.ExitStack() as stack:
            root = Path(tmp)
            # Calibrate the original wire UA with the same real launcher/runtime,
            # not a guessed SDK version or a fixture header. Keep it in memory.
            calibration = root / "calibration"
            calibration.mkdir()
            real_opencode(launcher, binary, calibration, fake.server_port, UPSTREAM_KEY)
            agents = {call["headers"].get("user-agent") for call in MockGo.calls}
            if len(agents) != 1:
                raise RuntimeError("real OpenCode User-Agent calibration failed")
            user_agent = agents.pop()
            if not user_agent or not user_agent.startswith("opencode/" + version.stdout.strip() + " "):
                raise RuntimeError("real OpenCode User-Agent does not identify pinned runtime")
            MockGo.calls = []
            database = stack.enter_context(disposable_postgres(postgres_bin, root))
            # Keep delivered policy bytes unchanged. Only namespace-local paths,
            # DB address and synthetic runtime credentials replace deployment values.
            environment = {**os.environ, **environment,
                           "DATABASE_URL": database, "LITELLM_MASTER_KEY": PROXY_KEY,
                           "LITELLM_SALT_KEY": "synthetic-only",
                           "CONFIG_FILE_PATH": str(bundle / "config.yaml"),
                           "PYTHONPATH": str(bundle), "HOME": str(root)}
            port = free_port()
            proxy = subprocess.Popen([
                "/app/.venv/bin/litellm", "--config", str(bundle / "config.yaml"),
                "--host", "127.0.0.1", "--port", str(port),
            ], env=environment, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL)
            stack.callback(stop_process, proxy)
            wait_ready(port, proxy)
            for alias in (ALIAS, OTHER):
                admin_request(port, "/model/new", {
                    "model_name": alias, "litellm_params": {
                        "model": "openai/gpt-4o-mini", "api_key": UPSTREAM_KEY,
                        "api_base": f"http://127.0.0.1:{fake.server_port}/v1",
                    }, "model_info": {"mode": "responses"},
                })
            key = admin_request(port, "/key/generate", {"models": [ALIAS]})["key"]
            if not isinstance(key, str) or key in (PROXY_KEY, UPSTREAM_KEY):
                raise RuntimeError("restricted synthetic key invalid")
            print("PASS: unchanged rendered callback config with local DB models and restricted key")
            client = root / "client"
            client.mkdir()
            sessions = []
            for resume in (False, True, False):
                before = len(MockGo.calls)
                native = real_opencode(launcher, binary, client, port, key,
                                       sessions[0] if resume else None)
                calls = MockGo.calls[before:]
                if not calls or any(
                    call["path"] != "/v1/responses" or call["model"] != "gpt-4o-mini"
                    or call["headers"].get("x-opencode-session") != native
                    or call["headers"].get("user-agent") != user_agent
                    or call["headers"].get("authorization") != "Bearer " + UPSTREAM_KEY
                    for call in calls
                ):
                    raise RuntimeError("real launcher upstream session/UA/credential mismatch")
                if any(value in (key, PROXY_KEY, "Bearer " + key, "Bearer " + PROXY_KEY)
                       for call in calls for value in call["headers"].values()):
                    raise RuntimeError("workload or admin credential reached synthetic provider")
                sessions.append(native)
            if sessions[0] != sessions[1] or sessions[0] == sessions[2]:
                raise RuntimeError("independent real OpenCode sessions contaminated")
            print("PASS: real pinned launcher consumed SSE; native sessions match, resume, and separate; UA preserved")
            before = len(MockGo.calls)
            status, _ = send(port, OTHER, sessions[0], key=key)
            require_unauthenticated_rejection(status, len(MockGo.calls) - before)
            print("PASS: restricted virtual key denies unrelated model without upstream")
            before = len(MockGo.calls)
            status, _ = send(port, "aginctus-unregistered", sessions[0], key=key)
            require_unauthenticated_rejection(status, len(MockGo.calls) - before)
            # Admin control proves the other model exists and is outside the hook.
            status, body = send(port, OTHER, sessions[0])
            if status != 200 or b"response.completed" not in body:
                raise RuntimeError("unrelated registered model control failed")
            unrelated = MockGo.calls[-1]["headers"]
            if unrelated.get("x-opencode-session") or any(
                unrelated.get(header) == value for header, value in SENTINELS.items()
            ):
                raise RuntimeError("unrelated model received client metadata")
            for session, ua, headers, body in [
                (None, user_agent, {}, {}),
                ("invalid-session", user_agent, {}, {}),
                (sessions[0], "python-httpx/0.28", {}, {}),
                (sessions[0], user_agent, {"x-opencode-session-id": sessions[2]}, {}),
                (None, user_agent, {}, {"proxy_server_request": {"headers": {
                    "x-opencode-session": sessions[0], "user-agent": user_agent}}}),
            ]:
                before = len(MockGo.calls)
                status, _ = send(port, ALIAS, session, key=key, user_agent=ua,
                                 extra_headers=headers, extra_body=body)
                if status not in (400, 401, 403, 422) or len(MockGo.calls) != before:
                    raise RuntimeError("production negative control did not reject before upstream")
            before = len(MockGo.calls)
            status, _ = send(port, ALIAS, sessions[0], authenticated=False)
            require_unauthenticated_rejection(status, len(MockGo.calls) - before)
            # Authenticated body injection must not replace the model's key or
            # the independently verified real request headers.
            status, body = send(port, ALIAS, sessions[0], key=key, user_agent=user_agent,
                                extra_body={"extra_headers": {
                                    "Authorization": "Bearer " + PROXY_KEY, **SENTINELS}})
            if status != 200 or b"response.completed" not in body:
                raise RuntimeError("body injection sanitization control failed")
            headers = MockGo.calls[-1]["headers"]
            if (headers.get("authorization") != "Bearer " + UPSTREAM_KEY
                    or headers.get("x-opencode-session") != sessions[0]
                    or any(headers.get(h) == v for h, v in SENTINELS.items())):
                raise RuntimeError("body injection escaped credential/header separation")
            print("PASS: unrelated alias scope, anonymous, malformed/missing/conflicting metadata, body spoof and injection controls")
    finally:
        fake.shutdown()
        fake.server_close()


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
    parser.add_argument("--production-launcher", type=Path,
                        help="run production launch.py with DB-backed rendered deployment")
    parser.add_argument("--opencode", type=Path, help="flake-pinned real OpenCode executable")
    parser.add_argument("--postgres-bin", type=Path, help="cached Nix PostgreSQL bin directory")
    args = parser.parse_args()
    try:
        if args.production_launcher:
            if not all((args.packaged_hook, args.opencode, args.postgres_bin)):
                raise RuntimeError("production mode requires packaged deployment, OpenCode and PostgreSQL")
            run_production(args.packaged_hook, args.production_launcher,
                           args.opencode, args.postgres_bin)
        else:
            run(args.fixture, args.require_safe or args.selective_hook or bool(args.packaged_hook),
                selective_hook=args.selective_hook, packaged_hook=args.packaged_hook)
    except (RuntimeError, OSError, ValueError, KeyError, ImportError) as error:
        print("FAIL: " + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
