"""Combine explicitly supplied test outcomes into an honest coverage snapshot.

Never runs checks, accepts raw live receipts, or treats local files as attestations.
Unprovided checks are NOT_RUN, including all live/billable acceptance claims.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import stat
from pathlib import Path

from scripts.checks import ci_summary, vm_evidence

MAX_INPUT_BYTES = 65536
TARGET = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,99}\Z")
SOURCES = (
    ("hermetic", "hermetic_json"),
    ("vm", "vm_run_id"),
    ("preflight/pilot-topology", "pilot_topology_json"),
    ("preflight/opencode-runtime", "opencode_runtime_json"),
)
LIVE_CHECKS = (
    "live/agent_tools",
    "live/upstream_inference",
    "live/credential_egress",
    "live/cleanup",
)
UNVERIFIED = "not_verified"


def read_input(path: Path) -> object:
    """Bound explicit, potentially untrusted JSON without following file symlinks."""
    flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
    fd = os.open(path, flags)
    with os.fdopen(fd, "rb") as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_size > MAX_INPUT_BYTES:
            raise ValueError("not a bounded regular file")
        data = source.read(MAX_INPUT_BYTES + 1)
    if len(data) > MAX_INPUT_BYTES:
        raise ValueError("oversized input")
    return json.loads(data.decode("utf-8"))


def validate_preflight(source: object, check: str) -> str:
    """Only return status after the complete allowlisted preflight shape matches."""
    if not isinstance(source, dict):
        raise ValueError("invalid preflight")
    status = source.get("status")
    if (
        source.get("schema_version") != 1
        or type(source.get("schema_version")) is not int
        or source.get("profile") != "pilot_preflight"
        or source.get("verification_tier") != "read_only_pilot_preflight"
        or source.get("upstream_inference") != UNVERIFIED
        or source.get("gateway_authorization") != UNVERIFIED
        or source.get("egress_policy") != UNVERIFIED
        or status not in ("pass", "fail", "error")
    ):
        raise ValueError("invalid preflight envelope")
    rows = source.get("results")
    if not isinstance(rows, list) or len(rows) != 1 or not isinstance(rows[0], dict):
        raise ValueError("invalid preflight rows")
    row = rows[0]
    code = row.get("exit_code")
    if (
        row.get("id") != f"preflight/{check}"
        or row.get("status") != status
        or (
            (status == "pass" and (type(code) is not int or code != 0))
            or (status == "fail" and (type(code) is not int or code == 0))
            or (status == "error" and code is not None)
        )
        or source.get("summary") != {
            "passed": int(status == "pass"),
            "failed": int(status == "fail"),
            "errors": int(status == "error"),
        }
    ):
        raise ValueError("inconsistent preflight result")
    target = source.get("target")
    if target is not None and (
        not isinstance(target, str)
        or (target != "configured_pilot" and not TARGET.fullmatch(target))
    ):
        raise ValueError("invalid preflight target")
    for object_ in (source, row):
        duration = object_.get("duration_ms")
        if duration is not None and (type(duration) is not int or duration < 0):
            raise ValueError("invalid preflight duration")
    return status


def observed(label: str, value: str, *, detail: str | None = None) -> dict:
    result = {"id": label, "status": value, "evidence": "local_report"}
    if detail is not None:
        result["detail"] = detail
    return result


def normalize_sources(
    *, hermetic_json: Path | None = None, vm_run_id: str | None = None,
    pilot_topology_json: Path | None = None,
    opencode_runtime_json: Path | None = None,
    vm_root: Path = vm_evidence.ROOT,
) -> dict:
    """Read only named evidence. Missing sources are unexecuted, not passed."""
    sources = {
        "hermetic": hermetic_json,
        "vm": vm_run_id,
        "preflight/pilot-topology": pilot_topology_json,
        "preflight/opencode-runtime": opencode_runtime_json,
    }
    results: list[dict] = []
    for name, _ in SOURCES:
        selected = sources[name]
        if selected is None:
            results.append({"id": name, "status": "not_run", "evidence": "none"})
            continue
        try:
            if name == "hermetic":
                parsed = ci_summary.validate_report(read_input(selected))
                if parsed is None:
                    raise ValueError("invalid hermetic evidence")
                status = parsed["status"]
                row = observed(name, status)
            elif name == "vm":
                parsed = vm_evidence.normalize(
                    vm_evidence.read_private_result(vm_root, selected), selected
                )
                status = parsed["status"]
                row = observed(name, status, detail=parsed["results"][0]["id"])
            else:
                check = name.removeprefix("preflight/")
                status = validate_preflight(read_input(selected), check)
                row = observed(name, status)
        except (OSError, UnicodeError, ValueError, TypeError):
            row = {"id": name, "status": "error", "evidence": "invalid"}
        results.append(row)

    # No currently trusted live evidence importer exists. A passing local
    # file or synthetic acceptance marker MUST NEVER fill one of these rows.
    for check in LIVE_CHECKS:
        results.append({"id": check, "status": "not_run", "evidence": "none"})

    counts = {
        "passed": sum(x["status"] == "pass" for x in results),
        "failed": sum(x["status"] == "fail" for x in results),
        "errors": sum(x["status"] == "error" for x in results),
        "not_run": sum(x["status"] == "not_run" for x in results),
    }
    if counts["errors"]:
        status = "error"
    elif counts["failed"]:
        status = "fail"
    else:
        status = "incomplete"
    return {
        "schema_version": 1,
        "profile": "coverage",
        "status": status,
        "summary": counts,
        "results": results,
        "upstream_inference": UNVERIFIED,
        "gateway_authorization": UNVERIFIED,
        "egress_policy": UNVERIFIED,
        "cleanup": UNVERIFIED,
        "complete_acceptance": False,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--format", choices=("text", "json"), default="text")
    parser.add_argument("--hermetic-json", type=Path)
    parser.add_argument("--vm-run-id")
    parser.add_argument("--pilot-topology-json", type=Path)
    parser.add_argument("--opencode-runtime-json", type=Path)
    args = parser.parse_args(argv)
    result = normalize_sources(
        hermetic_json=args.hermetic_json,
        vm_run_id=args.vm_run_id,
        pilot_topology_json=args.pilot_topology_json,
        opencode_runtime_json=args.opencode_runtime_json,
    )
    if args.format == "json":
        print(json.dumps(result, separators=(",", ":")))
    else:
        for item in result["results"]:
            suffix = f"  {item['detail']}" if "detail" in item else ""
            print(f"{item['status'].upper():10} {item['id']}{suffix}")
        s = result["summary"]
        print(
            f"Coverage: {s['passed']} passed, {s['failed']} failed, "
            f"{s['errors']} errors, {s['not_run']} not run"
        )
        print("NOT VERIFIED: live acceptance, upstream inference, egress, cleanup")
    # Coverage deliberately cannot become an acceptance success until an
    # independently trustworthy live evidence protocol is implemented.
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
