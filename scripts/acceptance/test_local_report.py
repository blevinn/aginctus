"""Hostless tests of private, incomplete-by-default local acceptance records."""

import contextlib
import io
import json
import os
import stat
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts.acceptance import local_report


RUN_ID = "aginctus-acceptance.ABCD1234"


class LocalReportTests(unittest.TestCase):
    def setup_root(self, parent):
        root = Path(parent) / "private-results"
        root.mkdir(mode=0o700)
        return root

    def write(self, root, **changes):
        args = {
            "run_id": RUN_ID,
            "mode": "mock",
            "exit_code": 0,
            "fixture": "pass",
            "tests": "pass",
            "stop": "pass",
            "close": "pass",
        }
        args.update(changes)
        return local_report.record(root, **args)

    def test_real_observed_tests_still_never_prove_live_inference_or_cleanup(self):
        with tempfile.TemporaryDirectory() as parent:
            root = self.setup_root(parent)
            path = self.write(root)
            document = json.loads(path.read_text())
            self.assertEqual(document["profile"], "local_acceptance")
            self.assertEqual(document["schema_version"], 1)
            self.assertEqual(document["run_id"], RUN_ID)
            self.assertEqual(document["mode"], "mock")
            self.assertEqual(document["status"], "incomplete")
            self.assertEqual(document["harness_exit_code"], 0)
            self.assertEqual(document["results"][0]["status"], "pass")
            self.assertEqual(document["results"][1]["status"], "pass")
            self.assertTrue(all(
                row["status"] == "not_run" for row in document["results"][2:]
            ))
            self.assertEqual(document["upstream_inference"], "not_verified")
            self.assertEqual(document["cleanup"], "not_verified")
            self.assertFalse(document["complete_acceptance"])
            self.assertEqual(document["cleanup_commands"], {
                "stop": "pass", "close": "pass"
            })
            self.assertEqual(stat.S_IMODE(path.parent.stat().st_mode), 0o700)
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            self.assertLess(path.stat().st_size, local_report.MAX_BYTES)

    def test_live_label_and_cleanup_command_success_do_not_imply_proof(self):
        with tempfile.TemporaryDirectory() as parent:
            root = self.setup_root(parent)
            document = json.loads(self.write(root, mode="live").read_text())
            self.assertEqual(document["verification_tier"], "local_live_partial")
            self.assertEqual(document["status"], "incomplete")
            self.assertEqual(document["results"][3]["status"], "not_run")
            self.assertEqual(document["results"][5]["status"], "not_run")

    def test_failed_or_contradictory_exit_never_passes(self):
        for code, fixture, tests, expected in (
            (1, "not_run", "not_run", "fail"),
            (7, "pass", "fail", "fail"),
            (0, "not_run", "pass", "error"),
        ):
            with self.subTest(code=code, fixture=fixture, tests=tests), tempfile.TemporaryDirectory() as parent:
                path = self.write(
                    self.setup_root(parent), exit_code=code,
                    fixture=fixture, tests=tests,
                    stop="fail", close="not_run",
                )
                report = json.loads(path.read_text())
                self.assertEqual(report["status"], expected)
                self.assertEqual(report["cleanup_commands"]["stop"], "fail")
                self.assertEqual(report["cleanup"], "not_verified")
                self.assertFalse(report["complete_acceptance"])

    def test_input_rejection_before_file_creation(self):
        with tempfile.TemporaryDirectory() as parent:
            root = self.setup_root(parent)
            changes = (
                {"run_id": "../../pwned"},
                {"run_id": "aginctus-acceptance.abc\nsecret"},
                {"mode": "provider-live"},
                {"exit_code": True},
                {"exit_code": -9},
                {"exit_code": 256},
                {"fixture": "gateway-observed:SECRET_CANARY"},
                {"tests": "upstream-inference-confirmed"},
                {"stop": "verified"},
                {"close": "success"},
            )
            for change in changes:
                with self.subTest(change=change), self.assertRaises(ValueError):
                    self.write(root, **change)
            self.assertEqual(list(root.iterdir()), [])

    def test_owner_only_root_required_and_no_symlink_following(self):
        with tempfile.TemporaryDirectory() as parent:
            root = self.setup_root(parent)
            alias = Path(parent) / "alias"
            alias.symlink_to(root, target_is_directory=True)
            bad = Path(parent) / "bad"
            bad.mkdir(mode=0o755)
            bad.chmod(0o755)
            for candidate in (alias, bad, Path(parent) / "missing"):
                with self.subTest(candidate=candidate), self.assertRaises(
                    (OSError, ValueError)
                ):
                    self.write(candidate)
            self.assertEqual(list(root.iterdir()), [])

    def test_existing_run_cannot_be_overwritten(self):
        with tempfile.TemporaryDirectory() as parent:
            root = self.setup_root(parent)
            path = self.write(root)
            before = path.read_bytes()
            with self.assertRaises(FileExistsError):
                self.write(root, mode="live", exit_code=5)
            self.assertEqual(before, path.read_bytes())

    def test_cli_root_check_and_safe_failure_output(self):
        with tempfile.TemporaryDirectory() as parent:
            root = self.setup_root(parent)
            self.assertEqual(local_report.main(["--check-root", str(root)]), 0)
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                result = local_report.main(["--check-root", str(root / "missing")])
            self.assertEqual(result, 1)
            self.assertNotIn(str(root), output.getvalue())
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                result = local_report.main([
                    "--output-root", str(root), "--run-id", RUN_ID,
                    "--mode", "live", "--exit-code", "0",
                    "--fixture", "pass", "--tests", "pass",
                    "--stop", "pass", "--close", "pass",
                ])
            self.assertEqual(result, 0)
            self.assertNotIn(str(root), output.getvalue())
            self.assertIn("not verified", output.getvalue().lower())

    def test_checkout_root_and_wrong_owner_fail_closed(self):
        with tempfile.TemporaryDirectory() as parent:
            root = self.setup_root(parent)
            with patch.object(local_report, "SOURCE_ROOT", root.parent), self.assertRaises(
                ValueError
            ):
                self.write(root)
            original_uid = os.getuid()
            original_fstat = local_report.os.fstat

            def wrong_owner(fd):
                metadata = original_fstat(fd)
                class DifferentOwner:
                    st_mode = metadata.st_mode
                    st_uid = original_uid + 1
                return DifferentOwner()

            with patch.object(local_report.os, "fstat", side_effect=wrong_owner), self.assertRaises(
                ValueError
            ):
                self.write(root)
            self.assertEqual(list(root.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
