"""Fixed disposable-guest synthetic Herdr/OpenCode scenario, never live inference.

The nine phase names/arguments mirror acceptance/opencode.sh. This separate
synthetic runner neither invokes live hooks nor issues their inference marker.
"""
from __future__ import annotations

import hashlib
import json
import os
import re
import runpy
import secrets
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path

if __package__:
    from . import synthetic_gateway_probe as gateway
    from .synthetic_responses import SyntheticServer
    from .tool_sequence import STEP_NAMES, ToolSequence, tool_arguments
else:
    import synthetic_gateway_probe as gateway
    from synthetic_responses import SyntheticServer
    from tool_sequence import STEP_NAMES, ToolSequence, tool_arguments

ALIAS = "aginctus-go-pilot"  # Activates the packaged native-session header hook.
PHASES = ("preflight", "open", "start", "prompt", "wait", "credential-check",
          "gateway-evidence", "stop", "close")
FILES = {
    "go.mod": "module example.invalid/aginctus-full-stack\ngo 1.22\n",
    "calc.go": "package calc\n\nfunc Add(a, b int) int { return a - b }\n",
    "calc_test.go": ('package calc\nimport "testing"\nfunc TestAdd(t *testing.T) {\n'
                     ' if Add(2, 3) != 5 { t.Fatal("bad addition") }\n}\n'),
    "opencode.json": json.dumps({
        "$schema": "https://opencode.ai/config.json",
        "permission": {"*": "deny", "read": "allow", "edit": "allow", "bash": "allow"},
    }) + "\n",
}


def command(*argv, data=None, check=True):
    result = subprocess.run(argv, input=data, capture_output=True, text=True,
                            timeout=180, check=False)
    if len(result.stdout) > 65536 or (check and result.returncode):
        raise gateway.Failure("full-stack guest operation failed")
    return result


def guest(*argv, data=None, check=True, agent=True):
    prefix = ("runuser", "-u", "agent", "--") if agent else ()
    return command("incus", "exec", "aginctus-dev", "--", *prefix, *argv,
                   data=data, check=check)


def denied(base, path, token, body=None):
    try:
        gateway.request(base, path, token, body)
    except gateway.Failure as exc:
        if str(exc) in ("synthetic gateway operation HTTP 401",
                        "synthetic gateway operation HTTP 403"):
            return
        raise
    raise gateway.Failure("full-stack authorization denial missing")


def verify_files(files, nonce):
    expected = dict(FILES)
    expected["calc.go"] = expected["calc.go"].replace("return a - b", "return a + b")
    expected[".aginctus-tool-test-ok"] = nonce
    if files != expected:
        raise gateway.Failure("independent exact fixture diff or tool-test marker missing")


def verify_transcript(document, session, remote, nonce):
    if (not isinstance(document, dict) or document.get("info", {}).get("id") != session
            or document.get("info", {}).get("directory") != remote
            or not isinstance(document.get("messages"), list)):
        raise gateway.Failure("native OpenCode session mapping missing")
    tools, terminal, stopped = [], False, False
    for message in document["messages"]:
        if not isinstance(message, dict) or message.get("info", {}).get("error"):
            raise gateway.Failure("native OpenCode session failed")
        for part in message.get("parts", []):
            if part.get("type") == "tool":
                step = len(tools)
                if (step >= 3 or part.get("tool") != STEP_NAMES[step]
                        or part.get("callID") != "call_" + nonce + "_" + str(step)
                        or part.get("state", {}).get("status") != "completed"
                        or part["state"].get("input") != tool_arguments(step, remote, nonce)):
                    raise gateway.Failure("native completed read/edit/bash trace missing")
                tools.append(part["tool"])
            if part.get("type") == "text" and part.get("text") == "synthetic-tools-complete:" + nonce:
                terminal = message.get("info", {}).get("role") == "assistant"
            if part.get("type") == "step-finish" and part.get("reason") == "stop":
                stopped = True
    if tools != list(STEP_NAMES) or not terminal or not stopped:
        raise gateway.Failure("native OpenCode terminal completion missing")


