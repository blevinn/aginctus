"""Read an existing private VM harness result as concise synthetic-only evidence.

This command NEVER invokes Nix, Incus, the VM harness or a provider. The
operator must explicitly select an existing 24-hex-character run ID.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import stat
from contextlib import ExitStack
from datetime import datetime, timedelta
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2] / ".vm-harness-results"
SCENARIOS = (
    "smoke",
    "storage-network",
    "runtime-lifecycle",
    "gateway-topology",
    "ownership-recovery",
)
RUN_ID = re.compile(r"[0-9a-f]{24}\Z")
REVISION = re.compile(r"(?:[0-9a-f]{40}|[0-9a-f]{64})\Z")
LOCK_HASH = re.compile(r"[0-9a-f]{64}\Z")
MAX_BYTES = 16 * 1024
TIER = "disposable_guest_no_live_provider"


def _private(fd: int, *, directory: bool) -> None:
    metadata = os.fstat(fd)
    kind_ok = stat.S_ISDIR(metadata.st_mode) if directory else stat.S_ISREG(
        metadata.st_mode
    )
    if (
        not kind_ok
        or metadata.st_uid != os.getuid()
        or metadata.st_mode & 0o077
    ):
        raise ValueError("invalid evidence permissions or file type")
    if not directory and metadata.st_size > MAX_BYTES:
        raise ValueError("oversized evidence record")


def read_private_result(root: Path, run_id: str) -> dict:
    """Read only <owned root>/<known ID>/result.json, never following links."""
    if not RUN_ID.fullmatch(run_id):
        raise ValueError("invalid VM run ID")
    directory_flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    file_flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
    with ExitStack() as stack:
        parent = os.open(root, directory_flags)
        stack.callback(os.close, parent)
        _private(parent, directory=True)
        run_dir = os.open(run_id, directory_flags, dir_fd=parent)
        stack.callback(os.close, run_dir)
        _private(run_dir, directory=True)
        file = os.open("result.json", file_flags, dir_fd=run_dir)
        stack.callback(os.close, file)
        _private(file, directory=False)
        # Duplicate the descriptor so the stack retains ownership of the
        # original. Limit reads even when files mutate after fstat.
        with os.fdopen(os.dup(file), "rb") as source:
            payload = source.read(MAX_BYTES + 1)
    if len(payload) > MAX_BYTES:
        raise ValueError("oversized evidence record")
    record = json.loads(payload.decode("utf-8"))
    if not isinstance(record, dict):
        raise ValueError("invalid evidence object")
    return record


def _optional_hash(value: object, pattern: re.Pattern[str]) -> bool:
    return value is None or (isinstance(value, str) and bool(pattern.fullmatch(value)))


def normalize(record: dict, run_id: str) -> dict:
    """Allowlist known VM v1 metadata; never copy arbitrary source fields."""
    scenario = record.get("scenario")
    status = record.get("status")
    exit_code = record.get("exit_code")
    failure_stage = record.get("failure_stage")
    created = record.get("created_utc")
    if (
        type(record.get("schema_version")) is not int
        or record["schema_version"] != 1
        or record.get("run_id") != run_id
        or scenario not in SCENARIOS
        or status not in ("pass", "fail")
        or type(exit_code) is not int
        or not -255 <= exit_code <= 255
        or (status == "pass") != (exit_code == 0)
        or failure_stage not in ("launch", "build", "runtime", None)
        or (status == "pass" and failure_stage is not None)
        or (status == "fail" and failure_stage is None)
        or record.get("verification_tier") != TIER
        or record.get("upstream_inference") != "not_verified"
        or record.get("cleanup") != "not_verified"
        or not _optional_hash(record.get("git_revision"), REVISION)
        or not _optional_hash(record.get("flake_lock_sha256"), LOCK_HASH)
        or (record.get("git_dirty") is not None and type(record["git_dirty"]) is not bool)
        or not isinstance(created, str)
    ):
        raise ValueError("invalid VM evidence contract")
    # The harness records UTC; reject injected timestamps and local-time claims.
    try:
        timestamp = datetime.fromisoformat(created)
    except ValueError as exc:
        raise ValueError("invalid VM evidence timestamp") from exc
    if timestamp.tzinfo is None or timestamp.utcoffset() != timedelta(0):
        raise ValueError("VM timestamp must be UTC")
    source = {
        "kind": "vm_harness",
        "run_id": run_id,
        "created_utc": timestamp.isoformat(),
        "git_revision": record.get("git_revision"),
        "git_dirty": record.get("git_dirty"),
        "flake_lock_sha256": record.get("flake_lock_sha256"),
    }
    return {
        "schema_version": 1,
        "profile": "disposable_vm",
        "status": status,
        "summary": {
            "passed": int(status == "pass"),
            "failed": int(status == "fail"),
            "errors": 0,
        },
        "results": [
            {
                "id": f"vm/{scenario}",
                "status": status,
                "exit_code": exit_code,
                "rerun": f"just test-vm-{scenario}",
            }
        ],
        "source": source,
        "failure_stage": failure_stage,
        "verification_tier": TIER,
        "upstream_inference": "not_verified",
        "cleanup": "not_verified",
    }


def unavailable() -> dict:
    """Return an error result even when the private evidence is unreadable."""
    return {
        "schema_version": 1,
        "profile": "disposable_vm",
        "status": "error",
        "summary": {"passed": 0, "failed": 0, "errors": 1},
        "results": [{"id": "vm/evidence", "status": "error", "exit_code": None}],
        "verification_tier": "not_verified",
        "upstream_inference": "not_verified",
        "cleanup": "not_verified",
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run_id", help="existing 24-character VM evidence run ID")
    parser.add_argument("--format", choices=("text", "json"), default="text")
    args = parser.parse_args(argv)
    try:
        result = normalize(read_private_result(ROOT, args.run_id), args.run_id)
    except (OSError, ValueError, UnicodeError):
        result = unavailable()

    if args.format == "json":
        print(json.dumps(result, separators=(",", ":")))
    elif result["status"] == "error":
        print("ERROR VM evidence unavailable or invalid (no verification claimed)")
    else:
        suite = result["results"][0]
        print(f"{suite['status'].upper():5} {suite['id']}  exit={suite['exit_code']}")
        print(f"Tier: {TIER}")
        if result["failure_stage"]:
            print(f"Failure stage: {result['failure_stage']}")
        print("Unverified: upstream inference, cleanup")
        if suite["status"] != "pass":
            print(f"Re-run explicitly: {suite['rerun']}")
    return 0 if result["status"] == "pass" else 1


if __name__ == "__main__":
    raise SystemExit(main())
