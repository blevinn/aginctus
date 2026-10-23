"""Hostless regression checks for the opt-in VM launcher."""
import json
import stat
import tempfile
import unittest
from pathlib import Path

from scripts.vm_harness import runner


class RunnerTests(unittest.TestCase):
    def test_sanitizes_inherited_host_authority(self):
        env = runner.clean_environment({
            "PATH": "/bin",
            "HOME": "/tmp/worker",
            "INCUS_REMOTE": "pilot",
            "INCUS_SOCKET": "/run/incus/unix.socket",
            "AGINCTUS_INCUS_INTEGRATION": "1",
            "AGINCTUS_INCUS_DISPOSABLE": "YES",
            "SSH_AUTH_SOCK": "/tmp/host-agent",
            "NIX_CONFIG": "substituters = https://untrusted.invalid",
            "QEMU_OPTS": "-drive file=/private",
        })
        self.assertEqual(env["AGINCTUS_INCUS_INTEGRATION"], "0")
        self.assertEqual(env["AGINCTUS_INCUS_DISPOSABLE"], "NO")
        for key in ("INCUS_REMOTE", "INCUS_SOCKET", "SSH_AUTH_SOCK", "NIX_CONFIG", "QEMU_OPTS"):
            self.assertNotIn(key, env)

    def test_allowlisted_nix_targets_no_host_shell(self):
        self.assertEqual(
            runner.build_arguments("smoke", False)[-1], ".#vm-harness-smoke"
        )
        self.assertEqual(
            runner.build_arguments("smoke", True)[-1], ".#vm-harness-smoke.driver"
        )
        self.assertNotIn("bash", runner.build_arguments("smoke", False))

    def test_storage_network_is_an_explicit_target(self):
        self.assertEqual(
            runner.build_arguments("storage-network", False)[-1],
            ".#vm-harness-storage-network",
        )

    def test_runtime_lifecycle_is_an_explicit_target(self):
        self.assertEqual(
            runner.build_arguments("runtime-lifecycle", False)[-1],
            ".#vm-harness-runtime-lifecycle",
        )

    def test_ownership_recovery_is_an_explicit_target(self):
        self.assertEqual(
            runner.build_arguments("ownership-recovery", False)[-1],
            ".#vm-harness-ownership-recovery",
        )

    def test_rejects_missing_or_foreign_driver(self):
        for value in ("", "/tmp/evil", "/nix/store/one\n/nix/store/two"):
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    runner.driver_from_build(value)

    def test_report_marks_live_provider_unverified(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "reports"
            report = runner.write_report("smoke", 0, "abc123", root)
            document = json.loads(report.read_text(encoding="utf-8"))
            self.assertEqual(document["schema_version"], 1)
            self.assertEqual(document["status"], "pass")
            self.assertEqual(document["upstream_inference"], "not_verified")
            self.assertEqual(document["cleanup"], "not_verified")
            self.assertEqual(document["git_revision"], "abc123")
            self.assertEqual(stat.S_IMODE(report.stat().st_mode), 0o600)
            failed = runner.write_report("smoke", 42, "abc123", root)
            self.assertEqual(
                json.loads(failed.read_text(encoding="utf-8"))["status"], "fail"
            )
            self.assertNotEqual(report.parent, failed.parent)

    def test_report_rejects_symlink_root(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            target = root / "target"
            target.mkdir()
            link = root / "link"
            link.symlink_to(target, target_is_directory=True)
            with self.assertRaises(ValueError):
                runner.write_report("smoke", 0, None, link)

    def test_driver_validation_checks_executable(self):
        with self.assertRaises(ValueError):
            runner.driver_from_build("/nix/store/fake-missing-driver")


if __name__ == "__main__":
    unittest.main()
