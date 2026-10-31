"""Hostless regression checks for the opt-in VM launcher."""

import contextlib
import hashlib
import io
import json
import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts.vm_harness import evidence, runner


class RunnerTests(unittest.TestCase):
    def test_full_stack_imports_only_fixed_artifact_even_on_runtime_failure(self):
        for code in (0, 42, "timeout"):
            with self.subTest(code=code), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                record = evidence.Evidence().record
                for section in ("phases", "gates"):
                    record[section] = dict.fromkeys(record[section], "pass")
                if code:
                    record["phases"]["prompt"] = "fail"
                    record["phases"]["wait"] = "not_verified"

                def run(command, **kwargs):
                    if command[0] == "git":
                        return subprocess.CompletedProcess(command, 0, "", "")
                    if command[0] == "nix":
                        return subprocess.CompletedProcess(command, 0, "/nix/store/driver\n", "")
                    evidence.write_artifact(Path(kwargs["cwd"]), record)
                    # Neither arbitrary files nor process output are evidence.
                    (Path(kwargs["cwd"]) / "secret.json").write_text("SECRET_CANARY")
                    if code == "timeout":
                        raise subprocess.TimeoutExpired(command, 900)
                    return subprocess.CompletedProcess(command, code, "SECRET_CANARY", "SECRET_CANARY")

                with (patch.object(runner, "ROOT", root),
                      patch.object(runner, "driver_from_build", return_value=Path("/nix/store/driver")),
                      patch.object(runner.subprocess, "run", side_effect=run),
                      contextlib.redirect_stdout(io.StringIO()),
                      contextlib.redirect_stderr(io.StringIO())):
                    self.assertEqual(runner.main(["test", "full-stack"]), 1 if code == "timeout" else code)
                path = next((root / ".vm-harness-results").glob("*/result.json"))
                result = json.loads(path.read_text())
                self.assertEqual(result["full_stack"], record)
                self.assertEqual(result["failure_stage"], "runtime" if code else None)
                self.assertNotIn("CANARY", path.read_text())

    def test_full_stack_green_exit_requires_complete_valid_artifact(self):
        for kind in ("missing", "incomplete", "symlink", "payload"):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)

                def run(command, **kwargs):
                    if command[0] == "git":
                        return subprocess.CompletedProcess(command, 0, "", "")
                    if command[0] == "nix":
                        return subprocess.CompletedProcess(command, 0, "/nix/store/driver\n", "")
                    state = Path(kwargs["cwd"])
                    if kind == "incomplete":
                        evidence.write_artifact(state, evidence.Evidence().record)
                    elif kind == "symlink":
                        (state / evidence.ARTIFACT).symlink_to(root / "secret")
                    elif kind == "payload":
                        (state / evidence.ARTIFACT).write_text('{"secret":"CANARY"}')
                    return subprocess.CompletedProcess(command, 0, "PASS: all gates", "")

                with (patch.object(runner, "ROOT", root),
                      patch.object(runner, "driver_from_build", return_value=Path("/nix/store/driver")),
                      patch.object(runner.subprocess, "run", side_effect=run),
                      contextlib.redirect_stdout(io.StringIO()),
                      contextlib.redirect_stderr(io.StringIO())):
                    self.assertEqual(runner.main(["test", "full-stack"]), 1)
                result = json.loads(next((root / ".vm-harness-results").glob("*/result.json")).read_text())
                self.assertEqual(result["status"], "fail")
                self.assertEqual(result["failure_stage"], "runtime")

    def test_artifact_rejects_paths_permissions_links_size_and_payloads(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            record = evidence.Evidence().record
            evidence.write_artifact(root, record)
            path = root / evidence.ARTIFACT
            self.assertEqual(evidence.read_artifact(root), record)
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            for mode in (0o644, 0o666):
                path.chmod(mode)
                with self.assertRaises(ValueError):
                    evidence.read_artifact(root)
            path.chmod(0o600)
            with (patch.object(evidence.os, "getuid", return_value=os.getuid() + 1),
                  self.assertRaises(ValueError)):
                evidence.read_artifact(root)
            with self.assertRaises(ValueError):
                evidence.read_artifact(root, "../secret")
            with self.assertRaises(FileExistsError):
                evidence.write_artifact(root, record)
            root.chmod(0o755)
            with self.assertRaises(ValueError):
                evidence.read_artifact(root)
            with self.assertRaises(ValueError):
                runner.write_report("smoke", 0, runner.Provenance(), root)
            root.chmod(0o700)
            hardlink = root / "hardlink"
            os.link(path, hardlink)
            with self.assertRaises(ValueError):
                evidence.read_artifact(root)
            hardlink.unlink()
            alias = root / "alias"
            alias.symlink_to(root, target_is_directory=True)
            with self.assertRaises(OSError):
                evidence.read_artifact(alias)
            for content in (b"x" * (evidence.MAX_BYTES + 1), b"{broken", b'{"secret":"CANARY"}'):
                path.write_bytes(content)
                with self.assertRaises(ValueError):
                    evidence.read_artifact(root)
            path.unlink()
            path.symlink_to(root / "secret")
            with self.assertRaises(OSError):
                evidence.read_artifact(root)
            path.unlink()
            os.mkfifo(path, 0o600)
            with self.assertRaises(ValueError):
                evidence.read_artifact(root)

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

    def test_gateway_topology_is_an_explicit_target(self):
        self.assertEqual(
            runner.build_arguments("gateway-topology", False)[-1],
            ".#vm-harness-gateway-topology.driver",
        )
        self.assertNotIn(
            "INCUS_SOCKET", runner.clean_environment({"INCUS_SOCKET": "pilot"})
        )

    def test_synthetic_gateway_uses_networked_batch_driver(self):
        self.assertEqual(
            runner.build_arguments("synthetic-gateway", False)[-1],
            ".#vm-harness-synthetic-gateway.driver",
        )

    def test_native_oci_uses_disposable_networked_driver(self):
        self.assertEqual(
            runner.build_arguments("native-oci", False)[-1],
            ".#vm-harness-native-oci.driver",
        )
        self.assertNotIn(
            "AGINCTUS_NATIVE_OCI_SPIKE",
            runner.clean_environment({"AGINCTUS_NATIVE_OCI_SPIKE": "disposable-vm"}),
        )

    def test_opencode_tool_loopback_is_an_isolated_fixed_target(self):
        self.assertEqual(
            runner.build_arguments("opencode-tool-loopback", False)[-1],
            ".#vm-harness-opencode-tool-loopback",
        )
        self.assertNotIn(
            "AGINCTUS_TESTENV_TOOL_WORKER",
            runner.clean_environment({"AGINCTUS_TESTENV_TOOL_WORKER": "DISPOSABLE_VM_ONLY"}),
        )

    def test_ownership_recovery_is_an_explicit_target(self):
        self.assertEqual(
            runner.build_arguments("ownership-recovery", False)[-1],
            ".#vm-harness-ownership-recovery",
        )

    def test_networked_scenario_records_runtime_result(self):
        for code in (0, 42):
            with self.subTest(code=code), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                results = [
                    subprocess.CompletedProcess([], 0, "abc123\n", ""),
                    subprocess.CompletedProcess([], 0, "", ""),
                    subprocess.CompletedProcess([], 0, "/nix/store/driver\n", ""),
                    subprocess.CompletedProcess([], code, "private runtime output", ""),
                ]
                with (
                    patch.object(runner, "ROOT", root),
                    patch.object(
                        runner,
                        "driver_from_build",
                        return_value=Path("/nix/store/driver/bin/nixos-test-driver"),
                    ),
                    patch.object(runner.subprocess, "run", side_effect=results) as run,
                    contextlib.redirect_stdout(io.StringIO()) as stdout,
                    contextlib.redirect_stderr(io.StringIO()),
                ):
                    self.assertEqual(runner.main(["test", "gateway-topology"]), code)
                invocation = run.call_args
                self.assertEqual(invocation.args[0][-1], "--no-interactive")
                self.assertNotEqual(invocation.kwargs["cwd"], root)
                self.assertNotIn("INCUS_SOCKET", invocation.kwargs["env"])
                record = json.loads(
                    next(
                        (root / ".vm-harness-results").glob("*/result.json")
                    ).read_text()
                )
                self.assertEqual(record["failure_stage"], "runtime" if code else None)
                self.assertEqual(record["status"], "fail" if code else "pass")
                self.assertNotIn("private runtime output", stdout.getvalue())

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
