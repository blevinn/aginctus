"""Exercise the hermetic reporting process with fake, isolated child commands."""

import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from subprocess import run

REPORTER = Path(__file__).resolve().parents[1] / "scripts/checks/report.py"
EXPECTED = (
    "go", "lint", "python", "shell-tests", "shell", "markdown", "docs-examples", "nix"
)


class ReportTests(unittest.TestCase):
    def invoke(self, *arguments, fail="", missing=False):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            log = tmp_path / "calls"
            env = dict(os.environ)
            env.update({
                "PATH": str(tmp_path) if missing else f"{tmp_path}:{env.get('PATH', '')}",
                "REPORT_TEST_LOG": str(log),
                "REPORT_TEST_FAIL": fail,
                "AGINCTUS_INCUS_INTEGRATION": "1",
                "AGINCTUS_INCUS_DISPOSABLE": "YES",
                "AGINCTUS_ACCEPTANCE_LIVE": "1",
                "AGINCTUS_ACCEPTANCE_LIVE_HOOKS": "/unexpected/hook",
                "ACCEPTANCE_TEST_FAIL": "1",
                "GOFLAGS": "-run=^$",
                "GOPROXY": "https://example.invalid",
                "GOSUMDB": "example.invalid",
                "GOTOOLCHAIN": "auto",
            })
            if not missing:
                stub = tmp_path / "just"
                stub.write_text(
                    "#!/bin/sh\n"
                    "test \"$AGINCTUS_INCUS_INTEGRATION\" = 0 || exit 30\n"
                    "test \"$AGINCTUS_INCUS_DISPOSABLE\" = NO || exit 31\n"
                    "test -z \"$AGINCTUS_ACCEPTANCE_LIVE\" || exit 32\n"
                    "test -z \"$AGINCTUS_ACCEPTANCE_LIVE_HOOKS\" || exit 33\n"
                    "test -z \"$ACCEPTANCE_TEST_FAIL\" || exit 34\n"
                    "test -z \"$GOFLAGS\" || exit 35\n"
                    "test \"$GOPROXY\" = off || exit 36\n"
                    "test \"$GOSUMDB\" = off || exit 37\n"
                    "test \"$GOTOOLCHAIN\" = local || exit 38\n"
                    "printf '%s\\n' \"$*\" >> \"$REPORT_TEST_LOG\"\n"
                    "echo UNSAFE_SECRET_CANARY\n"
                    "echo UNSAFE_SECRET_CANARY >&2\n"
                    "test \"$2\" != \"$REPORT_TEST_FAIL\" || exit 17\n",
                    encoding="utf-8",
                )
                stub.chmod(0o700)
            completed = run(
                [sys.executable, str(REPORTER), *arguments], env=env,
                text=True, capture_output=True, check=False, timeout=30
            )
            calls = log.read_text().splitlines() if log.exists() else []
            return completed, calls

    def test_json_success_and_guardrails(self):
        completed, calls = self.invoke("--format", "json")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(completed.stderr, "")
        self.assertEqual(calls, [f"check {suite}" for suite in EXPECTED])
        self.assertNotIn("UNSAFE_SECRET_CANARY", completed.stdout)
        report = json.loads(completed.stdout)
        self.assertEqual(report["schema_version"], 1)
        self.assertEqual(report["profile"], "hermetic")
        self.assertEqual(report["status"], "pass")
        self.assertEqual(report["summary"], {"passed": 8, "failed": 0, "errors": 0})
        self.assertEqual([entry["id"] for entry in report["results"]], list(EXPECTED))
        self.assertTrue(all(entry["duration_ms"] >= 0 for entry in report["results"]))

    def test_failed_suite_does_not_hide_later_results_or_logs(self):
        completed, calls = self.invoke("--format", "json", fail="python")
        self.assertEqual(completed.returncode, 1)
        self.assertEqual(completed.stderr, "")
        self.assertEqual(len(completed.stdout.splitlines()), 1)
        self.assertNotIn("UNSAFE_SECRET_CANARY", completed.stdout)
        self.assertEqual(len(calls), len(EXPECTED))
        result = json.loads(completed.stdout)
        self.assertEqual(result["status"], "fail")
        self.assertEqual(result["summary"], {"passed": 7, "failed": 1, "errors": 0})
        python = result["results"][2]
        self.assertEqual((python["status"], python["exit_code"]), ("fail", 17))
        self.assertEqual(python["rerun"], "just check python")

    def test_missing_launcher_is_explicit_error(self):
        completed, calls = self.invoke("--format", "json", missing=True)
        self.assertEqual(completed.returncode, 1)
        self.assertEqual(calls, [])
        report = json.loads(completed.stdout)
        self.assertEqual(report["summary"], {"passed": 0, "failed": 0, "errors": 8})
        self.assertTrue(all(r["status"] == "error" and r["exit_code"] is None
                            for r in report["results"]))

    def test_text_is_concise_and_json_verbose_is_rejected(self):
        completed, _ = self.invoke(fail="lint")
        self.assertEqual(completed.returncode, 1)
        self.assertEqual(len(completed.stdout.splitlines()), len(EXPECTED) + 2)
        self.assertIn("FAIL   lint", completed.stdout)
        self.assertIn("To diagnose lint: just check lint", completed.stdout)
        self.assertNotIn("UNSAFE_SECRET_CANARY", completed.stdout)
        rejected, calls = self.invoke("--format", "json", "--verbose")
        self.assertEqual(rejected.returncode, 2)
        self.assertEqual(calls, [])
        self.assertEqual(rejected.stdout, "")


if __name__ == "__main__":
    unittest.main()
