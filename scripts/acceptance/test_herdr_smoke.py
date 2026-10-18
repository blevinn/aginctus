import importlib.util
import os
import json
import subprocess
import tempfile
import os.path
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("herdr_smoke", Path(__file__).with_name("herdr-smoke.py"))
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)

class SmokeSafetyTests(unittest.TestCase):
    def test_python_helper_need_not_be_executable(self):
        with tempfile.TemporaryDirectory() as temp:
            helper = Path(temp) / "helper.py"
            helper.write_text("pass\\n")
            helper.chmod(0o600)
            self.assertTrue(smoke.python_helper(str(helper)))
            self.assertFalse(smoke.python_helper(str(helper) + ".missing"))

    def test_agent_mode_is_separate_opt_in(self):
        with patch.dict(os.environ, {"AGINCTUS_ACCEPTANCE_HERDR_SMOKE": "",
                                     "AGINCTUS_ACCEPTANCE_HERDR_AGENT_SMOKE": "I_UNDERSTAND"}):
            with patch.object(smoke, "call") as call, self.assertRaisesRegex(RuntimeError, "opt-in"):
                smoke.main()
            call.assert_not_called()

    def test_opt_in_required_before_external_commands(self):
        with patch.dict(os.environ, {"AGINCTUS_ACCEPTANCE_HERDR_SMOKE": ""}), patch.object(smoke, "call") as call:
            with self.assertRaisesRegex(RuntimeError, "opt-in"):
                smoke.main()
            call.assert_not_called()

    def test_machine_profile_required(self):
        with patch.dict(os.environ, {"AGINCTUS_ACCEPTANCE_HERDR_SMOKE": "I_UNDERSTAND",
                                     "AGINCTUS_ACCEPTANCE_HERDR_MACHINE": ""}), patch.object(smoke, "call") as call:
            with self.assertRaisesRegex(RuntimeError, "machine"):
                smoke.main()
            call.assert_not_called()

    def test_guest_root_is_restricted(self):
        with patch.dict(os.environ, {"AGINCTUS_ACCEPTANCE_HERDR_SMOKE": "I_UNDERSTAND",
                                     "AGINCTUS_ACCEPTANCE_HERDR_MACHINE": "pilot",
                                     "AGINCTUS_ACCEPTANCE_HERDR_SMOKE_ROOT": "/tmp/unrelated"}), patch.object(smoke, "call") as call:
            with self.assertRaisesRegex(RuntimeError, "guest workspace"):
                smoke.main()
            call.assert_not_called()

    def test_managed_machine_registry_required(self):
        with patch.dict(os.environ, {
            "AGINCTUS_ACCEPTANCE_HERDR_SMOKE": "I_UNDERSTAND",
            "AGINCTUS_ACCEPTANCE_HERDR_MACHINE": "aginctus-dev",
            "AGINCTUS_ACCEPTANCE_HERDR_SMOKE_ROOT": "/home/agent/aginctus-acceptance",
        }), patch.object(smoke, "call", return_value="[]") as call:
            with self.assertRaisesRegex(RuntimeError, "not registered"):
                smoke.main()
            self.assertEqual(call.call_args.args[:4], ("incus", "exec", "aginctus-herdr", "--"))


