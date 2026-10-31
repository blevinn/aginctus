"""Real temporary-file/proc-fixture negative controls; never inspect host /proc."""
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts.vm_harness import guest_credentials as helper


class GuestCredentialTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        self.directory = root / "opencode"
        self.directory.mkdir(mode=0o700)
        self.key = self.directory / "workload-key"
        self.key.write_text("scoped-allowed-only\n")
        self.key.chmod(0o600)
        self.payload = {"scoped": "scoped-allowed-only", "master": "master-forbidden-only",
                        "provider": "synthetic-provider-forbidden", "endpoint": "http://10.77.0.2:4000/v1",
                        "model": "aginctus-go-pilot"}
        self.config = self.directory / "gateway.json"
        self.config.write_text(json.dumps({"endpoint": self.payload["endpoint"],
                                          "model": self.payload["model"], "protocol": "responses"}))
        self.config.chmod(0o600)
        self.proc = root / "proc"
        (self.proc / "123").mkdir(parents=True)
        (self.proc / "123/environ").write_bytes(b"AGINCTUS_LITELLM_KEY=scoped-allowed-only\0")
        (self.proc / "123/cmdline").write_bytes(b"opencode\0")
        (self.proc / "self").mkdir()
        self.mounts = self.proc / "self/mountinfo"
        self.mounts.write_text("1 0 0:1 / / rw - btrfs /dev/loop0 rw\n")
        self.state = root / "known-mounted-state"
        self.state.mkdir()
        self.uid = os.getuid()

    def audit(self):
        helper.audit(self.payload, self.uid, self.directory, self.proc, (str(self.state),))

    def test_actual_scoped_key_is_permitted_in_environment_config_and_known_state(self):
        (self.state / "allowed").write_text(self.payload["scoped"])
        self.audit()

    def test_each_forbidden_secret_in_each_surface_fails_without_secret_diagnostics(self):
        for name in ("master", "provider"):
            for file in (self.proc / "123/environ", self.proc / "123/cmdline",
                         self.config, self.state / "mounted-secret", self.directory / "unexpected-secret"):
                with self.subTest(name=name, surface=file.name):
                    before = file.read_bytes() if file.exists() else None
                    file.write_text(self.payload[name])
                    with self.assertRaises(helper.Rejected) as caught:
                        self.audit()
                    self.assertNotIn(self.payload[name], str(caught.exception))
                    if before is None:
                        file.unlink()
                    else:
                        file.write_bytes(before)

    def test_mountpoint_bind_root_and_mount_secret_bytes_fail(self):
        for raw in (
            "1 0 0:1 / /run/secrets rw - tmpfs tmpfs rw\n",
            "1 0 0:1 /var/lib/aginctus/gateway /mnt/alias rw - btrfs /dev/loop0 rw\n",
            "1 0 0:1 / /mnt/master-forbidden-only rw - tmpfs tmpfs rw\n",
        ):
            with self.subTest(raw=raw):
                self.mounts.write_text(raw)
                with self.assertRaises(helper.Rejected):
                    self.audit()

    def test_missing_process_surface_and_symlinked_state_are_not_silent_passes(self):
        (self.proc / "123/environ").unlink()
        with self.assertRaises(helper.Rejected):
            self.audit()
        (self.proc / "123/environ").write_bytes(b"")
        (self.state / "link").symlink_to(self.config)
        with self.assertRaises(helper.Rejected):
            self.audit()

    def test_oversized_process_and_known_state_fail_instead_of_skipping(self):
        (self.proc / "123/environ").write_bytes(b"x" * 1048577)
        with self.assertRaises(helper.Rejected):
            self.audit()
        (self.proc / "123/environ").write_bytes(b"")
        oversized = self.state / "oversized"
        with oversized.open("wb") as stream:
            stream.truncate(helper.MAX_FILE + 1)
        with self.assertRaises(helper.Rejected):
            self.audit()

    def test_only_matching_private_key_is_removed_and_absence_is_read_back(self):
        helper.remove_key(self.payload["scoped"], self.uid, self.directory)
        helper.inspect_absent(self.directory)
        self.assertTrue(self.config.is_file())
        self.assertEqual(list(self.directory.parent.glob(".vm-key-cleanup-*")), [])

    def test_rotated_key_is_preserved_before_removal(self):
        self.key.write_text("unrelated-rotated-copy\n")
        with self.assertRaises(helper.Rejected):
            helper.remove_key(self.payload["scoped"], self.uid, self.directory)
        self.assertEqual(self.key.read_text(), "unrelated-rotated-copy\n")

    def test_rotation_between_read_and_capture_is_restored_without_deletion(self):
        rename = os.rename
        def rotate_then_capture(source, target):
            source.unlink()
            source.write_text("unrelated-rotated-copy\n")
            source.chmod(0o600)
            rename(source, target)
        with (patch.object(helper.os, "rename", side_effect=rotate_then_capture),
              self.assertRaises(helper.Rejected)):
            helper.remove_key(self.payload["scoped"], self.uid, self.directory)
        self.assertEqual(self.key.read_text(), "unrelated-rotated-copy\n")

    def test_new_replacement_after_capture_is_preserved_and_fails_absence(self):
        rename = os.rename
        def capture_then_rotate(source, target):
            rename(source, target)
            source.write_text("new-rotated-copy\n")
            source.chmod(0o600)
        with (patch.object(helper.os, "rename", side_effect=capture_then_rotate),
              self.assertRaises(helper.Rejected)):
            helper.remove_key(self.payload["scoped"], self.uid, self.directory)
        self.assertEqual(self.key.read_text(), "new-rotated-copy\n")

    def test_mismatched_capture_is_retained_if_another_rotation_blocks_restore(self):
        rename = os.rename
        def two_rotations(source, target):
            source.write_text("first-unrelated-copy\n")
            rename(source, target)
            source.write_text("second-unrelated-copy\n")
            source.chmod(0o600)
        with (patch.object(helper.os, "rename", side_effect=two_rotations),
              self.assertRaises(FileExistsError)):
            helper.remove_key(self.payload["scoped"], self.uid, self.directory)
        self.assertEqual(self.key.read_text(), "second-unrelated-copy\n")
        captures = list(self.directory.parent.glob(".vm-key-cleanup-*/key"))
        self.assertEqual(len(captures), 1)
        self.assertEqual(captures[0].read_text(), "first-unrelated-copy\n")

    def test_unsafe_metadata_symlink_and_hardlink_keys_are_never_removed(self):
        self.key.chmod(0o644)
        with self.assertRaises(helper.Rejected):
            helper.remove_key(self.payload["scoped"], self.uid, self.directory)
        self.key.chmod(0o600)
        self.key.unlink()
        self.key.symlink_to(self.config)
        with self.assertRaises(OSError):
            helper.remove_key(self.payload["scoped"], self.uid, self.directory)
        with self.assertRaises(helper.Rejected):
            helper.inspect_absent(self.directory)
        self.key.unlink()
        os.link(self.config, self.key)
        with self.assertRaises(helper.Rejected):
            helper.remove_key(self.payload["scoped"], self.uid, self.directory)
        self.assertTrue(self.key.exists())


if __name__ == "__main__":
    unittest.main()
