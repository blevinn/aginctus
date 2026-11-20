"""Exercise the real credential exchange without an Incus daemon or account."""

import io
import json
import tempfile
import unittest
from pathlib import Path

import siwc
import siwc_admin


class AdminTests(unittest.TestCase):
    def test_login_requires_the_active_worker_lease(self):
        with tempfile.TemporaryDirectory() as parent:
            store = siwc.Store(Path(parent) / "credentials")
            with self.assertRaises(siwc.Error):
                siwc_admin.exchange(store, io.StringIO(""), io.StringIO(), require_runtime=True)
            with store.lock("runtime", timeout=0):
                output = io.StringIO()
                siwc_admin.exchange(store, io.StringIO(""), output, require_runtime=True)
                self.assertIn("state", json.loads(output.getvalue()))

    def test_exchange_commits_and_disconnect_preserves_registration(self):
        with tempfile.TemporaryDirectory() as parent:
            store = siwc.Store(Path(parent) / "credentials")
            output = io.StringIO()
            siwc_admin.exchange(store, io.StringIO(""), output)
            initial = json.loads(output.getvalue())
            initial["state"]["accounts"]["personal"] = {"client_id": "oaiapp_fixture", "scopes": []}
            output = io.StringIO()
            siwc_admin.exchange(store, io.StringIO(json.dumps(initial) + "\n"), output)
            self.assertEqual(json.loads(output.getvalue().splitlines()[1]), {"ok": True})
            self.assertEqual(store.read()["accounts"], initial["state"]["accounts"])
            self.assertFalse((store.root / "pending.json").exists())

    def test_administration_excludes_streams_and_rejects_stale_updates(self):
        with tempfile.TemporaryDirectory() as parent:
            store = siwc.Store(Path(parent) / "credentials")
            with (store.lock("admin", shared=True, timeout=0),
                  store.lock("admin", shared=True, timeout=0),
                  self.assertRaises(siwc.Error), store.lock("admin", timeout=0)):
                self.fail("administration raced active inference")
            store.accept_pending()
            original = store.read()
            with self.assertRaises(siwc.Error):
                siwc_admin.exchange(store, io.StringIO('{"base_digest":"stale"}\n'), io.StringIO())
            self.assertEqual(store.read(), original)


if __name__ == "__main__":
    unittest.main()
