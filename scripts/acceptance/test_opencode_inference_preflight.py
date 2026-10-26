"""Hermetic #124 regressions: no Incus daemon, credentials or provider required."""
import importlib.util
import json
import subprocess
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "inference_preflight", Path(__file__).with_name("opencode-inference-preflight.py"))
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

CONFIG = {"endpoint": "http://10.42.40.4:4000/v1",
          "model": "approved-model", "protocol": "responses"}


class ReadinessTests(unittest.TestCase):
    def fake(self, *args):
        if args == ("stat", "-c", "%a %U %F", "--", mod.ROOT):
            return "700 agent directory\n"
        if args[:3] == ("stat", "-c", "%a %U %F"):
            return "600 agent regular file\n"
        if args[:2] == ("stat", "-c"):
            return "regular file\n"
        if "cat" in args:
            return json.dumps(CONFIG)
        return ""

    def config_check(self, **changes):
        cfg = {**CONFIG, **changes}

        def fake(*args):
            if "cat" in args:
                return json.dumps(cfg)
            return self.fake(*args)

        with patch.object(mod, "guest", side_effect=fake):
            return mod.check()

    def test_valid_readonly_metadata(self):
        with patch.object(mod, "guest", side_effect=self.fake) as guest:
            self.assertEqual(mod.check(), "approved-model")
            self.assertFalse(any("cat" in call.args and mod.KEY in call.args
                                 for call in guest.call_args_list))
            self.assertFalse(any("getent" in call.args for call in guest.call_args_list))
            self.assertIn(
                ("runuser", "-u", "agent", "--", "/bin/sh", "-c", mod.CHECK_DIR),
                [call.args for call in guest.call_args_list],
            )

    def test_guest_command_project_and_redaction(self):
        success = subprocess.CompletedProcess([], 0, stdout="ok\n")
        with patch.object(mod.subprocess, "run", return_value=success) as run:
            self.assertEqual(mod.guest("stat", "--", mod.KEY), "ok\n")
            argv = run.call_args.args[0]
            self.assertEqual(argv[:6],
                             ["incus", "--project", "default", "exec", "aginctus-dev", "--"])
            self.assertNotIn("cat", argv)
        error = subprocess.CalledProcessError(1, ["incus"], stderr="secret-token")
        with patch.object(mod.subprocess, "run", side_effect=error), self.assertRaisesRegex(
                RuntimeError, "guest command failed") as caught:
            mod.guest("stat", "--", mod.KEY)
        self.assertNotIn("secret-token", str(caught.exception))

    def test_phase_error_suppresses_subprocess_detail(self):
        with self.assertRaisesRegex(RuntimeError, "readiness phase failed: credential metadata") as caught:
            mod.stage("credential metadata",
                      lambda: (_ for _ in ()).throw(RuntimeError("secret-token")))
        self.assertNotIn("secret-token", str(caught.exception))

    def test_rejects_unsafe_key_before_config_read(self):
        def fake(*args):
            if args == ("stat", "-c", "%a %U %F", "--", mod.KEY):
                return "644 root regular file\n"
            return self.fake(*args)
        with patch.object(mod, "guest", side_effect=fake) as guest, self.assertRaisesRegex(
                RuntimeError, "credential"):
            mod.check()
        self.assertEqual(guest.call_count, 3)
        self.assertEqual(guest.call_args.args, ("stat", "-c", "%a %U %F", "--", mod.KEY))

    def test_rejects_wrong_dir_ownership_mode_or_symlink_before_listing(self):
        for metadata in ("755 agent directory", "700 root directory",
                         "700 agent symbolic link", "missing", ""):
            with self.subTest(metadata=metadata):
                def fake(*args):
                    if args == ("stat", "-c", "%a %U %F", "--", mod.ROOT):
                        return metadata
                    raise AssertionError("must reject before listing")
                with (
                    patch.object(mod, "guest", side_effect=fake) as guest,
                    self.assertRaisesRegex(RuntimeError, "credential runtime directory"),
                ):
                    mod.check()
                guest.assert_called_once()

    def test_unexpected_runtime_entry_or_link_blocks_config_read(self):
        # Guest helper suppresses filenames; a failed directory inventory
        # must prevent all subsequent key/config checks.
        with patch.object(mod, "guest") as guest:
            def fake(*args):
                if args == ("runuser", "-u", "agent", "--", "/bin/sh", "-c", mod.CHECK_DIR):
                    raise RuntimeError("SECRET_FILENAME_OR_PATH")
                return self.fake(*args)
            guest.side_effect = fake
            with self.assertRaisesRegex(
                RuntimeError, "readiness phase failed: credential directory contents"
            ) as caught:
                mod.check()
        self.assertNotIn("SECRET", str(caught.exception))
        self.assertEqual(guest.call_count, 2)
        self.assertIn("find", mod.CHECK_DIR)
        self.assertIn("! -name gateway.json", mod.CHECK_DIR)
        self.assertIn("! -name workload-key", mod.CHECK_DIR)

    def test_rejects_unreadable_key(self):
        def fake(*args):
            if "test" in args:
                raise RuntimeError("sensitive subprocess text")
            return self.fake(*args)
        with patch.object(mod, "guest", side_effect=fake), self.assertRaisesRegex(
                RuntimeError, "credential accessibility") as caught:
            mod.check()
        self.assertNotIn("sensitive", str(caught.exception))

    def test_rejects_bad_config_metadata(self):
        def fake(*args):
            if args[:3] == ("stat", "-c", "%F"):
                return "symbolic link\n"
            return self.fake(*args)
        with patch.object(mod, "guest", side_effect=fake), self.assertRaisesRegex(
                RuntimeError, "regular gateway configuration"):
            mod.check()

    def test_rejects_wrong_protocol_and_model(self):
        for value in ("chat-completions", None):
            with self.subTest(protocol=value), self.assertRaises(RuntimeError):
                self.config_check(protocol=value)
        for value in ("", "bad/model", " bad", "bad\nmodel", 9):
            with self.subTest(model=value), self.assertRaises(RuntimeError):
                self.config_check(model=value)

    def test_rejects_nonmanaged_endpoints(self):
        endpoints = (
            "http://8.8.8.8:4000/v1", "http://127.0.0.1:4000/v1",
            "http://169.254.1.2:4000/v1", "http://litellm.internal:4000/v1",
            "http://10.42.40.4:3999/v1", "https://10.42.40.4:4000/v1",
            "http://user@10.42.40.4:4000/v1",
            "http://10.42.40.4:4000/v1?key=x",
            "http://10.42.40.4:4000/v1\n", "http://10.42.40.4:bad/v1",
            "http://[::1]:4000/v1",
        )
        for endpoint in endpoints:
            with self.subTest(endpoint=endpoint), self.assertRaises(RuntimeError):
                self.config_check(endpoint=endpoint)

    def test_private_ipv4_ranges(self):
        for address in ("10.0.0.1", "172.16.1.1", "192.168.1.2"):
            with self.subTest(address=address):
                self.assertEqual(self.config_check(endpoint=f"http://{address}:4000/v1"),
                                 "approved-model")

    def test_rejects_invalid_instance_without_exec(self):
        for name in ("--bad", "../other", "bad name", ""):
            with (self.subTest(name=name),
                  patch.dict("os.environ", {"AGINCTUS_ACCEPTANCE_WORKLOAD": name}),
                  patch.object(mod, "guest") as guest,
                  self.assertRaisesRegex(RuntimeError, "workload")):
                mod.check()
            guest.assert_not_called()


if __name__ == "__main__":
    unittest.main()
