"""Opt-in real Herdr workspace lifecycle smoke test.

Two opt-in levels (both gated by an ``I_UNDERSTAND`` acknowledgment):

- base (``AGINCTUS_ACCEPTANCE_HERDR_SMOKE=I_UNDERSTAND``): runs the
  Herdr workspace lifecycle through ``open`` / ``inspect`` against a
  registered managed machine. No agent is launched.
- agent (``AGINCTUS_ACCEPTANCE_HERDR_AGENT_SMOKE=I_UNDERSTAND``):
  additionally launches the OpenCode agent, inspects it via
  ``herdr agent get``, and (if ``AGINCTUS_ACCEPTANCE_HERDR_AGENT_PROMPT``
  is also set) issues a single constrained prompt.

The agent level requires an inference-capable environment; the base
level does not.
"""
import json
import os
import re
import secrets
import subprocess
import sys
import tempfile
from pathlib import Path

PREFIX = "aginctus-acceptance."
SAFE = re.compile(r"^/home/agent/aginctus-acceptance/[a-zA-Z0-9.-]+$")

def call(*args):
    try:
        completed = subprocess.run(args, capture_output=True, text=True, timeout=40, check=True)
        return completed.stdout
    except (OSError, subprocess.SubprocessError):
        raise RuntimeError("read-only Herdr check failed") from None

def python_helper(path):
    return bool(path) and Path(path).is_file()

