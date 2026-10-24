"""Hostless regression checks for the opt-in VM launcher."""

import contextlib
import hashlib
import io
import json
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts.vm_harness import runner


class RunnerTests(unittest.TestCase):
    def test_sanitizes_inherited_host_authority(self):
        env = runner.clean_environment(
            {
                "PATH": "/bin",
                "HOME": "/tmp/worker",
                "INCUS_REMOTE": "pilot",
                "INCUS_SOCKET": "/run/incus/unix.socket",
                "AGINCTUS_INCUS_INTEGRATION": "1",
                "AGINCTUS_INCUS_DISPOSABLE": "YES",
                "SSH_AUTH_SOCK": "/tmp/host-agent",
                "NIX_CONFIG": "substituters = https://untrusted.invalid",
                "QEMU_OPTS": "-drive file=/private",
            }
        )
        self.assertEqual(env["AGINCTUS_INCUS_INTEGRATION"], "0")
        self.assertEqual(env["AGINCTUS_INCUS_DISPOSABLE"], "NO")
        for key in (
            "INCUS_REMOTE",
            "INCUS_SOCKET",
            "SSH_AUTH_SOCK",
            "NIX_CONFIG",
            "QEMU_OPTS",
        ):
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
            with self.subTest(value=value), self.assertRaises(ValueError):
                runner.driver_from_build(value)

    def test_report_marks_live_provider_unverified(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "reports"
            provenance = runner.Provenance("abc123", False, "lock-hash")
            report = runner.write_report("smoke", 0, provenance, root)
            document = json.loads(report.read_text(encoding="utf-8"))
            self.assertEqual(document["schema_version"], 1)
            self.assertEqual(document["status"], "pass")
            self.assertEqual(document["upstream_inference"], "not_verified")
            self.assertEqual(document["cleanup"], "not_verified")
            self.assertEqual(document["git_revision"], "abc123")
            self.assertEqual(stat.S_IMODE(report.stat().st_mode), 0o600)
            failed = runner.write_report("smoke", 42, provenance, root)
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
                runner.write_report("smoke", 0, runner.Provenance(), link)

    def test_main_captures_dirty_provenance_before_build(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            lock = root / "flake.lock"
            lock.write_bytes(b"before")
            commands = []

            def run(command, **kwargs):
                commands.append(command)
                if command[0] == "nix":
                    lock.write_bytes(b"after")
                    return subprocess.CompletedProcess(
                        command, 0, "/nix/store/test\n", ""
                    )
                output = "abc123\n" if command[1] == "rev-parse" else " M flake.lock\n"
                return subprocess.CompletedProcess(command, 0, output, "")

            with (
                patch.object(runner, "ROOT", root),
                patch.object(runner.subprocess, "run", side_effect=run),
                contextlib.redirect_stdout(io.StringIO()),
            ):
                self.assertEqual(runner.main(["test", "smoke"]), 0)
            document = json.loads(
                next((root / ".vm-harness-results").glob("*/result.json")).read_text()
            )
            self.assertEqual(
                [command[0] for command in commands], ["git", "git", "nix"]
            )
            self.assertEqual(document["git_revision"], "abc123")
            self.assertTrue(document["git_dirty"])
            self.assertEqual(
                document["flake_lock_sha256"], hashlib.sha256(b"before").hexdigest()
            )
            self.assertEqual(document["status"], "pass")

    def test_main_reports_launch_failure_and_tolerates_missing_git(self):
        for build_code in (None, 0, 42):
            with (
                self.subTest(build_code=build_code),
                tempfile.TemporaryDirectory() as temporary,
            ):
                root = Path(temporary)

                def run(command, **kwargs):
                    if command[0] == "git" or build_code is None:
                        raise FileNotFoundError(command[0])
                    return subprocess.CompletedProcess(
                        command, build_code, "", "synthetic diagnostic\n"
                    )

                with (
                    patch.object(runner, "ROOT", root),
                    patch.object(runner.subprocess, "run", side_effect=run),
                    contextlib.redirect_stdout(io.StringIO()),
                    contextlib.redirect_stderr(io.StringIO()),
                ):
                    self.assertEqual(
                        runner.main(["test", "smoke"]),
                        1 if build_code is None else build_code,
                    )
                report = next((root / ".vm-harness-results").glob("*/result.json"))
                document = json.loads(report.read_text())
                self.assertEqual(
                    document["status"], "pass" if build_code == 0 else "fail"
                )
                self.assertEqual(
                    document["failure_stage"],
                    "launch"
                    if build_code is None
                    else ("build" if build_code else None),
                )
                self.assertIsNone(document["git_revision"])
                self.assertIsNone(document["git_dirty"])
                self.assertIsNone(document["flake_lock_sha256"])
                self.assertNotIn("synthetic diagnostic", report.read_text())
                self.assertEqual(stat.S_IMODE(report.stat().st_mode), 0o600)
                self.assertEqual(stat.S_IMODE(report.parent.stat().st_mode), 0o700)

    def test_provenance_distinguishes_clean_from_unknown(self):
        for status_code, expected in ((0, False), (128, None)):
            with (
                self.subTest(status_code=status_code),
                tempfile.TemporaryDirectory() as temporary,
            ):
                results = [
                    subprocess.CompletedProcess([], 0, "abc123\n", ""),
                    subprocess.CompletedProcess([], status_code, "", ""),
                ]
                with (
                    patch.object(runner, "ROOT", Path(temporary)),
                    patch.object(runner.subprocess, "run", side_effect=results),
                ):
                    provenance = runner.capture_provenance({})
                self.assertEqual(provenance.git_revision, "abc123")
                self.assertIs(provenance.git_dirty, expected)

    def test_driver_validation_checks_executable(self):
        with self.assertRaises(ValueError):
            runner.driver_from_build("/nix/store/fake-missing-driver")

    def test_driver_validates_resolved_paths_and_permissions(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            store = root / "store"
            package = store / "test-driver"
            (package / "bin").mkdir(parents=True)
            driver = package / "bin" / "nixos-test-driver"
            driver.write_text("#!/bin/sh\nexit 0\n")
            with patch.object(runner, "STORE_ROOT", store):
                with self.assertRaises(ValueError):
                    runner.driver_from_build(str(package))
                driver.chmod(0o700)
                self.assertEqual(runner.driver_from_build(str(package)), driver)

                outside = root / "outside"
                (outside / "bin").mkdir(parents=True)
                foreign = outside / "bin" / "nixos-test-driver"
                foreign.write_text("#!/bin/sh\nexit 0\n")
                foreign.chmod(0o700)
                with self.assertRaises(ValueError):
                    runner.driver_from_build(str(store / ".." / "outside"))
                alias = store / "escape"
                alias.symlink_to(outside, target_is_directory=True)
                with self.assertRaises(ValueError):
                    runner.driver_from_build(str(alias))
                driver.unlink()
                driver.symlink_to(foreign)
                with self.assertRaises(ValueError):
                    runner.driver_from_build(str(package))

                # The pinned NixOS driver wraps a link to another store package.
                target = store / "base-driver"
                target.write_text("#!/bin/sh\nexit 0\n")
                target.chmod(0o700)
                driver.unlink()
                driver.symlink_to(target)
                self.assertEqual(runner.driver_from_build(str(package)), target)


if __name__ == "__main__":
    unittest.main()
