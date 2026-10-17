"""Offline, secret-safe tests for pinned LiteLLM virtual-key lifecycle (#122)."""
import contextlib
from datetime import datetime, timedelta, timezone
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location(
    "workload_key", Path(__file__).with_name("manage-workload-key.py"))
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

MASTER = "sk-master-synthetic-not-real"
VIRTUAL = "sk-virtual-synthetic-not-real"
MODEL = "aginctus-approved-test"


class MockGateway:
    def __init__(self):
        self.calls = []
        self.extra_model = False
        self.bad_grant = False
        self.deleted = False

    def open(self, req, timeout):
        self.calls.append(req)
        path = req.full_url.split(":4000", 1)[1]
        auth = req.get_header("Authorization")
        if path == "/v1/models":
            if auth == "Bearer " + VIRTUAL and self.deleted:
                raise mod.HTTPError(req.full_url, 401, "SECRET-DO-NOT-PRINT", None, None)
            if auth == "Bearer " + MASTER:
                payload = {"data": [{"id": MODEL}]}
            elif auth == "Bearer " + VIRTUAL:
                items = [MODEL, "unapproved"] if self.extra_model else [MODEL]
                payload = {"data": [{"id": x} for x in items]}
            else:
                raise mod.HTTPError(req.full_url, 401, "SECRET-DO-NOT-PRINT", None, None)
        elif path == "/key/generate":
            self.assert_json = json.loads(req.data)
            payload = {
                "key": VIRTUAL,
                "models": [MODEL] if not self.bad_grant else [],
                "max_budget": 1.0,
                "expires": (datetime.now(timezone.utc) + timedelta(minutes=59)).isoformat(),
            }
        elif path == "/key/delete":
            self.delete_json = json.loads(req.data)
            self.deleted = True
            payload = {"deleted_keys": ["synthetic-hash"]}
        else:
            raise AssertionError("unexpected endpoint")

        blob = json.dumps(payload).encode()
        class Response:
            status = 200
            def __enter__(self): return self
            def __exit__(self, *_): pass
            def read(self, n): return blob[:n]
        return Response()


class KeyLifecycleTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.state = self.root / "aginctus/gateway/local"
        self.state.mkdir(parents=True)
        master_file = self.state / "secrets.json"
        master_file.write_text(json.dumps({"AGINCTUS_GATEWAY_MASTER_KEY": MASTER}))
        master_file.chmod(0o600)
        self.keys = self.root / "keys"
        self.keys.mkdir(mode=0o700)
        self.output = self.keys / "workload.key"
        self.args = [
            "--endpoint", "http://10.144.44.70:4000",
            "--alias", MODEL, "--output", str(self.output),
        ]

    def call(self, gateway, action):
        stdout, stderr = io.StringIO(), io.StringIO()
        with mock.patch.dict(os.environ, {"XDG_STATE_HOME": str(self.root)}), \
                mock.patch.object(mod, "build_opener", return_value=gateway), \
                contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            rc = mod.main(self.args + [action])
        self.assertNotIn(MASTER, stdout.getvalue() + stderr.getvalue())
        self.assertNotIn(VIRTUAL, stdout.getvalue() + stderr.getvalue())
        self.assertNotIn("SECRET-DO-NOT-PRINT", stdout.getvalue() + stderr.getvalue())
        return rc, stdout.getvalue(), stderr.getvalue()

    def test_issue_and_revoke_model_bounded_key(self):
        remote = MockGateway()
        rc, out, _ = self.call(remote, "--issue")
        self.assertEqual(rc, 0)
        self.assertIn("PASS", out)
        self.assertEqual(self.output.read_text(), VIRTUAL + "\n")
        self.assertEqual(stat.S_IMODE(self.output.stat().st_mode), 0o600)
        self.assertEqual(remote.assert_json["models"], [MODEL])
        self.assertEqual(remote.assert_json["duration"], "1h")
        self.assertEqual(remote.assert_json["max_budget"], 1.0)
        self.assertEqual(remote.assert_json["allowed_routes"], ["llm_api_routes", "/v1/models"])
        self.assertEqual(remote.assert_json["max_parallel_requests"], 1)
        self.assertEqual(len(remote.calls), 3)
        rc, out, _ = self.call(remote, "--revoke")
        self.assertEqual(rc, 0)
        self.assertIn("revoked", out)
        self.assertFalse(self.output.exists())
        self.assertEqual(remote.delete_json, {"keys": [VIRTUAL]})
        self.assertEqual(len(remote.calls), 5)

    def test_scope_mismatch_keeps_file_for_revocation(self):
        remote = MockGateway()
        remote.extra_model = True
        rc, _, err = self.call(remote, "--issue")
        self.assertEqual(rc, 1)
        self.assertIn("scope mismatch", err)
        self.assertEqual(self.output.read_text(), VIRTUAL + "\n")
        rc, _, _ = self.call(remote, "--revoke")
        self.assertEqual(rc, 0)
        self.assertFalse(self.output.exists())

    def test_failed_grant_does_not_abandon_key_file(self):
        remote = MockGateway()
        remote.bad_grant = True
        rc, _, err = self.call(remote, "--issue")
        self.assertEqual(rc, 1)
        self.assertIn("exact model restriction", err)
        self.assertEqual(self.output.read_text(), VIRTUAL + "\n")

    def test_dry_run_is_inert_even_with_missing_credentials(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            rc = mod.main(self.args)
        self.assertEqual(rc, 0)
        self.assertIn("DRY RUN", output.getvalue())
        self.assertFalse(self.output.exists())

    def test_reject_existing_file_before_remote_call(self):
        self.output.write_text("old secret")
        self.output.chmod(0o600)
        remote = MockGateway()
        rc, _, err = self.call(remote, "--issue")
        self.assertEqual(rc, 1)
        self.assertIn("overwrite", err)
        self.assertEqual(len(remote.calls), 0)

    def test_unsafe_paths_and_endpoints(self):
        for value in ("https://10.0.0.1:4000", "http://1.2.3.4:4000",
                      "http://host.example:4000", "http://10.0.0.1:7777",
                      "http://user:pass@10.0.0.1:4000", "http://[::1]:4000"):
            with self.subTest(value=value), self.assertRaises(mod.KeyErrorSafe):
                mod.endpoint(value)
        self.assertEqual(mod.endpoint("http://10.144.44.70:4000"), "http://10.144.44.70:4000")
        other = self.keys / "other.key"
        other.write_text("secret")
        other.chmod(0o644)
        with self.assertRaises(mod.KeyErrorSafe):
            mod.protected_read(other)
        link = self.keys / "link"
        link.symlink_to(self.output)
        with self.assertRaises(mod.KeyErrorSafe):
            mod.protected_read(link)
        self.keys.chmod(0o755)
        with self.assertRaises(mod.KeyErrorSafe):
            mod.secure_output_path(self.output, creating=True)

    def test_refuses_master_as_workload_key(self):
        response = {"key": MASTER, "models": [MODEL], "max_budget": 1.0,
                    "expires": (datetime.now(timezone.utc) + timedelta(minutes=59)).isoformat()}
        with self.assertRaises(mod.KeyErrorSafe):
            mod.validate_grant(response, MODEL, MASTER)


if __name__ == "__main__":
    unittest.main()