def main():
    if os.environ.get("AGINCTUS_ACCEPTANCE_HERDR_SMOKE") != "I_UNDERSTAND":
        raise RuntimeError("explicit Herdr smoke opt-in is required")
    machine = os.environ.get("AGINCTUS_ACCEPTANCE_HERDR_MACHINE", "")
    if not machine or machine.startswith("-") or "\n" in machine:
        raise RuntimeError("reviewed saved Herdr machine profile required")
    # The operator must provision a guest-local parent with agent ownership.
    root = os.environ.get("AGINCTUS_ACCEPTANCE_HERDR_SMOKE_ROOT", "")
    if root != "/home/agent/aginctus-acceptance":
        raise RuntimeError("reviewed guest workspace parent required")
    client = os.environ.get("AGINCTUS_ACCEPTANCE_HERDR_CLIENT", "aginctus-herdr")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,99}", client):
        raise RuntimeError("invalid managed Herdr client name")
    registered = json.loads(call("incus", "exec", client, "--", "herdr", "machine", "list", "--json"))
    if not isinstance(registered, list) or not any(
        isinstance(entry, dict) and entry.get("label") == machine and entry.get("enabled") is True
        for entry in registered
    ):
        raise RuntimeError("required managed Herdr machine is not registered and enabled")
    fixture = Path(__file__).with_name("herdr-lifecycle.py")
    agent_smoke = os.environ.get("AGINCTUS_ACCEPTANCE_HERDR_AGENT_SMOKE") == "I_UNDERSTAND"
    for iteration in range(2):
        step = "prepare"
        identity = PREFIX + secrets.token_hex(4)
        # This cwd must pre-exist on the guest, so creating unique paths requires
        # a reviewed out-of-band staging mechanism. Do not create arbitrary paths.
        guest = root + "/" + identity
        if not SAFE.fullmatch(guest):
            raise RuntimeError("invalid guest directory")
        with tempfile.TemporaryDirectory(prefix=identity + "-") as temp:
            base = Path(temp) / identity / "workspace"
            base.mkdir(parents=True)
            env = os.environ.copy()
            env["AGINCTUS_ACCEPTANCE_GUEST_ROOT"] = guest
            # No implicit fabricated staging signal: the operator-supplied helper
            # must verify or stage this exact identity and path first.
            helper = env.get("AGINCTUS_ACCEPTANCE_HERDR_STAGE", str(Path(__file__).with_name("guest-owned.py")))
            if not python_helper(helper):
                raise RuntimeError("reviewed per-run guest staging helper required")
            cleanup = env.get("AGINCTUS_ACCEPTANCE_HERDR_CLEANUP", str(Path(__file__).with_name("guest-owned.py")))
            if not python_helper(cleanup):
                raise RuntimeError("reviewed per-run guest cleanup helper required")
            cleanup_failed = False
            try:
                step = "stage"
                subprocess.run([sys.executable, helper, "stage", str(base), identity, guest], check=True, timeout=60,
                               capture_output=True, text=True)
                env["AGINCTUS_ACCEPTANCE_GUEST_STAGED"] = identity
                step = "create"
                subprocess.run([sys.executable, str(fixture), "open", str(base), identity],
                               env=env, check=True, timeout=180, capture_output=True, text=True)
                state = json.loads((base.parent / ".herdr-owned.json").read_text())
                step = "inspect"
                response = json.loads(call("incus", "exec", client, "--", "herdr", "--machine", machine, "workspace", "get", state["workspace_id"]))
                if not isinstance(response, dict) or "result" not in response:
                    raise RuntimeError("workspace inspect response missing")
                if agent_smoke:
                    step = "agent-start"
                    subprocess.run([sys.executable, str(fixture), "start", str(base), identity],
                                   env=env, check=True, timeout=180, capture_output=True, text=True)
                    step = "agent-inspect"
                    state = json.loads((base.parent / ".herdr-owned.json").read_text())
                    result = json.loads(call("incus", "exec", client, "--", "herdr", "--machine",
                                             machine, "agent", "get", state["agent_name"]))
                    if not isinstance(result, dict) or "result" not in result:
                        raise RuntimeError("agent get returned no result")
                    print(f"PASS: OpenCode agent startup and inspection, iteration {iteration+1}")
                    if os.environ.get("AGINCTUS_ACCEPTANCE_HERDR_AGENT_PROMPT") == "I_UNDERSTAND":
                        step = "agent-prompt-wait"
                        subprocess.run([sys.executable, str(fixture), "prompt", str(base), identity,
                                        "Reply with the word READY and do not modify any files."],
                                       env=env, check=True, timeout=180, capture_output=True, text=True)
                        subprocess.run([sys.executable, str(fixture), "wait", str(base), identity],
                                       env=env, check=True, timeout=180, capture_output=True, text=True)
                        print(f"PASS: agent prompt/wait returned, iteration {iteration+1}")
            except (RuntimeError, OSError, subprocess.SubprocessError, ValueError, KeyError, json.JSONDecodeError):
                print(f"FAIL: Herdr smoke iteration {iteration+1}, phase {step} (details suppressed)", file=sys.stderr)
                raise
            finally:
                if (base.parent / ".herdr-owned.json").exists():
                    # Attempt scoped interrupt when an agent was requested, then
                    # close the recorded workspace even if interrupt fails.
                    if agent_smoke:
                        try:
                            subprocess.run([sys.executable, str(fixture), "stop", str(base), identity],
                                           env=env, check=True, timeout=180, capture_output=True, text=True)
                        except (OSError, subprocess.SubprocessError):
                            print("WARN: agent interrupt failed; closing owned workspace", file=sys.stderr)
                    try:
                        subprocess.run([sys.executable, str(fixture), "close", str(base), identity],
                                       env=env, check=True, timeout=180, capture_output=True, text=True)
                    except (OSError, subprocess.SubprocessError):
                        cleanup_failed = True
                        print("WARN: workspace close failed; cleanup helper will still run", file=sys.stderr)
                try:
                    subprocess.run([sys.executable, cleanup, "cleanup", str(base), identity, guest], check=True, timeout=60,
                                   capture_output=True, text=True)
                except (OSError, subprocess.SubprocessError):
                    cleanup_failed = True
                    print("WARN: guest cleanup helper failed; staged directory may leak", file=sys.stderr)
            # Preserve any primary exception raised in the try block; otherwise
            # a close/cleanup failure cannot be reported as smoke success.
            if cleanup_failed:
                raise RuntimeError("remote Herdr cleanup could not be verified")
            print(f"PASS: remote Herdr workspace create/get/close, iteration {iteration+1}")
    print("NOT VERIFIED: agent prompt/wait, guest task execution, gateway inference, and credentials" if agent_smoke else "NOT VERIFIED: OpenCode agent, guest task execution, gateway inference, and credentials")

if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, subprocess.SubprocessError, ValueError, KeyError, json.JSONDecodeError):
        print("FAIL: Herdr remote workspace smoke (details suppressed)", file=sys.stderr)
        sys.exit(1)