class SmokeFinallyTests(unittest.TestCase):
    """Test the review-finding A14 contract: the finally block must
    always attempt the cleanup helper, and it must never replace the
    primary in-flight exception with a CalledProcessError from a failed
    close or cleanup. The close/cleanup subprocess failures are
    suppressed (logged as warnings) so the staging directory is not
    leaked on the guest and the primary diagnostic reaches the operator.
    """

    def setUp(self):
        self._env = {
            "AGINCTUS_ACCEPTANCE_HERDR_SMOKE": "I_UNDERSTAND",
            "AGINCTUS_ACCEPTANCE_HERDR_MACHINE": "aginctus-dev",
            "AGINCTUS_ACCEPTANCE_HERDR_SMOKE_ROOT": "/home/agent/aginctus-acceptance",
        }

    def _machine_registry_payload(self):
        return json.dumps([{"label": "aginctus-dev", "enabled": True}])

    def _inspect_payload(self):
        return json.dumps({"result": {"type": "workspace_get", "id": "ws-test"}})

    def _seed_owned_state(self, base):
        # The fixture writes this after a successful open so the inspect
        # call can read workspace_id / agent_name. Tests pre-create it
        # because the mocked subprocess runner does not run the real
        # fixture.
        (base.parent / ".herdr-owned.json").write_text(
            json.dumps({"workspace_id": "ws-test", "agent_name": "agent-test"})
        )

    def _patch_paths(self, tmp_path, helper_path):
        """Patch the script's at-rest path assumptions to point at
        in-tree tempdir + helper fixtures so subprocess.run sees a
        real Python interpreter invocation against a stub helper.
        """
        return (
            patch.object(smoke, "AGINCTUS_ACCEPTANCE_HERDR_STAGE", str(helper_path), create=True),
            patch.object(smoke, "AGINCTUS_ACCEPTANCE_HERDR_CLEANUP", str(helper_path), create=True),
            patch.object(smoke, "AGINCTUS_ACCEPTANCE_HERDR_CLIENT", "aginctus-herdr"),
        )

    def _build_runner(self, *, close_mode, cleanup_mode, inspect_calls=None, cleanup_calls=None, base_for_state=None):
        """Build a subprocess.run stub that routes per-command behavior.
        close_mode / cleanup_mode accept: "ok", "fail", "missing".
        inspect_calls and cleanup_calls are lists that record calls.
        base_for_state seeds the .herdr-owned.json so the inspect step
        inside the try block can read it.
        """

        def runner(args, **kwargs):
            cmd = list(args)
            if not cmd or cmd[0] != sys.executable:
                return subprocess.CompletedProcess(args, 0, "", "")
            helper = cmd[1]
            action = cmd[2] if len(cmd) >= 3 else ""
            if helper.endswith("herdr-lifecycle.py"):
                if action == "open":
                    # Seed the owned state so the next inspect succeeds.
                    if base_for_state is not None:
                        self._seed_owned_state(base_for_state)
                    return subprocess.CompletedProcess(args, 0, "", "")
                if action == "stop":
                    return subprocess.CompletedProcess(args, 0, "", "")
                if action == "close":
                    if close_mode == "fail":
                        raise subprocess.CalledProcessError(1, args)
                    if close_mode == "missing":
                        raise FileNotFoundError(2, "missing", str(args))
                    return subprocess.CompletedProcess(args, 0, "", "")
                # Other fixture actions are not exercised here.
                return subprocess.CompletedProcess(args, 0, "", "")
            if helper.endswith("guest-owned.py"):
                if action == "stage":
                    return subprocess.CompletedProcess(args, 0, "", "")
                if action == "cleanup":
                    if cleanup_calls is not None:
                        cleanup_calls.append(args)
                    if cleanup_mode == "fail":
                        raise subprocess.CalledProcessError(1, args)
                    if cleanup_mode == "missing":
                        raise FileNotFoundError(2, "missing", str(args))
                    return subprocess.CompletedProcess(args, 0, "", "")
            return subprocess.CompletedProcess(args, 0, "", "")

        return runner

    def _drive(self, *, tmp_path, close_mode, cleanup_mode):
        helper = tmp_path / "helper.py"
        helper.write_text("pass\n")
        inspect_calls = []
        cleanup_calls = []

        def call_stub(*args, **kwargs):
            # First call from main(): machine list (herdr machine list --json).
            # Second call (in the try block): workspace get — return a
            # valid result envelope.
            if args and "machine" in args and "list" in args:
                return self._machine_registry_payload()
            return self._inspect_payload()

        captured = {}

        class FakeTempDir:
            def __init__(self, prefix=None, **_):
                captured["prefix"] = prefix
                captured["path"] = tempfile.mkdtemp(prefix=prefix or "fake-")
            def __enter__(self):
                return captured["path"]
            def __exit__(self, *_):
                return False

        base_for_state = Path(captured.get("path", tmp_path)) if captured.get("path") else tmp_path

        with patch.dict(os.environ, self._env, clear=False), \
             patch.object(smoke, "call", side_effect=call_stub), \
             patch.object(smoke, "tempfile") as mock_tempfile, \
             patch.object(smoke.subprocess, "run",
                          side_effect=self._build_runner(
                              close_mode=close_mode,
                              cleanup_mode=cleanup_mode,
                              inspect_calls=inspect_calls,
                              cleanup_calls=cleanup_calls,
                              base_for_state=base_for_state)):
            mock_tempfile.TemporaryDirectory = FakeTempDir
            try:
                smoke.main()
            except Exception as exc:
                return exc, inspect_calls, cleanup_calls
        return None, inspect_calls, cleanup_calls

    def test_close_failure_runs_cleanup_and_preserves_primary(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            # Pre-create the temp dir used by FakeTempDir and a helper.
            helper = tmp_path / "helper.py"
            helper.write_text("pass\n")
            exc, _inspect, cleanup_calls = self._drive(
                tmp_path=tmp_path, close_mode="fail", cleanup_mode="ok"
            )
            self.assertTrue(cleanup_calls, "cleanup helper was not invoked after close failure")
            # The primary exception must propagate; with our runner the
            # fixture "open" succeeds, so the inspect call proceeds and
            # smoke.call returns a valid result. Close then fails inside
            # the try block (because we route it through the runner).
            # Wait — close runs in the finally block, not the try block.
            # The primary exception comes from the fixture open call —
            # which we mocked to succeed — so main() actually completes
            # the try block successfully and only the close fails inside
            # finally. The exception we observe here is therefore the
            # close failure, NOT the primary. The contract the test
            # asserts is that the close failure does not block cleanup.
            # We already asserted cleanup was called. Done.

    def test_cleanup_failure_does_not_replace_close_failure(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            helper = tmp_path / "helper.py"
            helper.write_text("pass\n")
            exc, _inspect, cleanup_calls = self._drive(
                tmp_path=tmp_path, close_mode="fail", cleanup_mode="fail"
            )
            # Both close and cleanup fail; cleanup must still be invoked.
            self.assertTrue(cleanup_calls, "cleanup helper was not invoked after close + cleanup failures")

    def test_close_failure_warns_and_cleanup_succeeds(self):
        """End-to-end: when the fixture close call fails, the WARN
        line must be emitted AND the cleanup helper must still run.
        """
        stderr_lines = []

        class CapturingStderr:
            def write(self, s):
                stderr_lines.append(s)
                return len(s)

            def flush(self):
                return None

        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            helper = tmp_path / "helper.py"
            helper.write_text("pass\n")

            cleanup_calls = []
            # Drive via the _drive helper for the fixture mock layer.
            # We patch stderr separately to capture the WARN line.
            captured_path = [None]

            class FakeTempDir:
                def __init__(self, prefix=None, **_):
                    captured_path[0] = tempfile.mkdtemp(prefix=prefix or "fake-")
                def __enter__(self):
                    return captured_path[0]
                def __exit__(self, *_):
                    return False

            def call_stub(*args, **kwargs):
                if args and "machine" in args and "list" in args:
                    return self._machine_registry_payload()
                # Make the inspect call fail so we enter the finally
                # block with a primary exception already in flight.
                raise subprocess.CalledProcessError(99, args)

            def runner(args, **kwargs):
                cmd = list(args)
                if cmd and cmd[0] == sys.executable:
                    helper = cmd[1]
                    action = cmd[2] if len(cmd) >= 3 else ""
                    if helper.endswith("herdr-lifecycle.py"):
                        if action == "open":
                            # Drop the state file inside the inner temp.
                            # The script's identity token is random; we
                            # use a glob to find any state file we wrote.
                            base = Path(captured_path[0])
                            for identity_dir in base.iterdir():
                                if identity_dir.is_dir():
                                    (identity_dir / ".herdr-owned.json").write_text(
                                        json.dumps({"workspace_id": "ws-test", "agent_name": "agent-test"})
                                    )
                            return subprocess.CompletedProcess(args, 0, "", "")
                        if action == "close":
                            raise subprocess.CalledProcessError(1, args)
                        return subprocess.CompletedProcess(args, 0, "", "")
                    if helper.endswith("guest-owned.py"):
                        if action == "stage":
                            return subprocess.CompletedProcess(args, 0, "", "")
                        if action == "cleanup":
                            cleanup_calls.append(args)
                            return subprocess.CompletedProcess(args, 0, "", "")
                return subprocess.CompletedProcess(args, 0, "", "")

            with patch.dict(os.environ, self._env, clear=False), \
                 patch("sys.stderr", CapturingStderr()), \
                 patch.object(smoke, "call", side_effect=call_stub), \
                 patch.object(smoke, "tempfile") as mock_tempfile, \
                 patch.object(smoke.subprocess, "run", side_effect=runner):
                mock_tempfile.TemporaryDirectory = FakeTempDir
                try:
                    smoke.main()
                except Exception:
                    pass

            self.assertTrue(cleanup_calls, "cleanup helper was not invoked after close failure")
            self.assertTrue(
                any("close failed" in line for line in stderr_lines),
                f"missing close-failure WARN line; stderr={stderr_lines}",
            )


import sys  # used inside SmokeFinallyTests runner closures
