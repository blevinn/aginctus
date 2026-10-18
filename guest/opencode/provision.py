#!/usr/bin/env python3
"""Install a private OpenCode gateway configuration from trusted Incus stdin."""
import json
import os
from pathlib import Path
import pwd
import stat
import sys
import tempfile

from gateway_endpoint import validate_endpoint, validate_key, validate_model

DIRECTORY = Path("/var/lib/aginctus/opencode")


def atomic(directory, name, data, uid, gid):
    fd, tmp = tempfile.mkstemp(prefix=".gateway-", dir=directory)
    try:
        os.fchmod(fd, 0o600)
        os.fchown(fd, uid, gid)
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(tmp, directory / name)
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)


def install(payload, directory=DIRECTORY):
    config = json.loads(payload)
    if not isinstance(config, dict) or set(config) != {"endpoint", "model", "key"}:
        raise ValueError("invalid input fields")
    endpoint, model, key = (config[k] for k in ("endpoint", "model", "key"))
    if not all(isinstance(v, str) for v in (endpoint, model, key)):
        raise ValueError("invalid types")
    validate_endpoint(endpoint)
    validate_model(model)
    validate_key(key)
    account = pwd.getpwnam("agent")
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    info = directory.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o077:
        raise ValueError("unsafe directory")
    os.chown(directory, account.pw_uid, account.pw_gid)
    os.chmod(directory, 0o700)
    atomic(directory, "workload-key", key + "\n", account.pw_uid, account.pw_gid)
    atomic(directory, "gateway.json", json.dumps({"endpoint": endpoint, "model": model, "protocol": "responses"}) + "\n", account.pw_uid, account.pw_gid)


if __name__ == "__main__":
    try:
        if os.geteuid() != 0:
            raise ValueError("root required")
        install(sys.stdin.read(16384))
    except (OSError, ValueError, KeyError, json.JSONDecodeError):
        print("aginctus opencode: provisioning failed", file=sys.stderr)
        sys.exit(1)
