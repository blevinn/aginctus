"""Owned Herdr workspace/agent phases for #124. Does not assert gateway inference."""
import hashlib
import json
import os
import re
import subprocess
import sys
from pathlib import Path

PHASES = ("open", "start", "prompt", "wait", "stop", "close")
ID = re.compile(r"^aginctus-acceptance\.[A-Za-z0-9]{8}$")

def invoke(machine, *args):
    client = os.environ.get("AGINCTUS_ACCEPTANCE_HERDR_CLIENT", "aginctus-herdr")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,99}", client):
        raise RuntimeError("invalid managed Herdr client name")
    cmd = ["incus", "exec", client, "--", "herdr", "--machine", machine, *args]
    try:
        result = subprocess.run(cmd, text=True, capture_output=True, check=True, timeout=180)
    except (OSError, subprocess.SubprocessError):
        raise RuntimeError("Herdr operation failed") from None
    return result.stdout

def owned(workspace, identity):
    if not ID.fullmatch(identity):
        raise RuntimeError("invalid run identity")
    base = Path(workspace).absolute()
    if base.name != "workspace" or base.parent.name != identity or not base.is_dir():
        raise RuntimeError("invalid disposable workspace")
    if base.is_symlink() or base.parent.is_symlink():
        raise RuntimeError("symlinked workspace forbidden")
    return base

def state_path(base):
    return base.parent / ".herdr-owned.json"

def load(base, identity):
    file = state_path(base)
    if file.is_symlink() or not file.is_file() or file.stat().st_mode & 0o077:
        raise RuntimeError("missing private ownership record")
    state = json.loads(file.read_text())
    if state.get("identity") != identity or not all(
        isinstance(state.get(x), str) and state[x] for x in ("workspace_id", "pane_id", "machine", "agent_name")
    ):
        raise RuntimeError("invalid ownership record")
    return state

def record(base, state):
    path = state_path(base)
    if path.exists() or path.is_symlink():
        raise RuntimeError("ownership record already exists")
    fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(fd, "w") as stream:
        json.dump(state, stream)

def workspace_inventory(machine):
    """Query the selected Herdr machine, rejecting ambiguous or malformed lists.

    This is an independent *readback* of Herdr workspace state, not an
    assertion supplied by the agent or acceptance phase exit status.
    """
    raw = invoke(machine, "workspace", "list")
    if not isinstance(raw, str) or len(raw) > 65536:
        raise RuntimeError("bounded Herdr workspace inventory required")
    try:
        envelope = json.loads(raw)
    except ValueError:
        raise RuntimeError("invalid Herdr workspace inventory") from None
    result = envelope.get("result") if isinstance(envelope, dict) else None
    if (
        not isinstance(result, dict)
        or result.get("type") != "workspace_list"
        or not isinstance(result.get("workspaces"), list)
    ):
        raise RuntimeError("invalid Herdr workspace inventory")
    inventory = {}
    for workspace in result["workspaces"]:
        if not isinstance(workspace, dict):
            raise RuntimeError("invalid Herdr workspace entry")
        wid = workspace.get("workspace_id")
        label = workspace.get("label")
        # Unrelated workspaces may have no label; only the selected
        # owned ID must carry the exact label asserted by this run.
        if not isinstance(wid, str) or not wid or wid in inventory:
            raise RuntimeError("ambiguous Herdr workspace inventory")
        inventory[wid] = label
    return inventory


def phase(action, workspace, identity, prompt=None):
    base = owned(workspace, identity)
    machine = os.environ.get("AGINCTUS_ACCEPTANCE_HERDR_MACHINE", "")
    # Fail closed: Herdr's machine selector is a configured SSH profile, not an address.
    if not machine or machine.startswith("-") or "\n" in machine:
        raise RuntimeError("a reviewed Herdr machine profile is required")
    if action == "open":
        # A host-local workspace is NOT guest-local: operator must stage fixture
        # into this dedicated guest directory via a separately reviewed transport.
        remote = os.environ.get("AGINCTUS_ACCEPTANCE_GUEST_ROOT", "")
        if not remote.startswith("/home/agent/aginctus-acceptance/") or not remote.endswith("/" + identity):
            raise RuntimeError("verified guest workspace mapping required")
        if state_path(base).exists():
            raise RuntimeError("run already has an ownership record")
        # No create until the operator asserts that this is a new guest directory,
        # with fixtures copied and test permissions verified.
        if os.environ.get("AGINCTUS_ACCEPTANCE_GUEST_STAGED") != identity:
            raise RuntimeError("guest fixture staging not verified")
        response = json.loads(invoke(machine, "workspace", "create", "--cwd", remote, "--label", identity, "--no-focus"))
        result = response.get("result", {})
        wid = result.get("workspace", {}).get("workspace_id")
        pane = result.get("root_pane", {}).get("pane_id")
        if not isinstance(wid, str) or not isinstance(pane, str) or not wid or not pane:
            raise RuntimeError("Herdr returned no owned workspace identity; manual inspection required")
        agent = "acc_" + hashlib.sha256(identity.encode()).hexdigest()[:16]
        record(base, {"identity": identity, "machine": machine, "workspace_id": wid, "pane_id": pane, "agent_name": agent})
        return
    state = load(base, identity)
    if state["machine"] != machine:
        raise RuntimeError("machine changed after workspace creation")
    if action == "start":
        invoke(machine, "agent", "start", state["agent_name"], "--kind", "opencode", "--pane", state["pane_id"], "--timeout", "90000")
    elif action == "prompt":
        if prompt is None:
            raise RuntimeError("missing prompt")
        invoke(machine, "agent", "prompt", state["agent_name"], prompt, "--wait", "--until", "done", "--until", "idle", "--timeout", "150000")
    elif action == "wait":
        # The prompt wait establishes Herdr agent settled status only.
        # Gateway and guest test trace evidence MUST be independently verified.
        invoke(machine, "agent", "wait", state["agent_name"], "--until", "done", "--until", "idle", "--timeout", "150000")
    elif action == "stop":
        # Closing the owned workspace terminates its panes; no global agent stop.
        # Run close separately so a partial stop is still cleaned up.
        invoke(machine, "agent", "send-keys", state["pane_id"], "ctrl+c")
    elif action == "close":
        wid = state["workspace_id"]
        # Positive control: do not close a stale/reused ID, an unrelated
        # workspace, or anything absent from the independently read inventory.
        before = workspace_inventory(machine)
        if before.get(wid) != identity:
            raise RuntimeError("owned Herdr workspace not confirmed before close")
        invoke(machine, "workspace", "close", wid)
        # A zero exit from 'close' alone is not evidence the remote workspace
        # disappeared. Query the SAME saved machine again before acknowledging.
        if wid in workspace_inventory(machine):
            raise RuntimeError("owned Herdr workspace remains after close")
        # Retain ownership metadata on any failure so the operator can inspect.
        state_path(base).unlink()
    else:
        raise RuntimeError("unsupported phase")

if __name__ == "__main__":
    try:
        if len(sys.argv) not in (4, 5) or sys.argv[1] not in PHASES:
            raise RuntimeError("usage: herdr-lifecycle.py PHASE WORKSPACE RUN_ID [PROMPT]")
        if (sys.argv[1] == "prompt") != (len(sys.argv) == 5):
            raise RuntimeError("invalid prompt arguments")
        phase(sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4] if len(sys.argv) == 5 else None)
    except (RuntimeError, ValueError, KeyError, json.JSONDecodeError):
        print("FAIL: owned Herdr lifecycle operation (details suppressed)", file=sys.stderr)
        sys.exit(1)
