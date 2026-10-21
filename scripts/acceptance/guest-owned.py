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
# POSIX shell on the NixOS guest: no Python runtime dependency.
# Fixed script; run ID is a separately validated argument, never interpolated.
GUEST = r'''
set -eu
umask 077
mode="$1"
identity="$2"
root=/home/agent/aginctus-acceptance
base="$root/$identity"
marker="$base/.aginctus-owned"
owner="$(id -u)"
safe_dir() {
    [ -d "$1" ] && [ ! -L "$1" ] || exit 1
    [ "$(stat -c %u "$1")" = "$owner" ] || exit 1
    [ -z "$(find "$1" -maxdepth 0 -perm /022 -print)" ] || exit 1
}
check_marker() {
    safe_dir "$root"
    safe_dir "$base"
    [ -f "$marker" ] && [ ! -L "$marker" ] || exit 1
    [ "$(stat -c %u "$marker")" = "$owner" ] || exit 1
    [ "$(stat -c %a "$marker")" = 600 ] || exit 1
    [ "$(cat "$marker")" = "$identity" ] || exit 1
}
case "$mode" in
stage)
    if [ ! -e "$root" ] && [ ! -L "$root" ]; then
        mkdir -m 700 -- "$root"
    fi
    safe_dir "$root"
    mkdir -m 700 -- "$base"
    printf '%s\n' "$identity" > "$marker"
    chmod 600 -- "$marker"
    check_marker
    ;;
cleanup)
    if [ ! -e "$base" ] && [ ! -L "$base" ]; then exit 0; fi
    check_marker
    [ -z "$(find "$base" -mindepth 1 -maxdepth 1 ! -name .aginctus-owned -print -quit)" ] || exit 1
    rm -- "$marker"
    rmdir -- "$base"
    ;;
*) exit 1 ;;
esac
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
                    "/bin/sh", "-c", GUEST, "aginctus-guest-owned", mode, identity],
                   capture_output=True, text=True, check=True, timeout=60)

if __name__ == "__main__":
    try:
        if len(sys.argv) != 5:
            raise ValueError("usage: guest-owned.py stage|cleanup LOCAL_WORKSPACE RUN_ID GUEST_PATH")
        run(*sys.argv[1:])
    except (OSError, ValueError, subprocess.SubprocessError):
        print("FAIL: guest ownership operation (details suppressed)", file=sys.stderr)
        sys.exit(1)
