"""Summarize the existing safe hermetic `just check` suites.

Machine output never contains subprocess stdout/stderr. Failed suites can be
rerun explicitly with their fixed, human-readable command.
"""

from __future__ import annotations

import argparse
import json
import os
import re
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


MAX_LINE_BYTES = 4096
MAX_CASES = 8
CASE_CHARS = re.compile(r"[A-Za-z0-9_./:-]{1,192}\Z")
PYTHON_FAILURE = re.compile(
    r"(?:FAIL|ERROR): [A-Za-z_][A-Za-z0-9_]* "
    r"\(([A-Za-z_][A-Za-z0-9_.]{1,190})\)\Z"
)


class FailureCases:
    """Drain output in bounded lines, retaining only safe failure identifiers."""

    def __init__(self, suite: str) -> None:
        self.suite = suite
        self.cases: list[dict[str, str]] = []
        self.truncated = False
        self._seen: set[str] = set()
        self._line = bytearray()
        self._discard = False

    def feed(self, data: bytes) -> None:
        # Split only a fixed-size input chunk, never a complete subprocess log.
        parts = data.split(b"\n")
        for part in parts[:-1]:
            self._append(part, newline=True)
        self._append(parts[-1], newline=False)

    def _append(self, chunk: bytes, *, newline: bool) -> None:
        if not self._discard and len(self._line) + len(chunk) <= MAX_LINE_BYTES:
            self._line.extend(chunk)
        else:
            self._discard = True
        if newline:
            if not self._discard:
                self._parse(self._line.decode("utf-8", errors="replace").rstrip("\r"))
            self._line.clear()
            self._discard = False

    def _add(self, identifier: str, kind: str) -> None:
        if not CASE_CHARS.fullmatch(identifier) or identifier in self._seen:
            return
        self._seen.add(identifier)
        if len(self.cases) >= MAX_CASES:
            self.truncated = True
            return
        self.cases.append({"id": identifier, "kind": kind})

    def _parse(self, line: str) -> None:
        if self.suite == "python":
            match = PYTHON_FAILURE.fullmatch(line)
            if match:
                kind = "error" if line.startswith("ERROR:") else "fail"
                self._add(match.group(1), kind)
        elif self.suite == "go" and line.startswith("{"):
            try:
                event = json.loads(line)
            except ValueError:
                return
            if not isinstance(event, dict) or event.get("Action") != "fail":
                return
            package = event.get("Package")
            test = event.get("Test")
            if (
                isinstance(package, str)
                and isinstance(test, str)
                and (
                    package == "github.com/blevinn/aginctus"
                    or package.startswith("github.com/blevinn/aginctus/")
                )
                and CASE_CHARS.fullmatch(test)
            ):
                self._add(f"{package}:{test}", "fail")


def run_suite(suite: str, env: dict[str, str], verbose: bool) -> dict:
    argv = ("just", "check", suite)
    start = time.monotonic()
    cases = FailureCases(suite)
    try:
        child_env = dict(env)
        # Only the reporting runner opts into Go's JSON test event stream.
        child_env["AGINCTUS_CHECK_STRUCTURED"] = "1" if suite == "go" else "0"
        if verbose:
            exit_code = subprocess.run(
                argv, cwd=ROOT, env=child_env, stdin=subprocess.DEVNULL, check=False
            ).returncode
        else:
            # Drain merged child streams without retaining raw lines or logs.
            with subprocess.Popen(
                argv,
                cwd=ROOT,
                env=child_env,
                stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
            ) as process:
                assert process.stdout is not None
                while chunk := process.stdout.read(4096):
                    cases.feed(chunk)
                exit_code = process.wait()
        status = "pass" if exit_code == 0 else "fail"
    except OSError:
        # Never include launcher exception details, which may contain secrets.
        status, exit_code = "error", None
    result = {
        "id": suite,
        "status": status,
        "exit_code": exit_code,
        "duration_ms": round((time.monotonic() - start) * 1000),
        "rerun": " ".join(argv),
    }
    # Case IDs are best-effort diagnostics, not proof of an individual failure.
    if status == "fail" and cases.cases:
        result["failed_cases"] = cases.cases
        if cases.truncated:
            result["cases_truncated"] = True
    return result


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
            for case in result.get("failed_cases", []):
                print(f"         {case['kind'].upper()}: {case['id']}")
            if result.get("cases_truncated"):
                print("         (additional failed cases omitted)")
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