def verify_rows(rows, key_hash, session):
    """Gateway DB observation, scoped by a fresh runner-issued key per run.

    This is client-supplied session attribution, not authenticated session
    provenance. A spend success label alone never establishes tool execution.
    """
    if not isinstance(rows, list) or len(rows) != 4:
        raise gateway.Failure("four gateway-side tool-turn records required")
    sessions, requests = set(), set()
    for row in rows:
        if (not isinstance(row, dict) or row.get("api_key") != key_hash
                or row.get("model_group") != ALIAS or row.get("call_type") != "aresponses"
                or row.get("status") != "success"
                or not isinstance(row.get("request_id"), str) or not row["request_id"]
                or row.get("session_id") != session):
            raise gateway.Failure("gateway-side key/model/session/completion correlation missing")
        sessions.add(row["session_id"])
        requests.add(row["request_id"])
    if len(sessions) != 1 or len(requests) != 4:
        raise gateway.Failure("ambiguous gateway-side tool-turn correlation")


class Adapter:
    def __init__(self, base, identity, nonce, endpoint, scoped, upstream):
        self.base, self.identity, self.nonce = base, identity, nonce
        self.endpoint, self.scoped, self.upstream = endpoint, scoped, upstream
        self.remote = "/home/agent/aginctus-acceptance/" + identity
        self.lifecycle = runpy.run_path(str(Path(__file__).with_name("herdr-lifecycle.py")))
        self.staged = False
        self.session = None
        os.environ.update({
            "AGINCTUS_ACCEPTANCE_HERDR_CLIENT": "aginctus-herdr",
            "AGINCTUS_ACCEPTANCE_HERDR_MACHINE": "aginctus-dev",
            "AGINCTUS_ACCEPTANCE_GUEST_ROOT": self.remote,
            "AGINCTUS_ACCEPTANCE_GUEST_STAGED": identity,
        })

    def phase(self, action, workspace, identity, prompt=None):
        if action not in PHASES or workspace != str(self.base) or identity != self.identity:
            raise gateway.Failure("invalid fixed synthetic adapter invocation")
        if action == "preflight":
            # These are disposable-VM operations, never pilot bootstrap.
            command("incus", "exec", "aginctus-herdr", "--", "aginctus-ssh-client-identity", "ensure")
            for operation in ("authorize", "pin-host", "register"):
                command("aginctus", "--configuration-file", "/etc/aginctus/config.json",
                        "herdr", operation, "dev")
            guest("aginctus-opencode-gateway-provision", agent=False, data=json.dumps({
                "endpoint": self.endpoint + "/v1", "model": ALIAS, "key": self.scoped,
            }))
            guest("sh", "-eu", "-c",
                  'umask 077; mkdir -p /home/agent/aginctus-acceptance; mkdir "$1"',
                  "fixture", self.remote)
            self.staged = True
            for name, contents in FILES.items():
                guest("sh", "-eu", "-c", 'cat > "$1"', "fixture", self.remote + "/" + name,
                      data=contents)
            before = guest("sh", "-eu", "-c", 'cd "$1"; GOPROXY=off GOSUMDB=off go test ./...',
                           "fixture", self.remote, check=False)
            if before.returncode != 1 or "bad addition" not in before.stdout:
                raise gateway.Failure("broken fixture did not independently fail its Go assertion")
            for mode, address in (("gateway", self.endpoint.removeprefix("http://").removesuffix(":4000")),
                                  ("direct", "1.1.1.1")):
                result = json.loads(guest("aginctus-egress-probe", mode, address).stdout)
                if result.get("status") != "pass":
                    raise gateway.Failure("workload TCP/IPv4 no-route probe failed")
        elif action in ("open", "start", "prompt", "wait", "stop"):
            self.lifecycle["phase"](action, workspace, identity, prompt)
            if action == "wait":
                listing = json.loads(guest("opencode", "session", "list", "--format", "json", "--max-count", "16").stdout)
                matching = [row.get("id") for row in listing if row.get("directory") == self.remote]
                if (len(matching) != 1 or not isinstance(matching[0], str)
                        or not re.fullmatch(r"ses_[A-Za-z0-9_-]{8,128}", matching[0])):
                    raise gateway.Failure("unique native OpenCode workspace session missing")
                self.session = matching[0]
                transcript = json.loads(guest("opencode", "export", self.session).stdout)
                verify_transcript(transcript, self.session, self.remote, self.nonce)
                files = {}
                for name in (*FILES, ".aginctus-tool-test-ok"):
                    path = self.remote + "/" + name
                    guest("sh", "-eu", "-c", 'test -f "$1"; test ! -L "$1"', "fixture", path)
                    files[name] = guest("cat", path).stdout
                verify_files(files, self.nonce)
                # Copy only the independently checked Go fixture into the outer
                # disposable VM and test afresh, without trusting the agent's test.
                for name in ("go.mod", "calc.go", "calc_test.go"):
                    (self.base / name).write_text(files[name])
                command("sh", "-eu", "-c", 'cd "$1"; GOPROXY=off GOSUMDB=off go test ./...',
                        "independent-test", str(self.base))
        elif action == "credential-check":
            guest("sh", "-eu", "-c", r'''
dir=/var/lib/aginctus/opencode
test "$(stat -c %a "$dir")" = 700
for file in workload-key gateway.json; do
    test ! -L "$dir/$file"
    test "$(stat -c %a "$dir/$file")" = 600
    test "$(stat -c %U "$dir/$file")" = agent
done
for path in /home/agent/.aws/credentials /home/agent/.config/opencode/auth.json /home/agent/.local/share/opencode/auth.json; do
    test ! -e "$path"
done
test ! -e /run/aginctus-vm-gateway-state
''')
            if gateway.model_ids(gateway.request(self.endpoint, "/v1/models", self.scoped)) != {ALIAS}:
                raise gateway.Failure("workload model authorization scope mismatch")
            denied(self.endpoint, "/v1/models", None)
            denied(self.endpoint, "/v1/models", "synthetic-invalid-key")
            denied(self.endpoint, "/v1/responses", self.scoped,
                   {"model": "synthetic-forbidden", "input": "no inference", "stream": True})
        elif action == "gateway-evidence":
            observed = json.loads(gateway.request(self.upstream, "/_observed/" + self.nonce, gateway.TOKEN))
            if observed != {"count": 4}:
                raise gateway.Failure("ordered upstream tool continuations/terminal response missing")
            key_hash = hashlib.sha256(self.scoped.encode()).hexdigest()
            # Select only bounded, non-payload fields. Never log DB rows or key hashes.
            sql = ('SELECT COALESCE(json_agg(t), \'[]\'::json) FROM (SELECT request_id, '
                   'session_id, api_key, model_group, call_type, status FROM "LiteLLM_SpendLogs" '
                   "WHERE api_key = '" + key_hash + "' LIMIT 5) t;")
            for attempt in range(45):
                rows = command("incus", "exec", "--project", "aginctus-gateway", "postgres-1", "--",
                               "psql", "-U", "litellm", "-d", "litellm", "-At", "-c", sql).stdout
                try:
                    verify_rows(json.loads(rows), key_hash, self.session)
                    break
                except gateway.Failure:
                    if attempt == 44:
                        raise
                    time.sleep(2)
            print("gateway-observed:" + identity)
        elif action == "close":
            if self.lifecycle["state_path"](self.base).exists():
                self.lifecycle["phase"]("close", workspace, identity)
            else:
                # An open that failed after remote creation may lack local state.
                inventory = self.lifecycle["workspace_inventory"]("aginctus-dev")
                matching = [wid for wid, label in inventory.items() if label == identity]
                if len(matching) > 1:
                    raise gateway.Failure("ambiguous partially opened workspace")
                for wid in matching:
                    self.lifecycle["invoke"]("aginctus-dev", "workspace", "close", wid)
            if identity in self.lifecycle["workspace_inventory"]("aginctus-dev").values():
                raise gateway.Failure("Herdr workspace absence not independently observed")
        print("PASS: synthetic full-stack phase " + action)

    def remove_fixture(self):
        if self.staged:
            # Fixed random run-owned path; root parent and unrelated runs survive.
            guest("rm", "-rf", "--", self.remote)
            guest("sh", "-eu", "-c", 'test ! -e "$1"; test ! -L "$1"', "fixture", self.remote)


