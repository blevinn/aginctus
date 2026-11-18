"""Bounded synthetic full-stack observations, never payloads or attestation."""
from __future__ import annotations

import json
import os
import stat
from pathlib import Path

PHASES = ("preflight", "open", "start", "prompt", "wait", "credential-check",
          "gateway-evidence", "stop", "close")
GATES = ("synthetic_setup", "tool_transcript", "fixture_diff", "independent_test",
         "credential_scope", "gateway_correlation", "workload_network",
         "failure_cleanup_control", "owned_fixture_removed", "foreign_workspaces_preserved",
         "scoped_key_revoked", "workload_key_absent", "model_removed", "listener_stopped",
         "storage_preserved", "external_traffic", "external_deny")
MAX_BYTES = 4096
GUEST_ROOT = Path("/run/aginctus-full-stack")
ARTIFACT = "full-stack.json"


def normalize(record):
    """Reject unknown fields/values, then copy only fixed bounded observations."""
    if (not isinstance(record, dict)
            or set(record) != {"schema_version", "source", "phases", "gates"}
            or type(record["schema_version"]) is not int or record["schema_version"] != 1
            or record["source"] != "guest_observation_not_attestation"):
        raise ValueError("invalid full-stack evidence")
    result = {"schema_version": 1, "source": "guest_observation_not_attestation"}
    for section, names in (("phases", PHASES), ("gates", GATES)):
        values = record[section]
        if (not isinstance(values, dict) or set(values) != set(names)
                or any(values[name] not in ("pass", "fail", "not_verified") for name in names)):
            raise ValueError("invalid full-stack outcomes")
        result[section] = {name: values[name] for name in names}
    return result


def read_artifact(root, name=ARTIFACT):
    """Open only a fixed file in a private owned directory without following links."""
    if name not in (ARTIFACT, "result.json"):
        raise ValueError("invalid artifact name")
    directory = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        require_private(directory, directory=True)
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory)
        try:
            require_private(fd, directory=False)
            with os.fdopen(os.dup(fd), "rb") as source:
                payload = source.read(MAX_BYTES + 1)
        finally:
            os.close(fd)
    finally:
        os.close(directory)
    if len(payload) > MAX_BYTES:
        raise ValueError("oversized full-stack evidence")
    return normalize(json.loads(payload))


def complete(record):
    record = normalize(record)
    return all(value == "pass" for section in ("phases", "gates")
               for value in record[section].values())


def require_private(fd, *, directory):
    """Require a current-user-owned private directory or bounded regular file."""
    info = os.fstat(fd)
    if (not (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode))
            or info.st_uid != os.getuid() or info.st_mode & 0o077
            or (not directory and (info.st_size > MAX_BYTES or info.st_nlink != 1))):
        raise ValueError("invalid full-stack artifact permissions or type")


def write_artifact(root, record, name=ARTIFACT):
    """Exclusive, private, fixed-name output; no guest-selected paths or content."""
    if name not in (ARTIFACT, "result.json"):
        raise ValueError("invalid artifact name")
    payload = (json.dumps(normalize(record), sort_keys=True) + "\n").encode()
    if len(payload) > MAX_BYTES:
        raise ValueError("oversized full-stack evidence")
    directory = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        require_private(directory, directory=True)
        fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                     0o600, dir_fd=directory)
        with os.fdopen(fd, "wb") as target:
            target.write(payload)
    finally:
        os.close(directory)


class Evidence:
    def __init__(self):
        self.record = {"schema_version": 1, "source": "guest_observation_not_attestation",
                       "phases": dict.fromkeys(PHASES, "not_verified"),
                       "gates": dict.fromkeys(GATES, "not_verified")}

    def begin(self, name, section="gates"):
        if name not in self.record[section]:
            raise ValueError("unknown observation")
        self.record[section][name] = "fail"

    def passed(self, name, section="gates"):
        if name not in self.record[section]:
            raise ValueError("unknown observation")
        self.record[section][name] = "pass"


if __name__ == "__main__":
    # The immutable driver calls this fixed guest reader, never a guest path argument.
    try:
        print(json.dumps(read_artifact(GUEST_ROOT, "result.json")))
    except (OSError, ValueError, UnicodeError):
        raise SystemExit(1)
