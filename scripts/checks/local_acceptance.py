"""Read an already recorded, private local acceptance run without executing checks.

Never imports agent-written evidence, invokes live phases, or claims that
fixture tests or stop/close command exits prove real upstream inference.
"""

from __future__ import annotations

import argparse
import json
import os
import stat
from contextlib import ExitStack
from datetime import datetime, timedelta
from pathlib import Path

from scripts.acceptance import local_report

ROWS = (
    ("fixture_change", "host_fixture_check"),
    ("workspace_tests", "host_go_test"),
    ("agent_tools", "not_independently_observed"),
    ("upstream_inference", "not_independently_observed"),
    ("credential_egress", "not_independently_observed"),
    ("cleanup", "not_independently_observed"),
)
UNVERIFIED = "not_verified"


def _private(fd: int, *, directory: bool) -> None:
    metadata = os.fstat(fd)
    expected = stat.S_ISDIR if directory else stat.S_ISREG
    if (
        not expected(metadata.st_mode)
        or metadata.st_uid != os.getuid()
        or metadata.st_mode & 0o077
        or (not directory and metadata.st_size > local_report.MAX_BYTES)
    ):
        raise ValueError("invalid local acceptance evidence")


def _unique_pairs(pairs: list[tuple[str, object]]) -> dict:
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate field")
        result[key] = value
    return result


def read_private_result(root: Path, run_id: str) -> dict:
    """Read only <trusted-root>/<exact-run-id>/result.json without symlinks."""
    if not isinstance(run_id, str) or not local_report.RUN_ID.fullmatch(run_id):
        raise ValueError("invalid acceptance run ID")
    local_report.validate_root(root)
    directory_flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    file_flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
    with ExitStack() as stack:
        parent = os.open(root, directory_flags)
        stack.callback(os.close, parent)
        _private(parent, directory=True)
        run_directory = os.open(run_id, directory_flags, dir_fd=parent)
        stack.callback(os.close, run_directory)
        _private(run_directory, directory=True)
        file = os.open("result.json", file_flags, dir_fd=run_directory)
        stack.callback(os.close, file)
        _private(file, directory=False)
        with os.fdopen(os.dup(file), "rb") as source:
            data = source.read(local_report.MAX_BYTES + 1)
    if len(data) > local_report.MAX_BYTES:
        raise ValueError("oversized local acceptance evidence")
    parsed = json.loads(data.decode("utf-8"), object_pairs_hook=_unique_pairs)
    if not isinstance(parsed, dict):
        raise ValueError("invalid local acceptance record")
    return parsed


def normalize(record: dict, run_id: str) -> dict:
    """Allowlist only host-observed outcomes; no live proof can be imported."""
    mode = record.get("mode")
    status = record.get("status")
    code = record.get("harness_exit_code")
    created = record.get("created_utc")
    results = record.get("results")
    commands = record.get("cleanup_commands")
    if (
        type(record.get("schema_version")) is not int
        or record["schema_version"] != 1
        or record.get("profile") != "local_acceptance"
        or record.get("run_id") != run_id
        or record.get("scenario") != "opencode"
        or mode not in ("mock", "live")
        or record.get("verification_tier")
        != ("mock_orchestration" if mode == "mock" else "local_live_partial")
        or status not in ("incomplete", "fail", "error")
        or type(code) is not int
        or not 0 <= code <= 255
        or record.get("upstream_inference") != UNVERIFIED
        or record.get("credential_egress") != UNVERIFIED
        or record.get("cleanup") != UNVERIFIED
        or record.get("complete_acceptance") is not False
        or not isinstance(results, list)
        or len(results) != len(ROWS)
        or not isinstance(commands, dict)
        or set(commands) != {"stop", "close"}
        or any(value not in local_report.STATES for value in commands.values())
        or not isinstance(created, str)
    ):
        raise ValueError("invalid local acceptance envelope")
    for item, (expected_id, expected_source) in zip(results, ROWS, strict=True):
        if (
            not isinstance(item, dict)
            or item.get("id") != expected_id
            or item.get("source") != expected_source
            or item.get("status") != (
                "not_run" if expected_source == "not_independently_observed"
                else item.get("status")
            )
            or item.get("status") not in local_report.STATES
        ):
            raise ValueError("invalid local acceptance check")
    fixture, tests = results[0]["status"], results[1]["status"]
    expected_status = (
        "fail" if code != 0 else
        "incomplete" if fixture == "pass" and tests == "pass" else "error"
    )
    if status != expected_status:
        raise ValueError("inconsistent local acceptance result")
    if code == 0 and any(value != "pass" for value in commands.values()):
        raise ValueError("missing cleanup command outcome")
    try:
        timestamp = datetime.fromisoformat(created)
    except ValueError as exc:
        raise ValueError("invalid acceptance time") from exc
    if timestamp.tzinfo is None or timestamp.utcoffset() != timedelta(0):
        raise ValueError("expected UTC acceptance time")

    observed = [
        {"id": f"acceptance/{item['id']}", "status": item["status"]}
        for item in results[:2]
    ]
    observed.extend(
        {"id": f"acceptance/{phase}_command", "status": commands[phase]}
        for phase in ("stop", "close")
    )
    counts = {
        "passed": sum(row["status"] == "pass" for row in observed),
        "failed": sum(row["status"] == "fail" for row in observed),
        "errors": 0,
        "not_run": sum(row["status"] == "not_run" for row in observed),
    }
    return {
        "schema_version": 1,
        "profile": "local_acceptance_view",
        "status": status,
        "summary": counts,
        "results": observed,
        "source": {
            "run_id": run_id,
            "mode": mode,
            "created_utc": timestamp.isoformat(),
        },
        "verification_tier": record["verification_tier"],
        "agent_tools": UNVERIFIED,
        "upstream_inference": UNVERIFIED,
        "credential_egress": UNVERIFIED,
        "cleanup": UNVERIFIED,
        "complete_acceptance": False,
    }


def unavailable() -> dict:
    return {
        "schema_version": 1,
        "profile": "local_acceptance_view",
        "status": "error",
        "summary": {"passed": 0, "failed": 0, "errors": 1, "not_run": 0},
        "results": [{"id": "acceptance/evidence", "status": "error"}],
        "agent_tools": UNVERIFIED,
        "upstream_inference": UNVERIFIED,
        "credential_egress": UNVERIFIED,
        "cleanup": UNVERIFIED,
        "complete_acceptance": False,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run_id", help="exact existing acceptance run ID")
    parser.add_argument("--format", choices=("text", "json"), default="text")
    args = parser.parse_args(argv)
    try:
        root = Path(os.environ["AGINCTUS_ACCEPTANCE_REPORT_ROOT"])
        report = normalize(read_private_result(root, args.run_id), args.run_id)
    except (KeyError, OSError, TypeError, ValueError, UnicodeError):
        report = unavailable()
    if args.format == "json":
        print(json.dumps(report, separators=(",", ":")))
    else:
        for row in report["results"]:
            print(f"{row['status'].upper():10} {row['id']}")
        print("NOT VERIFIED: agent tools, real upstream inference, egress, cleanup")
    # An incomplete local record is never acceptance sign-off.
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
