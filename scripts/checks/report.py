#!/usr/bin/env python3
"""Summarize the existing safe hermetic `just check` suites.

Machine output never contains subprocess stdout/stderr. Failed suites can be
rerun explicitly with their fixed, human-readable command.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SUITES = (
    "go",
    "lint",
    "python",
    "shell-tests",
    "shell",
    "markdown",
    "docs-examples",
    "nix",
)
SAFE_ENV = {
    "AGINCTUS_INCUS_INTEGRATION": "0",
    "AGINCTUS_INCUS_DISPOSABLE": "NO",
    "AGINCTUS_ACCEPTANCE_LIVE": "",
    "AGINCTUS_ACCEPTANCE_LIVE_HOOKS": "",
    "ACCEPTANCE_TEST_FAIL": "",
    "GOFLAGS": "",
    "GOPROXY": "off",
    "GOSUMDB": "off",
    "GOTOOLCHAIN": "local",
}


def run_suite(suite: str, env: dict[str, str], verbose: bool) -> dict:
    argv = ("just", "check", suite)
    start = time.monotonic()
    try:
        completed = subprocess.run(
            argv,
            cwd=ROOT,
            env=env,
            stdin=subprocess.DEVNULL,
            stdout=None if verbose else subprocess.DEVNULL,
            stderr=None if verbose else subprocess.DEVNULL,
            check=False,
        )
    except OSError:
        # No OS exception details: paths and environment must not leak to JSON.
        status, exit_code = "error", None
    else:
        exit_code = completed.returncode
        status = "pass" if exit_code == 0 else "fail"
    return {
        "id": suite,
        "status": status,
        "exit_code": exit_code,
        "duration_ms": round((time.monotonic() - start) * 1000),
        "rerun": " ".join(argv),
    }


def run(*, output_format: str, verbose: bool) -> int:
    env = dict(os.environ)
    env.update(SAFE_ENV)
    start = time.monotonic()
    results = []
    for suite in SUITES:
        result = run_suite(suite, env, verbose)
        results.append(result)
        if output_format == "text":
            print(
                f"{result['status'].upper():5}  {suite:16}  "
                f"{result['duration_ms'] / 1000:7.1f}s",
                flush=True,
            )
    passed = sum(item["status"] == "pass" for item in results)
    failed = sum(item["status"] == "fail" for item in results)
    errors = sum(item["status"] == "error" for item in results)
    report = {
        "schema_version": 1,
        "profile": "hermetic",
        "status": "pass" if passed == len(SUITES) else "fail",
        "duration_ms": round((time.monotonic() - start) * 1000),
        "summary": {"passed": passed, "failed": failed, "errors": errors},
        "results": results,
    }
    if output_format == "json":
        print(json.dumps(report, separators=(",", ":")))
    else:
        print(f"Hermetic: {passed} passed, {failed} failed, {errors} errors")
        for item in results:
            if item["status"] != "pass":
                print(f"  To diagnose {item['id']}: {item['rerun']}")
    return 0 if report["status"] == "pass" else 1


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--format", choices=("text", "json"), default="text")
    parser.add_argument(
        "--verbose", action="store_true", help="stream unfiltered local test output"
    )
    args = parser.parse_args()
    if args.format == "json" and args.verbose:
        parser.error("--verbose cannot be combined with --format json")
    return run(output_format=args.format, verbose=args.verbose)


if __name__ == "__main__":
    raise SystemExit(main())
