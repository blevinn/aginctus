#!/usr/bin/env python3
"""Owned guest directory stage/cleanup through authenticated Incus guest exec.

Only for the empty Herdr workspace lifecycle smoke. Never removes fixture trees.
"""
import os
import re
import subprocess
import sys

IDENTITY = re.compile(r"aginctus-acceptance\.[A-Za-z0-9]{8}\Z")
INSTANCE = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,99}\Z")
ROOT = "/home/agent/aginctus-acceptance"
# Run as agent, not root; no shell or SSH, no host credential forwarding.
GUEST = r'''
import os, pathlib, stat, sys
mode, identity = sys.argv[1:]
root = pathlib.Path("/home/agent/aginctus-acceptance")
base = root / identity
marker = base / ".aginctus-owned"
def dir_safe(path):
    st = path.lstat()
    if not stat.S_ISDIR(st.st_mode) or st.st_uid != os.getuid() or st.st_mode & 0o022:
        raise RuntimeError("unsafe ownership or directory")
def verify():
    dir_safe(root)
    dir_safe(base)
    st = marker.lstat()
    if not stat.S_ISREG(st.st_mode) or st.st_uid != os.getuid() or st.st_mode & 0o077:
        raise RuntimeError("unsafe marker")
    if marker.read_text() != identity + "\\n":
        raise RuntimeError("marker mismatch")
if mode == "stage":
    if not root.exists() and not root.is_symlink():
        root.mkdir(mode=0o700)
    dir_safe(root)
    base.mkdir(mode=0o700)  # must not pre-exist
    try:
        with marker.open("x") as output:
            output.write(identity + "\\n")
        marker.chmod(0o600)
    except BaseException:
        base.rmdir()
        raise
    verify()
elif mode == "cleanup":
    if not base.exists() and not base.is_symlink():
        sys.exit(0)
    verify()
    # Strictly refuse removal if an agent has created any non-marker files.
    if set(p.name for p in base.iterdir()) != {marker.name}:
        raise RuntimeError("nonempty guest directory requires manual review")
    marker.unlink()
    base.rmdir()
else:
    raise RuntimeError("unknown operation")
'''

def run(mode, local_workspace, identity, guest):
    if mode not in ("stage", "cleanup") or not IDENTITY.fullmatch(identity):
        raise ValueError("invalid stage operation or run ID")
    if guest != ROOT + "/" + identity:
        raise ValueError("invalid guest path")
    if os.path.basename(local_workspace) != "workspace" or os.path.basename(os.path.dirname(local_workspace)) != identity:
        raise ValueError("invalid local workspace")
    instance = os.environ.get("AGINCTUS_ACCEPTANCE_WORKLOAD", "aginctus-dev")
    if not INSTANCE.fullmatch(instance):
        raise ValueError("invalid Incus workload")
    subprocess.run(["incus", "exec", instance, "--", "runuser", "-u", "agent", "--",
                    "python3", "-c", GUEST, mode, identity],
                   capture_output=True, text=True, check=True, timeout=60)

if __name__ == "__main__":
    try:
        if len(sys.argv) != 5:
            raise ValueError("usage: guest-owned.py stage|cleanup LOCAL_WORKSPACE RUN_ID GUEST_PATH")
        run(*sys.argv[1:])
    except (OSError, ValueError, subprocess.SubprocessError):
        print("FAIL: guest ownership operation (details suppressed)", file=sys.stderr)
        sys.exit(1)
