#!/usr/bin/env python3
"""Opt-in real Herdr workspace lifecycle smoke test. No agent or inference needed."""
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import sys
import tempfile

PREFIX = "aginctus-acceptance."
SAFE = re.compile(r"^/home/agent/aginctus-acceptance/[a-zA-Z0-9.-]+$")

def call(*args):
    try:
        completed = subprocess.run(args, capture_output=True, text=True, timeout=40, check=True)
        return completed.stdout
    except (OSError, subprocess.SubprocessError):
        raise RuntimeError("read-only Herdr check failed") from None

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
    for iteration in range(2):
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
            helper = env.get("AGINCTUS_ACCEPTANCE_HERDR_STAGE", "")
            if not helper or not Path(helper).is_file() or not os.access(helper, os.X_OK):
                raise RuntimeError("reviewed per-run guest staging helper required")
            cleanup = env.get("AGINCTUS_ACCEPTANCE_HERDR_CLEANUP", "")
            if not cleanup or not Path(cleanup).is_file() or not os.access(cleanup, os.X_OK):
                raise RuntimeError("reviewed per-run guest cleanup helper required")
            opened = False
            try:
                subprocess.run([helper, str(base), identity, guest], check=True, timeout=60,
                               capture_output=True, text=True)
                env["AGINCTUS_ACCEPTANCE_GUEST_STAGED"] = identity
                subprocess.run([sys.executable, str(fixture), "open", str(base), identity],
                               env=env, check=True, timeout=180, capture_output=True, text=True)
                opened = True
                state = json.loads((base.parent / ".herdr-owned.json").read_text())
                response = json.loads(call("incus", "exec", client, "--", "herdr", "--machine", machine, "workspace", "get", state["workspace_id"]))
                if not isinstance(response, dict) or "result" not in response:
                    raise RuntimeError("workspace inspect response missing")
            finally:
                if opened:
                    subprocess.run([sys.executable, str(fixture), "close", str(base), identity],
                                   env=env, check=True, timeout=180, capture_output=True, text=True)
                subprocess.run([cleanup, str(base), identity, guest], check=True, timeout=60,
                               capture_output=True, text=True)
            print(f"PASS: remote Herdr workspace create/get/close, iteration {iteration+1}")
    print("NOT VERIFIED: OpenCode agent, guest task execution, gateway inference, and credentials")

if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, subprocess.SubprocessError, ValueError, KeyError, json.JSONDecodeError):
        print("FAIL: Herdr remote workspace smoke (details suppressed)", file=sys.stderr)
        sys.exit(1)
