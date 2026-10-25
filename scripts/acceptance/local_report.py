"""Private, bounded host-observed outcome records for the OpenCode acceptance runner.

The shell harness calls this after its own checks, never the agent or guest.
This does not verify live upstream inference or independently prove cleanup.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import stat
import sys
from contextlib import ExitStack
from datetime import datetime, timezone
from pathlib import Path

RUN_ID = re.compile(r"aginctus-acceptance\.[A-Za-z0-9]{8}\Z")
STATES = ("pass", "fail", "not_run")
MAX_BYTES = 16 * 1024
SOURCE_ROOT = Path(__file__).resolve().parents[2]


def _directory(fd: int) -> None:
    meta = os.fstat(fd)
    if (
        not stat.S_ISDIR(meta.st_mode)
        or meta.st_uid != os.getuid()
        or meta.st_mode & 0o077
    ):
        raise ValueError("unsafe evidence directory")


def validate_root(output_root: Path) -> None:
    """Require an already provisioned private directory outside the checkout."""
    if not output_root.is_absolute() or output_root.is_symlink():
        raise ValueError("invalid evidence root")
    if output_root.resolve().is_relative_to(SOURCE_ROOT):
        raise ValueError("checkout is not an evidence root")
    fd = os.open(output_root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        _directory(fd)
    finally:
        os.close(fd)


def record(
    output_root: Path,
    *,
    run_id: str,
    mode: str,
    exit_code: int,
    fixture: str,
    tests: str,
    stop: str,
    close: str,
) -> Path:
    """Record bounded, fixed-schema results outside the agent's workspace.

    Inputs come from the host harness' own branch/command exit statuses.
    Statuses are not remotely authenticated and must not be accepted from
    an agent-controlled process as proof of actual execution.
    """
    if (
        not RUN_ID.fullmatch(run_id)
        or mode not in ("mock", "live")
        or type(exit_code) is not int
        or not 0 <= exit_code <= 255
        or any(value not in STATES for value in (fixture, tests, stop, close))
    ):
        raise ValueError("invalid runner observation")
    validate_root(output_root)

    record_status = "fail" if exit_code != 0 else "incomplete"
    # A contradictory zero exit must never produce a success-like outcome.
    if exit_code == 0 and (fixture != "pass" or tests != "pass"):
        record_status = "error"
    observations = [
        {"id": "fixture_change", "status": fixture, "source": "host_fixture_check"},
        {"id": "workspace_tests", "status": tests, "source": "host_go_test"},
        {"id": "agent_tools", "status": "not_run", "source": "not_independently_observed"},
        {"id": "upstream_inference", "status": "not_run", "source": "not_independently_observed"},
        {"id": "credential_egress", "status": "not_run", "source": "not_independently_observed"},
        {"id": "cleanup", "status": "not_run", "source": "not_independently_observed"},
    ]
    payload = {
        "schema_version": 1,
        "profile": "local_acceptance",
        "run_id": run_id,
        "scenario": "opencode",
        "mode": mode,
        "created_utc": datetime.now(timezone.utc).isoformat(),
        "status": record_status,
        "harness_exit_code": exit_code,
        "verification_tier": "mock_orchestration" if mode == "mock" else "local_live_partial",
        "results": observations,
        # The exit status of stop/close is NOT proof that owned resources are gone.
        "cleanup_commands": {"stop": stop, "close": close},
        "upstream_inference": "not_verified",
        "credential_egress": "not_verified",
        "cleanup": "not_verified",
        "complete_acceptance": False,
    }
    encoded = (json.dumps(payload, sort_keys=True, separators=(",", ":")) + "\n").encode()
    if len(encoded) > MAX_BYTES:
        raise ValueError("oversized evidence record")

    directory_flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    with ExitStack() as stack:
        parent = os.open(output_root, directory_flags)
        stack.callback(os.close, parent)
        _directory(parent)
        os.mkdir(run_id, mode=0o700, dir_fd=parent)
        child = os.open(run_id, directory_flags, dir_fd=parent)
        stack.callback(os.close, child)
        _directory(child)
        fd = os.open(
            "result.json", os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
            0o600, dir_fd=child,
        )
        with os.fdopen(fd, "wb") as output:
            output.write(encoded)
            output.flush()
            os.fsync(output.fileno())
        os.fsync(child)
        os.fsync(parent)
    return output_root / run_id / "result.json"


def main(argv: list[str] | None = None) -> int:
    if argv is None:
        argv = sys.argv[1:]
    # This pure preflight runs before an optionally billable live acceptance.
    if len(argv) == 2 and argv[0] == "--check-root":
        try:
            validate_root(Path(argv[1]))
        except (OSError, ValueError, RuntimeError):
            print("ERROR: unsafe acceptance evidence root")
            return 1
        return 0
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-root", type=Path, required=True)
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--mode", choices=("mock", "live"), required=True)
    parser.add_argument("--exit-code", type=int, required=True)
    for field in ("fixture", "tests", "stop", "close"):
        parser.add_argument(f"--{field}", choices=STATES, required=True)
    args = parser.parse_args(argv)
    try:
        result = record(
            args.output_root, run_id=args.run_id, mode=args.mode,
            exit_code=args.exit_code, fixture=args.fixture, tests=args.tests,
            stop=args.stop, close=args.close,
        )
    except (OSError, ValueError, RuntimeError):
        # No untrusted path, exception, child output or environment values.
        print("ERROR: local acceptance report unavailable or unsafe")
        return 1
    # Print no absolute path: it might reveal an operator directory.
    print(f"Acceptance evidence saved for {result.parent.name} (live inference not verified)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
