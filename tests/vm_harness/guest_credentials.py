"""Test-only guest-root credential observation/removal; secrets arrive on stdin.

This observes a bounded process/filesystem snapshot, not a compromised-guest
attestation. Unknown/oversized/unreadable state fails rather than being skipped.
"""
from __future__ import annotations

import json
import os
import pwd
import re
import stat
import sys
import tempfile
from pathlib import Path

DIRECTORY = Path("/var/lib/aginctus/opencode")
SCAN_ROOTS = (
    "/var/lib/aginctus/state/system", "/var/lib/aginctus/state/user",
    "/var/lib/herdr", "/home/agent/.config", "/home/agent/.local/share/opencode",
)
FORBIDDEN_MOUNTS = (
    "/run/aginctus-vm-gateway-state", "/var/lib/aginctus/gateway",
    "/var/lib/aginctus/oauth", "/run/secrets",
)
MAX_FILE = 64 * 1024 * 1024
MAX_TOTAL = 256 * 1024 * 1024


class Rejected(ValueError):
    pass


def private_directory(directory, uid):
    meta = directory.lstat()
    if (not stat.S_ISDIR(meta.st_mode) or meta.st_uid != uid
            or stat.S_IMODE(meta.st_mode) != 0o700):
        raise Rejected("unsafe credential directory")


def private_read(path, uid):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        meta = os.fstat(stream.fileno())
        if (not stat.S_ISREG(meta.st_mode) or meta.st_uid != uid
                or stat.S_IMODE(meta.st_mode) != 0o600 or meta.st_nlink != 1
                or meta.st_size > 16384):
            raise Rejected("unsafe credential file")
        value = stream.read(16385)
    if len(value) > 16384:
        raise Rejected("oversized credential file")
    return value, (meta.st_dev, meta.st_ino)


def inspect_absent(directory=DIRECTORY):
    # A dangling symlink is present, as is a concurrently rotated replacement.
    if os.path.lexists(directory / "workload-key"):
        raise Rejected("workload key remains")


def remove_key(expected, uid, directory=DIRECTORY):
    private_directory(directory, uid)
    path = directory / "workload-key"
    original, identity = private_read(path, uid)
    if original != (expected + "\n").encode():
        raise Rejected("workload key changed; refuse removal")
    # Move into a root-private sibling before deletion, then revalidate the
    # captured inode AND bytes. A rotated source is restored without overwrite;
    # if another replacement exists, preserve the capture for root recovery.
    quarantine = Path(tempfile.mkdtemp(prefix=".vm-key-cleanup-", dir=directory.parent))
    captured = quarantine / "key"
    try:
        os.rename(path, captured)
        try:
            value, captured_identity = private_read(captured, uid)
            if value != original or captured_identity != identity:
                raise Rejected("workload key rotated during removal")
        except (OSError, Rejected):
            os.link(captured, path, follow_symlinks=False)
            captured.unlink()
            raise
        captured.unlink()
        inspect_absent(directory)
    finally:
        # Never recursively remove uncertain captured material.
        if not os.path.lexists(captured):
            quarantine.rmdir()


def reject_bytes(value, forbidden):
    if any(secret in value for secret in forbidden):
        raise Rejected("forbidden credential material observed")


def check_mounts(raw, forbidden):
    reject_bytes(raw, forbidden)
    for line in raw.decode("utf-8").splitlines():
        fields = line.split()
        if len(fields) < 10 or "-" not in fields or len(fields) < fields.index("-") + 4:
            raise Rejected("invalid mount observation")
        # Decode mountinfo's octal escapes before checking known secret mounts.
        mounts = [re.sub(r"\\([0-7]{3})", lambda m: chr(int(m[1], 8)), field) for field in fields]
        reject_bytes("\0".join(mounts).encode(), forbidden)
        if any(mount == root or mount.startswith(root + "/")
               for mount in (mounts[3], mounts[4], mounts[fields.index("-") + 2])
               for root in FORBIDDEN_MOUNTS):
            raise Rejected("gateway secret mount observed")


