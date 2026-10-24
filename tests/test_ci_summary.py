"""Prove CI uses only the validated report schema for its summary."""

import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/checks/ci_summary.py"
SPEC = importlib.util.spec_from_file_location("ci_summary", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
ci_summary = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ci_summary)


def fixture(*, failed=""):
    results = [
        {
            "id": suite,
            "status": "fail" if suite == failed else "pass",
            "duration_ms": 25,
            "exit_code": 17 if suite == failed else 0,
            "rerun": f"just check {suite}",
            "stdout": "TOP_SECRET_SHOULD_NOT_RENDER",
        }
        for suite in ci_summary.EXPECTED_SUITES
    ]
    failure_count = sum(result["status"] == "fail" for result in results)
    return {
        "schema_version": 1,
        "profile": "hermetic",
        "status": "fail" if failed else "pass",
        "duration_ms": 200,
        "summary": {
            "passed": len(results) - failure_count,
            "failed": failure_count,
            "errors": 0,
        },
        "results": results,
    }


class CISummaryTests(unittest.TestCase):
    def test_complete_failure_report_and_no_raw_output(self):
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / "check-ci.json"
            path.write_text(json.dumps(fixture(failed="python")))
            result = ci_summary.load_report(path)
            self.assertIsNotNone(result)
            markdown = ci_summary.render_summary(
                result, {"AGGREGATE_RESULT": "failure"}, "a" * 40
            )
            self.assertIn("7 passed, 1 failed, 0 errors", markdown)
            self.assertIn("| `python` | FAIL | 0.0s | 17 |", markdown)
            self.assertEqual(markdown.count("| `"), len(ci_summary.EXPECTED_SUITES))
            self.assertNotIn("TOP_SECRET_SHOULD_NOT_RENDER", markdown)

    def test_rejects_partial_wrong_version_and_inconsistent_counts(self):
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / "check-ci.json"
            for invalid in (
                {"schema_version": 1, "results": []},
                dict(fixture(), schema_version=2),
                dict(fixture(), summary={"passed": 1, "failed": 0, "errors": 0}),
                dict(fixture(), results=fixture()["results"][:-1]),
            ):
                with self.subTest(invalid=invalid.get("schema_version")):
                    path.write_text(json.dumps(invalid))
                    self.assertIsNone(ci_summary.load_report(path))
            path.write_text("not json")
            self.assertIsNone(ci_summary.load_report(path))
            path.unlink()
            self.assertIsNone(ci_summary.load_report(path))

    def test_invalid_report_fails_closed_when_aggregate_claims_success(self):
        with tempfile.TemporaryDirectory() as root:
            base = Path(root)
            summary = base / "summary.md"
            env = {
                "RUNNER_TEMP": str(base),
                "GITHUB_STEP_SUMMARY": str(summary),
                "BUILD_HASH": "b" * 40,
                "AGGREGATE_RESULT": "success",
            }
            with patch.dict(os.environ, env):
                self.assertEqual(ci_summary.main(), 1)
            self.assertIn("report unavailable or invalid", summary.read_text())
            summary.unlink()
            with patch.dict(os.environ, dict(env, AGGREGATE_RESULT="failure")):
                self.assertEqual(ci_summary.main(), 0)
            self.assertIn("No suite results can be inferred", summary.read_text())

    def test_rejects_injected_fields_and_unsafe_step_names(self):
        data = fixture()
        data["results"][0]["id"] = "|malicious|"
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / "check-ci.json"
            path.write_text(json.dumps(data))
            self.assertIsNone(ci_summary.load_report(path))
        markdown = ci_summary.render_summary(
            None, {"AGGREGATE_RESULT": "success\nFAKE PASS"}, "unavailable"
        )
        self.assertIn("Hermetic suites: `unknown`", markdown)
        self.assertNotIn("FAKE PASS", markdown)


if __name__ == "__main__":
    unittest.main()