def exercise(adapter):
    """Always attempt scoped stop/close and fixture removal after partial failure."""
    opened = started = False
    try:
        for phase in PHASES[:-2]:
            if phase == "open":
                opened = True
            if phase == "start":
                started = True
            prompt = "Inspect and fix Add; execute go test ./... using real tools. nonce=" + adapter.nonce
            adapter.phase(phase, str(adapter.base), adapter.identity,
                          prompt if phase == "prompt" else None)
    finally:
        failed = False
        for phase, needed in (("stop", started), ("close", opened)):
            if needed:
                try:
                    adapter.phase(phase, str(adapter.base), adapter.identity)
                except (RuntimeError, ValueError, OSError, subprocess.SubprocessError, gateway.Failure):
                    failed = True
        try:
            adapter.remove_fixture()
        except (OSError, subprocess.SubprocessError, gateway.Failure):
            failed = True
        if failed:
            raise gateway.Failure("owned Herdr/fixture cleanup failed")


def run():
    if (len(sys.argv) != 1 or not str(Path(__file__).resolve()).startswith("/nix/store/")
            or not Path("/etc/aginctus-test-oci").is_file()):
        raise gateway.Failure("full-stack requires the immutable disposable guest fixture")
    management = gateway.network_ipv4("aginctus-mgmt")
    egress = gateway.network_ipv4("aginctus-egress")
    if management.network.overlaps(egress.network):
        raise gateway.Failure("overlapping synthetic networks")
    if gateway.STATE.is_symlink() or gateway.STATE.stat().st_mode & 0o077:
        raise gateway.Failure("protected gateway state required")
    master = json.loads(gateway.STATE.read_text())["AGINCTUS_GATEWAY_MASTER_KEY"]
    endpoint = "http://" + gateway.lite_ipv4(management) + ":4000"
    nonce = secrets.token_hex(4)
    identity = "aginctus-acceptance." + nonce
    server = SyntheticServer((str(egress.ip), 9080), tool_mode=True)
    server.tool_sequence = ToolSequence("/home/agent/aginctus-acceptance/" + identity)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    upstream = "http://" + str(egress.ip) + ":9080"
    model_id = scoped = None
    try:
        if ALIAS in gateway.model_ids(gateway.request(endpoint, "/v1/models", master)):
            raise gateway.Failure("refuse adoption of an existing synthetic alias")
        model = json.loads(gateway.request(endpoint, "/model/new", master, {
            "model_name": ALIAS,
            "litellm_params": {"model": "openai/synthetic-test", "api_base": upstream + "/v1",
                               "api_key": gateway.TOKEN},
            "model_info": {"mode": "responses"},
        }))
        model_id = model.get("model_info", {}).get("id")
        if not isinstance(model_id, str) or not model_id:
            raise gateway.Failure("registered model identity unavailable; destroy disposable VM")
        for _ in range(30):
            if ALIAS in gateway.model_ids(gateway.request(endpoint, "/v1/models", master)):
                break
            time.sleep(0.2)
        else:
            raise gateway.Failure("synthetic model registration not observed")
        token = json.loads(gateway.request(endpoint, "/key/generate", master, {
            "models": [ALIAS], "duration": "15m", "max_budget": 1, "rpm_limit": 10,
            "key_alias": identity,
        }))
        scoped = token.get("key")
        if not isinstance(scoped, str) or not scoped or scoped == master:
            scoped = None
            raise gateway.Failure("fresh scoped workload credential unavailable")
        with tempfile.TemporaryDirectory(prefix="aginctus-full-stack-") as temp:
            base = Path(temp) / identity / "workspace"
            base.mkdir(parents=True, mode=0o700)
            exercise(Adapter(base, identity, nonce, endpoint, scoped, upstream))
    finally:
        # Attempt every teardown even when another one fails. Uncertain remote
        # mutation/revocation is a failure, with complete VM disposal still required.
        failed = False
        if scoped:
            try:
                gateway.request(endpoint, "/key/delete", master, {"keys": [scoped]})
                denied(endpoint, "/v1/models", scoped)
            except gateway.Failure:
                failed = True
        if model_id:
            try:
                gateway.request(endpoint, "/model/delete", master, {"id": model_id})
                if ALIAS in gateway.model_ids(gateway.request(endpoint, "/v1/models", master)):
                    raise gateway.Failure("synthetic alias remains after removal")
            except gateway.Failure:
                failed = True
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
        if failed or thread.is_alive():
            raise gateway.Failure("synthetic gateway cleanup not confirmed")
    print(json.dumps({"profile": "synthetic_full_stack", "status": "pass",
                      "agent_tools": "read_edit_bash", "gateway_key_correlation": "pass",
                      "owned_workspace_removed": "pass", "scoped_key_revoked": "pass",
                      "upstream_inference": "not_verified", "cleanup": "not_verified"}))


if __name__ == "__main__":
    try:
        run()
    except (gateway.Failure, RuntimeError, ValueError, KeyError, OSError, subprocess.SubprocessError):
        print("FAIL: synthetic full-stack gate (details suppressed; no live inference)", file=sys.stderr)
        raise SystemExit(1)