def scan_tree(root, forbidden, budget):
    if not os.path.lexists(root):
        return
    # Do not silently follow a guest-writable symlink out of the observed tree.
    meta = root.lstat()
    if stat.S_ISLNK(meta.st_mode):
        raise Rejected("symlinked credential observation root")
    if stat.S_ISDIR(meta.st_mode):
        for child in root.iterdir():
            budget[1] += 1
            if budget[1] > 4096:
                raise Rejected("credential observation file limit")
            scan_tree(child, forbidden, budget)
        return
    if stat.S_ISSOCK(meta.st_mode):
        return  # Herdr's runtime socket contains no regular-file bytes.
    if not stat.S_ISREG(meta.st_mode) or meta.st_size > MAX_FILE:
        raise Rejected("unsupported credential observation file")
    fd = os.open(root, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    overlap = max(map(len, forbidden)) - 1
    with os.fdopen(fd, "rb") as stream:
        if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
            raise Rejected("credential observation file changed")
        tail, size = b"", 0
        while chunk := stream.read(65536):
            size += len(chunk)
            budget[0] += len(chunk)
            if size > MAX_FILE or budget[0] > MAX_TOTAL:
                raise Rejected("credential observation byte limit")
            reject_bytes(tail + chunk, forbidden)
            tail = (tail + chunk)[-overlap:]


def audit(payload, uid, directory=DIRECTORY, proc=Path("/proc"), roots=SCAN_ROOTS):
    forbidden = tuple(payload[name].encode() for name in ("master", "provider"))
    if any(len(value) < 8 for value in forbidden) or payload["scoped"].encode() in forbidden:
        raise Rejected("invalid trusted credential selectors")
    private_directory(directory, uid)
    key, _ = private_read(directory / "workload-key", uid)
    config, _ = private_read(directory / "gateway.json", uid)
    reject_bytes(key + config, forbidden)
    if key != (payload["scoped"] + "\n").encode() or json.loads(config) != {
        "endpoint": payload["endpoint"], "model": payload["model"], "protocol": "responses",
    }:
        raise Rejected("workload credential/configuration mismatch")
    pids = [path for path in proc.iterdir() if path.name.isdecimal()]
    if not 1 <= len(pids) <= 4096:
        raise Rejected("process observation limit")
    observed = 0
    for process in pids:
        try:
            for name in ("environ", "cmdline"):
                with (process / name).open("rb") as stream:
                    value = stream.read(1048577)
                if len(value) > 1048576:
                    raise Rejected("process observation byte limit")
                reject_bytes(value, forbidden)
            observed += 1
        except FileNotFoundError:
            if process.exists():
                raise Rejected("incomplete live process observation") from None
    if observed == 0:
        raise Rejected("no processes observed")
    with (proc / "self/mountinfo").open("rb") as stream:
        mounts = stream.read(1048577)
    if len(mounts) > 1048576:
        raise Rejected("mount observation byte limit")
    check_mounts(mounts, forbidden)
    budget = [0, 0]
    scan_tree(directory, forbidden, budget)
    for root in roots:
        scan_tree(Path(root), forbidden, budget)


def main():
    try:
        if (os.geteuid() != 0 or len(sys.argv) != 2
                or not str(Path(__file__).resolve()).startswith("/nix/store/")):
            raise Rejected("immutable guest-root helper required")
        mode = sys.argv[1]
        payload = json.loads(sys.stdin.buffer.read(16385))
        uid = pwd.getpwnam("agent").pw_uid
        if mode == "audit":
            audit(payload, uid)
        elif mode == "remove-key":
            remove_key(payload["scoped"], uid)
        elif mode == "inspect-key-absent":
            private_directory(DIRECTORY, uid)
            inspect_absent()
        else:
            raise Rejected("invalid helper mode")
    except (OSError, ValueError, TypeError, KeyError, RuntimeError):
        print("FAIL: guest credential observation/removal (details suppressed)", file=sys.stderr)
        return 1
    print("PASS: guest credential observation/removal")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
