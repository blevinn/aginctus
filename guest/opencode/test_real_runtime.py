"""Exercise the *real pinned* OpenCode CLI against a loopback Responses mock.

Run with: nix develop -c python3 guest/opencode/test_real_runtime.py /nix/store/.../bin/opencode
The test fails unless the real CLI makes an authenticated /v1/responses call
and consumes a complete SSE text response. Never needs a live LiteLLM gateway.
"""
import http.server
import json
import os
import secrets
import subprocess
import sys
import tempfile
import threading
from pathlib import Path
from typing import ClassVar

from tool_contract import ContractError, declarations, results

LAUNCHER = Path(__file__).with_name("launch.py")
EXPECTED = "AGINCTUS_MOCK_RESPONSE_OK"
MODEL = "aginctus-go-pilot"


class Handler(http.server.BaseHTTPRequestHandler):
    calls: ClassVar[list] = []
    tools: ClassVar[list] = []
    continuations: ClassVar[list] = []
    sequence = None
    sequence_stream = None

    def do_POST(self):
        payload = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        try:
            body = json.loads(payload)
        except (ValueError, UnicodeDecodeError):
            self.send_error(400)
            return
        # Record only runtime header metadata in the disposable loopback mock.
        # Never log either the bearer credential or session identifiers.
        session = (self.headers.get("x-opencode-session")
                   or self.headers.get("x-opencode-session-id"))
        user_agent = self.headers.get("User-Agent", "")
        type(self).calls.append(
            (self.path, self.headers.get("Authorization"), body.get("model"),
             session, user_agent)
        )
        try:
            tool_meta = declarations(body) if body.get("tools") else ()
            continuation_ids = results(body)
        except ContractError:
            self.send_error(400)
            return
        type(self).tools.append(tool_meta)
        type(self).continuations.append(continuation_ids)
        if self.path != "/v1/responses" or self.headers.get("Authorization") != "Bearer integration-only-key":
            self.send_error(403)
            return
        if type(self).sequence is not None:
            try:
                _, document = type(self).sequence.next(body)
            except ValueError as exc:
                print("Synthetic tool sequence rejected the request:", exc, file=sys.stderr)
                self.send_error(400)
                return
            wire = type(self).sequence_stream(document)
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.end_headers()
            self.wfile.write(wire)
            self.wfile.flush()
            return
        response = {
            "id": "resp_mock", "object": "response", "created_at": 1730000000,
            "model": MODEL, "status": "completed", "parallel_tool_calls": False,
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


def verify_tool_workspace(root, nonce, response_steps, continuations, stdout):
    """Inspect run-owned files, never infer execution from model text alone."""
    if "synthetic-tools-complete:" + nonce not in stdout or response_steps != 3:
        raise AssertionError("synthetic tool protocol did not reach terminal state")
    observed = {call_id for group in continuations for call_id in group}
    expected = {"call_" + nonce + "_" + str(i) for i in range(3)}
    if observed != expected:
        raise AssertionError("actual tool call/result continuity unverified")
    edited = root / "calc.go"
    if edited.is_symlink() or not edited.is_file() or "return a + b" not in edited.read_text():
        raise AssertionError("agent did not edit run-owned Go source")
    marker = root / ".aginctus-tool-test-ok"
    if marker.is_symlink() or not marker.is_file() or marker.read_text() != nonce:
        raise AssertionError("actual agent bash test command marker unavailable")


def main(binary, inspect_tools=False, execute_tools=False):
    if execute_tools:
        # This opt-in path asks a real coding agent to execute OS commands.
        # The flag/acknowledgement are NOT a security sandbox. Enforce an
        # immutable Nix-packaged test inside a non-root disposable VM user;
        # it must never be enabled on a pilot or developer host checkout.
        source = Path(__file__).resolve()
        executable = Path(binary).resolve(strict=True)
        if (
            os.geteuid() == 0
            or os.environ.get("AGINCTUS_TESTENV_TOOL_WORKER") != "DISPOSABLE_VM_ONLY"
            or not source.is_relative_to(Path("/nix/store"))
            or not executable.is_relative_to(Path("/nix/store"))
        ):
            raise RuntimeError("tool execution requires packaged disposable non-root VM")
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        state = root / "gateway"
        state.mkdir()
        (state / "workload-key").write_text("integration-only-key\n")
        (state / "workload-key").chmod(0o600)
        Handler.calls = []
        Handler.tools = []
        Handler.continuations = []
        Handler.sequence = None
        Handler.sequence_stream = None
        nonce = secrets.token_hex(12)
        if execute_tools:
            # Import only a repository-pinned fixture from this source tree.
            # The interpreter runs the actual pinned OpenCode launcher separately.
            sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
            from scripts.vm_harness.tool_sequence import ToolSequence, stream
            Handler.sequence = ToolSequence(str(root), model=MODEL)
            Handler.sequence_stream = stream
            (root / "go.mod").write_text(
                "module example.invalid/aginctus-tool-fixture\ngo 1.22\n")
            (root / "calc.go").write_text(
                "package calc\nfunc Add(a, b int) int { return a - b }\n")
            (root / "calc_test.go").write_text(
                "package calc\nimport \"testing\"\n"
                "func TestAdd(t *testing.T) {\n"
                " if Add(2, 3) != 5 { t.Fatal(\"bad addition\") }\n}\n")
            initial = subprocess.run(
                ["go", "test", "./..."], cwd=root, capture_output=True,
                timeout=60, check=False, env=dict(os.environ, GOPROXY="off"),
            )
            assert initial.returncode != 0, "broken Go fixture unexpectedly passed"
        if inspect_tools:
            (root / "calc.go").write_text("package calc\nfunc Add(a,b int) int { return a-b }\n")
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            (state / "gateway.json").write_text(json.dumps({
                "endpoint": f"http://127.0.0.1:{server.server_port}/v1",
                "model": MODEL, "protocol": "responses",
            }))
            env = dict(os.environ, AGINCTUS_OPENCODE_DIR=str(state),
                       HOME=str(root), XDG_CONFIG_HOME=str(root / "config"),
                       XDG_DATA_HOME=str(root / "data"), XDG_CACHE_HOME=str(root / "cache"),
                       XDG_STATE_HOME=str(root / "state"),
                       OPENCODE_DISABLE_AUTOUPDATE="true")
            try:
                result = subprocess.run(
                    [sys.executable, str(LAUNCHER), binary, "run", "--model", "aginctus/" + MODEL,
                     ("Inspect calc.go and calc_test.go, fix Add using the real read/edit "
                      "tools, run go test ./... with the bash tool. nonce=" + nonce)
                     if execute_tools else
                     ("Read calc.go and identify the error, then respond with " + EXPECTED)
                     if inspect_tools else EXPECTED + " (repeat this marker verbatim)"],
                    cwd=root, env=env, capture_output=True, text=True, timeout=180, check=False)
            except subprocess.TimeoutExpired as exc:
                captured = "\n".join(
                    value.decode("utf-8", "replace") if isinstance(value, bytes) else value or ""
                    for value in (exc.stdout, exc.stderr)
                )
                captured = captured.replace("integration-only-key", "[REDACTED]")
                captured = captured.replace(nonce, "[NONCE]")
                if captured:
                    print("OpenCode timeout output (bounded/redacted):", file=sys.stderr)
                    print(captured[-4000:], file=sys.stderr)
                raise AssertionError(
                    "pinned OpenCode timed out: "
                    f"requests={len(Handler.calls)} "
                    f"tool_continuations={sum(map(len, Handler.continuations))} "
                    f"timeout={exc.timeout}s"
                ) from None
            assert Handler.calls, "real OpenCode did not reach the mock gateway"
            assert all(p == "/v1/responses" and auth == "Bearer integration-only-key"
                       and model == MODEL for p, auth, model, _, _ in Handler.calls), (
                           "unexpected endpoint, authentication or model selection")
            # The custom provider needs its narrow plugin to relay the genuine
            # OpenCode sessionID. This must be observed on the real pinned
            # client, not inferred from launcher config or invented by Aginctus.
            sessions = [session for _, _, _, session, _ in Handler.calls]
            assert all(sessions), "pinned OpenCode did not emit a native session header"
            assert len(set(sessions)) == 1, "OpenCode changed session identity during one run"
            assert all("opencode" in ua.lower() for _, _, _, _, ua in Handler.calls), (
                "pinned OpenCode did not identify itself in User-Agent")
            assert result.returncode == 0, (
                "OpenCode exited unsuccessfully (stdout/stderr suppressed to avoid credential leaks)")
            if execute_tools:
                verify_tool_workspace(
                    root, nonce, Handler.sequence._steps.get(nonce),
                    Handler.continuations, result.stdout
                )
                final = subprocess.run(
                    ["go", "test", "./..."], cwd=root, capture_output=True,
                    timeout=60, check=False, env=dict(os.environ, GOPROXY="off"),
                )
                assert final.returncode == 0, (
                    "host verification of edited disposable fixture failed")
                print("PASS: real pinned OpenCode executed read/edit/bash and fixed Go fixture")
                print("NOTE: Herdr, gateway-side audit, VM egress and teardown not verified")
            else:
                assert EXPECTED in result.stdout, "OpenCode did not consume the SSE response"
            if inspect_tools:
                # Deliberately observe declarations, NOT successful tool use.
                # Subsequent PRs must exercise actual call/result round trips.
                observed = [tools for tools in Handler.tools if tools]
                assert observed, "real OpenCode did not declare any Responses tools"
                unique = {name for tools in observed for name, _ in tools}
                assert len(unique) >= 1, "real client declared no usable tool names"
                assert not any(Handler.continuations), (
                    "text-only mock unexpectedly received a tool-result continuation")
                print("Observed tool schema fingerprints:", observed)
                print("PASS: pinned OpenCode declared bounded real tool names and schemas")
                print("NOTE: tool invocation, execution, fixture edits not verified")
            else:
                print("PASS: real OpenCode sent native session and identifying User-Agent "
                      "with authenticated streaming Responses output")
        finally:
            server.shutdown()
            server.server_close()


if __name__ == "__main__":
    if len(sys.argv) not in (2, 3) or (
        len(sys.argv) == 3 and sys.argv[2] not in ("--inspect-tools", "--execute-tools")
    ):
        sys.exit("Usage: test_real_runtime.py PINNED_OPENCODE [--inspect-tools|--execute-tools]")
    main(sys.argv[1], inspect_tools=sys.argv[-1] == "--inspect-tools",
         execute_tools=sys.argv[-1] == "--execute-tools")
