"""Pilot diagnostics stay read-only and use the actual client registry."""
import contextlib
import importlib.util
import io
import json
import subprocess
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "herdr_pilot", Path(__file__).with_name("herdr-socket-pilot-validation.py"))
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
KEY = "ssh-ed25519 fixture aginctus:1"


class PilotTests(unittest.TestCase):
    def fake(self, instance, args, **kwargs):
        if args == ["sshd", "-T"]:
            return 0, ("SetEnv HERDR_SOCKET_PATH=" + mod.EXPECTED_SOCKET_PATH
                       + " XDG_CONFIG_HOME=" + mod.EXPECTED_CONFIG_HOME + "\n"
                       + "\n".join(
                key + " no" for key in (
                    "passwordauthentication", "kbdinteractiveauthentication", "permitrootlogin",
                    "allowagentforwarding", "allowtcpforwarding", "allowstreamlocalforwarding",
                    "x11forwarding", "permittunnel", "permituserrc"))
        if args[0] == "systemctl":
            return 0, "HERDR_SOCKET_PATH=" + mod.EXPECTED_SOCKET_PATH
        if args[0] == "aginctus-ssh-client-identity":
            return 0, "generation\t1\npublic_key\t" + KEY + "\n"
        if args[0] == "aginctus-ssh-authorize":
            return 0, KEY + "\n"
        if args[:3] == ["herdr", "machine", "list"]:
            return 0, json.dumps([{"label": mod.WORKLOAD, "target": mod.WORKLOAD,
                                   "session": "default", "enabled": True}])
        if args[:4] == ["ssh", "-o", "BatchMode=yes", mod.WORKLOAD]:
            return 0, mod.EXPECTED_CONFIG_HOME + "\n"
        if args[:3] == ["herdr", "machine", "status"]:
            return 0, json.dumps([{"label": mod.WORKLOAD, "status": "reachable"}])
        if args[:2] == ["herdr", "--machine"]:
            return 0, json.dumps({"result": {"type": "workspace_list", "workspaces": []}})
        if args[0] == "su":
            return 1, json.dumps({"error": {"code": "server_not_running"}})
        return 0, ""

    def run_check(self, fake=None):
        output = io.StringIO()
        with patch.object(mod.sys, "argv", ["pilot"]), \
                patch.object(mod, "run_remote", side_effect=fake or self.fake) as run, \
                contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
            rc = mod.main()
        return rc, output.getvalue(), run.call_args_list

    def test_remote_registry_and_workspace_use_client_without_override(self):
        rc, output, calls = self.run_check()
        self.assertEqual(rc, 0, output)
        for call in calls:
            instance, args = call.args
            if args[0] == "herdr":
                self.assertEqual(instance, mod.CLIENT)
                self.assertNotIn("HERDR_SOCKET_PATH", " ".join(args))
            if args[0].startswith("aginctus-ssh-"):
                self.assertEqual(args[1], "inspect")
        self.assertIn("existing guest only", output)
        self.assertNotIn(KEY, output)

    def test_lowercase_sshd_output_also_supported(self):
        def fake(instance, args, **kwargs):
            rc, output = self.fake(instance, args, **kwargs)
            if args == ["sshd", "-T"]:
                output = output.replace("SetEnv", "setenv")
            return rc, output
        rc, output, _ = self.run_check(fake)
        self.assertEqual(rc, 0, output)

    def test_absent_corrupt_and_transport_errors_block_without_bootstrap(self):
        for helper in ("aginctus-ssh-client-identity", "aginctus-ssh-host-trust",
                       "aginctus-ssh-authorize"):
            for code in (1, 2, 3, 124, 127):
                with self.subTest(helper=helper, code=code):
                    def fake(instance, args, **kwargs):
                        if args[0] == helper:
                            return code, "secret failure text"
                        return self.fake(instance, args, **kwargs)
                    rc, output, calls = self.run_check(fake)
                    self.assertEqual(rc, 1)
                    self.assertIn(f"rc={code}", output)
                    self.assertNotIn("secret failure text", output)
                    self.assertFalse(any(call.args[1][0] == "herdr" for call in calls))
                    self.assertNotIn("ensure", str(calls))
                    self.assertNotIn("reconcile", str(calls))

    def test_mismatched_authorization_blocks_remote_commands(self):
        def fake(instance, args, **kwargs):
            if args[0] == "aginctus-ssh-authorize":
                return 0, "different key"
            return self.fake(instance, args, **kwargs)
        rc, output, calls = self.run_check(fake)
        self.assertEqual(rc, 1)
        self.assertIn("FAIL: workload authorization matches", output)
        self.assertFalse(any(call.args[1][0] == "herdr" for call in calls))

    def test_empty_catalog_does_not_prove_socket_or_remote_connectivity(self):
        def fake(instance, args, **kwargs):
            if args[:3] == ["herdr", "machine", "list"]:
                return 0, "[]"
            return self.fake(instance, args, **kwargs)
        rc, output, calls = self.run_check(fake)
        self.assertEqual(rc, 1)
        self.assertIn("registration required", output)
        self.assertFalse(any("--machine" in call.args[1] for call in calls))

    def test_wrong_or_disabled_machine_and_empty_status_fail(self):
        for field, value in (("target", "other"), ("session", "other"), ("enabled", False)):
            def fake(instance, args, **kwargs):
                rc, output = self.fake(instance, args, **kwargs)
                if args[:3] == ["herdr", "machine", "list"]:
                    catalog = json.loads(output)
                    catalog[0][field] = value
                    output = json.dumps(catalog)
                return rc, output
            rc, output, _ = self.run_check(fake)
            self.assertEqual(rc, 1, output)
        def empty_status(instance, args, **kwargs):
            if args[:3] == ["herdr", "machine", "status"]:
                return 0, "[]"
            return self.fake(instance, args, **kwargs)
        rc, output, _ = self.run_check(empty_status)
        self.assertEqual(rc, 1, output)

    def test_missing_remote_xdg_config_blocks_workspace(self):
        def missing_xdg(instance, args, **kwargs):
            if args[:4] == ["ssh", "-o", "BatchMode=yes", mod.WORKLOAD]:
                return 0, ""
            return self.fake(instance, args, **kwargs)
        rc, output, calls = self.run_check(missing_xdg)
        self.assertEqual(rc, 1)
        self.assertIn("FAIL: managed SSH delivers Herdr explicit-session config directory", output)
        self.assertNotIn("secret", output)
        self.assertTrue(any(call.args[1][:4] == [
            "ssh", "-o", "BatchMode=yes", mod.WORKLOAD] for call in calls))

    def test_nonzero_or_invalid_workspace_output_is_not_success(self):
        for code, response in ((2, "secret failure"), (0, "healthy"), (0, "[]"),
                               (0, '{"error":{"code":"not_found"}}')):
            def fake(instance, args, **kwargs):
                if args[:2] == ["herdr", "--machine"]:
                    return code, response
                return self.fake(instance, args, **kwargs)
            rc, output, _ = self.run_check(fake)
            self.assertEqual(rc, 1)
            self.assertNotIn("secret failure", output)

    def test_transport_project_timeout_and_output_redaction(self):
        result = subprocess.CompletedProcess([], 3, stdout="secret", stderr="secret")
        with patch.object(mod.subprocess, "run", return_value=result) as run:
            self.assertEqual(mod.run_remote(mod.CLIENT, ["helper"]), (3, "secret"))
            self.assertEqual(run.call_args.args[0], [
                "incus", "--project", "default", "exec", mod.CLIENT, "--", "helper"])
        for error, code in ((subprocess.TimeoutExpired("incus", 30, output="secret"), 124),
                            (FileNotFoundError("secret"), 127)):
            with patch.object(mod.subprocess, "run", side_effect=error):
                self.assertEqual(mod.run_remote(mod.CLIENT, ["helper"]), (code, ""))


if __name__ == "__main__":
    unittest.main()
