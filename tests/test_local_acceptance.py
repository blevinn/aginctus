"""Hostless tests for the read-only local acceptance evidence viewer."""

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
from scripts.checks import local_acceptance

RUN_ID = "aginctus-acceptance.ABCD1234"


class LocalAcceptanceViewTests(unittest.TestCase):
    def make(self, directory, **changes):
        root = Path(directory) / "private"
        root.mkdir(mode=0o700, exist_ok=True)
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
        path = local_report.record(root, **args)
        return root, path

    def test_private_result_renders_only_real_local_observations(self):
        with tempfile.TemporaryDirectory() as directory:
            root, path = self.make(directory)
            parsed = local_acceptance.normalize(
                local_acceptance.read_private_result(root, RUN_ID), RUN_ID
            )
            self.assertEqual(parsed["status"], "incomplete")
            self.assertEqual(parsed["summary"], {
                "passed": 4, "failed": 0, "errors": 0, "not_run": 1
            })
            self.assertEqual([item["id"] for item in parsed["results"]], [
                "acceptance/fixture_change", "acceptance/workspace_tests",
                "acceptance/stop_command", "acceptance/close_command",
                "acceptance/local_workspace_removed",
            ])
            self.assertEqual(parsed["source"]["run_id"], RUN_ID)
            self.assertEqual(parsed["verification_tier"], "mock_orchestration")
            for name in ("agent_tools", "upstream_inference", "credential_egress", "cleanup"):
                self.assertEqual(parsed[name], "not_verified")
            self.assertFalse(parsed["complete_acceptance"])
            self.assertEqual(parsed["results"][-1]["status"], "not_run")
            self.assertLess(path.stat().st_size, local_report.MAX_BYTES)

    def test_failures_do_not_become_passes(self):
        with tempfile.TemporaryDirectory() as directory:
            root, _ = self.make(
                directory, mode="live", exit_code=1,
                fixture="pass", tests="fail", stop="fail",
            )
            parsed = local_acceptance.normalize(
                local_acceptance.read_private_result(root, RUN_ID), RUN_ID
            )
            self.assertEqual(parsed["status"], "fail")
            self.assertEqual(parsed["verification_tier"], "local_live_partial")
            self.assertEqual(parsed["summary"]["failed"], 2)
            self.assertEqual(parsed["upstream_inference"], "not_verified")

    def test_local_workspace_removed_is_independent_from_remote_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            root, _ = self.make(directory, local_workspace_removed="pass")
            parsed = local_acceptance.normalize(
                local_acceptance.read_private_result(root, RUN_ID), RUN_ID
            )
            self.assertEqual(parsed["summary"]["passed"], 5)
            self.assertEqual(parsed["results"][-1], {
                "id": "acceptance/local_workspace_removed", "status": "pass"
            })
            self.assertEqual(parsed["cleanup"], "not_verified")
            self.assertFalse(parsed["complete_acceptance"])

    def test_mutated_claims_and_contradictions_fail_closed(self):
        changes = (
            {"status": "pass"},
            {"schema_version": True},
            {"run_id": "aginctus-acceptance.EVIL1234"},
            {"mode": "billable"},
            {"verification_tier": "live_verified"},
            {"status": "fail"},
            {"harness_exit_code": True},
            {"cleanup": "verified"},
            {"upstream_inference": "verified"},
            {"credential_egress": "verified"},
            {"complete_acceptance": True},
            {"local_workspace_removed": "verified"},
            {"local_workspace_removed": "fail"},
            {"created_utc": "tomorrow"},
            {"created_utc": "2026-10-25T12:00:00-07:00"},
            {"cleanup_commands": {"stop": "verified", "close": "pass"}},
            {"cleanup_commands": {"stop": "not_run", "close": "pass"}},
            {"results": [
                {"id": key, "source": source, "status": "pass"}
                for key, source in local_acceptance.ROWS
            ]},
        )
        with tempfile.TemporaryDirectory() as directory:
            root, path = self.make(directory)
            valid = json.loads(path.read_text())
            for update in changes:
                with self.subTest(update=update), self.assertRaises(ValueError):
                    local_acceptance.normalize({**valid, **update}, RUN_ID)
            tampered = dict(valid)
            tampered["bogus_provider_evidence"] = "SECRET_CANARY"
            normalized = local_acceptance.normalize(tampered, RUN_ID)
            self.assertNotIn("CANARY", json.dumps(normalized))
            self.assertEqual(normalized["upstream_inference"], "not_verified")

    def test_no_follow_private_owner_and_size_enforced(self):
        with tempfile.TemporaryDirectory() as directory:
            root, path = self.make(directory)
            link = Path(directory) / "alias"
            link.symlink_to(root, target_is_directory=True)
            with self.assertRaises((OSError, ValueError)):
                local_acceptance.read_private_result(link, RUN_ID)
            for run_id in ("../secret", "aginctus-acceptance.abc"):
                with self.subTest(run_id=run_id), self.assertRaises(ValueError):
                    local_acceptance.read_private_result(root, run_id)
            path.chmod(0o644)
            with self.assertRaises(ValueError):
                local_acceptance.read_private_result(root, RUN_ID)
            path.chmod(0o600)
            original = path.parent / "original.json"
            path.rename(original)
            path.symlink_to(original)
            with self.assertRaises(OSError):
                local_acceptance.read_private_result(root, RUN_ID)
            path.unlink()
            original.rename(path)
            path.write_bytes(b"x" * (local_report.MAX_BYTES + 1))
            with self.assertRaises(ValueError):
                local_acceptance.read_private_result(root, RUN_ID)
            self.assertEqual(stat.S_IMODE(root.stat().st_mode), 0o700)

    def test_malformed_json_duplicates_and_unsafe_dir(self):
        with tempfile.TemporaryDirectory() as directory:
            root, path = self.make(directory)
            path.write_text('{"schema_version":1,"schema_version":1}')
            with self.assertRaises(ValueError):
                local_acceptance.read_private_result(root, RUN_ID)
            path.write_text("{bad-json")
            with self.assertRaises(ValueError):
                local_acceptance.read_private_result(root, RUN_ID)
            path.parent.chmod(0o755)
            with self.assertRaises(ValueError):
                local_acceptance.read_private_result(root, RUN_ID)

    def test_cli_outputs_one_safe_json_and_never_claims_complete_acceptance(self):
        with tempfile.TemporaryDirectory() as directory:
            root, _ = self.make(directory)
            out = io.StringIO()
            with patch.dict(
                os.environ, {"AGINCTUS_ACCEPTANCE_REPORT_ROOT": str(root)}
            ), contextlib.redirect_stdout(out):
                code = local_acceptance.main([RUN_ID, "--format", "json"])
            self.assertEqual(code, 1)
            parsed = json.loads(out.getvalue())
            self.assertEqual(parsed["status"], "incomplete")
            self.assertFalse(parsed["complete_acceptance"])
            self.assertNotIn(str(root), out.getvalue())
            out = io.StringIO()
            with patch.dict(
                os.environ, {"AGINCTUS_ACCEPTANCE_REPORT_ROOT": ""}
            ), contextlib.redirect_stdout(out):
                code = local_acceptance.main([RUN_ID, "--format", "json"])
            self.assertEqual(code, 1)
            self.assertEqual(json.loads(out.getvalue())["status"], "error")
            self.assertNotIn(str(directory), out.getvalue())


if __name__ == "__main__":
    unittest.main()
