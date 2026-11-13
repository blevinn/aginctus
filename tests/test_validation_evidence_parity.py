"""Hostless cross-language parity fixtures for the read-only Go report migration."""

from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

from scripts.acceptance import local_report
from scripts.checks import ci_summary, coverage, local_acceptance

ROOT = Path(__file__).resolve().parents[1]
RUN_ID = "aginctus-acceptance.ABCD1234"


class ValidationEvidenceParityTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.binary_directory = tempfile.TemporaryDirectory()
        cls.binary = Path(cls.binary_directory.name) / "aginctus-check"
        subprocess.run(
            ["go", "build", "-o", str(cls.binary), "./cmd/aginctus-check"],
            cwd=ROOT, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
        )

    @classmethod
    def tearDownClass(cls):
        cls.binary_directory.cleanup()

    def go_report(self, args, env=None):
        process = subprocess.run(
            [str(self.binary), "--format=json", *args],
            cwd=ROOT, env={**os.environ, **(env or {})},
            stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=15,
            check=False,
        )
        self.assertEqual(process.returncode, 1, process.stderr)
        self.assertNotIn("SECRET_CANARY", process.stdout)
        return json.loads(process.stdout)

    def test_hermetic_coverage_parity(self):
        rows = [
            {"id": name, "status": "pass", "duration_ms": 1,
             "exit_code": 0, "rerun": f"just check {name}"}
            for name in ci_summary.EXPECTED_SUITES
        ]
        doc = {
            "schema_version": 1, "profile": "hermetic", "status": "pass",
            "summary": {"passed": len(rows), "failed": 0, "errors": 0},
            "results": rows, "additional_private_metadata": "SECRET_CANARY",
        }
        with tempfile.TemporaryDirectory() as folder:
            source = Path(folder) / "hermetic.json"
            source.write_text(json.dumps(doc), encoding="utf-8")
            expected = coverage.normalize_sources(hermetic_json=source)
            observed = self.go_report(["--coverage", "--hermetic-json", str(source)])
            self.assertEqual(observed, expected)
            self.assertFalse(observed["complete_acceptance"])

    def test_local_acceptance_reader_and_coverage_parity(self):
        with tempfile.TemporaryDirectory() as folder:
            private = Path(folder) / "private"
            private.mkdir(mode=0o700)
            local_report.record(
                private, run_id=RUN_ID, mode="mock", exit_code=0,
                fixture="pass", tests="pass", stop="pass", close="pass",
            )
            env = {"AGINCTUS_ACCEPTANCE_REPORT_ROOT": str(private)}
            expected = local_acceptance.normalize(
                local_acceptance.read_private_result(private, RUN_ID), RUN_ID
            )
            observed = self.go_report(["--acceptance-run-id", RUN_ID], env=env)
            self.assertEqual(observed, expected)
            aggregate = coverage.normalize_sources(
                local_acceptance_run_id=RUN_ID, local_root=private
            )
            go_aggregate = self.go_report(
                ["--coverage", "--local-acceptance-run-id", RUN_ID], env=env
            )
            self.assertEqual(go_aggregate, aggregate)
            self.assertFalse(go_aggregate["complete_acceptance"])

    def test_invalid_selected_source_is_error_not_pass(self):
        with tempfile.TemporaryDirectory() as folder:
            source = Path(folder) / "invalid.json"
            source.write_text('{"schema_version":1,"profile":"hermetic"}')
            expected = coverage.normalize_sources(hermetic_json=source)
            observed = self.go_report(["--coverage", "--hermetic-json", str(source)])
            self.assertEqual(observed, expected)
            self.assertEqual(observed["status"], "error")

    def test_acceptance_utc_spelling_parity(self):
        for created in ("2026-11-08T22:00:00Z", "2026-11-08T22:00:00.1234Z"):
            with self.subTest(created=created), tempfile.TemporaryDirectory() as folder:
                private = Path(folder) / "private"
                private.mkdir(mode=0o700)
                local_report.record(
                    private, run_id=RUN_ID, mode="mock", exit_code=0,
                    fixture="pass", tests="pass", stop="pass", close="pass",
                )
                source = private / RUN_ID / "result.json"
                record = json.loads(source.read_text())
                record["created_utc"] = created
                source.write_text(json.dumps(record))
                expected = local_acceptance.normalize(record, RUN_ID)
                observed = self.go_report(
                    ["--acceptance-run-id", RUN_ID],
                    env={"AGINCTUS_ACCEPTANCE_REPORT_ROOT": str(private)},
                )
                self.assertEqual(observed, expected)

    def test_preflight_status_and_selection_parity(self):
        for check in ("pilot-topology", "opencode-runtime"):
            for status, code in (("pass", 0), ("fail", 1), ("error", None)):
                with self.subTest(check=check, status=status), tempfile.TemporaryDirectory() as folder:
                    source = Path(folder) / "preflight.json"
                    source.write_text(json.dumps({
                        "schema_version": 1, "profile": "pilot_preflight", "status": status,
                        "verification_tier": "read_only_pilot_preflight",
                        "upstream_inference": "not_verified",
                        "gateway_authorization": "not_verified", "egress_policy": "not_verified",
                        "summary": {"passed": int(status == "pass"),
                                    "failed": int(status == "fail"), "errors": int(status == "error")},
                        "results": [{"id": f"preflight/{check}", "status": status, "exit_code": code}],
                    }))
                    expected = coverage.normalize_sources(**{check.replace("-", "_") + "_json": source})
                    observed = self.go_report(["--coverage", f"--{check}-json", str(source)])
                    self.assertEqual(observed, expected)


if __name__ == "__main__":
    unittest.main()
