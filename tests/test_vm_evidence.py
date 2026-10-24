"""Hostless regression tests for read-only VM evidence presentation."""

import contextlib
import io
import json
import stat
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts.checks import vm_evidence
from scripts.vm_harness import runner


class VMEvidenceTests(unittest.TestCase):
    def create(self, directory, *, exit_code=0, launch_failed=False):
        evidence_root = Path(directory) / "evidence"
        provenance = runner.Provenance("a" * 40, False, "b" * 64)
        path = runner.write_report(
            "smoke", exit_code, provenance, evidence_root,
            launch_failed=launch_failed,
        )
        return evidence_root, path.parent.name, path

    def test_real_vm_harness_record_yields_synthetic_only_report(self):
        with tempfile.TemporaryDirectory() as directory:
            root, run_id, path = self.create(directory)
            raw = json.loads(path.read_text())
            raw["provider_token"] = "SECRET_CANARY"
            raw["unexpected_diagnostic"] = "RAW_TRACEBACK_CANARY"
            path.write_text(json.dumps(raw))
            output = vm_evidence.normalize(vm_evidence.read_private_result(root, run_id), run_id)
            self.assertEqual(output["schema_version"], 1)
            self.assertEqual(output["profile"], "disposable_vm")
            self.assertEqual(output["status"], "pass")
            self.assertEqual(output["summary"], {"passed": 1, "failed": 0, "errors": 0})
            self.assertEqual(output["results"][0]["id"], "vm/smoke")
            self.assertEqual(output["results"][0]["rerun"], "just test-vm-smoke")
            self.assertEqual(output["source"]["run_id"], run_id)
            self.assertEqual(output["source"]["git_revision"], "a" * 40)
            self.assertEqual(output["upstream_inference"], "not_verified")
            self.assertEqual(output["cleanup"], "not_verified")
            self.assertNotIn("CANARY", json.dumps(output))

    def test_failed_vm_evidence_remains_failed_not_live_verified(self):
        with tempfile.TemporaryDirectory() as directory:
            for code, launch_failed in ((42, False), (1, True)):
                with self.subTest(code=code, launch_failed=launch_failed):
                    root, run_id, _ = self.create(
                        directory, exit_code=code, launch_failed=launch_failed
                    )
                    output = vm_evidence.normalize(
                        vm_evidence.read_private_result(root, run_id), run_id
                    )
                    self.assertEqual(output["status"], "fail")
                    self.assertEqual(output["summary"]["failed"], 1)
                    self.assertEqual(
                        output["failure_stage"], "launch" if launch_failed else "build"
                    )
                    self.assertEqual(output["results"][0]["exit_code"], code)
                    self.assertEqual(output["upstream_inference"], "not_verified")

    def test_rejects_untrusted_contract_and_wrong_run_id(self):
        with tempfile.TemporaryDirectory() as directory:
            root, run_id, path = self.create(directory)
            raw = json.loads(path.read_text())
            mutations = (
                {"schema_version": 2},
                {"status": "fail"},
                {"exit_code": True},
                {"upstream_inference": "verified"},
                {"cleanup": "verified"},
                {"verification_tier": "live_provider"},
                {"failure_stage": "launch"},
                {"scenario": "../../secrets"},
                {"created_utc": "tomorrow"},
                {"created_utc": "2026-10-24T15:00:00-07:00"},
                {"git_revision": "SECRET_CANARY"},
                {"flake_lock_sha256": "bad"},
                {"git_dirty": "false"},
            )
            for change in mutations:
                with self.subTest(change=change):
                    with self.assertRaises(ValueError):
                        vm_evidence.normalize({**raw, **change}, run_id)
            with self.assertRaises(ValueError):
                vm_evidence.normalize(raw, "f" * 24)
            with self.assertRaises(ValueError):
                vm_evidence.read_private_result(root, "../../something")
            with self.assertRaises(ValueError):
                vm_evidence.read_private_result(root, "A" * 24)

    def test_nofollow_ownership_and_private_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            root, run_id, path = self.create(directory)
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            path.chmod(0o644)
            with self.assertRaises(ValueError):
                vm_evidence.read_private_result(root, run_id)
            path.chmod(0o600)

            alias = Path(directory) / "alias"
            alias.symlink_to(root, target_is_directory=True)
            with self.assertRaises(OSError):
                vm_evidence.read_private_result(alias, run_id)

            file_copy = path.parent / "original.json"
            path.rename(file_copy)
            path.symlink_to(file_copy)
            with self.assertRaises(OSError):
                vm_evidence.read_private_result(root, run_id)
            path.unlink()
            file_copy.rename(path)

            parent = path.parent
            real = Path(directory) / "moved"
            parent.rename(real)
            parent.symlink_to(real, target_is_directory=True)
            with self.assertRaises(OSError):
                vm_evidence.read_private_result(root, run_id)

    def test_oversized_and_bad_json_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root, run_id, path = self.create(directory)
            path.write_bytes(b"x" * (vm_evidence.MAX_BYTES + 1))
            with self.assertRaises(ValueError):
                vm_evidence.read_private_result(root, run_id)
            path.write_text("{broken JSON")
            with self.assertRaises(ValueError):
                vm_evidence.read_private_result(root, run_id)

    def test_entrypoint_emits_one_json_object_on_success_and_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root, run_id, path = self.create(directory)
            with patch.object(vm_evidence, "ROOT", root):
                for expected_status, code in (("pass", 0), ("fail", 1)):
                    if code:
                        raw = json.loads(path.read_text())
                        raw.update(status="fail", exit_code=41, failure_stage="build")
                        path.write_text(json.dumps(raw))
                    out = io.StringIO()
                    with contextlib.redirect_stdout(out):
                        self.assertEqual(
                            vm_evidence.main([run_id, "--format", "json"]), code
                        )
                    self.assertEqual(len(out.getvalue().splitlines()), 1)
                    self.assertEqual(json.loads(out.getvalue())["status"], expected_status)
                out = io.StringIO()
                with contextlib.redirect_stdout(out):
                    self.assertEqual(vm_evidence.main(["../invalid", "--format", "json"]), 1)
                error = json.loads(out.getvalue())
                self.assertEqual(error["status"], "error")
                self.assertNotIn("invalid", out.getvalue())

    def test_just_argument_quoting_prevents_shell_interpolation(self):
        recipe = (
            Path(__file__).resolve().parents[1] / ".just/check.just"
        ).read_text(encoding="utf-8")
        self.assertIn("vm-report run_id:", recipe)
        self.assertIn("vm-report-json run_id:", recipe)
        self.assertEqual(recipe.count("{{quote(run_id)}}"), 2)


if __name__ == "__main__":
    unittest.main()
