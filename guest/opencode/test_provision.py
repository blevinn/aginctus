#!/usr/bin/env python3
"""Guest provisioning tests with an isolated runtime directory."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("provision", Path(__file__).with_name("provision.py"))
provision = importlib.util.module_from_spec(spec)
spec.loader.exec_module(provision)


class GuestProvisionTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name) / "config"
        self.payload = {"endpoint": "http://10.42.40.12:4000/v1", "model": "test-model", "key": "test-workload-key"}

    def install(self, **updates):
        data = dict(self.payload, **updates)
        with patch.object(provision.pwd, "getpwnam") as user, patch.object(provision.os, "chown"), patch.object(provision.os, "fchown"):
            user.return_value.pw_uid = 1000
            user.return_value.pw_gid = 1000
            provision.install(json.dumps(data), self.path)

    def test_private_files_and_configuration(self):
        self.install()
        self.assertEqual((self.path / "workload-key").stat().st_mode & 0o777, 0o600)
        self.assertEqual((self.path / "gateway.json").stat().st_mode & 0o777, 0o600)
        self.assertEqual((self.path / "workload-key").read_text(), "test-workload-key\n")
        settings = json.loads((self.path / "gateway.json").read_text())
        self.assertEqual(settings["model"], "test-model")
        self.assertNotIn("test-workload-key", (self.path / "gateway.json").read_text())

    def test_reject_bad_key(self):
        with self.assertRaises(ValueError):
            self.install(key="bad\nkey")
        self.assertFalse(self.path.exists())

    def test_reject_invalid_model(self):
        with self.assertRaises(ValueError):
            self.install(model="other/provider")
        self.assertFalse(self.path.exists())

    def test_reject_symlink_directory(self):
        real = Path(self.tmp.name) / "real"
        real.mkdir()
        self.path.symlink_to(real, target_is_directory=True)
        with self.assertRaises(ValueError):
            self.install()
        self.assertFalse((real / "workload-key").exists())


if __name__ == "__main__":
    unittest.main()
