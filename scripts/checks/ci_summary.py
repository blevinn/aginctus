"""Render the structured hermetic JSON report in GitHub's step summary.

Never print or quote raw child output, untrusted exception text, or report file
content. A missing report is an explicit absence of evidence, not a pass.
"""

from __future__ import annotations

import json
import os
from pathlib import Path

EXPECTED_SUITES = (
    "go",
    "lint",
    "python",
    "shell-tests",
    "shell",
    "markdown",
    "docs-examples",
    "nix",
)
STEP_LABELS = (
    ("Namespace contract", "NAMESPACE_RESULT"),
    ("Hermetic suites", "AGGREGATE_RESULT"),
    ("Gateway parity", "PARITY_RESULT"),
    ("Release package preview", "RELEASE_PREVIEW_RESULT"),
    ("Whitespace", "WHITESPACE_RESULT"),
)


def load_report(path: Path) -> dict | None:
    """Accept only the expected schema and a complete suite accounting."""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, ValueError):
        return None
    if not isinstance(data, dict):
        return None
    if data.get("schema_version") != 1 or data.get("profile") != "hermetic":
        return None
    if data.get("status") not in ("pass", "fail"):
        return None
    results = data.get("results")
    if not isinstance(results, list) or len(results) != len(EXPECTED_SUITES):
        return None
    if [item.get("id") for item in results if isinstance(item, dict)] != list(
        EXPECTED_SUITES
    ):
        return None
    if any(
        item.get("status") not in ("pass", "fail", "error")
        or type(item.get("duration_ms")) is not int
        or item["duration_ms"] < 0
        or (
            item["status"] == "error"
            and item.get("exit_code") is not None
        )
        or (
            item["status"] != "error"
            and type(item.get("exit_code")) is not int
        )
        or (item["status"] == "pass" and item.get("exit_code") != 0)
        or (item["status"] == "fail" and item.get("exit_code") == 0)
        for item in results
    ):
        return None
    counts = {
        "passed": sum(item["status"] == "pass" for item in results),
        "failed": sum(item["status"] == "fail" for item in results),
        "errors": sum(item["status"] == "error" for item in results),
    }
    if data.get("summary") != counts:
        return None
    if (data["status"] == "pass") != (counts["passed"] == len(EXPECTED_SUITES)):
        return None
    return data


def render_summary(report: dict | None, steps: dict[str, str], revision: str) -> str:
    lines = ["## Hermetic CI", "", f"Checkout: `{revision}`", ""]
    for label, key in STEP_LABELS:
        outcome = steps.get(key, "unknown")
        # Step outcomes are a fixed GitHub enum; never echo arbitrary env text.
        if outcome not in ("success", "failure", "skipped", "cancelled"):
            outcome = "unknown"
        lines.append(f"- {label}: `{outcome}`")
    lines.append("")
    if report is None:
        lines.append(
            "### Hermetic suite report unavailable or invalid"
        )
        lines.append("")
        lines.append(
            "The aggregate step's outcome is authoritative. No suite results "
            "can be inferred from a missing or incomplete report."
        )
    else:
        counts = report["summary"]
        lines.extend(
            [
                "### Hermetic suite results",
                "",
                f"**{report['status'].upper()}** — "
                f"{counts['passed']} passed, {counts['failed']} failed, "
                f"{counts['errors']} errors.",
                "",
                "| Suite | Result | Duration | Exit |",
                "| --- | --- | ---: | ---: |",
            ]
        )
        for entry in report["results"]:
            code = entry["exit_code"]
            lines.append(
                f"| `{entry['id']}` | {entry['status'].upper()} | "
                f"{entry['duration_ms'] / 1000:.1f}s | "
                f"{code if code is not None else '—'} |"
            )
        lines.append("")
        lines.append(
            "Failing suites can be diagnosed by running "
            "`just check <suite>` locally. No raw test logs are included."
        )
    return "\n".join(lines) + "\n"


def main() -> int:
    environment = os.environ
    report_path = Path(environment["RUNNER_TEMP"]) / "check-ci.json"
    report = load_report(report_path)
    revision = environment.get("BUILD_HASH", "")
    # Only Git revision hex is eligible for insertion into Markdown.
    if len(revision) not in (40, 64) or any(ch not in "0123456789abcdef" for ch in revision):
        revision = "unavailable"
    summary = render_summary(report, dict(environment), revision)
    with Path(environment["GITHUB_STEP_SUMMARY"]).open("a", encoding="utf-8") as stream:
        stream.write(summary)
    # Fail closed on missing or inconsistent evidence. A failed aggregate
    # remains a failed check even if its report is missing or incomplete.
    if environment.get("AGGREGATE_RESULT") == "success":
        return 0 if report is not None and report["status"] == "pass" else 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
