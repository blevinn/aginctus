"""One-shot authenticated Incus exec protocol used by apply and provider login."""

import hashlib
import json
import os
import stat
import sys
from pathlib import Path

from siwc import MAX_STATE, Error, OwnerBusy, Store, private_read

ROOT = Path("/var/lib/aginctus/chatgpt")


def runtime_identity():
    identities = set()
    for process in Path("/proc").glob("[0-9]*"):
        try:
            args = (process / "cmdline").read_bytes().split(b"\0")
            executable = (process / "exe").resolve().name
            if not executable.startswith("python") or not any(
                    arg.rsplit(b"/", 1)[-1] == b"litellm" or arg.startswith(b"litellm.proxy.")
                    for arg in args):
                continue
            fields = dict(line.split(":", 1) for line in (process / "status").read_text().splitlines() if ":" in line)
            identities.add((int(fields["Uid"].split()[1]), int(fields["Gid"].split()[1])))
        except (FileNotFoundError, ProcessLookupError):
            continue
    if len(identities) != 1:
        raise Error("Cannot verify one LiteLLM runtime identity")
    return identities.pop()


def open_runtime_store():
    uid, gid = runtime_identity()
    if not any(line.split()[4] == str(ROOT) for line in Path("/proc/self/mountinfo").read_text().splitlines()):
        raise Error("SIWC requires its audited credential volume")
    info = ROOT.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid not in {0, uid}:
        raise Error("Unexpected credential volume owner")
    os.chown(ROOT, uid, gid)
    os.chmod(ROOT, 0o700)
    directory = ROOT / "siwc"
    try:
        directory.mkdir(mode=0o700)
    except FileExistsError:
        pass
    else:
        os.chown(directory, uid, gid)
    os.setgroups([])
    os.setgid(gid)
    os.setuid(uid)
    return Store(directory)


def exchange(store, source, destination, require_runtime=False):
    with store.lock("admin", timeout=30):
        if require_runtime:
            lease = store.lock("runtime", timeout=0)
            try:
                lease.__enter__()
            except OwnerBusy:
                pass
            else:
                lease.__exit__(None, None, None)
                raise Error("Managed SIWC runtime is not ready; run gateway apply")
        store.accept_pending()
        current = private_read(store.root / "state.json")
        digest = hashlib.sha256(current).hexdigest()
        destination.write(json.dumps({"state": json.loads(current), "base_digest": digest}) + "\n")
        destination.flush()
        line = source.readline(MAX_STATE + 1)
        if not line:
            return
        if len(line) > MAX_STATE or not line.endswith("\n"):
            raise Error("Invalid managed SIWC update size")
        pending = json.loads(line)
        if pending.get("base_digest") != digest:
            raise Error("Managed SIWC credential generation changed")
        store.write(pending, "pending.json")
        store.accept_pending()
        destination.write('{"ok":true}\n')
        destination.flush()


if __name__ == "__main__":
    try:
        exchange(open_runtime_store(), sys.stdin, sys.stdout, require_runtime=sys.argv[-1] != "prepare")
    except Exception:
        print("Managed SIWC operation failed; credentials were not printed to diagnostics", file=sys.stderr)
        sys.exit(1)
