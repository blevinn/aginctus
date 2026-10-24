"""Hostless regression checks for the opt-in VM launcher."""

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
